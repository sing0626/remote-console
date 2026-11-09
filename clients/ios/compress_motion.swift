import Foundation
import AVFoundation

let input = URL(fileURLWithPath: CommandLine.arguments[1])
let output = URL(fileURLWithPath: CommandLine.arguments[2])
let asset = AVURLAsset(url: input)
guard let export = AVAssetExportSession(asset: asset, presetName: AVAssetExportPreset960x540) else { fatalError("Cannot create video export") }
export.outputURL = output
export.outputFileType = .mp4
export.shouldOptimizeForNetworkUse = true
let finished = DispatchSemaphore(value: 0)
export.exportAsynchronously { finished.signal() }
guard finished.wait(timeout: .now() + 180) == .success, export.status == .completed else { fatalError("Video export failed: \(String(describing: export.error))") }
print(output.path)
