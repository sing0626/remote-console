const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
for(const language of ['zh-HK','zh-TW','zh-CN','en-US']){
 const store={webscreen_lang:'zh'},callbacks=[];
 const c={localStorage:{getItem:k=>store[k],setItem:(k,v)=>store[k]=v},navigator:{language},document:{documentElement:{},addEventListener:(name,fn)=>callbacks.push(fn),querySelectorAll:()=>[]}};
 vm.createContext(c);
 vm.runInContext(fs.readFileSync('public/static/zh-HK.js','utf8'),c);
 vm.runInContext(fs.readFileSync('public/static/i18n.js','utf8'),c);
 assert.equal(vm.runInContext('i18n.lang',c),'zh-HK','legacy simplified Chinese preference must migrate');
 for(const key of ['reconnect','home','menu','uhid_mouse','screen_off','unlock_network_error']){
  const text=vm.runInContext(`i18n.t('${key}')`,c);assert.ok(!/(?:连接|设备|键盘|鼠标|启动|网络|错误)/u.test(text),`${key} is not HK Traditional Chinese: ${text}`);
 }
 const missing=vm.runInContext('Object.keys(translations.en).filter(k => !marbleHongKong[k])',c);
 assert.equal(missing.length,0,'HK locale must cover every English key: '+missing.join(','));
 callbacks.forEach(fn=>fn());assert.equal(c.document.documentElement.lang,'zh-HK');
 vm.runInContext("i18n.setLang('zh')",c);assert.equal(store.marble_ui_language,'zh-HK');
}
console.log('HK localization passed: old preferences, 4 browser locales, full translation-key coverage, document language');
