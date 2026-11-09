#import "RCPlaybackAudioDevice.h"
#import <AVFoundation/AVFoundation.h>
#import <AudioToolbox/AudioToolbox.h>
#import <mach/mach_time.h>
#import <stdatomic.h>

static const UInt32 RCFrames = 480;
static const UInt32 RCChannels = 1;
static const Float64 RCSampleRate = 48000;

@interface RCPlaybackAudioDevice () {
    id<RTCAudioDeviceDelegate> _delegate;
    AudioQueueRef _queue;
    NSCondition *_condition;
    NSMutableArray<NSValue *> *_freeBuffers;
    NSThread *_worker;
    BOOL _running, _workerFinished, _initialized, _playoutInitialized, _recordingInitialized;
    atomic_uint_fast64_t _rendered, _nonSilent;
    atomic_bool _available;
}
- (void)returnBuffer:(AudioQueueBufferRef)buffer;
@end

static void RCQueueCallback(void *context, AudioQueueRef queue, AudioQueueBufferRef buffer) {
    [(__bridge RCPlaybackAudioDevice *)context returnBuffer:buffer];
}

@implementation RCPlaybackAudioDevice
- (instancetype)init {
    if ((self = [super init])) {
        _condition = [[NSCondition alloc] init];
        _freeBuffers = [[NSMutableArray alloc] init];
        atomic_init(&_rendered, 0); atomic_init(&_nonSilent, 0); atomic_init(&_available, false);
    }
    return self;
}
- (uint64_t)renderedFrames { return atomic_load(&_rendered); }
- (uint64_t)nonSilentFrames { return atomic_load(&_nonSilent); }
- (BOOL)outputAvailable { return atomic_load(&_available); }
- (double)deviceInputSampleRate { return RCSampleRate; }
- (double)deviceOutputSampleRate { return RCSampleRate; }
- (NSTimeInterval)inputIOBufferDuration { return 0.01; }
- (NSTimeInterval)outputIOBufferDuration { return 0.01; }
- (NSInteger)inputNumberOfChannels { return RCChannels; }
- (NSInteger)outputNumberOfChannels { return RCChannels; }
- (NSTimeInterval)inputLatency { return 0; }
- (NSTimeInterval)outputLatency { return 0.03; }
- (BOOL)isInitialized { return _initialized; }
- (BOOL)isPlayoutInitialized { return _playoutInitialized; }
- (BOOL)isRecordingInitialized { return _recordingInitialized; }
- (BOOL)isRecording { return NO; }
- (BOOL)isPlaying { [_condition lock]; BOOL value = _running; [_condition unlock]; return value; }
- (BOOL)initializeWithDelegate:(id<RTCAudioDeviceDelegate>)delegate {
    _delegate = delegate; _initialized = YES; return YES;
}
- (BOOL)initializePlayout { _playoutInitialized = YES; return YES; }
// No microphone is created, polled, or sent, including if ADM queries its input side.
- (BOOL)initializeRecording { _recordingInitialized = YES; return YES; }
- (BOOL)startRecording { return YES; }
- (BOOL)stopRecording { return YES; }

- (BOOL)startPlayout {
    if (self.isPlaying) return YES;
    NSError *error = nil;
    AVAudioSession *session = AVAudioSession.sharedInstance;
    if (![session setCategory:AVAudioSessionCategoryPlayback mode:AVAudioSessionModeDefault
                      options:AVAudioSessionCategoryOptionMixWithOthers error:&error]
        || ![session setActive:YES error:&error]) {
        NSLog(@"RemoteAudio output unavailable: %@", error.domain);
        atomic_store(&_available, false); return NO;
    }
    AudioStreamBasicDescription format = {0};
    format.mSampleRate = RCSampleRate;
    format.mFormatID = kAudioFormatLinearPCM;
    format.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked;
    format.mBytesPerPacket = 2 * RCChannels; format.mFramesPerPacket = 1;
    format.mBytesPerFrame = 2 * RCChannels; format.mChannelsPerFrame = RCChannels;
    format.mBitsPerChannel = 16;
    OSStatus result = AudioQueueNewOutput(&format, RCQueueCallback, (__bridge void *)self, NULL, NULL, 0, &_queue);
    if (result != noErr) { NSLog(@"RemoteAudio queue create failed: %d", (int)result); _queue = NULL; return NO; }
    for (NSUInteger i = 0; i < 3; i++) {
        AudioQueueBufferRef buffer = NULL;
        result = AudioQueueAllocateBuffer(_queue, RCFrames * 2 * RCChannels, &buffer);
        if (result != noErr) break;
        memset(buffer->mAudioData, 0, buffer->mAudioDataBytesCapacity);
        buffer->mAudioDataByteSize = RCFrames * 2 * RCChannels;
        result = AudioQueueEnqueueBuffer(_queue, buffer, 0, NULL);
        if (result != noErr) break;
    }
    if (result != noErr) { AudioQueueDispose(_queue, true); _queue = NULL; return NO; }
    // A single dedicated PCM thread satisfies WebRTC's render-thread contract.
    // Hardware callbacks only hand buffers back; they never call WebRTC.
    id<RTCAudioDeviceDelegate> delegate = _delegate;
    [delegate dispatchSync:^{
        [delegate notifyAudioOutputInterrupted];
        [delegate notifyAudioOutputParametersChange];
    }];
    [_condition lock]; _running = YES; _workerFinished = NO; [_freeBuffers removeAllObjects]; [_condition unlock];
    _worker = [[NSThread alloc] initWithBlock:^{ [self renderLoop]; }];
    _worker.name = @"RemoteConsole PCM output"; [_worker start];
    result = AudioQueueStart(_queue, NULL);
    if (result != noErr) { NSLog(@"RemoteAudio queue start failed: %d", (int)result); [self stopPlayout]; return NO; }
    atomic_store(&_available, true); return YES;
}
- (void)returnBuffer:(AudioQueueBufferRef)buffer {
    [_condition lock];
    if (_running) { [_freeBuffers addObject:[NSValue valueWithPointer:buffer]]; [_condition signal]; }
    [_condition unlock];
}
- (void)renderLoop {
    @autoreleasepool {
        uint64_t sampleTime = 0;
        id<RTCAudioDeviceDelegate> delegate = _delegate;
        for (;;) {
            [_condition lock];
            while (_running && _freeBuffers.count == 0) [_condition wait];
            if (!_running) { [_condition unlock]; break; }
            AudioQueueBufferRef buffer = [_freeBuffers.firstObject pointerValue];
            [_freeBuffers removeObjectAtIndex:0]; [_condition unlock];
            AudioBufferList output = {0}; output.mNumberBuffers = 1;
            output.mBuffers[0].mNumberChannels = RCChannels;
            output.mBuffers[0].mDataByteSize = RCFrames * 2 * RCChannels;
            output.mBuffers[0].mData = buffer->mAudioData;
            AudioTimeStamp timestamp = {0}; timestamp.mSampleTime = sampleTime;
            timestamp.mHostTime = mach_absolute_time();
            timestamp.mFlags = kAudioTimeStampSampleTimeValid | kAudioTimeStampHostTimeValid;
            AudioUnitRenderActionFlags flags = 0;
            OSStatus result = delegate.getPlayoutData(&flags, &timestamp, 0, RCFrames, &output);
            if (result != noErr) memset(buffer->mAudioData, 0, RCFrames * 2 * RCChannels);
            else {
                atomic_fetch_add(&_rendered, RCFrames);
                const int16_t *samples = buffer->mAudioData;
                for (UInt32 i = 0; i < RCFrames * RCChannels; i++) {
                    if (samples[i] > 4 || samples[i] < -4) { atomic_fetch_add(&_nonSilent, RCFrames); break; }
                }
            }
            sampleTime += RCFrames;
            buffer->mAudioDataByteSize = RCFrames * 2 * RCChannels;
            [_condition lock]; BOOL active = _running; [_condition unlock];
            if (active) AudioQueueEnqueueBuffer(_queue, buffer, 0, NULL);
        }
        [_condition lock]; _workerFinished = YES; [_condition broadcast]; [_condition unlock];
    }
}
- (BOOL)stopPlayout {
    [_condition lock]; BOOL wasRunning = _running; _running = NO; [_condition broadcast]; [_condition unlock];
    if (_queue) AudioQueueStop(_queue, true);
    if (wasRunning) {
        [_condition lock]; while (!_workerFinished) [_condition wait]; [_condition unlock];
    }
    if (_queue) { AudioQueueDispose(_queue, true); _queue = NULL; }
    _worker = nil; [_freeBuffers removeAllObjects]; atomic_store(&_available, false);
    return YES;
}
- (BOOL)terminateDevice {
    [self stopPlayout]; _delegate = nil;
    _initialized = NO; _playoutInitialized = NO; _recordingInitialized = NO; return YES;
}
@end
