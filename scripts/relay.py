#!/usr/bin/env python3
"""Native TURN relay and traffic manager; independent of the existing phone gateway."""
import argparse
import base64
import contextlib
import datetime as dt
import getpass
import hashlib
import hmac
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import time

VERSION = '0.4.2'
OWNER = 'halfcloud-relay'
ETC = Path('/etc/halfcloud-relay')
APP = Path('/usr/local/lib/halfcloud-relay/relay.py')
LAUNCHER = Path('/usr/local/bin/remote-console-relay')
STATE = Path('/var/lib/halfcloud-relay-traffic')
LOG = Path('/var/log/halfcloud-relay')
UNITS = Path('/etc/systemd/system')
SERVICE = 'halfcloud-relay.service'
STATS = 'halfcloud-relay-traffic.service'
TIMER = 'halfcloud-relay-logrotate.timer'
PREFIX = 'halfcloud-relay-v1:'
COOKIE = 0x2112A442


def run(*args, capture=False, check=True, **kwargs):
    return subprocess.run(list(map(str, args)), check=check, text=True, capture_output=capture, **kwargs)


def atomic(path, value, mode=0o600):
    path = Path(path)
    if path.is_symlink():
        raise ValueError('設定路徑是連結：'+str(path))
    path.parent.mkdir(parents=True, exist_ok=True)
    data = value.encode() if isinstance(value, str) else value
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
        temp = Path(stream.name)
        try:
            os.fchmod(stream.fileno(), mode) if hasattr(os, 'fchmod') else None
            stream.write(data)
        except BaseException:
            temp.unlink(missing_ok=True)
            raise
    try:
        temp.chmod(mode)
        os.replace(temp, path)
    finally:
        temp.unlink(missing_ok=True)


def write_json(path, value):
    atomic(path, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def public_ipv4(value):
    address = ipaddress.ip_address(value)
    if address.version != 4 or not address.is_global:
        raise ValueError('請輸入香港中繼嘅公網 IPv4')
    return str(address)


def validate_profile(value):
    if value.get('schema') != 1 or value.get('kind') != OWNER:
        raise ValueError('接駁碼格式不符')
    ip = public_ipv4(value.get('public_ip', ''))
    port = value.get('port')
    if type(port) is not int or not 1024 <= port <= 65535:
        raise ValueError('中繼連接埠無效')
    secret = value.get('secret', '')
    if not isinstance(secret, str) or not re.fullmatch(r'[A-Za-z0-9_-]{43,128}', secret):
        raise ValueError('中繼金鑰格式不符')
    return dict(schema=1, kind=OWNER, public_ip=ip, port=port, secret=secret)


def link_code(profile):
    data = json.dumps(validate_profile(profile), separators=(',', ':')).encode()
    return PREFIX+base64.urlsafe_b64encode(data).decode()


def parse_link(value):
    if len(value) > 4096 or not value.startswith(PREFIX):
        raise ValueError('請貼上完整嘅香港接駁碼')
    try:
        data = base64.b64decode(value[len(PREFIX):], altchars=b'-_', validate=True)
        return validate_profile(json.loads(data))
    except (ValueError, KeyError, TypeError) as error:
        raise ValueError('接駁碼無效') from error


def config():
    value = json.loads((ETC/'config.json').read_text())
    validate_profile(value)
    if not re.fullmatch(r'[A-Za-z0-9_.:-]{1,15}', value.get('interface', '')):
        raise ValueError('網卡設定無效')
    return value


def interface_info(interface=None):
    routes = json.loads(run('ip', '-j', '-4', 'route', 'get', '1.1.1.1', capture=True).stdout)
    interface = interface or routes[0]['dev']
    if not re.fullmatch(r'[A-Za-z0-9_.:-]{1,15}', interface) or interface == 'lo':
        raise ValueError('請指定對外網卡')
    addresses = json.loads(run('ip', '-j', '-4', 'address', 'show', 'dev', interface, capture=True).stdout)
    ips = [x['local'] for entry in addresses for x in entry['addr_info'] if x.get('scope') == 'global']
    if not ips:
        raise ValueError('指定網卡沒有 IPv4')
    source = next((r.get('prefsrc') for r in routes if r.get('dev') == interface and r.get('prefsrc') in ips), ips[0])
    return interface, source


def turn_configuration(value):
    external = value['public_ip']
    if value['relay_ip'] != external:
        external += '/'+value['relay_ip']
    return f'''listening-port={value['port']}
listening-ip=0.0.0.0
relay-ip={value['relay_ip']}
external-ip={external}
realm=halfcloud-relay
server-name=halfcloud-relay
fingerprint
use-auth-secret
static-auth-secret={value['secret']}
min-port=49160
max-port=49260
relay-threads=2
user-quota=16
total-quota=64
no-cli
no-tls
no-dtls
no-multicast-peers
log-file=stdout
pidfile=/run/halfcloud-relay/turnserver.pid
userdb=/run/halfcloud-relay/turndb
denied-peer-ip=0.0.0.0-0.255.255.255
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=100.64.0.0-100.127.255.255
denied-peer-ip=127.0.0.0-127.255.255.255
denied-peer-ip=169.254.0.0-169.254.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
denied-peer-ip=224.0.0.0-255.255.255.255
'''


def check_port(port):
    for kind in [socket.SOCK_STREAM, socket.SOCK_DGRAM]:
        with socket.socket(socket.AF_INET, kind) as sock:
            sock.bind(('0.0.0.0', port))


def install(args):
    if not Path('/run/systemd/system').is_dir() or not shutil.which('apt-get'):
        raise ValueError('請使用有 systemd 嘅 Ubuntu / Debian')
    existing = config() if (ETC/'config.json').exists() else None
    if not existing:
        for path in [ETC, APP.parent, LAUNCHER, STATE, LOG, UNITS/SERVICE, UNITS/STATS, UNITS/TIMER]:
            if path.exists() or path.is_symlink():
                raise ValueError('已有同名檔案，請先核對：'+str(path))
        check_port(args.port)
        if run('systemctl', 'is-enabled', 'coturn.service', capture=True, check=False).stdout.strip() in ['enabled', 'enabled-runtime']:
            raise ValueError('主機已有啟用嘅 coturn，請先核對現有服務設定')
    if shutil.disk_usage('/').free < 512*1024*1024:
        raise ValueError('請先騰出至少 512MB 可用磁碟空間')
    interface, local_ip = interface_info(args.interface or (existing or {}).get('interface'))
    if existing:
        value = existing
        if args.public_ip and args.public_ip != value['public_ip']:
            raise ValueError('更新時沿用現有地址；更換地址請另行部署')
    else:
        default = local_ip if ipaddress.ip_address(local_ip).is_global else ''
        raw = args.public_ip or input('香港公網 IPv4'+(' ['+default+']' if default else '')+'：').strip() or default
        value = dict(schema=1, kind=OWNER, public_ip=public_ipv4(raw), port=args.port,
                     secret=secrets.token_urlsafe(48), interface=interface, relay_ip=local_ip,
                     installed_at=dt.datetime.now(dt.timezone.utc).isoformat(), quota_gb=None, count='tx')
    validate_profile(value)
    # A new package must not start a default listener before our own unit exists.
    fresh_services = [name for name, executable in [('coturn.service','turnserver'), ('vnstat.service','vnstat')] if not shutil.which(executable)]
    for name in fresh_services:
        run('systemctl', 'mask', '--runtime', name)
    try:
        run('apt-get', 'update')
        run('apt-get', 'install', '-y', '--no-install-recommends', 'coturn', 'vnstat', 'logrotate', 'iproute2', 'ca-certificates', 'tzdata',
            env=dict(os.environ, DEBIAN_FRONTEND='noninteractive'))
    finally:
        for name in fresh_services:
            run('systemctl', 'unmask', '--runtime', name)
    for name in fresh_services:
        run('systemctl', 'disable', '--now', name)
    if not existing:
        check_port(value['port'])
    value['version'] = VERSION
    ETC.mkdir(parents=True, exist_ok=True)
    ETC.chmod(0o755)
    write_json(ETC/'config.json', value)
    atomic(ETC/'turnserver.conf', turn_configuration(value))
    atomic(ETC/'vnstat.conf', f'''DatabaseDir "{STATE}"
Interface "{value['interface']}"
UseUTC 0
UpdateInterval 30
PollInterval 5
SaveInterval 1
MonthRotate 1
5MinuteHours 24
HourlyDays 7
DailyDays 90
MonthlyMonths 24
YearlyYears 2
''', 0o644)
    LOG.mkdir(parents=True, exist_ok=True)
    atomic(ETC/'logrotate.conf', f'''{LOG}/turn.log {{
    size 5M
    rotate 3
    compress
    missingok
    notifempty
    copytruncate
}}
''')
    units = {
        SERVICE: f'''[Unit]
Description=HalfCloud TURN relay
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
DynamicUser=yes
RuntimeDirectory=halfcloud-relay
LoadCredential=turnserver.conf:{ETC}/turnserver.conf
ExecStart=/usr/bin/turnserver -c %d/turnserver.conf
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
MemoryHigh=256M
MemoryMax=384M
LimitNOFILE=16384
StandardOutput=append:{LOG}/turn.log
StandardError=append:{LOG}/turn.log
[Install]
WantedBy=multi-user.target
''',
        STATS: f'''[Unit]
Description=HalfCloud interface traffic statistics
After=network-online.target
[Service]
Type=simple
User=vnstat
Group=vnstat
StateDirectory=halfcloud-relay-traffic
RuntimeDirectory=halfcloud-relay-traffic
Environment=TZ=Asia/Hong_Kong
ExecStart=/usr/sbin/vnstatd --nodaemon --noadd --noremove --config {ETC}/vnstat.conf
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
MemoryMax=64M
[Install]
WantedBy=multi-user.target
''',
        'halfcloud-relay-logrotate.service': f'''[Unit]
Description=Rotate HalfCloud relay logs
[Service]
Type=oneshot
ExecStart=/usr/sbin/logrotate -s {ETC}/logrotate.state {ETC}/logrotate.conf
''',
        TIMER: '''[Unit]
Description=Rotate HalfCloud relay logs regularly
[Timer]
OnBootSec=10min
OnUnitActiveSec=15min
[Install]
WantedBy=timers.target
'''}
    for name, content in units.items():
        atomic(UNITS/name, content, 0o644)
    APP.parent.mkdir(parents=True, exist_ok=True)
    if Path(__file__).resolve() != APP:
        atomic(APP, Path(__file__).read_bytes(), 0o755)
    atomic(LAUNCHER, '#!/bin/sh\nexec python3 '+str(APP)+' "$@"\n', 0o755)
    # Create only our own database, with the package's unprivileged statistics user.
    import pwd
    owner = pwd.getpwnam('vnstat')
    STATE.mkdir(mode=0o750, exist_ok=True)
    os.chown(STATE, owner.pw_uid, owner.pw_gid)
    run('runuser', '-u', 'vnstat', '--', 'vnstatd', '--config', ETC/'vnstat.conf', '--initdb', '--noadd')
    names = run('vnstat', '--config', ETC/'vnstat.conf', '--dbiflist', '1', capture=True)
    if value['interface'] not in names.stdout.split():
        run('runuser', '-u', 'vnstat', '--', 'vnstat', '--config', ETC/'vnstat.conf', '-i', value['interface'], '--add')
    run('systemctl', 'daemon-reload')
    run('systemctl', 'enable', SERVICE, STATS, TIMER)
    run('systemctl', 'restart', SERVICE, STATS)
    run('systemctl', 'start', TIMER)
    for attempt in range(15):
        try:
            probe(value, host=value['relay_ip'])
            break
        except (OSError, ValueError):
            if attempt == 14:
                raise RuntimeError('中繼未通過認證檢查，請用 remote-console-relay logs 檢查')
            time.sleep(1)
    run('systemctl', 'is-active', '--quiet', SERVICE, STATS)
    print('\n半雲電話香港中繼 '+VERSION+' 已安裝。')
    print(f"請放行 TCP / UDP {value['port']}，以及 UDP 49160–49260。")
    print('輸入 remote-console-relay 開啟選單；traffic 查看流量。')
    export_link(value)


def _attr(kind, data):
    return struct.pack('!HH', kind, len(data))+data+b'\0'*((-len(data)) % 4)


def _packet(kind, attrs, transaction, key=None):
    body = b''.join(_attr(k, v) for k, v in attrs)
    header = struct.pack('!HHI12s', kind, len(body)+(24 if key else 0), COOKIE, transaction)
    if key:
        body += _attr(8, hmac.digest(key, header+body, 'sha1'))
    return header+body


def _attributes(packet, transaction):
    if len(packet) < 20:
        raise ValueError('中繼回應不完整')
    kind, length, cookie, tx = struct.unpack('!HHI12s', packet[:20])
    if cookie != COOKIE or tx != transaction or length != len(packet)-20:
        raise ValueError('中繼回應格式不符')
    result, offsets, offset = {}, {}, 20
    while offset < len(packet):
        if offset+4 > len(packet): raise ValueError('中繼屬性不完整')
        name, size = struct.unpack('!HH', packet[offset:offset+4])
        if offset+4+size > len(packet): raise ValueError('中繼屬性不完整')
        result[name] = packet[offset+4:offset+4+size]
        offsets[name] = offset
        offset += 4+size+(-size % 4)
    return kind, result, offsets


def _receive(sock, tcp):
    if not tcp: return sock.recv(65536)
    def read(size):
        data = b''
        while len(data) < size:
            block = sock.recv(size-len(data))
            if not block: raise ValueError('中繼 TCP 連線已中斷')
            data += block
        return data
    header = read(20)
    return header+read(struct.unpack('!H', header[2:4])[0])


def probe(profile, host=None):
    """Authenticated TURN allocations over both UDP and TCP, released immediately."""
    profile = validate_profile(profile)
    for tcp in [False, True]:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM if tcp else socket.SOCK_DGRAM) as sock:
            sock.settimeout(4)
            sock.connect((host or profile['public_ip'], profile['port']))
            requested = [(0x19, b'\x11\0\0\0')]
            transaction = secrets.token_bytes(12)
            sock.sendall(_packet(3, requested, transaction))
            kind, attrs, _ = _attributes(_receive(sock, tcp), transaction)
            if kind != 0x113 or 0x14 not in attrs or 0x15 not in attrs:
                raise ValueError('中繼沒有回傳 TURN 認證要求')
            username = str(int(time.time())+600)+':halfcloud-check'
            password = base64.b64encode(hmac.digest(profile['secret'].encode(), username.encode(), 'sha1'))
            key = hashlib.md5(username.encode()+b':'+attrs[0x14]+b':'+password).digest()
            auth = [(6, username.encode()), (0x14, attrs[0x14]), (0x15, attrs[0x15])]
            transaction = secrets.token_bytes(12)
            sock.sendall(_packet(3, requested+auth, transaction, key))
            response = _receive(sock, tcp)
            kind, attrs, offsets = _attributes(response, transaction)
            if kind != 0x103 or 0x16 not in attrs or 8 not in offsets:
                raise ValueError('中繼認證失敗；請核對接駁碼及主機時間')
            end = offsets[8]
            header = response[:2]+struct.pack('!H', end+24-20)+response[4:20]
            if not hmac.compare_digest(attrs[8], hmac.digest(key, header+response[20:end], 'sha1')):
                raise ValueError('中繼認證回應校驗失敗')
            transaction = secrets.token_bytes(12)
            sock.sendall(_packet(4, [(0xd, b'\0\0\0\0')]+auth, transaction, key))
            _attributes(_receive(sock, tcp), transaction)
    return True


def export_link(value):
    print('\n日本主機接駁指令：')
    print('bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/v'+VERSION+'/relay-install.sh) connect')
    print('喺日本主機執行後，貼上以下接駁碼（包含中繼金鑰，請私下保存）：\n')
    print(link_code(value))


def profile_paths(data):
    data = Path(data).resolve()
    registry = json.loads((data/'devices.json').read_text())
    if registry.get('version') != 1 or not registry.get('devices'):
        raise ValueError('電話登記格式不符')
    paths = {data/'ice.private.json'}
    ids = set()
    for phone in registry['devices']:
        ident = phone['id']
        if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}', ident) or ident in ids:
            raise ValueError('電話 ID 無效或重複')
        ids.add(ident)
        if phone['private'] == '/private':
            path = data/'ice.private.json'
        elif phone['private'] == '/private/devices/'+ident:
            path = data/'devices'/ident/'ice.private.json'
        else:
            raise ValueError('電話設定路徑不符')
        paths.add(path)
    for path in paths:
        if not path.is_file() or path.is_symlink() or not path.resolve().is_relative_to(data):
            raise ValueError('電話網絡設定檔案不符')
    return sorted(paths)


def connect_gateway(manager, profile=None, restore=False, check=probe):
    cfg = manager.config()
    data = manager.p.data.resolve()
    state_path = data/'relay-link-state.json'
    paths = profile_paths(data)
    state = json.loads(state_path.read_text()) if state_path.exists() else None
    if restore:
        if not state or state.get('schema') != 1:
            raise ValueError('未有香港中繼接駁記錄')
        targets = {}
        for path in paths:
            if hashlib.sha256(path.read_bytes()).hexdigest() != state['applied_sha256']:
                raise ValueError('網絡設定已另行變更，請先核對備份')
            relative = str(path.relative_to(data))
            targets[path] = base64.b64decode(state['originals'].get(relative, state['original_primary']))
    else:
        profile = validate_profile(profile)
        check(profile)
        updated = (json.dumps({'stun_urls': [f"stun:{profile['public_ip']}:{profile['port']}"],
                  'turn_urls': [f"turn:{profile['public_ip']}:{profile['port']}?transport=udp", f"turn:{profile['public_ip']}:{profile['port']}?transport=tcp"],
                  'turn_shared_secret': profile['secret']}, indent=2)+'\n').encode()
        digest = hashlib.sha256(updated).hexdigest()
        if state and state.get('applied_sha256') == digest and all(p.read_bytes() == updated for p in paths):
            print('已接駁至同一香港中繼。'); return
        targets = {path: updated for path in paths}
    before = {path: path.read_bytes() for path in paths}
    for value in before.values():
        if not isinstance(json.loads(value), dict): raise ValueError('原有 ICE 設定無效')
    backup = manager.p.backups/('relay-'+dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S')+'-'+secrets.token_hex(3))
    backup.mkdir(parents=True, mode=0o700)
    for path, value in before.items(): atomic(backup/path.relative_to(data), value)
    try:
        for path, value in targets.items(): atomic(path, value)
        manager.compose('up', '-d', '--force-recreate', 'gateway')
        if not manager.healthy(): raise RuntimeError('入口健康檢查失敗')
    except BaseException:
        for path, value in before.items(): atomic(path, value)
        manager.compose('up', '-d', '--force-recreate', 'gateway')
        if not manager.healthy():
            raise RuntimeError('已還原網絡設定；入口仍未恢復，請檢查 remote-console logs')
        raise
    if restore:
        state_path.unlink()
    else:
        # Re-linking another relay retains the original pre-HK settings for rollback.
        originals = state['originals'] if state else {str(p.relative_to(data)): base64.b64encode(b).decode() for p, b in before.items()}
        primary = state['original_primary'] if state else base64.b64encode(before[data/'ice.private.json']).decode()
        write_json(state_path, dict(schema=1, applied_sha256=digest, originals=originals, original_primary=primary,
                                   relay_ip=profile['public_ip'], backup=str(backup)))
    print('入口：https://'+cfg['domain']+'/control')
    print('已'+('還原原有' if restore else '接駁香港')+'中繼；電話配對及控制端授權沿用。')
    print('受控端 0.4.2 或更新版本會於約一分鐘內同步，新連線生效。')
    print('網絡設定備份：'+str(backup))


def japan(args):
    path = Path('/opt/remote-console-source/scripts/manage.py')
    if not path.is_file():
        raise ValueError('請先在日本主機以現有升級命令啟用 remote-console 管理選單')
    spec = importlib.util.spec_from_file_location('halfcloud_gateway_manager', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    manager = module.Manager()
    profile = None
    if args.command != 'disconnect':
        raw = args.link_file.read_text().strip() if args.link_file else getpass.getpass('貼上香港接駁碼（輸入時隱藏）：').strip()
        profile = parse_link(raw)
    with module.lock(manager.p.config.parent/'manager.lock'):
        connect_gateway(manager, profile, restore=args.command == 'disconnect')


def vnstat(*args, capture=False):
    env = os.environ.copy(); env['TZ'] = 'Asia/Hong_Kong'
    return run('vnstat', '--config', ETC/'vnstat.conf', '-i', config()['interface'], *args, capture=capture, env=env)


def traffic(args):
    value = config()
    result = json.loads(vnstat('--json', capture=True).stdout)
    if args.json:
        print(json.dumps(result, ensure_ascii=False)); return
    interface = next(x for x in result['interfaces'] if x['name'] == value['interface'])
    stats = interface['traffic']
    now = dt.datetime.now(__import__('zoneinfo').ZoneInfo('Asia/Hong_Kong'))
    def period(name):
        for item in stats.get(name, []):
            date = item['date']
            if date['year'] == now.year and date['month'] == now.month and (name == 'month' or date['day'] == now.day): return item
        return {'rx': 0, 'tx': 0}
    print('半雲電話 · 香港中繼流量（Asia/Hong_Kong）')
    print('網卡：'+value['interface']+'；整張網卡入／出流量，包含 SSH 等其他連線。')
    def size(number):
        for unit, scale in [('GB', 1e9), ('MB', 1e6), ('kB', 1e3)]:
            if number >= scale: return f'{number/scale:.2f} {unit}'
        return str(number)+' B'
    for label, item in [('今日', period('day')), ('本月', period('month')), ('開始統計至今', stats['total'])]:
        print(f"{label}：入 {size(item['rx'])}　出 {size(item['tx'])}　合計 {size(item['rx']+item['tx'])}")
    if value.get('quota_gb'):
        month = period('month'); used = month['tx']+(month['rx'] if value['count'] == 'total' else 0)
        print(f"每月參考額度：{used/1e9:.2f} / {value['quota_gb']} GB（{'雙向' if value['count']=='total' else '出站'}，{used/(value['quota_gb']*1e9)*100:.1f}%）")
    else:
        print('每月參考額度：未設定；統計由安裝開始，供應商帳單以其後台為準。')


def uninstall():
    config()
    run('systemctl', 'disable', '--now', SERVICE, STATS, TIMER)
    for name in [SERVICE, STATS, TIMER, 'halfcloud-relay-logrotate.service']:
        (UNITS/name).unlink(missing_ok=True)
    run('systemctl', 'daemon-reload')
    LAUNCHER.unlink(missing_ok=True)
    APP.unlink(missing_ok=True)
    print('中繼已解除安裝；設定、統計及日誌保留，重新執行安裝可啟用。')


def update():
    config()
    with tempfile.TemporaryDirectory(prefix='halfcloud-relay-update-') as temp:
        script = Path(temp)/'install.sh'
        run('curl', '-fL', '--retry', '3', '--connect-timeout', '15', '--max-time', '120',
            'https://raw.githubusercontent.com/sing0626/remote-console/main/relay-install.sh', '-o', script)
        run('bash', script, 'install')


@contextlib.contextmanager
def relay_lock():
    import fcntl
    with open('/run/lock/halfcloud-relay.lock', 'a') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        yield


def execute(args):
    with relay_lock() if args.command in ['install', 'quota', 'uninstall', 'start', 'stop', 'restart'] else contextlib.nullcontext():
        return _execute(args)


def _execute(args):
    command = args.command
    if command == 'install': install(args)
    elif command in ['connect', 'disconnect']: japan(args)
    elif command == 'traffic': traffic(args)
    elif command == 'live': vnstat('--live')
    elif command == 'link': export_link(config())
    elif command == 'check': probe(config()); print('UDP / TCP TURN 認證成功；媒體連接埠仍需在外部連線測試。')
    elif command == 'logs': run('tail', '-n', '80', '-f', LOG/'turn.log')
    elif command == 'restart': config(); run('systemctl', 'restart', SERVICE)
    elif command == 'start': config(); run('systemctl', 'enable', '--now', SERVICE, STATS, TIMER)
    elif command == 'stop': config(); run('systemctl', 'stop', SERVICE)
    elif command == 'status':
        value = config(); print('半雲電話 · 香港中繼 '+value['version'])
        run('systemctl', '--no-pager', '--full', 'status', SERVICE, STATS, check=False)
        traffic(args)
    elif command == 'quota':
        value = config()
        if args.gb is None or args.gb < 0: raise ValueError('請輸入每月 GB；0 表示未設定')
        value.update(quota_gb=args.gb or None, count=args.count); write_json(ETC/'config.json', value); traffic(args)
    elif command == 'uninstall':
        if input('輸入 UNINSTALL 確認解除安裝（設定及統計保留）：') == 'UNINSTALL': uninstall()
    elif command == 'update': update()


def main():
    p = argparse.ArgumentParser(description='半雲電話 · 香港中繼管理')
    p.add_argument('command', nargs='?', choices=['install', 'connect', 'disconnect', 'status', 'traffic', 'live', 'link', 'check', 'start', 'stop', 'restart', 'logs', 'quota', 'update', 'uninstall'])
    p.add_argument('gb', nargs='?', type=int)
    p.add_argument('--version', action='version', version=VERSION)
    p.add_argument('--public-ip'); p.add_argument('--interface'); p.add_argument('--port', type=int, default=3478)
    p.add_argument('--link-file', type=Path); p.add_argument('--json', action='store_true'); p.add_argument('--count', choices=['tx', 'total'], default='tx')
    args = p.parse_args()
    if os.name != 'posix' or os.geteuid() != 0: p.error('請喺 Linux 以 root 執行')
    os.umask(0o077)
    interactive = args.command is None or args.command == 'uninstall' or args.command == 'connect' and not args.link_file or args.command == 'install' and not args.public_ip and not (ETC/'config.json').exists()
    if interactive and not sys.stdin.isatty(): sys.stdin = open('/dev/tty')
    try:
        if args.command: execute(args); return
        choices = [('status','狀態及流量'),('traffic','每日／每月流量'),('live','即時流量'),('link','顯示日本接駁碼'),('check','檢查 TURN 認證'),('restart','重新啟動'),('update','更新'),('uninstall','解除安裝')]
        while True:
            print('\n半雲電話 · 香港中繼 '+VERSION)
            print('\n'.join(f'{i}. {label}' for i, (_, label) in enumerate(choices, 1))+'\n0. 離開')
            value = input('選擇：').strip()
            if value == '0': return
            if not value.isdigit() or not 1 <= int(value) <= len(choices): continue
            args.command = choices[int(value)-1][0]
            try: execute(args)
            except KeyboardInterrupt: print('\n已返回選單。')
            except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError, KeyError) as error: print('未完成：'+str(error), file=sys.stderr)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError, KeyError) as error:
        raise SystemExit('未完成：'+str(error))


if __name__ == '__main__':
    main()
