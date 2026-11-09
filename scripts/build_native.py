"""Build generic Android receiver binaries; no device or deployment is contacted."""
import argparse,os,subprocess
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--go',default='go');a=p.parse_args();root=Path(__file__).resolve().parents[1]
def build(target,arch,package,output,android=False):
 env=os.environ.copy();env.update(GOOS=target,GOARCH=arch,CGO_ENABLED='0');output.parent.mkdir(parents=True,exist_ok=True)
 flags='-s -w'+(' -checklinkname=0' if android else '')
 subprocess.run([a.go,'build','-trimpath','-ldflags',flags,'-o',str(output),package],cwd=root,env=env,check=True)
build('linux','amd64','./linuxRecorder',root/'sdriver/linux/bin/recorder')
build('android','arm64','.',root/'app/src/main/jniLibs/arm64-v8a/libwebscreen.so',True)
