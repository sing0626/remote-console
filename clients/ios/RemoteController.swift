import SwiftUI
import WebRTC

@main struct RemoteConsoleApp: App {
    var body: some Scene { WindowGroup { ConsoleRoot().preferredColorScheme(.dark) } }
}

struct NativeVideo: UIViewRepresentable {
    @ObservedObject var session: NativeSession
    func makeUIView(context: Context) -> RemoteVideoSurface { RemoteVideoSurface(session: session) }
    func updateUIView(_ uiView: RemoteVideoSurface, context: Context) { uiView.setTrack(session.videoTrack) }
    static func dismantleUIView(_ uiView: RemoteVideoSurface, coordinator: ()) { uiView.releaseAll(); uiView.setTrack(nil) }
}

final class RemoteVideoSurface: UIView, RTCVideoViewDelegate {
    private let metal = RTCMTLVideoView(frame: .zero)
    private weak var session: NativeSession?
    private var track: RTCVideoTrack?
    private var pointers: [ObjectIdentifier: UInt8] = [:]
    init(session: NativeSession) {
        self.session = session; super.init(frame: .zero); isMultipleTouchEnabled = true
        metal.videoContentMode = .scaleAspectFit; metal.isUserInteractionEnabled = false; metal.delegate = self; addSubview(metal)
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override func layoutSubviews() { super.layoutSubviews(); metal.frame = bounds }
    func setTrack(_ value: RTCVideoTrack?) { if track === value { return }; track?.remove(metal); track = value; value?.add(metal) }
    func videoView(_ videoView: RTCVideoRenderer, didChangeVideoSize size: CGSize) { DispatchQueue.main.async { [weak self] in guard let session = self?.session else { return }; if size.width > 0 && size.height > 0 { session.videoWidth = Int(size.width); session.videoHeight = Int(size.height) } } }
    private func position(_ touch: UITouch, clamp: Bool) -> (Int,Int)? { guard let s = session else { return nil }; let p = touch.location(in: self); return ControlProtocol.position(x: p.x,y: p.y,viewW: bounds.width,viewH: bounds.height,videoW: s.videoWidth,videoH: s.videoHeight,clamp: clamp) }
    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) { guard session?.ready == true else { return }; for touch in touches { guard pointers.count < 10, let p = position(touch,clamp: false), let id = (UInt8(0)...UInt8(9)).first(where: { !pointers.values.contains($0) }) else { continue }; pointers[ObjectIdentifier(touch)] = id; session?.touch(action: 0,id: id,x: p.0,y: p.1) } }
    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) { for touch in touches { if let id = pointers[ObjectIdentifier(touch)], let p = position(touch,clamp: true) { session?.touch(action: 2,id: id,x: p.0,y: p.1) } } }
    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) { for touch in touches { if let id = pointers.removeValue(forKey: ObjectIdentifier(touch)), let p = position(touch,clamp: true) { session?.touch(action: 1,id: id,x: p.0,y: p.1) } } }
    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) { releaseAll() }
    func releaseAll() { session?.releaseTouches(); pointers.removeAll() }
}
