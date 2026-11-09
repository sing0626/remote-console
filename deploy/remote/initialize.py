"""Initialize a new independent deployment from an explicitly supplied phone PUBLIC key."""
import argparse,json,os,subprocess,sys
from pathlib import Path
from register_device import public_key,atomic_write

def initialize(domain,ip,keyfile,output,compose):
 output=Path(output).resolve();compose=Path(compose).resolve()
 if output.exists() and any(output.iterdir()):raise ValueError('Existing deployment was preserved; choose an empty directory')
 if compose.exists():raise ValueError('Existing generated Compose file was preserved')
 key,fingerprint=public_key(keyfile)
 subprocess.run([sys.executable,str(Path(__file__).with_name('prepare.py')),'--domain',domain,'--public-ip',ip,'--output',str(output)],check=True,capture_output=True)
 atomic_write(output/'enrollment.pub',key)
 atomic_write(output/'devices.json',json.dumps({'version':1,'default_device':'primary','devices':[{'id':'primary','name':'Android phone A','upstream':'http://127.0.0.1:18081','key':'/private/enrollment.pub','private':'/private'}]},indent=2)+'\n')
 atomic_write(compose,json.dumps({'x-remoteconsole-generated':1,'services':{}},indent=2)+'\n')
 return {'initialized':True,'primary_public_key_fingerprint':fingerprint,'configuration':str(output),'entry':'https://'+domain+'/control'}
if __name__=='__main__':
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--domain',required=True);p.add_argument('--public-ip',required=True);p.add_argument('--owner-key',required=True,type=Path);p.add_argument('--output',default=Path('private'),type=Path);p.add_argument('--compose',default=Path('compose.devices.generated.json'),type=Path);a=p.parse_args()
 try:print(json.dumps(initialize(a.domain,a.public_ip,a.owner_key,a.output,a.compose),indent=2))
 except (ValueError,OSError,subprocess.CalledProcessError) as e:raise SystemExit(str(e))
