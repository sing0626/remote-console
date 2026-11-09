"""Derive all platform icons from the same project-owned SVG and opaque master PNG."""
from pathlib import Path
import json
import shutil
import xml.etree.ElementTree as ET
from PIL import Image

root=Path(__file__).resolve().parents[1]
brand=json.loads((root/'branding/brand.json').read_text())
master=root/'branding/icon-1024.png'
image=Image.open(master).convert('RGB')
if image.size!=(1024,1024):raise SystemExit('The brand master must be 1024 x 1024')
def png(path,size):
    path.parent.mkdir(parents=True,exist_ok=True)
    image.resize((size,size),Image.Resampling.LANCZOS).save(path)
def write(path,text):
    path.parent.mkdir(parents=True,exist_ok=True);path.write_text(text,encoding='utf-8')
def js(path,value):write(path,json.dumps(value,indent=2,ensure_ascii=False)+'\n')
for module in ['app','controller']:
    res=root/module/'src/main/res'
    for density,size in [('mdpi',48),('hdpi',72),('xhdpi',96),('xxhdpi',144),('xxxhdpi',192)]:
        png(res/f'mipmap-{density}/ic_launcher.png',size)
    js(root/'branding/platform-icons.json',{'source':'branding/icon.svg','master':'branding/icon-1024.png','platforms':['ios-control','android-control','android-server','vps-server']})
    adaptive='<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android"><background android:drawable="@color/ic_launcher_background"/><foreground android:drawable="@drawable/ic_halfcloud_foreground"/><monochrome android:drawable="@drawable/ic_halfcloud_foreground"/></adaptive-icon>\n'
    write(res/'mipmap-anydpi-v26/ic_launcher.xml',adaptive)
    label=brand['host_label'] if module=='app' else brand['controller_label']
    write(res/'values/brand.xml','<resources><color name="ic_launcher_background">'+brand['background']+'</color><string name="suite_name">'+brand['name']+'</string><string name="app_name">'+label+'</string></resources>\n')
    # Adaptive mask owns the corner shape. The foreground has a generous safe area.
    foreground='''<vector xmlns:android="http://schemas.android.com/apk/res/android" android:width="108dp" android:height="108dp" android:viewportWidth="1024" android:viewportHeight="1024">
<path android:pathData="M426,602H308C242,602 196,564 196,507C196,461 223,425 267,412C267,338 323,286 390,286C437,286 474,309 498,347" android:fillColor="#00000000" android:strokeColor="#BFF9E4" android:strokeWidth="49" android:strokeLineCap="round" android:strokeLineJoin="round"/>
<path android:pathData="M545,252H685Q758,252 758,325V693Q758,766 685,766H545Q472,766 472,693V325Q472,252 545,252Z" android:fillColor="#129AE8CE" android:strokeColor="#BFF9E4" android:strokeWidth="49"/>
<path android:pathData="M583,302H648M591,711H640" android:fillColor="#00000000" android:strokeColor="#BFF9E4" android:strokeWidth="24" android:strokeLineCap="round"/>
<path android:pathData="M535,501L579,545L683,441" android:fillColor="#00000000" android:strokeColor="#9AE8CE" android:strokeWidth="34" android:strokeLineCap="round" android:strokeLineJoin="round"/>
</vector>'''
    write(res/'drawable/ic_halfcloud_foreground.xml',foreground)
    if module=='app':
        notification=foreground.replace('108dp','24dp').replace('#BFF9E4','#FFFFFFFF').replace('#9AE8CE','#FFFFFFFF').replace('#129AE8CE','#00000000')
        write(res/'drawable/ic_stat_webscreen.xml',notification)
    png(res/'drawable-nodpi/brand_mark.png',256)
assets=root/'clients/ios/Assets.xcassets'
js(assets/'Contents.json',{'info':{'author':'xcode','version':1}})
png(assets/'AppIcon.appiconset/AppIcon.png',1024)
js(assets/'AppIcon.appiconset/Contents.json',{'images':[{'filename':'AppIcon.png','idiom':'universal','platform':'ios','size':'1024x1024'}],'info':{'author':'xcode','version':1}})
png(assets/'BrandMark.imageset/brand-mark.png',256)
js(assets/'BrandMark.imageset/Contents.json',{'images':[{'filename':'brand-mark.png','idiom':'universal'}],'info':{'author':'xcode','version':1}})
shutil.copy2(root/'branding/icon.svg',root/'public/static/logo.svg')
shutil.copy2(root/'branding/icon.svg',root/'deploy/remote/gateway/releaseinfo/icon.svg')
png(root/'public/static/logo.png',192)
print('Generated matching 半雲電話 icons for all four components')
