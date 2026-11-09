"""Build the public FRP client from its pinned release into ignored Android build inputs."""
import argparse,os,subprocess
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--go',default='go');a=p.parse_args();root=Path(__file__).resolve().parents[1];source=root/'vendor-build/frp';version='v0.72.0'
if not source.exists():subprocess.run(['git','clone','--depth','1','--branch',version,'https://github.com/fatedier/frp.git',str(source)],check=True)
actual=subprocess.check_output(['git','describe','--tags','--exact-match'],cwd=source,text=True).strip()
if actual!=version:raise SystemExit('Unexpected FRP source revision; existing checkout preserved')
env=os.environ.copy();env.update(GOOS='android',GOARCH='arm64',CGO_ENABLED='0');out=root/'app/src/main/jniLibs/arm64-v8a/libfrpc.so';out.parent.mkdir(parents=True,exist_ok=True)
subprocess.run([a.go,'build','-trimpath','-ldflags','-s -w -checklinkname=0','-tags','frpc,noweb','-o',str(out),'./cmd/frpc'],cwd=source,env=env,check=True)
