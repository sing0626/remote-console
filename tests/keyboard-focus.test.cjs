const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
const handlers={},packets=[];let modal=false;
const c={window:{},document:{querySelector:()=>modal?{}:null,addEventListener:(name,fn)=>handlers[name]=fn},
 ArrayBuffer,DataView,sendDataChannelMessage:(_,p)=>packets.push(new DataView(p).getUint8(1))};
vm.createContext(c);vm.runInContext(fs.readFileSync('public/static/capabilities/keyboard.js','utf8'),c);
const e=local=>({code:'Enter',target:{tagName:local?'TEXTAREA':'BODY',closest:()=>local?{}:null},preventDefault(){}});
handlers.keydown(e(true));handlers.keyup(e(true));assert.equal(packets.length,0,'typing in the local editor must not send phantom remote keys');
modal=true;handlers.keydown(e(false));handlers.keyup(e(false));assert.equal(packets.length,0,'dialog shortcuts must stay local');
modal=false;handlers.keydown(e(false));handlers.keyup(e(false));assert.deepEqual(packets,[0,1]);
console.log('Keyboard focus passed: local text, modal isolation, normal remote press/release');
