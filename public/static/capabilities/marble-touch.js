(() => {
 const video=document.querySelector('#remoteVideo');
 const pointers=new Map();
 function position(e){
  if(!updateVideoCache())return null;
  const r=window.cachedRect.VideoRect,c=window.cachedRect.ContentRect;
  return {x:Math.max(0,Math.min(video.videoWidth-1,Math.round((e.clientX-r.left-c.left)/c.width*video.videoWidth))),y:Math.max(0,Math.min(video.videoHeight-1,Math.round((e.clientY-r.top-c.top)/c.height*video.videoHeight)))};
 }
 function send(action,p){
  const b=new ArrayBuffer(10),v=new DataView(b);
  v.setUint8(0,2);v.setUint8(1,action);v.setUint8(2,p.id);v.setUint16(3,p.x);v.setUint16(5,p.y);v.setUint16(7,action===1?0:p.pressure);v.setUint8(9,action===1?0:1);
  sendDataChannelMessage(window.dataChannelOrdered,b);
 }
 function releaseAll(){for(const p of pointers.values())send(1,p);pointers.clear();}
 video.addEventListener('pointerdown',e=>{
  if(e.pointerType==='mouse'&&(e.button!==0||window.isUHIDMouseEnabled))return;
  if(pointers.size>=10)return;
  e.preventDefault();const xy=position(e);if(!xy)return;
  const used=new Set([...pointers.values()].map(p=>p.id));let id=0;while(used.has(id))id++;
  const p={...xy,id,pressure:Math.round((e.pressure>0?e.pressure:1)*65535)};
  pointers.set(e.pointerId,p);video.setPointerCapture(e.pointerId);send(0,p);
 },{passive:false});
 video.addEventListener('pointermove',e=>{
  const p=pointers.get(e.pointerId);if(!p)return;e.preventDefault();
  const xy=position(e);if(xy){Object.assign(p,xy);p.pressure=Math.round((e.pressure>0?e.pressure:1)*65535);send(2,p);}
 },{passive:false});
 function up(e){const p=pointers.get(e.pointerId);if(!p)return;e.preventDefault();const xy=position(e);if(xy)Object.assign(p,xy);send(1,p);pointers.delete(e.pointerId);}
 for(const type of ['pointerup','pointercancel','lostpointercapture'])video.addEventListener(type,up,{passive:false});
 video.addEventListener('contextmenu',e=>e.preventDefault());
 addEventListener('blur',releaseAll);addEventListener('pagehide',releaseAll);
 document.addEventListener('visibilitychange',()=>{if(document.hidden)releaseAll();});
 window.marbleReleaseTouches=releaseAll;
})();
