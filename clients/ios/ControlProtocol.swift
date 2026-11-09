import Foundation

enum ControlProtocol {
    static func key(_ code: UInt16, down: Bool) -> Data { Data([0, down ? 0 : 1, UInt8(code >> 8), UInt8(code & 255)]) }
    static func touch(action: UInt8, id: UInt8, x: Int, y: Int) -> Data {
        let x = UInt16(clamping: x), y = UInt16(clamping: y)
        return Data([2, action, id, UInt8(x >> 8), UInt8(x & 255), UInt8(y >> 8), UInt8(y & 255), action == 1 ? 0 : 255, action == 1 ? 0 : 255, action == 1 ? 0 : 1])
    }
    static func clipboard(_ text: String) -> Data? {
        let value = Data(text.utf8)
        guard value.count <= 60_000 else { return nil }
        let n = UInt32(value.count)
        var packet = Data([9,0,0,0,0,0,0,0,0,1,UInt8((n >> 24) & 255),UInt8((n >> 16) & 255),UInt8((n >> 8) & 255),UInt8(n & 255)])
        packet.append(value); return packet
    }
    static func position(x: Double, y: Double, viewW: Double, viewH: Double, videoW: Int, videoH: Int, clamp: Bool) -> (Int,Int)? {
        guard viewW > 0, viewH > 0, videoW > 0, videoH > 0 else { return nil }
        let scale = min(viewW / Double(videoW), viewH / Double(videoH))
        let left = (viewW - Double(videoW) * scale) / 2, top = (viewH - Double(videoH) * scale) / 2
        if !clamp && (x < left || y < top || x >= left + Double(videoW) * scale || y >= top + Double(videoH) * scale) { return nil }
        return (min(videoW - 1, max(0, Int((x - left) / scale))), min(videoH - 1, max(0, Int((y - top) / scale))))
    }
}

struct StreamProfile {
    var fps = 60
    var size = 1920
    var bitrate = 8_000_000
    var relay = false
    var options: [String: String] {
        ["max_fps": String(fps), "max_size": String(size), "video_bit_rate": String(bitrate), "video_codec": "h264", "audio_codec": "opus", "audio": "true", "control": "true", "video_codec_options": "profile=1"]
    }
}
