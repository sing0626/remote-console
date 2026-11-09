"""Prepare private config on the selected Linux VPS. Does not start services.

Requires openssl, a public IPv4 and DNS name. Public HTTPS and web authentication
are provided by the existing 1Panel/OpenResty installation.
"""
import argparse,ipaddress,json,os,re,secrets,subprocess
from pathlib import Path

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--domain',required=True);p.add_argument('--public-ip',required=True)
p.add_argument('--output',type=Path,default=Path(__file__).resolve().parent/'private')
a=p.parse_args()
if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?',a.domain) or '.' not in a.domain:raise SystemExit('Use an ASCII DNS name')
if not ipaddress.ip_address(a.public_ip).is_global or ':' in a.public_ip:raise SystemExit('Use a public IPv4 for this deployment template')
out=a.output.resolve()
if out.exists() and any(out.iterdir()):raise SystemExit('Output already contains files; preserve credentials and choose another destination')
os.umask(0o077);out.mkdir(parents=True,exist_ok=True);(out/'pki').mkdir();(out/'controller-trust').mkdir(mode=0o700)
token=secrets.token_urlsafe(40);turn=secrets.token_urlsafe(40)
def write(name,value):
 f=out/name;f.write_text(value,encoding='utf-8');f.chmod(0o600)
write('frps.toml',f'''bindPort = 7000
proxyBindAddr = "127.0.0.1"
auth.method = "token"
auth.token = "{token}"
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
allowPorts = [{{ single = 18081 }}]
transport.tls.force = true
transport.tls.certFile = "/pki/server.crt"
transport.tls.keyFile = "/pki/server.key"
''')
write('turnserver.conf',f'''listening-port=3478
external-ip={a.public_ip}
realm={a.domain}
server-name={a.domain}
fingerprint
use-auth-secret
static-auth-secret={turn}
min-port=49160
max-port=49260
user-quota=8
total-quota=32
no-cli
no-multicast-peers
denied-peer-ip=0.0.0.0-0.255.255.255
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=127.0.0.0-127.255.255.255
denied-peer-ip=169.254.0.0-169.254.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
denied-peer-ip=224.0.0.0-255.255.255.255
''')
write('ice.private.json',json.dumps({'stun_urls':[f'stun:{a.public_ip}:3478'],'turn_urls':[f'turn:{a.public_ip}:3478?transport=udp',f'turn:{a.public_ip}:3478?transport=tcp'],'turn_shared_secret':turn},indent=2)+'\n')
write('phone-frpc.toml',f'''serverAddr = "{a.public_ip}"
serverPort = 7000
auth.method = "token"
auth.token = "{token}"
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
transport.tls.enable = true
transport.tls.serverName = "{a.domain}"
transport.tls.trustedCaFile = "/data/local/tmp/remote-console/remote/ca.crt"
[[proxies]]
name = "remote-console-signaling"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8079
remotePort = 18081
''')
def run(args):subprocess.run(args,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
pki=out/'pki'
run(['openssl','req','-x509','-newkey','rsa:3072','-nodes','-days','3650','-subj','/CN=Remote Console Tunnel CA','-keyout',str(pki/'ca.key'),'-out',str(pki/'ca.crt')])
run(['openssl','req','-new','-newkey','rsa:3072','-nodes','-subj','/CN='+a.domain,'-keyout',str(pki/'server.key'),'-out',str(pki/'server.csr')])
write('pki/server.ext',f'subjectAltName=DNS:{a.domain}\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n')
run(['openssl','x509','-req','-in',str(pki/'server.csr'),'-CA',str(pki/'ca.crt'),'-CAkey',str(pki/'ca.key'),'-CAcreateserial','-days','825','-extfile',str(pki/'server.ext'),'-out',str(pki/'server.crt')])
print('Private configuration created. Keep pki/ca.key on the server. Transfer only the public CA certificate and the two phone config files to your phone.')
