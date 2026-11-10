"""Exercise native Android tools against the synthetic loopback WebRTC fixture."""
import json
import re
import subprocess
import time
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path

OUT = Path('ui-evidence')
OUT.mkdir(exist_ok=True)

def adb(*args):
    return subprocess.check_output(['adb', *map(str, args)], stderr=subprocess.STDOUT)

def tree():
    adb('shell', 'uiautomator', 'dump', '/sdcard/halfcloud-test-ui.xml')
    raw = adb('shell', 'cat', '/sdcard/halfcloud-test-ui.xml')
    (OUT/'last-ui.xml').write_bytes(raw)
    return list(ET.fromstring(raw).iter('node'))

def find(text, contains=False, timeout=20):
    until = time.monotonic()+timeout
    while time.monotonic() < until:
        for node in tree():
            labels = [node.get('text', ''), node.get('content-desc', '')]
            if any(text in value if contains else text == value for value in labels):
                return node
        time.sleep(.4)
    raise AssertionError('Missing UI: '+text)

def tap_node(node):
    b = list(map(int, re.findall(r'\d+', node.attrib['bounds'])))
    adb('shell', 'input', 'tap', (b[0]+b[2])//2, (b[1]+b[3])//2)

def tap(text, contains=False):
    tap_node(find(text, contains))

def shot(name):
    (OUT/(name+'.png')).write_bytes(adb('exec-out', 'screencap', '-p'))

def connected():
    return find('已連接 · 可操作', timeout=70)

adb('install', '-r', 'controller/build/outputs/apk/debug/controller-debug.apk')
adb('shell', 'am', 'start', '-n', 'com.sing.halfcloud.controller/.ControllerActivity')
tap('電話網址')
adb('shell', 'input', 'text', 'http://10.0.2.2:18741/control')
adb('shell', 'input', 'keyevent', '4')
tap('連接電話')
connected()
shot('connected')
tap('更多')
tap('電話檔案')
find('Download', contains=True)
shot('files')
adb('shell', 'input', 'keyevent', '4')
tap('更多')
tap('Root 終端')
tap_node(next(n for n in tree() if n.get('class') == 'android.widget.EditText'))
adb('shell', 'input', 'text', 'printf%shalfcloud-test')
adb('shell', 'input', 'keyevent', '4')
tap('執行')
find('[synthetic fixture]', contains=True)
shot('terminal')
adb('shell', 'input', 'keyevent', '4')
tap('更多')
tap('電話與中繼')
find('Synthetic fixture', contains=True)
find('香港測試中繼', contains=True)
shot('dashboard')
adb('shell', 'input', 'keyevent', '4')
request = urllib.request.Request('http://127.0.0.1:18741/fixture/drop', b'', method='POST')
with urllib.request.urlopen(request, timeout=10) as response:
    assert response.status == 200
connected()
shot('reconnected')
with urllib.request.urlopen('http://127.0.0.1:18741/metrics') as response:
    metrics = json.load(response)
assert metrics['toolReads'] >= 1 and metrics['terminalJobs'] >= 1, metrics
assert metrics['credentialSessions'] > metrics['credentialSessionsAtDrop'], metrics
assert metrics['approvalRequests'] == metrics['approvalRequestsAtDrop'], metrics
(OUT/'metrics.json').write_text(json.dumps(metrics, indent=2))
print('Native Android video/control, tools and automatic trusted reconnect passed')
