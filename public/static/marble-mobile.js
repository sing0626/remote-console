(() => {
 const $=s=>document.querySelector(s), video=$('#remoteVideo'), bar=$('#marble-status');
 if($('#remote-device-name'))$('#remote-device-name').textContent=sessionStorage.getItem('remote_device_name')||'Android 裝置';
 const requestedFps=Number(CONFIG.driver_config.max_fps)||30;let lastFrames=0;
 const states={
  idle:['準備連接','正在連接電話','畫面準備好就會顯示。'],
  connecting:['連接中','正在連接電話','正在建立畫面同控制通道。'],
  connected:['已連接','',''],
  recovering:['恢復連線中','網絡暫時中斷','正嘗試恢復原有連線，請稍等。'],
  busy:['暫時無法加入','暫時未能加入共用控制','最多 4 部裝置同時連入，並使用相同畫質。請稍後再試。'],
  failed:['未能連接','暫時連唔到電話','確認電話同控制裝置連上同一個 Wi-Fi，再試一次。'],
  disconnected:['已斷線','連線已中斷','你可以重新連接，繼續操作電話。'],
  expired:['需要配對','配對已失效','請用電話 App 顯示嘅 PIN 重新配對。']
 };
 let viewerCount=1;
 let state='idle', lastPeer=null, lastBytes=0,lastTime=0;
 window.marbleAudioEnabled=false;
 window.marbleRefreshControls=()=>{
  const ready=state==='connected' && window.dataChannelOrdered?.readyState==='open' && window.marbleCapabilities?.can_control;
  document.querySelectorAll('[data-remote]').forEach(el=>el.disabled=!ready);
  $('#marble-audio').disabled=state!=='connected';
 };
 window.marbleSetConnectionState=next=>{
  if(!states[next])next='failed';state=next;
  document.body.dataset.connection=state;bar.dataset.state=state;
  $('#connection-label').textContent=states[state][0];
  $('#connection-title').textContent=states[state][1];$('#connection-detail').textContent=states[state][2];
  $('#connection-overlay').hidden=state==='connected';
  $('#marble-retry').hidden=['idle','connecting','recovering','expired','connected'].includes(state);
  $('#pair-again').hidden=state!=='expired';
  $('#tools-reconnect').disabled=state==='connecting';
  if(state!=='connected'){$('#stream-info').textContent='連線後顯示串流資訊';bar.dataset.stats=JSON.stringify({connected:false,state});$('#marble-play').hidden=true;}
  window.marbleRefreshControls();
 };
 window.marbleShowPlayButton=()=>{$('#marble-play').hidden=false;};
 $('#marble-play').onclick=async()=>{try{await video.play();$('#marble-play').hidden=true;}catch{showToast('未能播放畫面，請重新連接。');}};
 video.addEventListener('playing',()=>{$('#marble-play').hidden=true;});
 for(const id of ['marble-retry','tools-reconnect'])$('#'+id).onclick=()=>{closeSheets();start();};
 for(const id of ['marble-fullscreen','tools-fullscreen'])$('#'+id).onclick=()=>{closeSheets();fullscreenPlayer();};
 const tools=$('#marble-tools-dialog'),textDialog=$('#marble-text-dialog');
 function closeSheets(){for(const dialog of [tools,textDialog])if(dialog.open)dialog.close();}
 $('#marble-more').onclick=()=>{window.marbleReleaseTouches?.();tools.showModal();};
 $('#marble-text').onclick=()=>{window.marbleReleaseTouches?.();$('#marble-input-status').textContent='';textDialog.showModal();$('#marble-input').focus();};
 document.querySelectorAll('[data-close]').forEach(el=>el.onclick=()=>document.getElementById(el.dataset.close).close());
 for(const dialog of [tools,textDialog])dialog.addEventListener('click',e=>{if(e.target!==dialog)return;const r=dialog.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)dialog.close();});
 $('#marble-text-form').onsubmit=e=>{
  e.preventDefault();const text=$('#marble-input').value,data=new TextEncoder().encode(text);
  if(!data.length){$('#marble-input-status').textContent='請先輸入文字。';return;}
  if(data.length>16000){$('#marble-input-status').textContent='文字過長，請分段傳送。';return;}
  if(state!=='connected'||window.dataChannelOrdered?.readyState!=='open'){$('#marble-input-status').textContent='連線已中斷，請重新連接後再傳送。';return;}
  const b=new Uint8Array(14+data.length),v=new DataView(b.buffer);b[0]=9;b[9]=1;v.setUint32(10,data.length);b.set(data,14);
  if(!sendDataChannelMessage(window.dataChannelOrdered,b)){$('#marble-input-status').textContent='未能傳送，請保留文字再試。';return;}
  $('#marble-input').value='';textDialog.close();showToast('已傳送文字',1600);
 };
 $('#marble-audio').onclick=async()=>{
  const enable=!window.marbleAudioEnabled;let failed=false;
  for(const audio of document.querySelectorAll('audio[data-marble-audio]')){audio.muted=!enable;if(enable)try{await audio.play();}catch{failed=true;}}
  window.marbleAudioEnabled=enable&&!failed;
  const button=$('#marble-audio');button.setAttribute('aria-pressed',String(window.marbleAudioEnabled));
  const label=window.marbleAudioEnabled?'靜音':'開聲';button.querySelector('span').textContent=label;button.title=label;button.setAttribute('aria-label',label);
  if(failed){for(const audio of document.querySelectorAll('audio[data-marble-audio]'))audio.muted=true;showToast('聲音未能播放，請再按一次開聲。',2500);}
 };
 function viewport(){
  const v=window.visualViewport;document.documentElement.style.setProperty('--app-height',(v?.height||window.innerHeight)+'px');
  document.documentElement.style.setProperty('--keyboard-inset',Math.max(0,window.innerHeight-(v?.height||window.innerHeight)-(v?.offsetTop||0))+'px');
  requestAnimationFrame(()=>{if(typeof updateVideoCache==='function')updateVideoCache();});
 }
 window.visualViewport?.addEventListener('resize',viewport);window.visualViewport?.addEventListener('scroll',viewport);addEventListener('resize',viewport);viewport();
 setInterval(async()=>{
  const pc=window.currentPeerConnection;if(state!=='connected'||!pc||pc.connectionState!=='connected')return;
  if(lastPeer!==pc){lastPeer=pc;lastBytes=0;lastTime=0;lastFrames=0;}
  try{
   const reports=await pc.getStats();if(pc!==window.currentPeerConnection||state!=='connected')return;
   let pair,media,audio;
   for(const r of reports.values()){
    if(r.type==='transport'&&r.selectedCandidatePairId)pair=reports.get(r.selectedCandidatePairId);
    if(r.type==='inbound-rtp'&&r.kind==='video')media=r;
    if(r.type==='inbound-rtp'&&r.kind==='audio')audio=r;
   }
   if(!pair)for(const r of reports.values())if(r.type==='candidate-pair'&&r.state==='succeeded'&&r.nominated)pair=r;
   const local=pair&&reports.get(pair.localCandidateId),remote=pair&&reports.get(pair.remoteCandidateId);
   const relay=local?.candidateType==='relay'||remote?.candidateType==='relay';
   $('#connection-label').textContent=(pair?(relay?'中繼連線':'P2P 直連'):'已連接')+(pair?.currentRoundTripTime!=null?` · ${Math.round(pair.currentRoundTripTime*1000)} ms`:'');
   let speed='',fps=0;if(media){if(lastTime&&media.timestamp>lastTime)fps=Math.max(0,(media.framesDecoded-lastFrames)*1000/(media.timestamp-lastTime));lastFrames=media.framesDecoded;if(lastTime&&media.timestamp>lastTime)speed=` · ${((media.bytesReceived-lastBytes)*8/(media.timestamp-lastTime)/1000).toFixed(1)} Mbps`;lastTime=media.timestamp;lastBytes=media.bytesReceived;}
   $('#stream-info').textContent=video.videoWidth?`${video.videoWidth} × ${video.videoHeight} · ${Math.round(fps)} / ${requestedFps} FPS${speed} · ${viewerCount} 部共用控制`:'';
   bar.dataset.stats=JSON.stringify({connected:true,relay:pair?relay:null,localType:local?.candidateType,remoteType:remote?.candidateType,framesDecoded:media?.framesDecoded,actualFps:fps,requestedFps,bytesReceived:media?.bytesReceived,rtt:pair?.currentRoundTripTime,audioBytesReceived:audio?.bytesReceived,audioEnergy:audio?.totalAudioEnergy});
  }catch{}
 },1500);
 let sessionPolling=false;
 setInterval(async()=>{
  if(state!=='connected'||sessionPolling)return;
  sessionPolling=true;
  try{const response=await fetch('/api/screen/sessions',{credentials:'same-origin'});if(response.ok){const status=await response.json();viewerCount=Math.max(1,status.connected);window.marbleSyncBacklight?.(status.backlight_off);bar.dataset.sessions=JSON.stringify(status);}}catch{}finally{sessionPolling=false;}
 },2000);
 addEventListener('pagehide' ,()=>{window.marbleReleaseTouches?.();window.marbleStopConnection?.();});
 addEventListener('pageshow',e=>{if(e.persisted)window.marbleSetConnectionState('disconnected');});
 window.marbleSetConnectionState('idle');
})();
