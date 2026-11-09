((root,factory)=>{const api=factory();if(typeof module==='object')module.exports=api;else root.RemoteStreamPreferences=api;})(typeof window==='object'?window:this,()=>{
 const choices={fps:[30,60,120],size:[960,1280,1920],mbps:[2,4,8,12,20]};
 function normalize(value={}){return Object.fromEntries(Object.entries(choices).map(([k,values])=>[k,values.includes(Number(value[k]))?Number(value[k]):({fps:30,size:1280,mbps:4}[k])]));}
 function driver(value){const p=normalize(value);return {video_codec:'h264',audio_codec:'opus',video_bit_rate:String(p.mbps*1000000),max_size:String(p.size),max_fps:String(p.fps),audio:'true',control:'true',video_codec_options:'profile=1'};}
 return {normalize,driver};
});
