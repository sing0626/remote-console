const jitterBufferTargetMs = 0; // 0 is a cake

// Load CONFIG from sessionStorage if available, otherwise use URL params or defaults
var CONFIG = (function () {

    // // if url has ?debug, use hardcoded debug config
    const urlParams = new URLSearchParams(window.location.search);
    if (urlParams.has('debug')) {
        console.log("Using debug config from URL parameter");
        return {
            device_type: "sunshine",
            device_id: "127.0.0.1:5555",
            device_ip: "127.0.0.1",
            device_port: "5555",
            av_sync: false,
            driver_config: {
                max_fps: "60",
                video_codec: "h265",
                audio_codec: "opus",
                video_bit_rate: "4000000",
                audio: "true",
                file_path: "test/test.h265",
                deviceID: "emulator-5554",
                // new_display: "1920x1080/60",
            }
        };
    }

    // Try to load from sessionStorage first (set by console.js)
    const stored = sessionStorage.getItem('webscreen_device_configs_now');
    console.log("Stored config:", stored);
    if (stored) {
        try {
            const parsed = JSON.parse(stored);
            console.log("Using stored config:", parsed);
            // Clear it after reading to avoid stale data
            // sessionStorage.removeItem('webscreen_device_configs_now');
            return parsed;
        } catch (e) {
            console.warn('Failed to parse stored config:', e);
        }
    }

    // Fallback to hardcoded defaults for testing
    return {
        device_type: "none",
        device_id: "emulator-5554",
        device_ip: "0",
        device_port: "0",
        av_sync: false,
        driver_config: {
            video_codec: "h264",
            audio: "true",
            audio_codec: "opus",
            video_bit_rate: "8000000"
        }
    };
})();

let marbleConnectionEpoch = 0;
async function start() {
 const epoch=++marbleConnectionEpoch;
 window.marbleStopConnection?.();
 window.marbleCapabilities=null;
 window.marbleSetConnectionState?.('connecting');
 if(CONFIG.device_type==='none'){window.marbleSetConnectionState?.('failed');return;}
 let iceConfiguration;
 try{const response=await fetch('/api/ice-config',{credentials:'same-origin'});if(epoch!==marbleConnectionEpoch)return;if(!response.ok){window.marbleSetConnectionState?.(response.status===401?'expired':'failed');return;}iceConfiguration=await response.json();if(!Array.isArray(iceConfiguration.iceServers))throw Error('Invalid ICE configuration');}catch{if(epoch===marbleConnectionEpoch)window.marbleSetConnectionState?.('failed');return;}
 if(epoch!==marbleConnectionEpoch)return;
 const pc=new RTCPeerConnection(iceConfiguration);
 window.currentPeerConnection=pc;
 let socket=null,finished=false,deadline,iceTimer,resolveIce,recoveryTimer;
 const current=()=>epoch===marbleConnectionEpoch&&!finished;
 const stop=()=>{
  if(finished)return;finished=true;clearTimeout(deadline);clearTimeout(iceTimer);clearTimeout(recoveryTimer);resolveIce?.();
  window.marbleReleaseTouches?.();
  if(window.marbleStatsTimer){clearInterval(window.marbleStatsTimer);window.marbleStatsTimer=null;}
  pc.close();socket?.close();
  document.querySelectorAll('audio[data-marble-audio]').forEach(el=>{el.pause();el.srcObject=null;el.remove();});
 };
 window.marbleStopConnection=()=>{stop();window.marbleSetConnectionState?.('disconnected');};
 const fail=state=>{if(!current())return;stop();window.marbleSetConnectionState?.(state);};
 deadline=setTimeout(()=>fail('failed'),25000);
 const connected=()=>{if(current()&&pc.connectionState==='connected'&&window.marbleCapabilities){clearTimeout(deadline);window.marbleSetConnectionState?.('connected');}};
 pc.onconnectionstatechange=()=>{
  if(!current())return;
  if(pc.connectionState==='connected'){clearTimeout(recoveryTimer);recoveryTimer=null;connected();}
  else if(pc.connectionState==='failed')fail('failed');
  else if(pc.connectionState==='disconnected'){
   window.marbleReleaseTouches?.();window.marbleSetConnectionState?.('recovering');
   if(!recoveryTimer)recoveryTimer=setTimeout(()=>fail('disconnected'),10000);
  }else if(pc.connectionState==='closed')fail('disconnected');
 };
 pc.ontrack=event=>{
  if(!current())return;
  if(event.track.kind==='video'){
   const el=document.getElementById('remoteVideo');el.srcObject=event.streams[0]||new MediaStream([event.track]);
   el.play().catch(()=>{if(current())window.marbleShowPlayButton?.();});
   el.addEventListener('loadedmetadata',triggerCheck,{once:true});el.onresize=triggerCheck;
  }else if(event.track.kind==='audio'){
   const el=document.createElement('audio');el.dataset.marbleAudio='true';
   el.srcObject=event.streams[0]||new MediaStream([event.track]);el.autoplay=true;el.muted=!window.marbleAudioEnabled;
   document.body.appendChild(el);el.play().catch(()=>{});
  }
 };
 pc.addTransceiver('video',{direction:'recvonly'});pc.addTransceiver('audio',{direction:'recvonly'});
 const ordered=pc.createDataChannel('control-ordered',{ordered:true});
 const unordered=pc.createDataChannel('control-unordered',{ordered:false});
 window.dataChannelOrdered=ordered;window.dataChannelUnordered=unordered;
 ordered.onopen=()=>{if(current())window.marbleRefreshControls?.();};
 ordered.onclose=()=>{if(current())fail('disconnected');};
 unordered.onmessage=event=>{
  if(!current())return;
  const bytes=new Uint8Array(event.data);
  if(bytes[0]===0x64)showToast(new TextDecoder().decode(bytes.slice(1)),3000);
  // Clipboard reads are explicit user actions; do not copy phone contents on connect.
 };
 try{
  const offer=await pc.createOffer();if(!current())return;
  await pc.setLocalDescription(offer);if(!current())return;
  await new Promise(resolve=>{
   resolveIce=resolve;
   if(pc.iceGatheringState==='complete')resolve();
   else{pc.onicecandidate=e=>{if(!e.candidate)resolve();};iceTimer=setTimeout(resolve,8000);}
  });
  clearTimeout(iceTimer);if(!current())return;
  const protocol=location.protocol==='https:'?'wss:':'ws:';
  socket=new WebSocket(`${protocol}//${location.host}/screen/ws`);window.ws=socket;socket.binaryType='arraybuffer';
  socket.onopen=()=>{if(current())socket.send(JSON.stringify({...CONFIG,sdp:pc.localDescription.sdp}));};
  socket.onmessage=async event=>{
   if(!current())return;
   try{
    const message=JSON.parse(event.data);
    if(message.status==='error'){fail(['controller_busy','session_unavailable'].includes(message.code)?'busy':'failed');return;}
    if(message.status!=='ok')return;
    if(message.stage==='webrtc_init'){
     if(!message.sdp?.length)throw Error('Empty answer');
     const sdp=message.sdp.split(/\r\n|\r|\n/).map(s=>s.trim()).filter(Boolean).join('\r\n')+'\r\n';
     await pc.setRemoteDescription({type:'answer',sdp});
    }else if(message.stage==='webrtc_metainfo'){
     window.marbleCapabilities=message.capabilities;
     await updateUIBasedOnCapabilities(message.capabilities);if(!current())return;
     connected();window.marbleRefreshControls?.();lastJitterDelay=0;lastEmittedCount=0;
     window.marbleStatsTimer=setInterval(()=>{if(current())force_sync(pc).catch(()=>{});},1000);
     pc.getReceivers().forEach(r=>{if(r.track.kind==='video'&&r.jitterBufferTarget!==undefined)r.jitterBufferTarget=jitterBufferTargetMs;});
     triggerCheck();
    }
   }catch(error){console.error('Stream negotiation failed',error);fail('failed');}
  };
  socket.onclose=async()=>{
   if(!current())return;
   if(pc.connectionState==='connected'){fail('disconnected');return;}
   try{const response=await fetch('/api/device/list',{credentials:'same-origin'});if(current())fail(response.status===401?'expired':'failed');}
   catch{fail('failed');}
  };
 }catch(error){console.error('Connection setup failed',error);fail('failed');}
}

function sendDataChannelMessage(channel, msg) {
    // console.log("sendDataChannelMessage:", channel, msg);
    if (channel && channel.readyState === 'open') {
        // console.log("Sending message via DataChannel:", channel.label);
        try { channel.send(msg); return true; } catch { return false; }
    }
    return false;
}

let lastJitterDelay = 0;
let lastEmittedCount = 0;

async function force_sync(pc) {
    if (!pc) {
        console.warn("RTCPeerConnection is not defined. Cannot force sync.");
        return;
    };

    // 获取 WebRTC 统计信息
    const stats = await pc.getStats();
    if (pc !== window.currentPeerConnection || pc.connectionState !== "connected") return;

    stats.forEach(report => {
        // 找到视频接收通道的统计
        if (report.type === 'inbound-rtp' && report.kind === 'video') {

            // jitterBufferDelay 是累积的总延迟时间（秒）
            // jitterBufferEmittedCount 是累积的总帧数
            // 我们需要计算“当前”的平均延迟，所以要减去上一次的值取差值

            const deltaDelay = report.jitterBufferDelay - lastJitterDelay;
            const deltaCount = report.jitterBufferEmittedCount - lastEmittedCount;

            // 更新历史值
            lastJitterDelay = report.jitterBufferDelay;
            lastEmittedCount = report.jitterBufferEmittedCount;

            // 计算最近一秒内的平均帧延迟
            let currentDelay = 0;
            if (deltaCount > 0) {
                currentDelay = deltaDelay / deltaCount;
            }

            // let delay_ms = (currentDelay * 5000).toFixed(2);
            // if (delay_ms > 50) {
            //     console.log(`WebRTC Internal Delay: ${delay_ms} ms`);
            // }

            const videoEl = document.getElementById('remoteVideo');
            if (!videoEl) return;

            if (currentDelay > 0.1) {
                if (videoEl.playbackRate !== 1.1) {
                    // console.log("轻微延迟，启用 1.1x 倍速追赶");
                    videoEl.playbackRate = 1.1;
                }
            }
            // 延迟恢复正常后，切回 1.0
            else if (currentDelay <= 0.1 && videoEl.playbackRate !== 1.0) {
                // console.log("延迟恢复正常，切回 1.0x");
                videoEl.playbackRate = 1.0;
            }
        }
    });
};


function loadScript(src) {
    return new Promise((resolve, reject) => {
        // Check if script is already loaded
        if ([...document.scripts].some(s => new URL(s.src, location.href).pathname === src)) {
            resolve();
            return;
        }
        const script = document.createElement('script');
        script.src = src;
        script.onload = resolve;
        script.onerror = error => { script.remove(); reject(error); };
        document.head.appendChild(script);
    });
}

async function updateUIBasedOnCapabilities(caps) {
    if (!caps) return;

    // Helper to show elements
    const show = (selector) => {
        document.querySelectorAll(selector).forEach(el => el.style.display = ''); // Remove inline display:none to revert to CSS
    };

    // Handle Control
    if (caps.can_control) {
        // Load control scripts
        try {
            if (caps.is_linux) {
                await loadScript('/static/capabilities/linux_mouse.js');
                await loadScript('/static/capabilities/keyboard.js');
                await loadScript('/static/capabilities/marble-touch.js');
                // show('.feature-linux-mouse');
            }
            if (caps.is_android) {
                await loadScript('/static/capabilities/keyboard.js');
                await loadScript('/static/capabilities/marble-touch.js');
                await loadScript('/static/capabilities/scroll.js');
                await loadScript('/static/capabilities/buttons.js');
                show('.feature-android-buttons');
                show('.feature-control');
            }

            console.log("Control scripts loaded");
        } catch (e) {
            console.error("Failed to load control scripts", e);
        }

        // Handle Clipboard
        if (caps.can_clipboard) {
            try {
                // Add timestamp to force cache busting
                await loadScript('/static/capabilities/clipboard.js');
                show('.feature-clipboard');
            } catch (e) {
                console.error("Failed to load clipboard script", e);
            }
        }
        // Handle UHID
        if (caps.can_uhid) {
            try {
                await loadScript('/static/capabilities/uhid_mouse.js');
                await loadScript('/static/capabilities/uhid_keyboard.js');
                await loadScript('/static/capabilities/uhid_gamepad.js');
                show('.feature-uhid');
                console.log("UHID scripts loaded");
            } catch (e) {
                console.error("Failed to load UHID scripts", e);
            }
        }

        await loadScript('/static/capabilities/keep_screen_on.js');
        // 受控端黑屏（隐私保护）——与设备能力无关，始终加载
        await loadScript('/static/capabilities/backlight.js');
        window.marbleRefreshControls?.();

    }

}
