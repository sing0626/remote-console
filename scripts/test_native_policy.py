"""Run dependency-free native Android policy/protocol checks using the JDK."""
from pathlib import Path
import shutil,subprocess,tempfile
root=Path(__file__).resolve().parents[1]
base='com/sing/halfcloud/controller'
with tempfile.TemporaryDirectory(prefix='halfcloud-policy-') as temp:
    names=['NetworkPolicy','ControlPacket','EndpointPolicy']
    sources=[root/'controller/src/main/java'/base/(name+'.java') for name in names]
    sources += [root/'controller/src/test/java'/base/(name+'Test.java') for name in names]
    subprocess.run([shutil.which('javac') or 'javac','-encoding','UTF-8','-d',temp,*map(str,sources)],check=True)
    for name in names:subprocess.run([shutil.which('java') or 'java','-cp',temp,'com.sing.halfcloud.controller.'+name+'Test'],check=True)
