import Foundation
import Combine
import AVFoundation
import UIKit
import WebRTC

struct RemoteHost: Identifiable {
    let id: String
    let name: String
    let online: Bool
}

final class NativeSession: NSObject, ObservableObject {
    @Published var status = "未連接"
    @Published var deviceName = "Android"
    @Published var detail = "等待聲畫資料"
    @Published var ready = false
    @Published var active = false
    @Published var muted = true
    @Published var notice = ""
    @Published var clipboardText: String?
    @Published var videoTrack: RTCVideoTrack?
    @Published var videoWidth = 1080
    @Published var videoHeight = 2400
    @Published var relayRegions: [[String: String]] = []
    private var networkPolicy = AdaptiveNetworkPolicy()
    private var retryCount = 0, currentBitrate = 8_000_000
    private var activeRegion = "", qualityToken = ""
    private var retryWork: DispatchWorkItem?
    private var cookieStore = URLSessionConfiguration.ephemeral.httpCookieStorage
    @Published var approvalCode = ""
    @Published var approvalName = ""
    private static let playback = RCPlaybackAudioDevice()
    private static let factory: RTCPeerConnectionFactory = {
        RTCInitializeSSL()
        return RTCPeerConnectionFactory(encoderFactory: RTCDefaultVideoEncoderFactory(), decoderFactory: RTCDefaultVideoDecoderFactory(), audioDevice: playback)
    }()
    private var peer: RTCPeerConnection?
    private var ordered: RTCDataChannel?
    private var unordered: RTCDataChannel?
    private var socket: URLSessionWebSocketTask?
    private var http: URLSession?
    private var origin: URL?
    private var basicUser = "", basicPassword = ""
    private var routingDevice: String?, routingName: String?
    private var controllerCredential: String?
    private var pendingApproval: (String,String)?
    private var epoch = UUID()
    private var connected = false, metadata = false, canControl = false, canClipboard = false, sentOffer = false, localReady = false
    private var device: [String: Any] = [:], options: [String: String] = [:]
    private var audioTrack: RTCAudioTrack?
    private var touches: [UInt8: (Int,Int)] = [:]
    private var statsTimer: Timer?, deadline: DispatchWorkItem?
    private var previousFrames = 0.0, previousBytes = 0.0, previousTime = Date()
    private var requestedFPS = 60
    private var resume: (URL, String, StreamProfile, String, String, String?, String?)?

    private func onMain(_ action: @escaping () -> Void) { DispatchQueue.main.async(execute: action) }
    private func valid(_ pc: RTCPeerConnection) -> Bool { peer === pc }
    private enum Failure: LocalizedError {
        case message(String)
        case http(Int)
        var errorDescription: String? { switch self {case .message(let value): return value;case .http(let code): return "電話暫時未能連接（HTTP \(code)）。"} }
    }
    static func directory(endpoint: URL, username: String, password: String) async throws -> [RemoteHost]? {
        var components = URLComponents(url: endpoint, resolvingAgainstBaseURL: false)!
        components.path = ""; components.query = nil; components.fragment = nil
        guard let origin = components.url, let target = URL(string: "/_remoteconsole/devices", relativeTo: origin) else { throw Failure.message("電話網址無效。") }
        let probe = NativeSession()
        probe.origin = origin; probe.basicUser = username; probe.basicPassword = password
        let config = URLSessionConfiguration.ephemeral; config.timeoutIntervalForRequest = 15
        let http = URLSession(configuration: config, delegate: probe, delegateQueue: .main)
        defer { http.invalidateAndCancel() }
        var request = URLRequest(url: target); request.setValue(origin.absoluteString, forHTTPHeaderField: "Origin")
        if !username.isEmpty && origin.scheme == "https" { request.setValue("Basic " + Data("\(username):\(password)".utf8).base64EncodedString(), forHTTPHeaderField: "Authorization") }
        let (data, raw) = try await http.data(for: request)
        guard let response = raw as? HTTPURLResponse else { throw Failure.message("未能取得電話清單。") }
        if response.statusCode == 401 && (response.value(forHTTPHeaderField: "WWW-Authenticate") ?? "").lowercased().contains("basic") { throw Failure.message("請喺進階設定核對外網帳戶同密碼，再載入電話清單。") }
        if [301,302,303,307,308,401,404].contains(response.statusCode) { return nil }
        guard response.statusCode == 200 else { throw Failure.message("未能取得電話清單（HTTP \(response.statusCode)）。") }
        if (response.value(forHTTPHeaderField: "Content-Type") ?? "").contains("text/html") { return nil }
        guard data.count <= 128 * 1024, let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw Failure.message("電話清單格式無效。") }
        guard let version = object["registry_version"] as? Int else { return nil }
        guard version == 1, let items = object["devices"] as? [[String: Any]], !items.isEmpty, items.count <= 32 else { throw Failure.message("電話清單版本未能識別，請更新半雲電話。") }
        var result: [RemoteHost] = [], seen = Set<String>()
        for item in items {
            guard let id = item["id"] as? String, id.range(of: "^[a-z][a-z0-9_-]{0,31}$", options: .regularExpression) != nil,
                  seen.insert(id).inserted, let name = item["name"] as? String, !name.isEmpty, name.count <= 80,
                  let online = item["online"] as? Bool else { throw Failure.message("電話清單包含無效裝置。") }
            result.append(RemoteHost(id: id, name: name, online: online))
        }
        return result
    }
    private func request(_ path: String, json: [String: Any]? = nil) async throws -> [String: Any] {
        guard let origin = origin, let http = http, let url = URL(string: path, relativeTo: origin) else { throw Failure.message("連線已結束。") }
        var request = URLRequest(url: url)
        request.setValue(origin.absoluteString, forHTTPHeaderField: "Origin")
        if let credential = controllerCredential { request.setValue(credential, forHTTPHeaderField: "X-RemoteConsole-Controller") }
        if let routingDevice = routingDevice { request.setValue(routingDevice, forHTTPHeaderField: "X-RemoteConsole-Device") }
        if !basicUser.isEmpty && origin.scheme == "https" { request.setValue("Basic " + Data("\(basicUser):\(basicPassword)".utf8).base64EncodedString(), forHTTPHeaderField: "Authorization") }
        if let json = json { request.httpMethod = "POST"; request.setValue("application/json", forHTTPHeaderField: "Content-Type"); request.httpBody = try JSONSerialization.data(withJSONObject: json) }
        let (data, response) = try await http.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw Failure.message("電話回應無效。") }
        if response.statusCode == 429 { throw Failure.message("配對次數過多，請稍後再試。") }
        if response.statusCode == 401 { throw Failure.message(controllerCredential == nil ? "登入或配對已過期，請核對進階設定。" : "控制端授權已失效，請重新連接電話。") }
        guard response.statusCode == 200 else { throw Failure.http(response.statusCode) }
        guard let value = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw Failure.message("電話回應格式無效。") }
        return value
    }
    func connect(endpoint: URL, pin: String, profile: StreamProfile, username: String = "", password: String = "", deviceID: String? = nil, hostName: String? = nil) {
        retryCount = 0
        begin(endpoint: endpoint,pin: pin,profile: profile,username: username,password: password,deviceID: deviceID,hostName: hostName)
    }
    private func begin(endpoint: URL, pin: String, profile: StreamProfile, username: String, password: String, deviceID: String?, hostName: String?) {
        retryWork?.cancel(); retryWork = nil; stop(); ready = false; networkPolicy = AdaptiveNetworkPolicy(); currentBitrate = profile.bitrate
        var components = URLComponents(url: endpoint, resolvingAgainstBaseURL: false)!
        components.path = ""; components.query = nil; components.fragment = nil
        guard let origin = components.url else { return }
        self.origin = origin; basicUser = username; basicPassword = password; requestedFPS = profile.fps
        routingDevice = deviceID; routingName = hostName
        resume = (endpoint,pin,profile,username,password,deviceID,hostName)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 18; configuration.httpShouldSetCookies = true; configuration.httpCookieStorage = cookieStore
        http = URLSession(configuration: configuration, delegate: self, delegateQueue: .main)
        deviceName = hostName ?? "Android"
        active = true; status = "正在配對"; detail = "正在連接電話"; notice = ""
        let generation = epoch
        Task { @MainActor in
            do {
                if pin.isEmpty { try await authorizeController(generation: generation) }
                else {
                    let auth = try await request("/api/unlock", json: ["pin": pin])
                    guard generation == epoch else { return }
                    guard auth["result"] as? String == "success" else { throw Failure.message("配對 PIN 唔正確，請核對後再試。餘下 \(auth["leftTries"] ?? 0) 次。") }
                }
                guard generation == epoch else { return }
                let list = try await request("/api/device/list")
                guard generation == epoch else { return }
                guard let devices = list["devices"] as? [[String: Any]], let device = devices.first(where: { $0["device_type"] as? String == "android" }) else { throw Failure.message("未搵到在線嘅 Android 電話。") }
                self.device = device; deviceName = routingName ?? device["display_name"] as? String ?? device["name"] as? String ?? device["device_id"] as? String ?? "Android"
                let sessions = try await request("/api/screen/sessions")
                guard generation == epoch else { return }
                if let shared = sessions["stream_options"] as? [String: String], !shared.isEmpty { options = shared; requestedFPS = Int(shared["max_fps"] ?? "") ?? requestedFPS; notice = "已沿用其他連線嘅共用畫質。" } else { options = profile.options }
                var ice = try await request("/api/ice-config")
                let available = ice["relay_regions"] as? [[String: Any]] ?? []
                relayRegions = available.compactMap { item in guard let id=item["id"] as? String,let name=item["name"] as? String else {return nil};return ["id":id,"name":name] }
                activeRegion = ""
                if !relayRegions.isEmpty {
                    if profile.region == "auto" { activeRegion = relayRegions[retryCount % relayRegions.count]["id"] ?? "" }
                    else if relayRegions.contains(where: {$0["id"] == profile.region}) { activeRegion = profile.region }
                    if !activeRegion.isEmpty { ice = try await request("/api/ice-config?region="+activeRegion) }
                }
                guard generation == epoch else { return }
                guard generation == epoch else { return }
                let config = RTCConfiguration(); config.sdpSemantics = .unifiedPlan
                config.iceServers = (ice["iceServers"] as? [[String: Any]] ?? []).compactMap { item in
                    let urls = item["urls"] as? [String] ?? (item["urls"] as? String).map { [$0] } ?? []
                    return urls.isEmpty ? nil : RTCIceServer(urlStrings: urls, username: item["username"] as? String, credential: item["credential"] as? String)
                }
                if profile.relay || (retryCount > 0 && profile.region == "auto" && !relayRegions.isEmpty) || ice["iceTransportPolicy"] as? String == "relay" { config.iceTransportPolicy = .relay }
                guard let pc = Self.factory.peerConnection(with: config, constraints: RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil), delegate: self) else { throw Failure.message("未能建立原生聲畫引擎。") }
                peer = pc
                let receiver = RTCRtpTransceiverInit(); receiver.direction = .recvOnly
                pc.addTransceiver(of: .video, init: receiver); pc.addTransceiver(of: .audio, init: receiver)
                ordered = pc.dataChannel(forLabel: "control-ordered", configuration: RTCDataChannelConfiguration()); ordered?.delegate = self
                let unorderedConfig = RTCDataChannelConfiguration(); unorderedConfig.isOrdered = false
                unordered = pc.dataChannel(forLabel: "control-unordered", configuration: unorderedConfig); unordered?.delegate = self
                status = "正在建立聲畫連線"
                let timeout = DispatchWorkItem { [weak self] in guard let self = self, self.epoch == generation else { return }; self.fail("連線逾時，請檢查電話同網絡後再試。") }
                deadline = timeout; DispatchQueue.main.asyncAfter(deadline: .now() + 35, execute: timeout)
                pc.offer(for: RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)) { [weak self, weak pc] offer, error in
                    self?.onMain { [weak self, weak pc] in
                        guard let self = self, let pc = pc, self.valid(pc) else { return }
                        guard let offer = offer, error == nil else { self.fail("未能建立聲畫描述。"); return }
                        pc.setLocalDescription(offer) { [weak self, weak pc] error in
                            self?.onMain { [weak self, weak pc] in
                                guard let self = self, let pc = pc, self.valid(pc) else { return }
                                guard error == nil else { self.fail("聲畫描述未能套用。"); return }
                                self.localReady = true
                                if pc.iceGatheringState == .complete { self.sendOffer() }
                                DispatchQueue.main.asyncAfter(deadline: .now() + 8) { [weak self, weak pc] in guard let self = self, let pc = pc, self.valid(pc) else { return }; self.sendOffer() }
                            }
                        }
                    }
                }
            } catch { if generation == epoch { fail(error.localizedDescription,retry: retryable(error)) } }
        }
    }
    func reconnect(profile: StreamProfile? = nil) { guard let r = resume else { return }; connect(endpoint: r.0, pin: r.1, profile: profile ?? r.2, username: r.3, password: r.4, deviceID: r.5, hostName: r.6) }
    private func sendOffer() {
        guard !sentOffer, localReady, let pc = peer, let sdp = pc.localDescription?.sdp, let origin = origin, let http = http else { return }
        sentOffer = true
        var components = URLComponents(url: origin, resolvingAgainstBaseURL: false)!
        components.scheme = origin.scheme == "https" ? "wss" : "ws"; components.path = "/screen/ws"
        var request = URLRequest(url: components.url!); request.setValue(origin.absoluteString, forHTTPHeaderField: "Origin")
        if let credential = controllerCredential { request.setValue(credential, forHTTPHeaderField: "X-RemoteConsole-Controller") }
        if let routingDevice = routingDevice { request.setValue(routingDevice, forHTTPHeaderField: "X-RemoteConsole-Device") }
        if let cookies = http.configuration.httpCookieStorage?.cookies(for: origin) { for (key,value) in HTTPCookie.requestHeaderFields(with: cookies) { request.setValue(value, forHTTPHeaderField: key) } }
        if !basicUser.isEmpty && origin.scheme == "https" { request.setValue("Basic " + Data("\(basicUser):\(basicPassword)".utf8).base64EncodedString(), forHTTPHeaderField: "Authorization") }
        let ws = http.webSocketTask(with: request); socket = ws; ws.resume()
        let generation = epoch
        let payload: [String: Any] = ["device_type": "android", "device_id": device["device_id"] ?? "", "device_ip": device["ip"] ?? "", "device_port": String(describing: device["port"] ?? "0"), "relay_region": activeRegion, "av_sync": false, "driver_config": options, "sdp": sdp]
        Task { @MainActor in
            do {
                let data = try JSONSerialization.data(withJSONObject: payload)
                try await ws.send(.string(String(decoding: data, as: UTF8.self)))
                while generation == epoch {
                    let message = try await ws.receive()
                    guard generation == epoch else { return }
                    let data: Data
                    switch message { case .string(let value): data = Data(value.utf8); case .data(let value): data = value; @unknown default: continue }
                    let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
                    if object["status"] as? String == "error" { throw Failure.message("電話未能開始串流，請稍後再連接。") }
                    if object["stage"] as? String == "webrtc_init", let answer = object["sdp"] as? String {
                        let sdp = answer.components(separatedBy: .newlines).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.joined(separator: "\r\n") + "\r\n"
                        pc.setRemoteDescription(RTCSessionDescription(type: .answer, sdp: sdp)) { [weak self, weak pc] error in self?.onMain { [weak self, weak pc] in guard let self = self, let pc = pc, self.valid(pc) else { return }; if error != nil { self.fail("聲畫格式未能配對。") } } }
                    }
                    if object["stage"] as? String == "webrtc_metainfo" {
                        qualityToken = object["quality_token"] as? String ?? ""
                        let caps = object["capabilities"] as? [String: Any] ?? [:]
                        canControl = caps["can_control"] as? Bool ?? false; canClipboard = caps["can_clipboard"] as? Bool ?? false; metadata = true
                        if let media = object["media_meta"] as? [String: Any] { videoWidth = media["width"] as? Int ?? videoWidth; videoHeight = media["height"] as? Int ?? videoHeight }
                        refreshState(); startStats()
                    }
                }
            } catch { if generation == epoch { fail(error.localizedDescription,retry: retryable(error)) } }
        }
    }
    private func refreshState() {
        let live = connected && metadata && ordered?.readyState == .open
        ready = live && canControl
        status = live ? (canControl ? "已連接 · 可操作" : "已連接 · 只限觀看") : "正在連接"
        if live { deadline?.cancel(); send(Data([16])); let generation = epoch; DispatchQueue.main.asyncAfter(deadline: .now()+30) { [weak self] in guard let self=self,self.epoch==generation else{return};self.retryCount=0 } }
    }
    private func retryable(_ error: Error) -> Bool {
        if case Failure.http(let code) = error { return code >= 500 }
        let e=error as NSError; return e.domain == NSURLErrorDomain && e.code != NSURLErrorCancelled && e.code != NSURLErrorUserAuthenticationRequired
    }
    private func fail(_ value: String, retry: Bool = true) {
        guard active else {return};stop(); ready=false
        guard retry,let r=resume,r.2.autoReconnect else {status=value;notice=value;return}
        retryCount += 1;let delay=AdaptiveNetworkPolicy.retryDelay(retryCount)
        status="網絡中斷 · \(Int(delay)) 秒後重連"
        let work=DispatchWorkItem { [weak self] in guard let self=self,self.active else{return};self.begin(endpoint:r.0,pin:r.1,profile:r.2,username:r.3,password:r.4,deviceID:r.5,hostName:r.6) }
        retryWork=work;DispatchQueue.main.asyncAfter(deadline:.now()+delay,execute:work)
    }
    private func stop() {
        cancelControllerApproval()
        approvalCode = ""; approvalName = ""
        releaseTouches(); epoch = UUID(); deadline?.cancel(); deadline = nil; statsTimer?.invalidate(); statsTimer = nil
        ordered?.delegate = nil; unordered?.delegate = nil; ordered?.close(); unordered?.close(); ordered = nil; unordered = nil
        peer?.delegate = nil; peer?.close(); peer = nil; videoTrack = nil; audioTrack = nil
        socket?.cancel(with: .normalClosure, reason: nil); socket = nil; http?.invalidateAndCancel(); http = nil
        qualityToken = ""; previousFrames = 0; previousBytes = 0; previousTime = Date(); connected = false; metadata = false; sentOffer = false; localReady = false; canControl = false; canClipboard = false
    }
    func disconnect() { retryWork?.cancel();retryWork=nil;retryCount=0;stop(); resume = nil; routingDevice = nil; routingName = nil; controllerCredential = nil; basicUser = ""; basicPassword = ""; active = false; ready = false; status = "未連接"; clipboardText = nil }

    struct ToolError: LocalizedError {
        let code: Int
        var errorDescription: String? { code==401 ? "授權已失效，請重新授權。" : "操作未完成（HTTP \(code)）。" }
    }
    var toolScope: String { (origin?.absoluteString ?? "")+"/"+(routingDevice ?? "local") }
    private func toolRequest(_ method:String,_ path:String,body:Data?=nil,headers:[String:String]=[:]) throws -> URLRequest {
        guard let origin=origin,let url=URL(string:path,relativeTo:origin),http != nil else{throw Failure.message("電話正在重新連接，請稍後續傳。")}
        var req=URLRequest(url:url);req.httpMethod=method;req.httpBody=body;req.timeoutInterval=60
        req.setValue(origin.absoluteString,forHTTPHeaderField:"Origin")
        if let id=routingDevice{req.setValue(id,forHTTPHeaderField:"X-RemoteConsole-Device")}
        if let credential=controllerCredential{req.setValue(credential,forHTTPHeaderField:"X-RemoteConsole-Controller")}
        if !basicUser.isEmpty && origin.scheme=="https" {req.setValue("Basic "+Data("\(basicUser):\(basicPassword)".utf8).base64EncodedString(),forHTTPHeaderField:"Authorization")}
        for (k,v) in headers{req.setValue(v,forHTTPHeaderField:k)};return req
    }
    func tool(_ method:String,_ path:String,body:[String:Any]?=nil) async throws -> [String:Any] {
        let data=try body.map{try JSONSerialization.data(withJSONObject:$0)}
        return try await toolData(method,path,body:data,headers:["Content-Type":"application/json"])
    }
    func toolData(_ method:String,_ path:String,body:Data?=nil,headers:[String:String]=[:]) async throws -> [String:Any] {
        let request=try toolRequest(method,path,body:body,headers:headers);guard let http=http else{throw Failure.message("連線已結束。")}
        let(data,raw)=try await http.data(for:request);guard let response=raw as? HTTPURLResponse else{throw Failure.message("電話回應無效。")}
        guard (200..<300).contains(response.statusCode) else{throw ToolError(code:response.statusCode)}
        guard data.count<=2*1024*1024,let object=try JSONSerialization.jsonObject(with:data) as? [String:Any] else{throw Failure.message("工具回應無效。")};return object
    }
    func toolDownload(_ path:String,headers:[String:String]) async throws -> (URL,HTTPURLResponse) {
        let request=try toolRequest("GET",path,headers:headers);guard let http=http else{throw Failure.message("連線已結束。")}
        let(file,raw)=try await http.download(for:request);guard let response=raw as? HTTPURLResponse else{throw Failure.message("電話回應無效。")}
        guard [200,206,416].contains(response.statusCode) else{try? FileManager.default.removeItem(at:file);throw ToolError(code:response.statusCode)};return(file,response)
    }

    private func authorizationRequest(_ path: String, body: [String: Any]? = nil) async throws -> (Int,[String: Any]) {
        guard let origin = origin, let http = http, let url = URL(string: "/_remoteconsole/trust/"+path, relativeTo: origin) else { throw Failure.message("連線已結束。") }
        var request = URLRequest(url: url); request.setValue(origin.absoluteString, forHTTPHeaderField: "Origin")
        if !basicUser.isEmpty && origin.scheme == "https" { request.setValue("Basic " + Data("\(basicUser):\(basicPassword)".utf8).base64EncodedString(), forHTTPHeaderField: "Authorization") }
        if let body = body { request.httpMethod = "POST"; request.setValue("application/json", forHTTPHeaderField: "Content-Type"); request.httpBody = try JSONSerialization.data(withJSONObject: body) }
        let (data, raw) = try await http.data(for: request)
        guard let response = raw as? HTTPURLResponse, data.count <= 131072 else { throw Failure.message("未能連接授權服務。") }
        if response.statusCode == 401 && (response.value(forHTTPHeaderField: "WWW-Authenticate") ?? "").lowercased().contains("basic") { throw Failure.message("請核對進階設定嘅外網帳戶同密碼。") }
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw Failure.message("呢個入口未支援控制端授權，請更新外網主機。") }
        return (response.statusCode,object)
    }
    @MainActor private func authorizeController(generation: UUID) async throws {
        guard let origin = origin else { throw Failure.message("連線已結束。") }
        status = "正在驗證控制端"
        let (code, capabilities) = try await authorizationRequest("status")
        guard generation == epoch else { throw CancellationError() }
        guard code == 200, capabilities["supported"] as? Bool == true else { throw Failure.message("呢個入口未支援控制端授權，請更新外網主機；舊版區域網配對可喺進階設定填寫 PIN。") }
        if let credential = try ControllerCredentialVault.load(origin) {
            let (code, object) = try await authorizationRequest("session",body: ["credential": credential])
            guard generation == epoch else { throw CancellationError() }
            if code == 200 && object["authenticated"] as? Bool == true { controllerCredential = credential; return }
            guard code == 401 else { throw Failure.message("暫時未能驗證控制端，請稍後再連接。") }
            try ControllerCredentialVault.remove(origin)
        }
        let name = UIDevice.current.model
        let (created, ticket) = try await authorizationRequest("requests",body: ["name":name,"platform":"ios","kind":"native"])
        guard generation == epoch else { throw CancellationError() }
        guard created == 201, let id = ticket["request_id"] as? String, let secret = ticket["request_secret"] as? String,
              let code = ticket["approval_code"] as? String else { throw Failure.message("暫時未能提出授權要求，請稍後再試。") }
        pendingApproval = (id,secret); approvalCode = code; approvalName = name; status = "等候受控電話批准"
        let until = Date().addingTimeInterval(300); var interval = min(10,max(2,ticket["interval"] as? Int ?? 2))
        while Date() < until {
            try await Task.sleep(nanoseconds: UInt64(interval)*1_000_000_000)
            guard generation == epoch else { throw CancellationError() }
            let (code, object) = try await authorizationRequest("poll",body: ["request_id":id,"request_secret":secret])
            guard generation == epoch else { throw CancellationError() }
            if code == 200 && object["state"] as? String == "approved", let credential = object["credential"] as? String {
                try ControllerCredentialVault.save(credential,origin:origin); controllerCredential = credential
                pendingApproval = nil; approvalCode = ""; approvalName = ""; status = "已信任 · 正在連接電話"; return
            }
            if code == 202 || code == 429 { interval = min(10,max(2,object["interval"] as? Int ?? 2)); continue }
            pendingApproval = nil
            throw Failure.message(code == 403 ? "受控電話已拒絕授權。" : "授權要求已過期，請重新連接。")
        }
        throw Failure.message("授權要求已過期，請重新連接。")
    }
    private func cancelControllerApproval() {
        guard let pending = pendingApproval, let origin = origin, let url = URL(string: "/_remoteconsole/trust/cancel",relativeTo:origin) else { return }
        pendingApproval = nil
        var request = URLRequest(url:url); request.httpMethod = "POST"; request.setValue(origin.absoluteString, forHTTPHeaderField:"Origin"); request.setValue("application/json",forHTTPHeaderField:"Content-Type")
        request.httpBody = try? JSONSerialization.data(withJSONObject:["request_id":pending.0,"request_secret":pending.1])
        if !basicUser.isEmpty && origin.scheme == "https" { request.setValue("Basic " + Data("\(basicUser):\(basicPassword)".utf8).base64EncodedString(),forHTTPHeaderField:"Authorization") }
        let configuration = URLSessionConfiguration.ephemeral; configuration.timeoutIntervalForRequest = 5
        let cleanup = URLSession(configuration:configuration)
        Task { _ = try? await cleanup.data(for:request); cleanup.invalidateAndCancel() }
    }
    private func send(_ packet: Data) { guard let channel = ordered, channel.readyState == .open, channel.bufferedAmount < 256_000 else { return }; channel.sendData(RTCDataBuffer(data: packet, isBinary: true)) }
    func key(_ code: UInt16) { guard ready else { return }; send(ControlProtocol.key(code, down: true)); send(ControlProtocol.key(code, down: false)) }
    func touch(action: UInt8, id: UInt8, x: Int, y: Int) {
        guard ready else { return }
        if action != 0 && touches[id] == nil { return }
        send(ControlProtocol.touch(action: action, id: id, x: x, y: y))
        if action == 1 { touches.removeValue(forKey: id) } else { touches[id] = (x,y) }
    }
    func releaseTouches() { for (id,p) in touches { send(ControlProtocol.touch(action: 1, id: id, x: p.0, y: p.1)) }; touches.removeAll() }
    func rotate() { if ready { releaseTouches(); send(Data([11])) } }
    func requestFrame() { send(Data([16])) }
    func paste(_ text: String) { guard ready && canClipboard else { notice = "電話未支援文字貼上。"; return }; guard let data = ControlProtocol.clipboard(text) else { notice = "文字太長，請分段傳送。"; return }; send(data) }
    func requestClipboard() { if ready && canClipboard { send(Data([8,0])) } }
    func toggleAudio() { muted.toggle(); audioTrack?.isEnabled = !muted }
    private func attach(_ track: RTCMediaStreamTrack?) { if let video = track as? RTCVideoTrack { videoTrack = video }; if let audio = track as? RTCAudioTrack { audioTrack = audio; audio.isEnabled = !muted } }
    private func startStats() {
        statsTimer?.invalidate(); previousFrames = 0; previousBytes = 0; previousTime = Date()
        statsTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            guard let self = self, let pc = self.peer else { return }
            pc.statistics { [weak self, weak pc] report in self?.onMain { [weak self, weak pc] in guard let self = self, let pc = pc, self.valid(pc) else { return }; self.updateStats(report) } }
        }
    }
    private func updateStats(_ report: RTCStatisticsReport) {
        var frames = 0.0, bytes = 0.0, fps = 0.0, rtt = 0.0, packets = 0.0, lost = 0.0, jitter = 0.0, pairID = "", route = "P2P 直連"
        let all = report.statistics
        func number(_ item: NSObject?) -> Double { (item as? NSNumber)?.doubleValue ?? 0 }
        for stat in all.values {
            let v = stat.values
            if stat.type == "inbound-rtp" && (v["kind"] as? String == "video" || v["mediaType"] as? String == "video") { packets = number(v["packetsReceived"]); lost = number(v["packetsLost"]); jitter = number(v["jitter"]) * 1000; frames = number(v["framesDecoded"]); bytes = number(v["bytesReceived"]); fps = number(v["framesPerSecond"]) }
            if stat.type == "transport", let id = v["selectedCandidatePairId"] as? String { pairID = id }
        }
        if let pair = all[pairID] {
            rtt = number(pair.values["currentRoundTripTime"]) * 1000
            for field in ["localCandidateId","remoteCandidateId"] { if let id = pair.values[field] as? String, all[id]?.values["candidateType"] as? String == "relay" { route = "TURN 中繼" } }
        }
        let now = Date(), elapsed = max(0.1, now.timeIntervalSince(previousTime))
        if fps == 0 { fps = max(0,(frames - previousFrames) / elapsed) }
        let rate = max(0,(bytes - previousBytes) * 8 / elapsed / 1_000_000)
        if let profile=resume?.2,profile.adaptive,!qualityToken.isEmpty,
           let next=networkPolicy.sample(now: now.timeIntervalSince1970,packets:packets,losses:lost,rtt:rtt,jitter:jitter,current:currentBitrate,ceiling:profile.bitrate) {
            currentBitrate=next;let generation=epoch,token=qualityToken
            Task { if let result=try? await tool("POST","/api/tools/quality",body:["token":token,"bitrate":next]),generation==epoch {currentBitrate=result["bitrate"] as? Int ?? currentBitrate} }
        }
        previousFrames = frames; previousBytes = bytes; previousTime = now
        NSLog("RemoteAudio rendered=%llu nonSilent=%llu available=%d",Self.playback.renderedFrames,Self.playback.nonSilentFrames,Self.playback.outputAvailable ? 1 : 0)
        detail = String(format: "%@ · %.0f ms\n%.0f / %d FPS · %.1f Mbps",route,rtt,fps,requestedFPS,rate)
    }
}

extension NativeSession: RTCPeerConnectionDelegate {
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) { onMain { [weak self] in guard let self = self, self.valid(peerConnection) else { return }; stream.videoTracks.forEach { self.attach($0) }; stream.audioTracks.forEach { self.attach($0) } } }
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        onMain { [weak self] in
            guard let self = self, self.valid(peerConnection) else { return }
            if newState == .connected || newState == .completed { self.connected = true; self.refreshState() }
            else if newState == .failed { self.fail("聲畫連線失敗，可試用 TURN 中繼模式。") }
            else if newState == .disconnected { self.connected = false; self.releaseTouches(); self.ready = false; self.status = "網絡中斷 · 正在恢復"; let generation = self.epoch; DispatchQueue.main.asyncAfter(deadline: .now() + 10) { [weak self] in guard let self = self, self.epoch == generation, !self.connected else { return }; self.fail("網絡已中斷，請重新連接。") } }
        }
    }
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) { if newState == .complete { onMain { [weak self] in guard let self = self, self.valid(peerConnection) else { return }; self.sendOffer() } } }
    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didStartReceivingOn transceiver: RTCRtpTransceiver) { onMain { [weak self] in guard let self = self, self.valid(peerConnection) else { return }; self.attach(transceiver.receiver.track) } }
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd rtpReceiver: RTCRtpReceiver, streams: [RTCMediaStream]) { onMain { [weak self] in guard let self = self, self.valid(peerConnection) else { return }; self.attach(rtpReceiver.track) } }
}
extension NativeSession: RTCDataChannelDelegate {
    func dataChannelDidChangeState(_ dataChannel: RTCDataChannel) { onMain { [weak self] in guard let self = self, dataChannel === self.ordered || dataChannel === self.unordered else { return }; self.refreshState() } }
    func dataChannel(_ dataChannel: RTCDataChannel, didReceiveMessageWith buffer: RTCDataBuffer) {
        onMain { [weak self] in guard let self = self, dataChannel === self.ordered || dataChannel === self.unordered, let type = buffer.data.first else { return }; let value = String(decoding: buffer.data.dropFirst(), as: UTF8.self); if type == 0x64 { self.notice = value }; if type == 0x17 { self.clipboardText = value } }
    }
}
extension NativeSession: URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) { completionHandler(nil) }
    func urlSession(_ session: URLSession, task: URLSessionTask, didReceive challenge: URLAuthenticationChallenge, completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodHTTPBasic else { completionHandler(.performDefaultHandling,nil); return }
        guard let origin = origin, origin.scheme == "https", challenge.protectionSpace.host == origin.host, challenge.protectionSpace.port == (origin.port ?? 443), challenge.previousFailureCount < 1, !basicUser.isEmpty else { completionHandler(.cancelAuthenticationChallenge,nil); return }
        completionHandler(.useCredential,URLCredential(user: basicUser,password: basicPassword,persistence: .forSession))
    }
}
