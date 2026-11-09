"""Disposable Ubuntu CI: native service, authenticated relay data, durable traffic."""
import argparse
import base64
import hashlib
import hmac
import ipaddress
import json
import os
from pathlib import Path
import secrets
import socket
import struct
import subprocess
import sys
import time
import relay

if os.geteuid()!=0:raise SystemExit('Run only as root on a disposable Ubuntu runner')
fixture_ip='93.184.216.34'
peer_ip='93.184.216.35'
interface,local=relay.interface_info()
subprocess.run(['ip','address','add',fixture_ip+'/32','dev',interface],check=True)
subprocess.run(['ip','address','add',peer_ip+'/32','dev',interface],check=True)
try:
    args=argparse.Namespace(public_ip=fixture_ip,interface=interface,port=3478)
    environment=os.environ.copy()
    environment['HALFCLOUD_RELAY_REF']=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
    subprocess.run(['bash',str(Path(__file__).resolve().parents[1]/'relay-install.sh'),'install',
                    '--public-ip',fixture_ip,'--interface',interface],env=environment,check=True)
    profile=relay.config()
    with (relay.ETC/'turnserver.conf').open('a') as output:output.write('\nverbose\n')
    subprocess.run(['systemctl','restart',relay.SERVICE],check=True)
    time.sleep(2)
    relay.probe(profile)
    secret_before=(relay.ETC/'config.json').read_bytes()
    # A TURN allocation and Send Indication through the real unprivileged coturn.
    with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as client, socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as peer:
        client.settimeout(5);peer.settimeout(5)
        client.connect((local,3478));peer.bind((peer_ip,0))
        tx=secrets.token_bytes(12);requested=[(0x19,b'\x11\0\0\0')]
        client.send(relay._packet(3,requested,tx))
        kind,attrs,_=relay._attributes(client.recv(65536),tx)
        assert kind==0x113
        username=str(int(time.time())+600)+':data-check'
        password=base64.b64encode(hmac.digest(profile['secret'].encode(),username.encode(),'sha1'))
        key=hashlib.md5(username.encode()+b':'+attrs[0x14]+b':'+password).digest()
        auth=[(6,username.encode()),(0x14,attrs[0x14]),(0x15,attrs[0x15])]
        tx=secrets.token_bytes(12);client.send(relay._packet(3,requested+auth,tx,key))
        assert relay._attributes(client.recv(65536),tx)[0]==0x103
        peer_xor=struct.pack('!BBHI',0,1,peer.getsockname()[1]^(relay.COOKIE>>16),int(ipaddress.ip_address(peer_ip))^relay.COOKIE)
        tx=secrets.token_bytes(12);client.send(relay._packet(9,[(0x12,peer_xor),(0xc,b'\x40\x00\0\0')]+auth,tx,key))
        assert relay._attributes(client.recv(65536),tx)[0]==0x109
        payload=b'halfcloud-authenticated-relay-data-'+secrets.token_bytes(32)
        client.send(struct.pack('!HH',0x4000,len(payload))+payload)
        data,source=peer.recvfrom(65536);assert data==payload
        peer.sendto(data,source)
        response=client.recv(65536)
        assert struct.unpack('!HH',response[:4])==(0x4000,len(payload)) and response[4:4+len(payload)]==payload
        tx=secrets.token_bytes(12);client.send(relay._packet(4,[(0xd,b'\0\0\0\0')]+auth,tx,key))
        client.recv(65536)
    print('Authenticated UDP / TCP allocations and bidirectional relay payload passed.')
    try:relay.probe(dict(profile,secret='wrong-'+'x'*48))
    except ValueError:pass
    else:raise AssertionError('Wrong TURN secret accepted')
    # Generate interface traffic after collection has started; not provider billing.
    subprocess.run(['curl','-fsSL','https://raw.githubusercontent.com/coturn/coturn/master/README.turnserver','-o','/dev/null'],check=True)
    time.sleep(35)
    subprocess.run(['systemctl','restart',relay.STATS],check=True)
    time.sleep(2)
    def total():
        data=json.loads(relay.vnstat('--json',capture=True).stdout)
        return next(x['traffic']['total'] for x in data['interfaces'] if x['name']==interface)
    first=total();assert first['rx']+first['tx']>0,first
    subprocess.run(['systemctl','restart',relay.STATS],check=True);time.sleep(2)
    second=total();assert second['rx']>=first['rx'] and second['tx']>=first['tx']
    relay.traffic(argparse.Namespace(json=False))
    # Same-host rerun is a recovery/update, preserving relay secret and the DB.
    relay.install(args)
    assert json.loads((relay.ETC/'config.json').read_bytes())['secret']==json.loads(secret_before)['secret']
    third=total();assert third['rx']>=first['rx'] and third['tx']>=first['tx']
    relay.uninstall()
    assert (relay.ETC/'config.json').is_file() and (relay.STATE/'vnstat.db').is_file()
    print('Native install, authenticated data, traffic persistence, rerun and retained-data uninstall passed.')
finally:
    subprocess.run(['tail','-n','45',str(relay.LOG/'turn.log')],check=False)
    subprocess.run(['journalctl','-u',relay.STATS,'--no-pager','-n','15'],check=False)
    subprocess.run(['ip','address','del',fixture_ip+'/32','dev',interface],check=False)
    subprocess.run(['ip','address','del',peer_ip+'/32','dev',interface],check=False)
