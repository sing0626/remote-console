(() => {
 const form=document.querySelector('#pair'),pin=document.querySelector('#pin'),button=document.querySelector('#pair-submit'),message=document.querySelector('#message'),show=document.querySelector('#show-pin');
 let inFlight=false,blockedUntil=0,timer;
 function updateCountdown(){
  const seconds=Math.max(0,Math.ceil((blockedUntil-performance.now())/1000));
  button.disabled=inFlight||seconds>0;
  if(seconds>0){message.textContent=`嘗試次數過多，請等候 ${Math.floor(seconds/60)}:${String(seconds%60).padStart(2,'0')} 後再試。`;return;}
  clearInterval(timer);blockedUntil=0;message.textContent='可以再試，請對照被控電話 App 顯示的「配對 PIN」。';
 }
 function block(seconds){blockedUntil=performance.now()+Math.max(1,Number(seconds)||60)*1000;clearInterval(timer);updateCountdown();timer=setInterval(updateCountdown,1000);}
 show.onclick=()=>{pin.type=pin.type==='password'?'text':'password';show.textContent=pin.type==='password'?'顯示 PIN':'隱藏 PIN';};
 form.onsubmit=async e=>{
  e.preventDefault();if(inFlight||performance.now()<blockedUntil)return;
  const code=pin.value.trim();if(!/^[0-9]{6}$/.test(code)){message.textContent='請輸入 App 顯示的 6 位配對 PIN。';return;}
  inFlight=true;button.disabled=true;
  try{
   const response=await fetch('/api/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pin:code})});
   const data=await response.json();
   if(data.result==='success'){location.replace('/control');return;}
   if(data.retryAfterSeconds||data.leftTries===0){block(data.retryAfterSeconds||600);return;}
   if(data.code==='pin_incorrect'||data.message==='Incorrect PIN'){message.textContent=`PIN 不正確，尚可嘗試 ${data.leftTries} 次。請對照被控電話 App 顯示的「配對 PIN」。`;return;}
   message.textContent='暫時未能配對，請稍後再試。';
  }catch{message.textContent='未能連接電話，請檢查 Wi-Fi 後再試。';}
  finally{inFlight=false;button.disabled=performance.now()<blockedUntil;}
 };
 addEventListener('pagehide',()=>clearInterval(timer));
})();
