const assert=require('node:assert/strict'),prefs=require('../public/static/stream-preferences.js');
for(const rate of [30,60,120])assert.equal(prefs.driver({fps:rate}).max_fps,String(rate));
assert.deepEqual(prefs.normalize({fps:999,size:'1920',mbps:999}),{fps:30,size:1920,mbps:4});
assert.equal(prefs.driver({fps:'120',mbps:12}).video_bit_rate,'12000000');
assert.equal(prefs.driver({fps:60}).video_codec,'h264');
console.log('Stream preferences passed: 30/60/120, numeric storage values, invalid values, bitrate conversion');
