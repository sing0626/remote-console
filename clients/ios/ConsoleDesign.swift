import SwiftUI

enum ConsoleColor {
    static let ink = Color(red: 0.91, green: 0.97, blue: 0.97)
    static let muted = Color(red: 0.56, green: 0.67, blue: 0.72)
    static let mint = Color(red: 0.60, green: 0.91, blue: 0.81)
    static let base = Color(red: 0.035, green: 0.09, blue: 0.122)
}

struct ConsoleBackdrop: View {
    var body: some View {
        GeometryReader { geo in
            ZStack {
                ConsoleColor.base
                RadialGradient(colors: [Color(red: 0.08, green: 0.30, blue: 0.29).opacity(0.85), .clear], center: .topLeading, startRadius: 0, endRadius: geo.size.width * 1.15)
                RadialGradient(colors: [Color(red: 0.16, green: 0.20, blue: 0.40).opacity(0.65), .clear], center: .trailing, startRadius: 0, endRadius: geo.size.width * 0.95)
                LinearGradient(colors: [.clear, ConsoleColor.base.opacity(0.92)], startPoint: .top, endPoint: .bottom)
            }
        }.ignoresSafeArea().allowsHitTesting(false)
    }
}

private struct GlassSurface: ViewModifier {
    var radius: CGFloat
    @Environment(\.accessibilityReduceTransparency) private var opaque
    @ViewBuilder func body(content: Content) -> some View {
        if opaque {
            content.background(Color(red: 0.09, green: 0.15, blue: 0.19), in: RoundedRectangle(cornerRadius: radius, style: .continuous))
        } else {
            #if compiler(>=6.2)
            if #available(iOS 26.0, *) {
                content.glassEffect(.regular.tint(.white.opacity(0.025)), in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            } else { frosted(content) }
            #else
            frosted(content)
            #endif
        }
    }
    private func frosted(_ content: Content) -> some View {
        content.background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous).stroke(LinearGradient(colors: [.white.opacity(0.22), .white.opacity(0.04), ConsoleColor.mint.opacity(0.12)], startPoint: .topLeading, endPoint: .bottomTrailing), lineWidth: 0.7))
            .shadow(color: .black.opacity(0.20), radius: 22, y: 12)
    }
}
private struct InteractiveCapsuleGlass: ViewModifier {
    @Environment(\.accessibilityReduceTransparency) private var opaque
    @ViewBuilder func body(content: Content) -> some View {
        if opaque {
            content.background(Color(red: 0.09, green: 0.15, blue: 0.19), in: Capsule(style: .continuous))
        } else {
            #if compiler(>=6.2)
            if #available(iOS 26.0, *) {
                content.glassEffect(.regular.interactive(), in: Capsule(style: .continuous))
            } else { frosted(content) }
            #else
            frosted(content)
            #endif
        }
    }
    private func frosted(_ content: Content) -> some View {
        content.background(.ultraThinMaterial, in: Capsule(style: .continuous))
            .overlay(Capsule(style: .continuous).stroke(.white.opacity(0.12), lineWidth: 0.7))
    }
}
extension View {
    func glassSurface(_ radius: CGFloat = 24) -> some View { modifier(GlassSurface(radius: radius)) }
    func interactiveCapsuleGlass() -> some View { modifier(InteractiveCapsuleGlass()) }
    func insetField() -> some View {
        self.padding(.horizontal,15).frame(height: 52).background(.black.opacity(0.19), in: RoundedRectangle(cornerRadius: 15))
            .overlay(RoundedRectangle(cornerRadius: 15).stroke(.white.opacity(0.07), lineWidth: 0.7))
    }
}
private struct PressFeedback: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.opacity(configuration.isPressed ? 0.72 : 1).scaleEffect(configuration.isPressed && !reduceMotion ? 0.97 : 1)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.16), value: configuration.isPressed)
    }
}

struct ConsoleRoot: View {
    @StateObject private var session = NativeSession()
    @StateObject private var nativeTools = NativeToolsModel()
    @Environment(\.scenePhase) private var phase
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var address = ""
    @State private var pin = ""
    @State private var username = ""
    @State private var password = ""
    @State private var error = ""
    @State private var profile = StreamProfile()
    private enum Panel { case tools, settings, keyboard, hosts, files, terminal, dashboard }
    private final class PanelSelection: ObservableObject {
        @Published var panel: Panel = .tools
    }
    private enum PanelAction { case fullscreen, disconnect, reconnect, rotate, clipboard, frame, paste(String), connectHost(String) }
    private struct ConnectionDraft {
        let endpoint: URL
        let pin: String
        let profile: StreamProfile
        let username: String
        let password: String
    }
    @StateObject private var panelSelection = PanelSelection()
    @State private var panelIsPresented = false
    @State private var pendingPanelAction: PanelAction?
    @State private var hosts: [RemoteHost] = []
    @State private var hostsEndpoint = ""
    @State private var selectedHostID: String?
    @State private var loadingHosts = false
    @State private var pendingConnection: ConnectionDraft?
    @State private var directoryHint = "同一個網址，揀你想連接嘅電話"
    private enum HomeField: Hashable { case address, legacyPin, username, password }
    @FocusState private var homeFocused: HomeField?
    @FocusState private var composerFocused: Bool
    @State private var advanced = false
    @State private var fullscreen = false
    @State private var designPreview = false
    @State private var text = ""
    @State private var saved = UserDefaults.standard.stringArray(forKey: "remoteEndpoints") ?? []

    var body: some View {
        ZStack { ConsoleBackdrop(); if session.active { remote } else { devices } }
            .foregroundColor(ConsoleColor.ink).tint(ConsoleColor.mint).buttonStyle(PressFeedback())
            .toolbar { ToolbarItemGroup(placement: .keyboard) { Spacer(); Button("完成") { homeFocused = nil; composerFocused = false }.accessibilityIdentifier("home.dismissKeyboard") } }
            .sheet(isPresented: $panelIsPresented, onDismiss: panelDidDismiss) { panelContent }
            .alert("未能連接", isPresented: Binding(get: { !error.isEmpty }, set: { if !$0 { error = "" } })) { Button("知道", role: .cancel) {} } message: { Text(error) }
            .alert("電話剪貼簿", isPresented: Binding(get: { session.clipboardText != nil }, set: { if !$0 { session.clipboardText = nil } })) {
                Button("複製到 iPhone") { UIPasteboard.general.string = session.clipboardText; session.clipboardText = nil }
                Button("關閉", role: .cancel) { session.clipboardText = nil }
            } message: { Text(session.clipboardText ?? "") }
            .onChange(of: phase) { value in if value != .active { session.releaseTouches() }; if value == .background { session.disconnect() } }
            .onChange(of: session.active) { UIApplication.shared.isIdleTimerDisabled = $0 }
            .onAppear(perform: load)
    }
    private func load() {
        let store = UserDefaults.standard
        profile.fps = store.object(forKey: "nativeFPS") as? Int ?? 60
        profile.size = store.object(forKey: "nativeSize") as? Int ?? 1920
        profile.bitrate = store.object(forKey: "nativeBitrate") as? Int ?? 8_000_000
        profile.relay = store.bool(forKey: "nativeRelay"); profile.region = store.string(forKey:"nativeRegion") ?? "auto"; profile.adaptive = store.object(forKey:"nativeAdaptive") as? Bool ?? true; profile.autoReconnect = store.object(forKey:"nativeReconnect") as? Bool ?? true
        if ProcessInfo.processInfo.arguments.contains("--preview-session") {
            designPreview = true; session.active = true; session.deviceName = "Android 電話"; session.status = "介面預覽 · 未連接"; session.detail = "未有聲畫連線"
        }
        if ProcessInfo.processInfo.arguments.contains("--preview-settings") { openPanel(.settings) }
    }
    @ViewBuilder private var panelContent: some View {
        Group {
            switch panelSelection.panel {
            case .tools: toolsSheet
            case .settings: profileSheet
            case .keyboard: keyboardSheet
            case .hosts: hostsSheet
            case .files: NativeToolsView(page:.files,model:nativeTools,session:session){closePanel()}
            case .terminal: NativeToolsView(page:.terminal,model:nativeTools,session:session){closePanel()}
            case .dashboard: NativeToolsView(page:.dashboard,model:nativeTools,session:session){closePanel()}
            }
        }.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .modifier(StablePanelGeometry())
    }
    private func openPanel(_ next: Panel) {
        session.releaseTouches(); composerFocused = false; homeFocused = nil
        if panelIsPresented {
            var transaction = Transaction(animation: nil)
            transaction.disablesAnimations = true
            withTransaction(transaction) { panelSelection.panel = next }
        } else { panelSelection.panel = next; panelIsPresented = true }
    }
    private func closePanel(after action: PanelAction? = nil) {
        pendingPanelAction = action; composerFocused = false; panelIsPresented = false
    }
    private func panelDidDismiss() {
        composerFocused = false
        let action = pendingPanelAction; pendingPanelAction = nil
        switch action {
        case .fullscreen: toggleFullscreen()
        case .disconnect: session.disconnect()
        case .reconnect: session.reconnect(profile: profile)
        case .rotate: session.rotate()
        case .clipboard: session.requestClipboard()
        case .frame: session.requestFrame()
        case .paste(let value): session.paste(value); text = ""
        case .connectHost(let id):
            let draft = pendingConnection; pendingConnection = nil
            if let draft = draft, let host = hosts.first(where: { $0.id == id && $0.online }) { beginConnection(draft, host: host) }
        case nil: pendingConnection = nil
        }
    }
    private func toggleFullscreen() {
        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.2)) { fullscreen.toggle() }
    }
    private var devices: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 24) {
                brand
                HStack(alignment: .bottom) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("裝置空間").font(.system(size: 32, weight: .semibold)).tracking(-0.8)
                        Text("你的 Android 電話，隨時連接。").font(.system(size: 13)).foregroundColor(ConsoleColor.muted)
                    }
                    Spacer(minLength: 8)
                    Image(systemName: "cloud").font(.system(size: 36, weight: .ultraLight)).foregroundColor(ConsoleColor.mint.opacity(0.65)).padding(.bottom,3)
                }.padding(.top,8)
                pairingCard
                savedDevices
                HStack(spacing: 0) {
                    benefit("hand.draw", "觸控遙控"); Spacer(); benefit("waveform", "聲音同步"); Spacer(); benefit("rectangle.on.rectangle", "多裝置")
                }.padding(.horizontal,6)
                HStack(spacing: 6) { Image(systemName: "lock.shield"); Text("信任一次 · 之後免 PIN · 隨時撤銷") }.font(.system(size: 10)).foregroundColor(ConsoleColor.muted.opacity(0.8)).frame(maxWidth: .infinity).padding(.bottom,12)
            }.padding(.horizontal,22).padding(.top,14).padding(.bottom,20).frame(maxWidth: 520).frame(maxWidth: .infinity)
        }
    }
    private var brand: some View {
        HStack(spacing: 11) {
            Image("BrandMark").resizable().frame(width: 44,height: 44).clipShape(RoundedRectangle(cornerRadius: 14,style: .continuous))
            VStack(alignment: .leading,spacing: 4) {
                Text("半雲電話").font(.system(size: 17, weight: .semibold))
                Text("控制端").font(.system(size: 10,weight: .medium)).foregroundColor(ConsoleColor.muted)
            }
            Spacer()
            Text("v\(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")").font(.system(size: 10,weight: .medium,design: .monospaced)).foregroundColor(ConsoleColor.mint).padding(.horizontal,11).padding(.vertical,8).background(ConsoleColor.mint.opacity(0.08), in: Capsule()).overlay(Capsule().stroke(ConsoleColor.mint.opacity(0.15),lineWidth: 0.7))
        }
    }
    private var pairingCard: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 12) {
                Image(systemName: "network").font(.system(size: 21,weight: .light)).foregroundColor(ConsoleColor.mint).frame(width: 42,height: 42).background(ConsoleColor.mint.opacity(0.08),in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) { Text("連線入口").font(.system(size: 18,weight: .semibold)); Text("一個網址，連接所有受控電話").font(.system(size: 11)).foregroundColor(ConsoleColor.muted) }
                Spacer()
            }
            VStack(alignment: .leading,spacing: 8) {
                fieldLabel("電話網址", "link")
                TextField("https://你的電話網址",text: $address).font(.system(size: 13,design: .monospaced)).textContentType(.URL).keyboardType(.URL).autocapitalization(.none).disableAutocorrection(true).insetField().focused($homeFocused,equals: .address).submitLabel(.done).onSubmit { homeFocused = nil }.accessibilityIdentifier("home.address")
            }
            VStack(alignment: .leading,spacing: 8) {
                fieldLabel("受控電話", "iphone")
                Button { loadHosts(connectAfterSelection: false) } label: {
                    HStack {
                        Text(hostsEndpoint == Endpoint.parse(address)?.absoluteString ? (hosts.first(where: { $0.id == selectedHostID })?.name ?? "揀受控電話") : "揀受控電話").font(.system(size: 13, weight: .medium))
                        Spacer()
                        if loadingHosts { ProgressView().tint(ConsoleColor.mint) } else { Image(systemName: "chevron.down").font(.system(size: 11,weight: .semibold)) }
                    }.padding(.horizontal,16).frame(height: 44).contentShape(Capsule(style: .continuous))
                }.buttonStyle(.plain).interactiveCapsuleGlass().disabled(loadingHosts).accessibilityIdentifier("home.hosts")
                Text(directoryHint).font(.system(size: 10)).foregroundColor(ConsoleColor.muted)
            }
            Text("首次喺受控電話批准一次，之後可自動登入。 ").font(.system(size: 11)).foregroundColor(ConsoleColor.muted)
            primaryButton(loadingHosts ? "載入電話清單…" : "連接電話",icon: "arrow.up.right",action: connect).disabled(loadingHosts).accessibilityIdentifier("home.connect")
            HStack {
                Button { openPanel(.settings) } label: { Label("畫質與連線",systemImage: "slider.horizontal.3") }.accessibilityIdentifier("home.settings")
                Spacer()
                Button { withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.2)) { advanced.toggle() } } label: { HStack(spacing: 5) { Text("進階"); Image(systemName: advanced ? "chevron.up" : "chevron.down").font(.system(size: 9,weight: .semibold)) } }.accessibilityIdentifier("home.advanced")
            }.font(.system(size: 12,weight: .medium)).foregroundColor(ConsoleColor.muted).frame(minHeight: 28)
            if advanced {
                VStack(alignment: .leading,spacing: 10) {
                    TextField("外網帳戶（如有）",text: $username).font(.system(size: 14)).autocapitalization(.none).disableAutocorrection(true).insetField()
                    SecureField("外網密碼",text: $password).font(.system(size: 14)).insetField()
                    SecureField("舊版 PIN（區域網相容模式，選填）",text: $pin).keyboardType(.numberPad).insetField().focused($homeFocused,equals: .legacyPin).accessibilityIdentifier("home.pin")
                    Text("只喺反向代理另設登入時填寫。").font(.caption2).foregroundColor(ConsoleColor.muted)
                }
            }
        }.padding(22).glassSurface(30)
    }
    private var savedDevices: some View {
        VStack(alignment: .leading,spacing: 12) {
            HStack { Text("常用入口").font(.system(size: 14,weight: .semibold)); Spacer(); Text(saved.isEmpty ? "尚未儲存" : "\(saved.count) 個").font(.system(size: 10)).foregroundColor(ConsoleColor.muted) }
            if saved.isEmpty {
                HStack(spacing: 12) { Image(systemName: "plus").font(.system(size: 15)).foregroundColor(ConsoleColor.muted).frame(width: 37,height: 42).background(.white.opacity(0.03),in: RoundedRectangle(cornerRadius: 11)); VStack(alignment: .leading,spacing: 4) { Text("新增第一個入口").font(.system(size: 12,weight: .medium)); Text("先輸入網址，再選擇受控電話").font(.system(size: 10)).foregroundColor(ConsoleColor.muted) }; Spacer() }.padding(17).background(.white.opacity(0.025),in: RoundedRectangle(cornerRadius: 20)).overlay(RoundedRectangle(cornerRadius: 20).stroke(.white.opacity(0.055),lineWidth: 0.7))
            }
            ForEach(saved,id: \.self) { value in
                Button { address = value } label: { HStack(spacing: 12) { Image(systemName: "iphone").font(.system(size: 23,weight: .light)).foregroundColor(ConsoleColor.mint).frame(width: 36); VStack(alignment: .leading,spacing: 4) { Text("遙控入口").font(.system(size: 13,weight: .medium)); Text(URL(string: value)?.host ?? value).font(.system(size: 10)).foregroundColor(ConsoleColor.muted).lineLimit(1) }; Spacer(); Image(systemName: "arrow.up.right").font(.system(size: 12)).foregroundColor(ConsoleColor.muted) }.padding(16).glassSurface(20) }.buttonStyle(PressFeedback()).contextMenu { Button("移除電話",role: .destructive) { saved.removeAll { $0 == value }; UserDefaults.standard.set(saved,forKey: "remoteEndpoints") } }
            }
        }
    }
    private func benefit(_ icon: String,_ title: String) -> some View { VStack(spacing: 8) { Image(systemName: icon).font(.system(size: 17,weight: .light)).foregroundColor(ConsoleColor.mint.opacity(0.8)); Text(title).font(.system(size: 10)).foregroundColor(ConsoleColor.muted) } }
    private func fieldLabel(_ value: String,_ icon: String) -> some View { Label(value,systemImage: icon).font(.system(size: 10,weight: .medium)).foregroundColor(ConsoleColor.muted) }
    private func primaryButton(_ title: String,icon: String = "arrow.right",action: @escaping () -> Void) -> some View {
        Button(action: action) { HStack { Text(title).font(.system(size: 15,weight: .semibold)); Spacer(); Image(systemName: icon).font(.system(size: 16,weight: .semibold)) }.padding(.horizontal,18).frame(height: 51).foregroundColor(ConsoleColor.base).background(LinearGradient(colors: [Color(red: 0.65,green: 0.94,blue: 0.85),ConsoleColor.mint],startPoint: .topLeading,endPoint: .bottomTrailing),in: RoundedRectangle(cornerRadius: 16)).overlay(RoundedRectangle(cornerRadius: 16).stroke(.white.opacity(0.3),lineWidth: 0.7)).shadow(color: ConsoleColor.mint.opacity(0.10),radius: 15,y: 7) }
    }
    private func connect() {
        homeFocused = nil
        guard pin.isEmpty || (pin.count == 6 && pin.utf8.allSatisfy({ (48...57).contains($0) })) else { error = "舊版 PIN 需要 6 位數字。"; return }
        loadHosts(connectAfterSelection: true)
    }
    private func loadHosts(connectAfterSelection: Bool) {
        homeFocused = nil
        guard !loadingHosts else { return }
        guard let url = Endpoint.parse(address) else { error = "請輸入有效電話網址；外網要用 HTTPS。"; return }
        let draft = ConnectionDraft(endpoint: url, pin: pin, profile: profile, username: username, password: password)
        loadingHosts = true
        Task { @MainActor in
            defer { loadingHosts = false }
            do {
                let directory = try await NativeSession.directory(endpoint: url, username: draft.username, password: draft.password)
                guard Endpoint.parse(address)?.absoluteString == url.absoluteString else { return }
                if directory == nil && hostsEndpoint == url.absoluteString && !hosts.isEmpty {
                    self.error = "入口暫時未能提供電話清單，請重新整理後再試。"; return
                }
                if hostsEndpoint != url.absoluteString { selectedHostID = nil }
                hostsEndpoint = url.absoluteString; hosts = directory ?? []
                guard let directory = directory else {
                    selectedHostID = nil; directoryHint = "呢個入口直接連接一部電話"
                    if connectAfterSelection { beginConnection(draft, host: nil) }
                    return
                }
                directoryHint = "\(directory.count) 部電話共用呢個入口，信任控制端後可直接切換"
                if connectAfterSelection {
                    if directory.count == 1 { selectedHostID = directory.first?.id }
                    if let host = directory.first(where: { $0.id == selectedHostID && $0.online }) {
                        if panelIsPresented { pendingConnection = draft; closePanel(after: .connectHost(host.id)) }
                        else { beginConnection(draft, host: host) }
                    }
                    else { pendingConnection = draft; openPanel(.hosts) }
                } else { pendingConnection = nil; openPanel(.hosts) }
            } catch { self.error = error.localizedDescription }
        }
    }
    private func beginConnection(_ draft: ConnectionDraft, host: RemoteHost?) {
        pin = ""; pendingConnection = nil
        let url = draft.endpoint
        saved.removeAll { $0 == url.absoluteString }; saved.insert(url.absoluteString,at: 0); saved = Array(saved.prefix(12)); UserDefaults.standard.set(saved,forKey: "remoteEndpoints")
        designPreview = false; fullscreen = false
        session.connect(endpoint: url, pin: draft.pin, profile: draft.profile, username: draft.username, password: draft.password, deviceID: host?.id, hostName: host?.name)
    }
    private var hostsSheet: some View {
        ZStack { ConsoleBackdrop(); ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 20) {
                sheetTitle("揀受控電話",subtitle: "同一個網址，信任一次後即可切換電話") { closePanel() }
                ForEach(hosts) { host in
                    Button {
                        selectedHostID = host.id
                        if pendingConnection != nil { closePanel(after: .connectHost(host.id)) } else { closePanel() }
                    } label: {
                        HStack(spacing: 16) {
                            Image(systemName: "iphone").font(.system(size: 28, weight: .light)).foregroundColor(ConsoleColor.mint)
                            VStack(alignment: .leading, spacing: 8) {
                                Text(host.name).font(.system(size: 17, weight: .semibold)).foregroundColor(ConsoleColor.ink)
                                Text(host.online ? "已連線 · 撳入去操作" : "離線 · 等候電話連線").font(.system(size: 11)).foregroundColor(host.online ? ConsoleColor.mint : ConsoleColor.muted)
                            }
                            Spacer()
                            Image(systemName: selectedHostID == host.id ? "checkmark.circle.fill" : "arrow.up.right").foregroundColor(ConsoleColor.mint)
                        }.padding(22).glassSurface(24).contentShape(RoundedRectangle(cornerRadius: 24, style: .continuous)).opacity(host.online ? 1 : 0.55)
                    }.disabled(!host.online).accessibilityIdentifier("picker.host.\(host.id)")
                }
                Button { loadHosts(connectAfterSelection: pendingConnection != nil) } label: { Label(loadingHosts ? "載入中…" : "重新整理", systemImage: "arrow.clockwise").font(.system(size: 13,weight: .medium)).padding(.horizontal,18).padding(.vertical,13) }.buttonStyle(.plain).interactiveCapsuleGlass().disabled(loadingHosts)
                Text("已信任控制端可直接切換此入口內所有電話，各部電話嘅連線互相分開。").font(.system(size: 11)).foregroundColor(ConsoleColor.muted).lineSpacing(4)
            }.padding(24).padding(.top,12)
        } }.preferredColorScheme(.dark)
    }
    private var remote: some View {
        VStack(spacing: 12) {
            if !fullscreen { sessionHeader }
            ZStack(alignment: .bottomLeading) {
                Color.black
                if designPreview { sessionPlaceholder } else { NativeVideo(session: session) }
                if !designPreview && session.videoTrack == nil && session.approvalCode.isEmpty {
                    VStack(spacing: 15) { if session.notice.isEmpty { ProgressView().tint(ConsoleColor.mint) }; Text(session.status).font(.system(size: 13)).multilineTextAlignment(.center); if !session.notice.isEmpty { Button("重新連接") { session.reconnect(profile: profile) } } }.padding(28).frame(maxWidth: .infinity,maxHeight: .infinity)
                }
                if !session.approvalCode.isEmpty {
                    VStack(alignment: .leading, spacing: 18) {
                        Text("等候受控電話批准").font(.system(size: 18,weight: .semibold))
                        Text(session.approvalCode).font(.system(size: 28,weight: .semibold,design: .monospaced)).foregroundColor(ConsoleColor.mint).accessibilityIdentifier("session.approvalCode")
                        Text("喺受控電話開「半雲電話 → 控制端授權」，核對代碼同名稱「\(session.approvalName)」後批准。之後可自動登入所有電話。").font(.system(size: 12)).foregroundColor(ConsoleColor.muted).lineSpacing(5)
                    }.padding(25).glassSurface(24).padding(20)
                }
            }.clipShape(RoundedRectangle(cornerRadius: fullscreen ? 0 : 22)).overlay(RoundedRectangle(cornerRadius: fullscreen ? 0 : 22).stroke(.white.opacity(0.08),lineWidth: 0.7))
            HStack(spacing: 7) {
                Image(systemName: "waveform.path").foregroundColor(ConsoleColor.mint)
                Text(session.detail.replacingOccurrences(of: "\n", with: " · ")).lineLimit(1).minimumScaleFactor(0.75).layoutPriority(1).accessibilityIdentifier("session.detail")
                Spacer(minLength: 0)
            }.font(.system(size: 9,design: .monospaced)).monospacedDigit().foregroundColor(ConsoleColor.muted).frame(height: 14).padding(.horizontal,12).padding(.vertical,2)
            if !session.notice.isEmpty { Text(session.notice).font(.system(size: 11)).foregroundColor(ConsoleColor.muted).padding(.horizontal,10) }
            controlDock
        }.padding(.horizontal,fullscreen ? 0 : 12).padding(.top,fullscreen ? 0 : 8).padding(.bottom,8)
    }
    private var sessionHeader: some View {
        HStack(spacing: 10) {
            iconButton("chevron.left",label: "電話清單") { session.disconnect() }
            Image("BrandMark").resizable().frame(width: 28,height: 28).clipShape(RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading,spacing: 4) {
                Text(session.deviceName).font(.system(size: 14,weight: .semibold)).lineLimit(1)
                HStack(spacing: 5) { Circle().fill(session.ready ? ConsoleColor.mint : Color.orange.opacity(0.8)).frame(width: 5,height: 5); Text(session.status).font(.system(size: 9)).foregroundColor(ConsoleColor.muted).lineLimit(2).accessibilityIdentifier("session.status") }
            }
            Spacer(minLength: 0)
            iconButton(session.muted ? "speaker.slash" : "speaker.wave.2",label: session.muted ? "開聲" : "靜音") { session.toggleAudio() }
            iconButton("arrow.up.left.and.arrow.down.right",label: "全螢幕") { toggleFullscreen() }.accessibilityIdentifier("session.fullscreen")
        }.padding(.horizontal,8).padding(.vertical,5).buttonStyle(.plain).interactiveCapsuleGlass()
    }
    private func iconButton(_ icon: String,label: String,action: @escaping () -> Void) -> some View { Button(action: action) { Image(systemName: icon).font(.system(size: 16,weight: .medium)).foregroundColor(ConsoleColor.ink.opacity(0.85)).frame(width: 36,height: 40).contentShape(Rectangle()) }.accessibilityLabel(label) }
    private var controlDock: some View {
        HStack(spacing: 2) {
            dockButton("返回","arrow.left") { session.key(4) }.disabled(!session.ready)
            dockButton("主畫面","house") { session.key(3) }.disabled(!session.ready).accessibilityIdentifier("session.home")
            dockButton("最近","square.stack") { session.key(187) }.disabled(!session.ready)
            dockButton("文字","keyboard") { openPanel(.keyboard) }.disabled(!session.ready && !designPreview).accessibilityIdentifier("session.keyboard")
            dockButton("更多","ellipsis",active: true) { openPanel(.tools) }.accessibilityIdentifier("session.tools")
        }.padding(7).buttonStyle(.plain).interactiveCapsuleGlass().padding(.horizontal,4)
    }
    private func dockButton(_ title: String,_ icon: String,active: Bool = false,action: @escaping () -> Void) -> some View {
        Button(action: action) { VStack(spacing: 5) { Image(systemName: icon).font(.system(size: 21,weight: .regular)); Text(title).font(.system(size: 9,weight: .medium)) }.frame(maxWidth: .infinity).frame(height: 52).foregroundColor(active ? ConsoleColor.mint : ConsoleColor.ink.opacity(session.ready ? 0.9 : 0.4)).background(active ? ConsoleColor.mint.opacity(0.09) : .clear,in: RoundedRectangle(cornerRadius: 18)) }
    }
    private var sessionPlaceholder: some View {
        ZStack {
            LinearGradient(colors: [Color(red: 0.06,green: 0.18,blue: 0.21),Color(red: 0.13,green: 0.12,blue: 0.28),ConsoleColor.base],startPoint: .topLeading,endPoint: .bottomTrailing)
            VStack(spacing: 18) { Image(systemName: "iphone.radiowaves.left.and.right").font(.system(size: 70,weight: .ultraLight)).foregroundColor(ConsoleColor.mint.opacity(0.6)); Text("介面預覽").font(.system(size: 19,weight: .medium)); Text("未連接電話，並非實際串流畫面").font(.system(size: 11)).foregroundColor(ConsoleColor.muted) }
        }
    }
    private func sheetTitle(_ title: String,subtitle: String,close: @escaping () -> Void) -> some View {
        HStack { VStack(alignment: .leading,spacing: 6) { Text(title).font(.system(size: 25,weight: .semibold)); Text(subtitle).font(.system(size: 11)).foregroundColor(ConsoleColor.muted) }; Spacer(); iconButton("xmark",label: "關閉",action: close).buttonStyle(.plain).interactiveCapsuleGlass().accessibilityIdentifier("panel.close") }
    }
    private var profileSheet: some View {
        ZStack { ConsoleBackdrop(); ScrollView(showsIndicators: false) { VStack(alignment: .leading,spacing: 24) {
            sheetTitle("畫質與連線",subtitle: "揀一個適合你而家網絡嘅設定") { persistProfile(); closePanel() }
            VStack(alignment: .leading,spacing: 18) {
                settingsCaption("影片流暢度","依裝置及網絡而定")
                choices([30,60,120],selection: $profile.fps) { "\($0) FPS" }
                Divider().overlay(.white.opacity(0.06))
                settingsCaption("畫面大小","最長邊解像度")
                choices([960,1280,1920,2560],selection: $profile.size) { String($0) }
                settingsCaption("位元率","更清晰會使用更多數據")
                choices([2,4,8,16],selection: Binding(get: { profile.bitrate / 1_000_000 },set: { profile.bitrate = $0 * 1_000_000 })) { "\($0) M" }
            }.padding(20).glassSurface(25)
            VStack(alignment: .leading,spacing: 14) { Toggle(isOn: $profile.relay) { HStack(spacing: 12) { Image(systemName: "point.3.connected.trianglepath.dotted").foregroundColor(ConsoleColor.mint); VStack(alignment: .leading,spacing: 4) { Text("TURN 中繼模式").font(.system(size: 14,weight: .medium)); Text("直連不穩定時可以開啟").font(.system(size: 10)).foregroundColor(ConsoleColor.muted) } } }.tint(ConsoleColor.mint); Text("多人連線會沿用現有串流畫質。實際 FPS 視乎電話、畫面活動同網絡。").font(.system(size: 11)).foregroundColor(ConsoleColor.muted).lineSpacing(4) }.padding(20).glassSurface(25)
            VStack(alignment:.leading,spacing:14){
                Toggle("自適應位元率",isOn:$profile.adaptive).accessibilityIdentifier("settings.adaptive")
                Toggle("網絡變更後自動重連",isOn:$profile.autoReconnect)
                Picker("中繼地區",selection:$profile.region){Text("自動 · 故障時切換").tag("auto");ForEach(session.relayRegions,id:\.self){region in Text(region["name"] ?? "中繼").tag(region["id"] ?? "auto")}}
            }.font(.system(size:13)).tint(ConsoleColor.mint).padding(20).glassSurface(25)
            primaryButton(session.active && !designPreview ? "套用並重新連接" : "儲存設定",icon: "checkmark") { persistProfile(); closePanel(after: session.active && !designPreview ? .reconnect : nil) }
        }.padding(24).padding(.top,12) } }.preferredColorScheme(.dark)
    }
    private func settingsCaption(_ title: String,_ subtitle: String) -> some View { HStack { Text(title).font(.system(size: 13,weight: .medium)); Spacer(); Text(subtitle).font(.system(size: 9)).foregroundColor(ConsoleColor.muted) } }
    private func choices(_ values: [Int],selection: Binding<Int>,title: @escaping (Int) -> String) -> some View {
        HStack(spacing: 6) { ForEach(values,id: \.self) { value in Button { selection.wrappedValue = value } label: { Text(title(value)).font(.system(size: 12,weight: .semibold,design: .rounded)).frame(maxWidth: .infinity).frame(height: 43).foregroundColor(selection.wrappedValue == value ? ConsoleColor.base : ConsoleColor.muted).background(selection.wrappedValue == value ? ConsoleColor.mint : .white.opacity(0.04),in: RoundedRectangle(cornerRadius: 12)) } } }
    }
    private func persistProfile() { let store = UserDefaults.standard; store.set(profile.fps,forKey: "nativeFPS"); store.set(profile.size,forKey: "nativeSize"); store.set(profile.bitrate,forKey: "nativeBitrate"); store.set(profile.relay,forKey: "nativeRelay"); store.set(profile.region,forKey:"nativeRegion"); store.set(profile.adaptive,forKey:"nativeAdaptive"); store.set(profile.autoReconnect,forKey:"nativeReconnect") }
    private var keyboardSheet: some View {
        ZStack { ConsoleBackdrop(); VStack(alignment: .leading,spacing: 22) { sheetTitle("傳送文字",subtitle: "中文、Emoji，一樣貼到電話") { closePanel() }; TextEditor(text: $text).focused($composerFocused).accessibilityIdentifier("composer.input").padding(12).frame(minHeight: 140).glassSurface(20); HStack { Button { session.key(66) } label: { Label("Enter",systemImage: "return").font(.system(size: 13)) }; Spacer(); Button { closePanel(after: .paste(text)) } label: { Label("貼到電話",systemImage: "arrow.up.right").font(.system(size: 14,weight: .semibold)).padding(.horizontal,18).padding(.vertical,14).foregroundColor(ConsoleColor.base).background(ConsoleColor.mint,in: Capsule()) } } }.padding(24) }.preferredColorScheme(.dark)
    }
    private var toolsSheet: some View {
        ZStack { ConsoleBackdrop(); ScrollView { VStack(alignment: .leading,spacing: 22) {
            sheetTitle("遙控工具",subtitle: "常用操作，隨手就到") { closePanel() }
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(),spacing: 10),count: 3),spacing: 10) {
                tool("畫質","slider.horizontal.3") { openPanel(.settings) }.accessibilityIdentifier("tools.settings")
                tool("全螢幕","arrow.up.left.and.arrow.down.right") { closePanel(after: .fullscreen) }.accessibilityIdentifier("tools.fullscreen")
                tool("旋轉","rotate.right") { closePanel(after: .rotate) }
                tool("音量 +","speaker.plus") { session.key(24) }
                tool("音量 −","speaker.minus") { session.key(25) }
                tool("電源","power") { session.key(26) }
                tool("剪貼簿","doc.on.clipboard") { closePanel(after: .clipboard) }
                tool("清晰畫面","sparkles") { closePanel(after: .frame) }
                tool("重新連接","arrow.clockwise") { closePanel(after: .reconnect) }
            }
            HStack(spacing:10){tool("電話檔案","folder"){openPanel(.files)}.accessibilityIdentifier("tools.files");tool("Root 終端","terminal"){openPanel(.terminal)}.accessibilityIdentifier("tools.terminal");tool("電話與中繼","chart.bar"){openPanel(.dashboard)}.accessibilityIdentifier("tools.dashboard")}
            Button("結束連線",role: .destructive) { closePanel(after: .disconnect) }.font(.system(size: 13,weight: .medium)).frame(maxWidth: .infinity).frame(height: 48).background(.white.opacity(0.04),in: RoundedRectangle(cornerRadius: 15)).foregroundColor(Color(red: 0.95,green: 0.65,blue: 0.61)).accessibilityIdentifier("tools.disconnect")
        }.padding(24).padding(.top,12) } }.preferredColorScheme(.dark)
    }
    private func tool(_ title: String,_ icon: String,action: @escaping () -> Void) -> some View { Button(action: action) { VStack(spacing: 12) { Image(systemName: icon).font(.system(size: 23,weight: .light)).foregroundColor(ConsoleColor.mint); Text(title).font(.system(size: 11)).foregroundColor(ConsoleColor.ink) }.frame(maxWidth: .infinity).frame(height: 90).background(.white.opacity(0.045),in: RoundedRectangle(cornerRadius: 18)).overlay(RoundedRectangle(cornerRadius: 18).stroke(.white.opacity(0.07),lineWidth: 0.7)) } }
}

private struct StablePanelGeometry: ViewModifier {
    @ViewBuilder func body(content: Content) -> some View {
        if #available(iOS 16.0, *) {
            content.presentationDetents([.large]).presentationDragIndicator(.visible)
        } else { content }
    }
}
