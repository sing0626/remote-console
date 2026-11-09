const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
async function main(){
 const elements={pair:{},pin:{value:'246810',type:'password'},'pair-submit':{disabled:false},message:{textContent:''},'show-pin':{}};
 let now=1000,tick,calls=0,redirect,release;
 const replies=[{result:'failed',code:'pin_locked',leftTries:0,retryAfterSeconds:5},{result:'success'}];
 const c={document:{querySelector:s=>elements[s.slice(1)]},performance:{now:()=>now},setInterval:fn=>{tick=fn;return 1;},clearInterval:()=>{},addEventListener:()=>{},location:{replace:p=>redirect=p},fetch:async()=>{calls++;return{json:async()=>replies.shift()};}};
 vm.createContext(c);vm.runInContext(fs.readFileSync('public/static/marble-pairing.js','utf8'),c);
 const submit=()=>elements.pair.onsubmit({preventDefault(){}});
 await submit();assert.equal(elements['pair-submit'].disabled,true);assert.match(elements.message.textContent,/0:05/);
 await submit();assert.equal(calls,1,'cooldown must not send duplicate requests');
 now+=5001;tick();assert.equal(elements['pair-submit'].disabled,false,'cooldown must expire without more requests');
 await submit();assert.equal(redirect,'/control');
 elements['show-pin'].onclick();assert.equal(elements.pin.type,'text');elements['show-pin'].onclick();assert.equal(elements.pin.type,'password');
 elements.pin.value='12';await submit();assert.equal(calls,2,'invalid local format should not consume attempts');
 elements.pin.value='246810';c.fetch=()=>{calls++;return new Promise(resolve=>{release=()=>resolve({json:async()=>({result:'failed',code:'pin_incorrect',leftTries:4})});});};
 const pending=submit();await submit();assert.equal(calls,3,'double submit must send only one request');release();await pending;
 assert.match(elements.message.textContent,/4 次/);
 console.log('Pairing UI passed: countdown, cooldown, reveal PIN, format validation, double-submit protection');
}
main().catch(e=>{console.error(e);process.exitCode=1;});
