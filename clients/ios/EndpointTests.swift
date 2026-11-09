import Foundation

@main enum EndpointTests {
    static func main() {
        precondition(Endpoint.parse("control.example.com")?.absoluteString == "https://control.example.com/control")
        precondition(Endpoint.parse("http://192.168.1.20:8079")?.absoluteString == "http://192.168.1.20:8079/control")
        for value in ["http://control.example.com", "http://10.0.0.1.attacker.example", "http://127.foo.0.0.1", "http://010.0.0.1", "https://user:secret@control.example.com", "https://control.example.com/?pin=123456"] {
            precondition(Endpoint.parse(value) == nil, "Unexpected endpoint accepted: \(value)")
        }
        precondition(ControlProtocol.key(187,down: true) == Data([0,0,0,187]))
        precondition(ControlProtocol.touch(action: 1,id: 3,x: 123,y: 456) == Data([2,1,3,0,123,1,200,0,0,0]))
        let packet = ControlProtocol.clipboard("中文🙂")!
        precondition(packet.count == 24 && packet[9] == 1 && packet[13] == 10)
        precondition(ControlProtocol.position(x: 0,y: 50,viewW: 100,viewH: 100,videoW: 100,videoH: 200,clamp: false) == nil)
        let middle = ControlProtocol.position(x: 50,y: 50,viewW: 100,viewH: 100,videoW: 100,videoH: 200,clamp: false)!
        precondition(middle.0 == 50 && middle.1 == 100)
        let edge = ControlProtocol.position(x: 200,y: -10,viewW: 100,viewH: 100,videoW: 100,videoH: 200,clamp: true)!
        precondition(edge.0 == 99 && edge.1 == 0)
        print("iOS endpoint, binary control, Unicode and letterboxing policy tests passed.")
    }
}
