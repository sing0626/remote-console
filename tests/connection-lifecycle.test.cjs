const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
const peers=[],sockets=[],states=[],timers=new Map();let nextTimer=1;
class Peer{
 constructor(){peers.push(this);this.connectionState='new';this.iceGatheringState='gathering';}
 createOffer(){return Promise.resolve({type:'offer',sdp:'test-offer'});}
 setLocalDescription(offer){this.localDescription=offer;return Promise.resolve();}
 createDataChannel(){return {readyState:'connecting',send(){}};}
 addTransceiver(){}
 close(){this.connectionState='closed';this.onconnectionstatechange?.();}
}
class Socket{
 constructor(){sockets.push(this);this.readyState=1;}
 close(){this.readyState=3;this.onclose?.();}
 send(data){this.sent=JSON.parse(data);}
}
const c={console:{log(){},warn(){},error(){}},URLSearchParams,URL,TextDecoder,Uint8Array,
 location:{protocol:'http:',host:'phone',href:'http://phone/screen/marble'},
 sessionStorage:{getItem:()=>JSON.stringify({device_type:'android',driver_config:{}})},
 document:{querySelectorAll:()=>[]},RTCPeerConnection:Peer,WebSocket:Socket,
 window:{location:{search:''},marbleSetConnectionState:s=>states.push(s)},
 setTimeout:(fn,ms)=>{const id=nextTimer++;timers.set(id,{fn,ms});return id;},
 clearTimeout:id=>timers.delete(id),clearInterval(){},setInterval(){},fetch:async()=>({status:200,ok:true,json:async()=>({iceServers:[{urls:'stun:test.invalid'}]})})};
vm.createContext(c);vm.runInContext(fs.readFileSync('public/static/connect.js','utf8'),c);
const flush=async()=>{for(let i=0;i<12;i++)await Promise.resolve();};
(async()=>{
 const first=c.start();await flush();const old=peers[0];
 const second=c.start();await flush();assert.equal(old.connectionState,'closed');
 peers[1].onicecandidate({candidate:null});await second;await first;
 assert.equal(sockets.length,1,'cancelled ICE gathering must never open a stale socket');
 await sockets[0].onmessage({data:JSON.stringify({status:'error',code:'controller_busy'})});
 assert.equal(states.at(-1),'busy');old.onconnectionstatechange();
 assert.equal(states.at(-1),'busy','old callbacks must not overwrite a newer session status');
 const third=c.start();await flush();peers[2].onicecandidate({candidate:null});await third;
 sockets[1].onopen();assert.equal(sockets[1].sent.sdp,'test-offer');
 c.window.marbleCapabilities={can_control:true};peers[2].connectionState='connected';peers[2].onconnectionstatechange();assert.equal(states.at(-1),'connected');
 peers[2].connectionState='disconnected';peers[2].onconnectionstatechange();assert.equal(states.at(-1),'recovering');
 assert.notEqual(peers[2].connectionState,'closed','brief interruption must retain the peer');
 peers[2].connectionState='connected';peers[2].onconnectionstatechange();assert.equal(states.at(-1),'connected');
 assert.ok(![...timers.values()].some(t=>t.ms===10000),'recovery timer must clear after reconnection');
 await sockets[1].onclose();assert.equal(states.at(-1),'disconnected');assert.equal(peers[2].connectionState,'closed');
 const pending=c.start();await flush();
 const deadline=[...timers.values()].find(t=>t.ms===25000);assert.ok(deadline);deadline.fn();await pending;
 assert.equal(states.at(-1),'failed');assert.equal(peers[3].connectionState,'closed');
 assert.equal(sockets.length,2,'timed-out gathering must not create a socket');
 console.log('Connection lifecycle passed: pending reconnect cancellation, busy denial, stale callback isolation, signaling loss, bounded startup');
})().catch(e=>{console.error(e);process.exitCode=1;});
