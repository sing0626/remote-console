import SwiftUI
import UniformTypeIdentifiers
import CryptoKit
import Combine

struct PhoneFile: Identifiable {
    let path: String, name: String
    let directory: Bool
    let size: Int64
    var id: String { path }
}
struct SavedCommand: Codable, Identifiable { var id: String; var command: String }
struct PendingTransfer: Codable { var scope: String; var kind: String; var local: String; var remote: String; var name: String }
struct ExportedFile: Identifiable { let url: URL; var id: String {url.path} }
struct FileShare: UIViewControllerRepresentable {
    let url: URL
    func makeUIViewController(context: Context) -> UIActivityViewController { UIActivityViewController(activityItems:[url],applicationActivities:nil) }
    func updateUIViewController(_ controller:UIActivityViewController,context:Context) {}
}

@MainActor final class NativeToolsModel: ObservableObject {
    @Published var folder = "", files: [PhoneFile] = [], nextOffset = -1
    @Published var message = "", progress = 0.0, busy = false
    @Published var command = "", output = "$ ", scriptName = "", scripts: [SavedCommand] = []
    @Published var running = false, jobID = ""
    @Published var deviceSummary = "正在讀取電話狀態…", relaySummary = "正在讀取中繼流量…", warning = ""
    @Published var exported: ExportedFile?
    @Published var pending: PendingTransfer?
    private var work: Task<Void,Never>?, commandWork: Task<Void,Never>?
    private var scope = "", outputOffset: Int64 = 0
    init() {
        if let data=UserDefaults.standard.data(forKey:"toolScripts"),let list=try? JSONDecoder().decode([SavedCommand].self,from:data){scripts=list}
        if let data=UserDefaults.standard.data(forKey:"pendingTransfer"){pending=try? JSONDecoder().decode(PendingTransfer.self,from:data)}
    }
    static func query(_ value:String) -> String { value.addingPercentEncoding(withAllowedCharacters:CharacterSet(charactersIn:"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")) ?? "" }
    static func bytes(_ value:Int64) -> String { ByteCountFormatter.string(fromByteCount:value,countStyle:.decimal) }
    private func bind(_ session:NativeSession) {
        if scope==session.toolScope{return};scope=session.toolScope;folder="";files=[];output="$ ";jobID="";running=false
    }
    private func savePending(_ item:PendingTransfer?) {pending=item;if let item=item,let data=try? JSONEncoder().encode(item){UserDefaults.standard.set(data,forKey:"pendingTransfer")}else{UserDefaults.standard.removeObject(forKey:"pendingTransfer")}}
    private static func cache() throws -> URL {
        let root=FileManager.default.urls(for:.applicationSupportDirectory,in:.userDomainMask)[0].appendingPathComponent("Transfers",isDirectory:true)
        try FileManager.default.createDirectory(at:root,withIntermediateDirectories:true);return root
    }
    func list(_ session:NativeSession,append:Bool=false) async {
        bind(session);let path=folder,offset=append ? nextOffset : 0
        do {let result=try await session.tool("GET","/api/tools/files?path="+Self.query(path)+"&offset=\(max(0,offset))")
            guard folder==path else{return}
            let items=(result["entries"] as? [[String:Any]] ?? []).compactMap { item -> PhoneFile? in guard let name=item["name"] as? String,let path=item["path"] as? String else{return nil};return PhoneFile(path:path,name:name,directory:item["directory"] as? Bool ?? false,size:(item["size"] as? NSNumber)?.int64Value ?? 0) }
            if append{files+=items}else{files=items};nextOffset=result["next_offset"] as? Int ?? -1
        }catch{message=error.localizedDescription}
    }
    func parent(_ session:NativeSession){folder=(folder as NSString).deletingLastPathComponent;if folder=="."{folder=""};Task{await list(session)}}
    func enter(_ file:PhoneFile,_ session:NativeSession){folder=file.path;Task{await list(session)}}
    private func join(_ name:String) -> String {folder.isEmpty ? name : folder+"/"+name}
    func mkdir(_ name:String,_ session:NativeSession) async {
        guard !name.isEmpty,!name.contains("/"),name != ".." else{message="資料夾名稱無效。";return}
        do {_ = try await session.tool("POST","/api/tools/files/mkdir",body:["path":join(name)]);await list(session)}catch{message=error.localizedDescription}
    }
    func remove(_ file:PhoneFile,_ session:NativeSession) async {do{_ = try await session.tool("POST","/api/tools/files/remove",body:["path":file.path]);await list(session)}catch{message=error.localizedDescription}}
    func upload(_ url:URL,_ session:NativeSession) {
        guard !busy else{return};bind(session);let destination=join(url.lastPathComponent),scope=session.toolScope
        busy=true;message="正在準備檔案…";progress=0
        work=Task {
            let access=url.startAccessingSecurityScopedResource();defer{if access{url.stopAccessingSecurityScopedResource()};busy=false}
            do {
                let local=try Self.cache().appendingPathComponent(UUID().uuidString+".source")
                try await Task.detached(priority:.utility){try FileManager.default.copyItem(at:url,to:local)}.value
                let item=PendingTransfer(scope:scope,kind:"upload",local:local.path,remote:destination,name:url.lastPathComponent);savePending(item)
                try await send(item,session);try? FileManager.default.removeItem(at:local);savePending(nil);await list(session)
            }catch is CancellationError{message="已暫停，按續傳繼續。"}catch{message=error.localizedDescription+"；保留進度，可按續傳。"}
        }
    }
    func resume(_ session:NativeSession){guard !busy,let item=pending else{return};guard item.scope==session.toolScope else{message="請連接原本嗰部電話再續傳。";return};busy=true
        work=Task{defer{busy=false};do{if item.kind=="upload"{try await send(item,session);try? FileManager.default.removeItem(at:URL(fileURLWithPath:item.local));await list(session)}else{try await receive(item,session)};savePending(nil)}catch is CancellationError{message="已暫停。"}catch{message=error.localizedDescription+"；保留進度，可按續傳。"}}
    }
    func pause(){work?.cancel();message="正在暫停…"}
    private func send(_ item:PendingTransfer,_ session:NativeSession) async throws {
        message="正在計算 SHA-256…";let file=URL(fileURLWithPath:item.local)
        let (size,sha)=try await Task.detached(priority:.utility){()throws->(UInt64,String) in
            let handle=try FileHandle(forReadingFrom:file);defer{try? handle.close()};var hash=SHA256(),length:UInt64=0
            while let block=try handle.read(upToCount:128*1024),!block.isEmpty{try Task.checkCancellation();hash.update(data:block);length+=UInt64(block.count);if length>16*1024*1024*1024{throw CocoaError(.fileReadTooLarge)}}
            return(length,hash.finalize().map{String(format:"%02x",$0)}.joined())
        }.value
        try Task.checkCancellation()
        var state=try await session.tool("POST","/api/tools/uploads",body:["path":item.remote,"size":size,"sha256":sha,"overwrite":false])
        guard let id=state["id"] as? String else{throw CocoaError(.fileWriteUnknown)}
        let handle=try FileHandle(forReadingFrom:file);defer{try? handle.close()};var failures=0
        while state["complete"] as? Bool != true {
            try Task.checkCancellation();let offset=(state["offset"] as? NSNumber)?.uint64Value ?? 0
            if offset>=size{break};try handle.seek(toOffset:offset)
            guard let block=try handle.read(upToCount:Int(min(4*1024*1024,size-offset))),!block.isEmpty else{throw CocoaError(.fileReadCorruptFile)}
            do {state=try await session.toolData("PUT","/api/tools/uploads/\(id)?offset=\(offset)",body:block,headers:["Content-Type":"application/octet-stream"]);failures=0}
            catch {try Task.checkCancellation();if let e=error as? NativeSession.ToolError,[401,403,413].contains(e.code){throw error};failures+=1;if failures>5{throw error};try await Task.sleep(nanoseconds:UInt64(min(10,failures*2))*1_000_000_000);state=try await session.tool("GET","/api/tools/uploads/"+id)}
            let done=(state["offset"] as? NSNumber)?.doubleValue ?? 0;progress=size==0 ? 1:done/Double(size);message="上傳 \(item.name) · \(Self.bytes(Int64(done))) / \(Self.bytes(Int64(size)))"
        }
        try Task.checkCancellation();_ = try await session.tool("POST","/api/tools/uploads/\(id)/finish",body:[:]);progress=1;message="已上傳並通過 SHA-256 校驗。"
    }
    func download(_ file:PhoneFile,_ session:NativeSession){guard !busy else{return};bind(session)
        do {let root=try Self.cache();let key=SHA256.hash(data:Data((session.toolScope+"/"+file.path).utf8)).map{String(format:"%02x",$0)}.joined();let item=PendingTransfer(scope:session.toolScope,kind:"download",local:root.appendingPathComponent(key+".part").path,remote:file.path,name:file.name);savePending(item);resume(session)}catch{message=error.localizedDescription}
    }
    private func receive(_ item:PendingTransfer,_ session:NativeSession) async throws {
        let file=URL(fileURLWithPath:item.local),tag=file.appendingPathExtension("etag");var failures=0
        while true {
            try Task.checkCancellation();let offset=(try? FileManager.default.attributesOfItem(atPath:file.path)[.size] as? NSNumber)?.uint64Value ?? 0
            let etag=(try? String(contentsOf:tag,encoding:.utf8)) ?? ""
            var headers=["Range":"bytes=\(offset)-\(offset+4*1024*1024-1)"]
            if offset>0 && !etag.isEmpty{headers["If-Range"]=etag}
            do {
                let(temp,response)=try await session.toolDownload("/api/tools/files/download?path="+Self.query(item.remote),headers:headers)
                defer{try? FileManager.default.removeItem(at:temp)}
                let contentRange=response.value(forHTTPHeaderField:"Content-Range") ?? ""
                if response.statusCode==416 && contentRange=="bytes */\(offset)" && FileManager.default.fileExists(atPath:file.path){break}
                guard response.statusCode != 416 else{throw NativeSession.ToolError(code:416)}
                let nextTag=response.value(forHTTPHeaderField:"ETag") ?? ""
                if response.statusCode==206 {guard contentRange.hasPrefix("bytes \(offset)-") else{throw CocoaError(.fileReadCorruptFile)}}
                if response.statusCode==200 || offset==0 {if FileManager.default.fileExists(atPath:file.path){try FileManager.default.removeItem(at:file)};try FileManager.default.moveItem(at:temp,to:file)}
                else {try await Task.detached(priority:.utility){let src=try FileHandle(forReadingFrom:temp),out=try FileHandle(forWritingTo:file);defer{try? src.close();try? out.close()};try out.seekToEnd();while let block=try src.read(upToCount:128*1024),!block.isEmpty{try out.write(contentsOf:block)}}.value}
                try nextTag.write(to:tag,atomically:true,encoding:.utf8);failures=0
                let done=(try FileManager.default.attributesOfItem(atPath:file.path)[.size] as? NSNumber)?.uint64Value ?? 0
                let total=UInt64(contentRange.split(separator:"/").last ?? "") ?? done
                progress=total==0 ? 1:Double(done)/Double(total);message="下載 \(item.name) · \(Self.bytes(Int64(done))) / \(Self.bytes(Int64(total)))"
                if response.statusCode==200 || done>=total{break}
            }catch{try Task.checkCancellation();if let e=error as? NativeSession.ToolError,[401,403,404,416].contains(e.code){throw error};failures+=1;if failures>5{throw error};try await Task.sleep(nanoseconds:UInt64(min(10,failures*2))*1_000_000_000)}
        }
        let export=try Self.cache().appendingPathComponent(UUID().uuidString,isDirectory:true);try FileManager.default.createDirectory(at:export,withIntermediateDirectories:true);let destination=export.appendingPathComponent(item.name);try FileManager.default.copyItem(at:file,to:destination);exported=ExportedFile(url:destination);message="下載完成，可以儲存到「檔案」。";progress=1
    }
    func saveCommand(){let name=scriptName.trimmingCharacters(in:.whitespacesAndNewlines);guard !name.isEmpty,!command.trimmingCharacters(in:.whitespacesAndNewlines).isEmpty else{message="請填名稱同指令。";return};scripts.removeAll{$0.id==name};scripts.append(SavedCommand(id:name,command:command));UserDefaults.standard.set(try? JSONEncoder().encode(scripts),forKey:"toolScripts");scriptName="";message="已儲存常用指令。"}
    func deleteCommand(_ name:String){scripts.removeAll{$0.id==name};UserDefaults.standard.set(try? JSONEncoder().encode(scripts),forKey:"toolScripts")}
    func run(_ session:NativeSession,apk:String?=nil){guard !running else{return};running=true;output=apk == nil ? "$ "+command+"\n":"正在安裝 APK…\n";outputOffset=0
        commandWork=Task{defer{running=false};do{let value=try await session.tool("POST",apk == nil ? "/api/tools/terminal/jobs":"/api/tools/apk/install",body:apk.map{["path":$0]} ?? ["command":command,"timeout_seconds":120]);guard let id=value["id"] as? String else{throw CocoaError(.coderReadCorrupt)};jobID=id
            while !Task.isCancelled {let result=try await session.tool("GET","/api/tools/terminal/jobs/\(id)?offset=\(outputOffset)");outputOffset=(result["offset"] as? NSNumber)?.int64Value ?? outputOffset;output+=result["output"] as? String ?? "";if output.count>131072{output=String(output.suffix(131072))};if result["done"] as? Bool==true{output+="\n[結束代碼 \(result["exit_code"] ?? -1)]";jobID="";break};try await Task.sleep(nanoseconds:450_000_000)}
        }catch{message=error.localizedDescription}}
    }
    func cancelJob(_ session:NativeSession) async{guard !jobID.isEmpty else{return};do{_ = try await session.tool("POST","/api/tools/terminal/jobs/\(jobID)/cancel",body:[:])}catch{message=error.localizedDescription}}
    func dashboard(_ session:NativeSession) async {
        do {let state=try await session.tool("GET","/api/tools/status"),d=state["device"] as? [String:Any] ?? [:]
            deviceSummary="受控端 v\(state["version"] ?? "—") · \(state["root"] as? Bool==true ? "Root 已就緒":"Root 未就緒")\n\(d["model"] ?? "Android") · Android \(d["android"] ?? "—")\n電量 \(d["battery_percent"] ?? "—")%\(d["charging"] as? Bool==true ? " · 充電中":"")\n溫度 \(d["temperature_c"] ?? "—")°C\n\(d["network"] ?? "未能取得網絡")"
        }catch{deviceSummary="電話狀態未能取得："+error.localizedDescription}
        do {let value=try await session.tool("GET","/_remoteconsole/relays/status"),items=value["relays"] as? [[String:Any]] ?? [];var lines=["中繼流量 · 香港時間"];warning=""
            if items.isEmpty{lines.append("未接駁流量回報")}
            for item in items {lines.append("\n\(item["name"] ?? "中繼") · \(item["available"] as? Bool==true ? "已更新":"回報暫未更新")");if let report=item["report"] as? [String:Any]{for (period,label) in [("day","今日"),("month","本月"),("total","累計")]{if let n=report[period] as? [String:Any]{lines.append(label+"  入 "+Self.bytes((n["rx"] as? NSNumber)?.int64Value ?? 0)+" / 出 "+Self.bytes((n["tx"] as? NSNumber)?.int64Value ?? 0))}};if let quota=report["quota_gb"] as? Double,quota>0{lines.append("參考額度 \(quota) GB")}};if let w=item["warning"] as? String{warning=w}}
            relaySummary=lines.joined(separator:"\n")
        }catch{relaySummary="中繼流量未能取得；請升級入口並啟用香港流量回報。"}
    }
}

struct NativeToolsView: View {
    enum Page: Hashable {case files,terminal,dashboard}
    let page: Page
    @ObservedObject var model:NativeToolsModel
    @ObservedObject var session:NativeSession
    let close:()->Void
    @State private var importer=false, folderName=""
    @State private var deletion:PhoneFile?, installation:PhoneFile?
    private var title:String {page == .files ? "電話檔案":page == .terminal ? "Root 終端":"電話與中繼"}
    var body:some View {
        ZStack {ConsoleBackdrop();ScrollView {VStack(alignment:.leading,spacing:18){
            HStack {VStack(alignment:.leading,spacing:6){Text(title).font(.system(size:25,weight:.semibold));Text(page == .files ? "共用儲存空間 · 支援斷點續傳":page == .terminal ? "指令喺受控電話執行":"每 15 秒更新 · 香港時間").font(.system(size:11)).foregroundColor(ConsoleColor.muted)};Spacer();Button(action:close){Image(systemName:"xmark").frame(width:44,height:44)}.buttonStyle(.plain).interactiveCapsuleGlass().accessibilityIdentifier("panel.close")}
            if !model.message.isEmpty {Text(model.message).font(.system(size:12)).foregroundColor(ConsoleColor.mint).textSelection(.enabled)}
            if page == .files {fileBrowser} else if page == .terminal {terminal} else {dashboard}
        }.padding(24).padding(.top,12)}}.foregroundColor(ConsoleColor.ink).preferredColorScheme(.dark)
        .fileImporter(isPresented:$importer,allowedContentTypes:[.item],allowsMultipleSelection:false){result in switch result{case .success(let urls):if let url=urls.first{model.upload(url,session)};case .failure(let error):model.message=error.localizedDescription}}
        .sheet(item:$model.exported){item in FileShare(url:item.url)}
        .alert("刪除檔案？",isPresented:Binding(get:{deletion != nil},set:{if !$0{deletion=nil}})){Button("刪除",role:.destructive){if let file=deletion{Task{await model.remove(file,session)}};deletion=nil};Button("取消",role:.cancel){deletion=nil}} message:{Text(deletion?.name ?? "")}
        .alert("安裝 APK？",isPresented:Binding(get:{installation != nil},set:{if !$0{installation=nil}})){Button("安裝"){if let file=installation{model.run(session,apk:file.path)};installation=nil};Button("取消",role:.cancel){installation=nil}} message:{Text(installation?.name ?? "")}
        .task(id:page){if page == .files{await model.list(session)}else if page == .dashboard {while !Task.isCancelled{await model.dashboard(session);try? await Task.sleep(nanoseconds:15_000_000_000)}}}
    }
    private func action(_ title:String,_ icon:String,_ work:@escaping()->Void)->some View {Button(action:work){Label(title,systemImage:icon).font(.system(size:12,weight:.medium)).padding(.horizontal,14).frame(height:42)}.buttonStyle(.plain).interactiveCapsuleGlass()}
    private var fileBrowser:some View {VStack(alignment:.leading,spacing:16){
        Text("/sdcard"+(model.folder.isEmpty ? "":"/"+model.folder)).font(.system(size:12,design:.monospaced)).foregroundColor(ConsoleColor.muted)
        HStack{action("上層","arrow.up"){model.parent(session)};action("上傳","square.and.arrow.up"){importer=true}.accessibilityIdentifier("files.upload");action("重新整理","arrow.clockwise"){Task{await model.list(session)}}}
        HStack{TextField("新增資料夾",text:$folderName).insetField();action("建立","folder.badge.plus"){let name=folderName;folderName="";Task{await model.mkdir(name,session)}}}
        if model.busy {ProgressView(value:model.progress).tint(ConsoleColor.mint);action("暫停","pause.fill"){model.pause()}}else if model.pending != nil {action("續傳上次檔案","arrow.clockwise"){model.resume(session)}}
        VStack(spacing:0){ForEach(model.files){file in HStack(spacing:12){Image(systemName:file.directory ? "folder.fill":"doc").foregroundColor(ConsoleColor.mint);Button {if file.directory{model.enter(file,session)}else{model.download(file,session)}}label:{VStack(alignment:.leading,spacing:5){Text(file.name).font(.system(size:14));if !file.directory{Text(NativeToolsModel.bytes(file.size)).font(.system(size:10)).foregroundColor(ConsoleColor.muted)}}.buttonStyle(.plain);Spacer();Menu{if file.directory{Button("開啟"){model.enter(file,session)}}else{Button("下載"){model.download(file,session)};if file.name.lowercased().hasSuffix(".apk"){Button("安裝 APK"){installation=file}}};Button("刪除",role:.destructive){deletion=file}}label:{Image(systemName:"ellipsis").frame(width:36,height:44)}}.padding(.horizontal,14).padding(.vertical,8);Divider().opacity(0.15)}}.glassSurface(20)
        if model.nextOffset>=0{action("載入更多","arrow.down"){Task{await model.list(session,append:true)}}}
        if !model.output.isEmpty && model.output != "$ " {Text(model.output).font(.system(size:11,design:.monospaced)).textSelection(.enabled).padding(14).glassSurface(16)}
    }}
    private var terminal:some View {VStack(alignment:.leading,spacing:16){
        TextEditor(text:$model.command).font(.system(size:13,design:.monospaced)).frame(height:100).padding(10).glassSurface(16).accessibilityIdentifier("terminal.command")
        HStack{action("執行","play.fill"){model.run(session)}.disabled(model.running).accessibilityIdentifier("terminal.run");action("停止","stop.fill"){Task{await model.cancelJob(session)}}.disabled(model.jobID.isEmpty)}
        HStack{TextField("常用指令名稱",text:$model.scriptName).insetField();action("儲存","bookmark"){model.saveCommand()}}
        if !model.scripts.isEmpty{Menu("常用指令"){ForEach(model.scripts){script in Button(script.id){model.command=script.command}}}.tint(ConsoleColor.mint)}
        Text(model.output).font(.system(size:12,design:.monospaced)).textSelection(.enabled).frame(maxWidth:.infinity,alignment:.leading).padding(16).glassSurface(20).accessibilityIdentifier("terminal.output")
        Text("單次指令最長 120 秒；關閉工具面板後，已開始嘅工作會繼續執行。").font(.system(size:10)).foregroundColor(ConsoleColor.muted)
    }}
    private var dashboard:some View {VStack(alignment:.leading,spacing:20){Text(model.deviceSummary).font(.system(size:14)).lineSpacing(8).frame(maxWidth:.infinity,alignment:.leading).padding(20).glassSurface(24).accessibilityIdentifier("dashboard.device");Text(model.relaySummary).font(.system(size:13)).lineSpacing(8).frame(maxWidth:.infinity,alignment:.leading).padding(20).glassSurface(24).accessibilityIdentifier("dashboard.relay");if !model.warning.isEmpty{Label(model.warning,systemImage:"exclamationmark.triangle").foregroundColor(.orange)};action("重新整理","arrow.clockwise"){Task{await model.dashboard(session)}}}}
}
