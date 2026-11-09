#import <Foundation/Foundation.h>
#import <WebRTC/RTCAudioDevice.h>
NS_ASSUME_NONNULL_BEGIN
// Receive-only PCM output. Never opens a recording device or VoiceProcessingIO.
@interface RCPlaybackAudioDevice : NSObject <RTCAudioDevice>
@property(nonatomic, readonly) uint64_t renderedFrames;
@property(nonatomic, readonly) uint64_t nonSilentFrames;
@property(nonatomic, readonly) BOOL outputAvailable;
@end
NS_ASSUME_NONNULL_END
