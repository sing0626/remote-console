"""Check that the shipped role-specific apps derive their identity from one suite."""
from pathlib import Path
import hashlib
import json
import plistlib
import struct
import subprocess
import xml.etree.ElementTree as ET

root=Path(__file__).resolve().parents[1]
brand=json.loads((root/'branding/brand.json').read_text(encoding='utf-8'))
release=json.loads((root/'release.json').read_text(encoding='utf-8'))
subprocess.run(['python3' if __import__('os').name!='nt' else 'python',str(root/'scripts/sync_release.py'),'--check'],check=True)
icon=(root/'branding/icon.svg').read_bytes()
for name in ['public/static/logo.svg','deploy/remote/gateway/releaseinfo/icon.svg']:
    assert (root/name).read_bytes()==icon,'Diverging brand icon: '+name
for density in ['mdpi','hdpi','xhdpi','xxhdpi','xxxhdpi']:
    first=root/f'app/src/main/res/mipmap-{density}/ic_launcher.png'
    second=root/f'controller/src/main/res/mipmap-{density}/ic_launcher.png'
    assert first.read_bytes()==second.read_bytes(),'Diverging Android icons'
for module,label in [('app','host_label'),('controller','controller_label')]:
    values={e.attrib['name']:e.text for e in ET.parse(root/f'{module}/src/main/res/values/brand.xml').getroot()}
    assert values['suite_name']==brand['name'] and values['app_name']==brand[label]
    gradle=(root/f'{module}/build.gradle.kts').read_text(encoding='utf-8')
    assert 'versionName = rootProject.extra["suiteVersion"] as String' in gradle
    assert 'versionCode = rootProject.extra["suiteBuild"] as Int' in gradle
info=plistlib.loads((root/'clients/ios/Info.plist').read_bytes())
assert info['CFBundleDisplayName']==brand['name']
assert info['CFBundleShortVersionString']==release['version'] and info['CFBundleVersion']==str(release['build'])
png=(root/'clients/ios/Assets.xcassets/AppIcon.appiconset/AppIcon.png').read_bytes()
assert struct.unpack('>II',png[16:24])==(1024,1024) and png[25]==2,'App icon must be opaque RGB 1024px'
print(brand['name']+' '+release['version']+': four-component release and shared icons verified')
