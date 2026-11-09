"""Reject runtime identities, personal artifacts and signing material from publication."""
from pathlib import Path
import re,subprocess,sys
root=Path(__file__).resolve().parents[1]
r=subprocess.run(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=root,capture_output=True)
if r.returncode:raise SystemExit('Run this check in a Git checkout')
files={Path(x.decode()) for x in r.stdout.split(b'\0') if x}
forbidden={'.key','.pem','.pub','.crt','.csr','.p12','.pfx','.jks','.keystore','.apk','.ipa','.mp4','.log','.ips'}
errors=[]
for rel in sorted(files):
 p=root/rel
 if not p.is_file():continue
 if 'private' in rel.parts or 'downloads' in rel.parts or rel.suffix.lower() in forbidden or '.private.' in rel.name or '.generated.' in rel.name:errors.append(str(rel)+': runtime or private artifact');continue
 data=p.read_bytes()
 if re.search(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',data):errors.append(str(rel)+': private key')
 if re.search(rb'[A-Za-z]:[\\/]+Users[\\/]+[A-Za-z]',data):errors.append(str(rel)+': personal home directory')
if errors:raise SystemExit('\n'.join(errors))
print('Publication tree passed: '+str(len(files))+' files; runtime identities and personal artifacts excluded')
