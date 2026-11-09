#!/usr/bin/env python3
"""Root-only Linux deployment manager. Code upgrades never regenerate phone/trust data."""
import argparse
import contextlib
import datetime
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

REPO = 'sing0626/remote-console'
PROJECT = 'remote-console-generic'
MARKER = '.remote-console-managed'


class Paths:
    def __init__(self, root=Path('/')):
        self.source = root / 'opt/remote-console-source'
        self.previous = root / 'opt/remote-console-previous'
        self.data = root / 'var/lib/remote-console-generic'
        self.config = root / 'etc/remote-console-generic/config.json'
        self.backups = root / 'var/backups/remote-console-generic'
        self.launcher = root / 'usr/local/bin/remote-console'
        self.legacy = root / 'opt/remote-console'


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
        tmp = Path(stream.name)
        stream.write((json.dumps(value, indent=2, ensure_ascii=False)+'\n').encode())
    tmp.chmod(0o600)
    os.replace(tmp, path)


def mark(path, **metadata):
    write_json(path / MARKER, dict(managed_by=PROJECT, schema=1, **metadata))


def owned(path):
    if path.is_symlink():
        return False
    try:
        item = json.loads((path / MARKER).read_text())
        return item['managed_by'] == PROJECT and item['schema'] == 1
    except (OSError, ValueError, KeyError):
        return False


def remove_owned(path):
    if not owned(path):
        raise ValueError('拒絕移除未標記為本工具管理嘅目錄：'+str(path))
    shutil.rmtree(path)


def fetch_json(url):
    req = urllib.request.Request(url, headers={'User-Agent': 'RemoteConsole-manager', 'Accept': 'application/vnd.github+json'})
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


def extract_source(archive, destination):
    """GitHub sources only: reject link/path traversal, devices and oversized payloads."""
    with tarfile.open(archive, 'r:gz') as tar:
        members = tar.getmembers()
        total = 0
        roots = set()
        for member in members:
            parts = Path(member.name).parts
            if not parts or member.name.startswith('/') or '..' in parts or '\\' in member.name:
                raise ValueError('不安全嘅來源路徑')
            if not (member.isdir() or member.isfile()):
                raise ValueError('來源壓縮檔含有連結或特殊檔案')
            roots.add(parts[0]); total += member.size
        if len(roots) != 1 or total > 300*1024*1024 or len(members) > 15000:
            raise ValueError('來源壓縮檔格式或大小不符')
        for member in members:
            parts = Path(member.name).parts[1:]
            if not parts:
                continue
            target = destination.joinpath(*parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with tar.extractfile(member) as src, target.open('wb') as dst:
                    shutil.copyfileobj(src, dst)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)
    for required in ['scripts/manage.py', 'deploy/remote/initialize.py', 'deploy/remote/gateway.Dockerfile']:
        if not (destination/required).is_file():
            raise ValueError('來源版本缺少安裝檔案：'+required)


def download_source(destination, release=None):
    if release is None:
        release = fetch_json(f'https://api.github.com/repos/{REPO}/releases/latest')['tag_name']
    if not re.fullmatch(r'v\d+\.\d+\.\d+', release):
        raise ValueError('請選擇伺服器穩定版本，例如 v0.2.0')
    commit = fetch_json(f'https://api.github.com/repos/{REPO}/commits/{release}')['sha']
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('無效來源版本')
    with tempfile.TemporaryDirectory(prefix='remote-console-download-') as tmp:
        archive = Path(tmp)/'source.tar.gz'
        req = urllib.request.Request(f'https://codeload.github.com/{REPO}/tar.gz/{commit}', headers={'User-Agent': 'RemoteConsole-manager'})
        with urllib.request.urlopen(req, timeout=90) as src, archive.open('wb') as dst:
            size = 0
            while chunk := src.read(1024*1024):
                size += len(chunk)
                if size > 100*1024*1024:
                    raise ValueError('下載檔案超出大小限制')
                dst.write(chunk)
        extract_source(archive, destination)
    mark(destination, release=release, commit=commit)
    return {'release': release, 'commit': commit}


def toml_setting(path, name):
    values = re.findall(r'^'+re.escape(name)+r'\s*=\s*([^\r\n]+)', path.read_text(), re.M)
    if len(values) != 1:
        raise ValueError('設定缺少或重複：'+name)
    return values[0].strip().strip('"')


def identity_snapshot(data):
    """Preserve durable identities; normal last-used timestamps may change on reconnect."""
    result = {}
    for path in data.rglob('*'):
        if not path.is_file(): continue
        relative = path.relative_to(data).as_posix()
        if '.generated.' in path.name or relative in [MARKER, 'enrollment.pub']: continue
        if relative.startswith('controller-trust/.trust-'): continue
        content = path.read_bytes()
        if relative == 'controller-trust/controllers.json':
            trust = json.loads(content)
            for record in trust['controllers']:
                record.pop('last_used_at', None)
            trust['controllers'].sort(key=lambda item: item['id'])
            content = json.dumps(trust, sort_keys=True).encode()
        result[relative] = hashlib.sha256(content).hexdigest()
    return result


def validate_legacy(legacy):
    data = legacy/'private'
    registry = json.loads((data/'devices.json').read_text())
    phones = registry.get('devices', [])
    default = registry.get('default_device')
    ids = [phone['id'] for phone in phones]
    if registry.get('version') != 1 or not 1 <= len(phones) <= 32 or len(ids) != len(set(ids)) or default not in ids:
        raise ValueError('未知電話登記資料已保留')
    extra = json.loads((legacy/'compose.devices.json').read_text())
    expected = {'signaling-'+ident for ident in ids if ident != default}
    if extra.get('x-remoteconsole-generated') != 1 or set(extra['services']) != expected:
        raise ValueError('未知額外電話服務已保留')
    for relative in ['frps.toml', 'turnserver.conf', 'phone-frpc.toml', 'ice.private.json',
                     'pki/ca.crt', 'pki/server.crt', 'pki/server.key', 'controller-trust/controllers.json']:
        if not (data/relative).is_file() or not (data/relative).stat().st_size:
            raise ValueError('舊部署缺少必要設定：'+relative)
    domain = toml_setting(data/'phone-frpc.toml', 'transport.tls.serverName')
    ip = toml_setting(data/'phone-frpc.toml', 'serverAddr')
    if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?', domain) or '.' not in domain or not ipaddress.ip_address(ip).is_global or ':' in ip:
        raise ValueError('未知舊入口設定已保留')
    ports = set()
    for phone in phones:
        ident = phone['id']
        if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}', ident) or ident == 'tunnel':
            raise ValueError('未知電話 ID 已保留')
        primary = ident == default
        private = '/private' if primary else '/private/devices/'+ident
        key_ref = '/run/owner-enrollment.pub' if primary else private+'/enrollment.pub'
        if phone.get('private') != private or phone.get('key') != key_ref:
            raise ValueError('未知電話身份映射已保留')
        profile = data if primary else data/'devices'/ident
        key = legacy/'owner-enrollment.pub' if primary else profile/'enrollment.pub'
        subprocess.run(['openssl', 'pkey', '-pubin', '-in', str(key), '-noout'], check=True, capture_output=True)
        server, client = profile/'frps.toml', profile/'phone-frpc.toml'
        tunnel = int(toml_setting(server, 'bindPort'))
        proxy = int(toml_setting(client, 'remotePort'))
        if not 1024 <= tunnel <= 65535 or not 1024 <= proxy <= 65535 or tunnel in ports or proxy in ports or tunnel == proxy:
            raise ValueError('未知隧道埠設定已保留')
        ports.update([tunnel, proxy])
        if phone.get('upstream') != 'http://127.0.0.1:'+str(proxy) or toml_setting(client, 'serverPort') != str(tunnel) or toml_setting(server, 'auth.token') != toml_setting(client, 'auth.token'):
            raise ValueError('電話隧道設定不一致，現有資料已保留')
        for relative in ['ice.private.json', 'pki/ca.crt']:
            if not (profile/relative).is_file(): raise ValueError('電話設定不完整：'+relative)
    trust = json.loads((data/'controller-trust/controllers.json').read_text())
    if trust.get('version') != 1 or not isinstance(trust.get('controllers'), list):
        raise ValueError('未知控制端授權格式已保留')
    return registry


def compose_document(source, data, project=PROJECT, registry=None):
    build = str(source/'deploy/remote')
    def service(dockerfile, volumes, **more):
        return dict(build={'context': build, 'dockerfile': dockerfile}, restart='unless-stopped', network_mode='host', volumes=volumes, **more)
    services = {
        'gateway': service('gateway.Dockerfile', [f'{data}:/private:ro', f'{data}/controller-trust:/state'],
                           command=['-trust-store', '/state/controllers.json'], read_only=True,
                           cap_drop=['ALL'], security_opt=['no-new-privileges:true']),
        'signaling-tunnel': service('frps.Dockerfile', [f'{data}/frps.toml:/etc/frp/frps.toml:ro', f'{data}/pki:/pki:ro']),
        'turn': dict(image='coturn/coturn:4.18.0-r0', user='0:0', restart='unless-stopped', network_mode='host',
                     command=['-c', '/etc/coturn/turnserver.conf'], volumes=[f'{data}/turnserver.conf:/etc/coturn/turnserver.conf:ro'])
    }
    extra = data/'compose.devices.generated.json'
    if registry is not None or extra.exists():
        registry = registry or json.loads((data/'devices.json').read_text())
        default = registry['default_device']
        primary = next(item for item in registry['devices'] if item['id'] == default)
        if primary['key'] == '/run/owner-enrollment.pub':
            services['gateway']['volumes'].append(f'{data}/enrollment.pub:/run/owner-enrollment.pub:ro')
        # Reconstruct only registered secondary tunnels; do not execute arbitrary services from data.
        for phone in registry['devices']:
            ident = phone['id']
            if ident == default:
                continue
            if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}', ident):
                raise ValueError('無效裝置 ID')
            services['signaling-'+ident] = service('frps.Dockerfile', [f'{data}/devices/{ident}/frps.toml:/etc/frp/frps.toml:ro', f'{data}/pki:/pki:ro'])
    return {'name': project, 'services': services}


class Manager:
    def __init__(self, paths=None, downloader=download_source):
        self.p = paths or Paths()
        self.downloader = downloader
        self.project = PROJECT
        if self.p.config.exists():
            cfg = json.loads(self.p.config.read_text())
            if cfg.get('managed_by') == PROJECT and cfg.get('layout') == 'legacy-v0.1.14':
                self.p.data = self.p.legacy/'private'
                self.project = 'remote-console'

    def config(self):
        cfg = json.loads(self.p.config.read_text())
        if cfg.get('managed_by') != PROJECT or cfg.get('schema') != 1 or cfg.get('compose_project', PROJECT) != self.project or not owned(self.p.source) or not owned(self.p.data):
            raise ValueError('未知安裝或資料目錄；現有內容會保留')
        return cfg

    def compose(self, *args, source=None):
        source = source or self.p.source
        runtime = self.p.data/'compose.runtime.generated.json'
        write_json(runtime, compose_document(source, self.p.data, self.project))
        return subprocess.run(['docker', 'compose', '--project-name', self.project, '-f', str(runtime), *args], check=True)

    def healthy(self):
        for attempt in range(30):
            try:
                runtime = self.p.data/'compose.runtime.generated.json'
                result = subprocess.run(['docker', 'compose', '--project-name', self.project, '-f', str(runtime), 'ps', '--all', '--format', 'json'],
                                        check=True, capture_output=True, text=True)
                output = result.stdout.strip()
                rows = json.loads(output) if output.startswith('[') else [json.loads(line) for line in output.splitlines()]
                expected = set(compose_document(self.p.source, self.p.data)['services'])
                running = {row['Service'] for row in rows if row.get('State') == 'running' and row.get('Health', '') in ['', 'healthy']}
                with urllib.request.urlopen('http://127.0.0.1:18079/_remoteconsole/status', timeout=2) as response:
                    value = json.load(response)
                    if running == expected and response.status == 200 and value.get('gateway_ready') is True:
                        return True
            except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
                pass
            time.sleep(2)
        return False

    def stage(self, release):
        self.p.source.parent.mkdir(parents=True, exist_ok=True)
        stage = Path(tempfile.mkdtemp(prefix='.remote-console-stage-', dir=self.p.source.parent))
        # Marker permits bounded cleanup even if download fails part way through.
        mark(stage)
        try:
            version = self.downloader(stage, release)
            return stage, version
        except BaseException:
            remove_owned(stage)
            raise

    def backup(self):
        cfg = self.config()
        self.p.backups.mkdir(parents=True, mode=0o700, exist_ok=True)
        name = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
        target = self.p.backups/f'{name}.tar.gz'
        with tarfile.open(target, 'w:gz') as tar:
            tar.add(self.p.data, arcname='data', recursive=True)
            tar.add(self.p.config, arcname='config.json')
        target.chmod(0o600)
        digest = hashlib.sha256(target.read_bytes()).hexdigest()
        target.with_suffix('.sha256').write_text(digest+'  '+target.name+'\n')
        print('備份（含憑證，請妥善保存）：'+str(target))
        return target

    def install_launcher(self):
        self.p.launcher.parent.mkdir(parents=True, exist_ok=True)
        self.p.launcher.write_text('#!/bin/sh\n# managed by remote-console-generic\nexec python3 /opt/remote-console-source/scripts/manage.py "$@"\n')
        self.p.launcher.chmod(0o755)

    def migrate(self, release=None):
        """Adopt v0.1.14 in place: never initialize or rewrite phone/trust profiles."""
        for path in [self.p.source, self.p.previous, self.p.config, self.p.data, self.p.launcher]:
            if path.exists() or path.is_symlink():
                raise ValueError('現有管理安裝已保留；已升級請用 remote-console update：'+str(path))
        legacy = self.p.legacy
        data = legacy/'private'
        if legacy.is_symlink() or not legacy.is_dir() or (legacy/'installer.version').read_text().strip() != 'v0.1.14':
            raise ValueError('只接受已知 v0.1.14 部署，未知安裝會保留')
        # No symlinks, external configuration references, or unknown Docker services.
        if any(path.is_symlink() for path in legacy.rglob('*')):
            raise ValueError('舊部署含有連結；請先核對實際資料位置')
        registry = validate_legacy(legacy)
        expected = {'gateway', 'turn', 'signaling-tunnel'} | {'signaling-'+d['id'] for d in registry['devices'] if d['id'] != registry['default_device']}
        def old_compose(*args, capture=False):
            return subprocess.run(['docker', 'compose', '--project-name', 'remote-console', '-f', str(legacy/'compose.yml'), *args],
                                  cwd=legacy, check=True, capture_output=capture, text=capture)
        doc = json.loads(old_compose('config', '--format', 'json', capture=True).stdout)
        if doc.get('name') != 'remote-console' or set(doc['services']) != expected:
            raise ValueError('舊 Compose 專案或服務與已知結構不符，現有服務會保留')
        for name, service in doc['services'].items():
            if service.get('network_mode') != 'host':
                raise ValueError('未知網絡設定已保留：'+name)
            for volume in service.get('volumes', []):
                if volume.get('type') != 'bind' or not Path(volume['source']).resolve().is_relative_to(legacy.resolve()):
                    raise ValueError('未知外部資料掛載已保留：'+name)
        ids = subprocess.run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=remote-console'], check=True, capture_output=True, text=True).stdout.split()
        if not ids:
            raise ValueError('未搵到現有 remote-console 容器；請先啟動舊部署')
        containers = json.loads(subprocess.run(['docker', 'inspect', *ids], check=True, capture_output=True, text=True).stdout)
        for item in containers:
            labels = item['Config']['Labels']
            if labels.get('com.docker.compose.service') not in expected or Path(labels.get('com.docker.compose.project.working_dir', '')).resolve() != legacy.resolve():
                raise ValueError('同名 Docker 專案包含未知容器，現有服務會保留')
        stage, version = self.stage(release)
        if not (stage/'scripts/manage.py').is_file() or 'def migrate(' not in (stage/'scripts/manage.py').read_text():
            remove_owned(stage)
            raise ValueError('所選版本未支援舊部署升級')
        old_data = self.p.data
        self.p.data = data
        self.project = 'remote-console'
        added = [data/'enrollment.pub', data/'compose.devices.generated.json', data/'compose.runtime.generated.json', data/MARKER]
        if any(path.exists() for path in added):
            remove_owned(stage); self.p.data = old_data; self.project = PROJECT
            raise ValueError('舊資料已有管理暫存檔案；請先檢查，現有服務已保留')
        candidate = stage/'migration-compose.generated.json'
        write_json(candidate, compose_document(stage, data, self.project, registry))
        try:
            subprocess.run(['docker', 'compose', '--project-name', self.project, '-f', str(candidate), 'build', '--pull'], check=True)
        except BaseException:
            remove_owned(stage); self.p.data = old_data; self.project = PROJECT
            raise
        # Pause only the gateway while taking the pre-migration trust snapshot.
        # FRP/TURN are left running; no phone Root commands or re-enrollment needed.
        stopped = False
        try:
            old_compose('stop', 'gateway'); stopped = True
            self.p.backups.mkdir(parents=True, mode=0o700, exist_ok=True)
            stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
            backup = self.p.backups/('legacy-v0.1.14-'+stamp+'.tar.gz')
            with tarfile.open(backup, 'w:gz') as tar:
                tar.add(legacy, arcname='remote-console')
            backup.chmod(0o600)
            backup.with_suffix('.sha256').write_text(hashlib.sha256(backup.read_bytes()).hexdigest()+'  '+backup.name+'\n')
            print('舊部署完整備份：'+str(backup))
            before = identity_snapshot(data)
            shutil.copy2(legacy/'owner-enrollment.pub', data/'enrollment.pub')
            (data/'enrollment.pub').chmod(0o600)
            shutil.copy2(legacy/'compose.devices.json', data/'compose.devices.generated.json')
            mark(data)
            stage.rename(self.p.source)
            cfg = dict(managed_by=PROJECT, schema=1, layout='legacy-v0.1.14', compose_project=self.project,
                       domain=toml_setting(data/'phone-frpc.toml', 'transport.tls.serverName'),
                       public_ip=toml_setting(data/'phone-frpc.toml', 'serverAddr'), **version)
            write_json(self.p.config, cfg)
            self.install_launcher()
            self.compose('config', '--quiet')
            self.compose('up', '-d', '--remove-orphans')
            if not self.healthy():
                raise RuntimeError('升級後服務未通過健康檢查')
            if identity_snapshot(data) != before:
                raise RuntimeError('電話或控制端身份核對失敗')
        except BaseException as error:
            if stopped:
                # Rebuild the preserved original program, using original Compose/project/data.
                # Never rewind the controller store: a concurrent revocation must stay revoked.
                try:
                    old_compose('up', '-d', '--build', '--remove-orphans')
                except BaseException as recovery_error:
                    raise RuntimeError('升級未完成，舊版恢復亦失敗；資料與備份已保留。請檢查 Docker 日誌。') from recovery_error
            for path in added:
                path.unlink(missing_ok=True)
            if self.p.config.exists(): self.p.config.unlink()
            if self.p.launcher.exists(): self.p.launcher.unlink()
            for path in [stage, self.p.source]:
                if path.exists(): remove_owned(path)
            self.p.data = old_data; self.project = PROJECT
            raise RuntimeError('升級未完成，已恢復原有服務；配對及授權資料保留。原因：'+str(error)) from error
        print('已升級至 '+version['release']+' 並接入 remote-console 選單。原本電話 ID、配對及控制端授權保留。')
        print('原始程式及 Compose 留喺 '+str(legacy)+'；電話資料沿用 '+str(data))
        self.instructions(cfg)

    def install(self, domain, ip, key, release=None):
        for path in [self.p.source, self.p.previous, self.p.config, self.p.data, self.p.launcher]:
            if path.exists() or path.is_symlink():
                raise ValueError('現有安裝或檔案已保留：'+str(path)+'；已安裝請用 remote-console update')
        # Do not claim or stop any existing container, including another deployment.
        result = subprocess.run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project='+PROJECT], check=True, capture_output=True, text=True)
        if result.stdout.strip():
            raise ValueError('已有同名 Docker 專案，現有服務已保留')
        for kind, ports in [(socket.SOCK_STREAM, [7000, 3478, 18079, 18081]), (socket.SOCK_DGRAM, [3478])]:
            for port in ports:
                with socket.socket(socket.AF_INET, kind) as sock:
                    sock.bind(('0.0.0.0', port))
        if not ipaddress.ip_address(ip).is_global or ':' in ip:
            raise ValueError('請輸入公網 IPv4')
        if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?', domain) or '.' not in domain:
            raise ValueError('請輸入域名，不含 https:// 或路徑')
        stage, version = self.stage(release)
        try:
            self.p.data.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run([sys.executable, str(stage/'deploy/remote/initialize.py'), '--domain', domain, '--public-ip', ip,
                            '--owner-key', str(key.resolve()), '--output', str(self.p.data), '--compose', str(stage/'initial.generated.json')], check=True)
            shutil.move(str(stage/'initial.generated.json'), self.p.data/'compose.devices.generated.json')
            mark(self.p.data)
            stage.rename(self.p.source)
        except BaseException:
            # Failed initialization remains private for inspection; never erase unknown files.
            if stage.exists():
                remove_owned(stage)
            raise
        cfg = dict(managed_by=PROJECT, schema=1, domain=domain, public_ip=ip, **version)
        write_json(self.p.config, cfg)
        self.install_launcher()
        try:
            self.compose('config', '--quiet')
            self.compose('up', '-d', '--build')
            if not self.healthy():
                raise RuntimeError('本機服務未通過健康檢查')
        except BaseException:
            print('安裝設定已保存；用 remote-console logs 檢查，再用 remote-console restart 重試。', file=sys.stderr)
            raise
        self.instructions(cfg)

    def instructions(self, cfg):
        print('\n入口：https://'+cfg['domain']+'/control')
        print('用現有 HTTPS 反代 → http://127.0.0.1:18079；保留 Host / Origin 及 WebSocket。無需 Caddy。')
        print('放行 TCP 7000、TCP/UDP 3478、UDP 49160–49260；後端 18079/18081 不對外開放。')
        print('電話設定網址：https://'+cfg['domain'])
        if cfg.get('layout') == 'legacy-v0.1.14':
            print('現有電話沿用原設定，毋須重新配對、改網址或重開電話。')
        else:
            print('將此 HTTPS 網址寫入受控電話 Root 工作目錄 enrollment-url，然後重啟電話受控服務。')
        print('首次控制端請喺電話批准一次，之後免 PIN；電話可撤銷。')
        print('管理選單：remote-console')

    def update(self, release=None):
        cfg = self.config()
        stage, version = self.stage(release)
        if cfg['commit'] == version['commit']:
            remove_owned(stage); print('已經係 '+version['release']); return
        try:
            self.backup()
            self.compose('config', '--quiet', source=stage)
            # Build before switching: existing running containers remain available.
            self.compose('build', '--pull', source=stage)
        except BaseException:
            remove_owned(stage)
            raise
        if self.p.previous.exists():
            remove_owned(self.p.previous)
        self.p.source.rename(self.p.previous)
        stage.rename(self.p.source)
        try:
            self.compose('up', '-d', '--remove-orphans')
            if not self.healthy():
                raise RuntimeError('新版服務未通過健康檢查')
        except BaseException:
            failed = self.p.source
            recovery = Path(str(stage)+'-failed')
            failed.rename(recovery)
            self.p.previous.rename(self.p.source)
            try:
                self.compose('up', '-d', '--build', '--remove-orphans')
                if not self.healthy():
                    raise RuntimeError('舊版服務亦未通過健康檢查，請檢查 logs')
            finally:
                remove_owned(recovery)
            raise RuntimeError('新版啟動失敗，已恢復舊版程式；電話與控制端憑證保持原有資料')
        write_json(self.p.config, dict(cfg, **version, previous_release=cfg['release'], previous_commit=cfg['commit']))
        print('已更新至 '+version['release']+'；電話配對與控制端授權已保留。')

    def rollback(self):
        cfg = self.config()
        if not owned(self.p.previous):
            raise ValueError('冇可還原嘅上一版')
        self.backup()
        stage = Path(str(self.p.source)+'-rollback')
        if stage.exists():
            raise ValueError('還原暫存目錄已存在，請先檢查')
        self.p.source.rename(stage); self.p.previous.rename(self.p.source); stage.rename(self.p.previous)
        try:
            self.compose('up', '-d', '--build', '--remove-orphans')
            if not self.healthy():
                raise RuntimeError('上一版服務未通過健康檢查')
        except BaseException:
            self.p.source.rename(stage); self.p.previous.rename(self.p.source); stage.rename(self.p.previous)
            self.compose('up', '-d', '--build', '--remove-orphans')
            raise
        version = json.loads((self.p.source/MARKER).read_text())
        write_json(self.p.config, dict(cfg, release=version['release'], commit=version['commit']))
        print('已還原至 '+version['release']+'；保留目前授權資料。')

    def add_device(self, ident, name, key):
        self.config(); self.backup()
        subprocess.run([sys.executable, str(self.p.source/'deploy/remote/register_device.py'), '--private', str(self.p.data),
                        '--primary-key', str(self.p.data/'enrollment.pub'), '--id', ident, '--name', name, '--public-key', str(key.resolve()),
                        '--compose', str(self.p.data/'compose.devices.generated.json')], check=True)
        self.compose('up', '-d', '--build', '--remove-orphans')

    def uninstall(self, purge=False):
        self.config()
        # Validate every deletion target before stopping containers or changing any files.
        for path in [self.p.source, self.p.data, self.p.previous]:
            if path.exists() and not owned(path):
                raise ValueError('未知目錄已保留：'+str(path))
        if self.p.launcher.is_symlink() or (self.p.launcher.exists() and 'managed by '+PROJECT not in self.p.launcher.read_text()):
            raise ValueError('未知管理指令已保留')
        self.backup()
        self.compose('down', '--remove-orphans')
        if not purge:
            print('服務已解除安裝；資料、程式及管理指令已保留，可用 remote-console start 重新啟用。')
            return
        remove_owned(self.p.source)
        if self.p.previous.exists():
            remove_owned(self.p.previous)
        remove_owned(self.p.data)
        self.p.config.unlink()
        self.p.launcher.unlink(missing_ok=True)
        print('已移除本工具嘅程式及資料。獨立備份保留於 '+str(self.p.backups)+'；Docker 及其他服務不受影響。')


def ask(prompt, default=None):
    value = input(prompt+(' ['+default+']' if default else '')+'：').strip()
    return value or default or ''


def key_input():
    answer = ask('電話 RSA 公鑰檔案路徑（或貼上 PEM 第一行，再貼餘下內容）')
    if answer == '-----BEGIN PUBLIC KEY-----':
        lines = [answer]
        while lines[-1] != '-----END PUBLIC KEY-----':
            lines.append(input().strip())
            if len(lines) > 40:
                raise ValueError('公鑰格式過長')
        handle = tempfile.NamedTemporaryFile(prefix='remote-console-public-', delete=False)
        handle.write(('\n'.join(lines)+'\n').encode()); handle.close()
        return Path(handle.name), True
    return Path(answer).expanduser(), False


def wizard(manager, args):
    print('RemoteConsole · 新獨立部署（需要自己嘅域名、HTTPS 反代及電話公鑰）')
    domain = args.domain or ask('HTTPS 域名，例如 control.example.com').lower()
    ip = args.public_ip or ask('此主機公網 IPv4')
    key, temporary = (args.owner_key, False) if args.owner_key else key_input()
    try:
        manager.install(domain, ip, key, args.release)
    finally:
        if temporary:
            key.unlink(missing_ok=True)


@contextlib.contextmanager
def lock(path):
    import fcntl
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    with path.open('a') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def main():
    parser = argparse.ArgumentParser(description='RemoteConsole 管理選單')
    parser.add_argument('command', nargs='?', choices=['install', 'migrate', 'update', 'status', 'start', 'stop', 'restart', 'logs', 'add-device', 'backup', 'rollback', 'uninstall', 'info'])
    parser.add_argument('--release', help='指定伺服器穩定版本；預設最新版本')
    parser.add_argument('--domain'); parser.add_argument('--public-ip'); parser.add_argument('--owner-key', type=Path)
    parser.add_argument('--id'); parser.add_argument('--name'); parser.add_argument('--public-key', type=Path)
    parser.add_argument('--purge', action='store_true', help='明確刪除本工具程式及資料（仍保留備份）')
    args = parser.parse_args()
    if os.name != 'posix' or os.geteuid() != 0:
        raise SystemExit('請喺 Linux 以 root 或 sudo 執行。')
    os.umask(0o077)
    manager = Manager()
    # curl | bash also works: prompts must read the terminal rather than script input.
    if not sys.stdin.isatty() and (not args.command or args.command == 'install' and not all([args.domain, args.public_ip, args.owner_key])):
        sys.stdin = open('/dev/tty')
    def execute(command):
        if command == 'install': wizard(manager, args)
        elif command == 'migrate': manager.migrate(args.release)
        elif command == 'update': manager.update(args.release)
        elif command == 'backup': manager.backup()
        elif command == 'rollback': manager.rollback()
        elif command == 'uninstall':
            if not args.purge or ask('輸入 DELETE 確認刪除程式及資料（備份保留）') == 'DELETE':
                manager.uninstall(args.purge)
        elif command == 'add-device':
            ident = args.id or ask('新電話 ID（英文字母開頭）')
            name = args.name or ask('電話顯示名稱')
            key, temporary = (args.public_key, False) if args.public_key else key_input()
            try: manager.add_device(ident, name, key)
            finally:
                if temporary: key.unlink(missing_ok=True)
        else:
            cfg = manager.config()
            if command == 'info': manager.instructions(cfg)
            elif command == 'status':
                print('版本：'+cfg['release']); manager.compose('ps'); manager.instructions(cfg)
            elif command == 'logs': manager.compose('logs', '--tail', '150', '-f')
            elif command == 'stop': manager.compose('stop')
            elif command in ['start', 'restart']: manager.compose('up', '-d', '--build', '--remove-orphans')
    try:
        with lock(manager.p.config.parent/'manager.lock'):
            if args.command:
                execute(args.command); return
            choices = ['install', 'status', 'start', 'stop', 'restart', 'logs', 'update', 'add-device', 'backup', 'rollback', 'uninstall', 'info']
            labels = ['安裝', '狀態', '啟動', '停止', '重新啟動', '即時日誌（Ctrl+C 返回）', '更新（保留配對及授權）', '新增電話', '備份', '還原上一版程式', '解除安裝（保留資料）', '連線／反代設定']
            while True:
                print('\nRemoteConsole · 遙控台管理\n'+'\n'.join(f'{n}. {label}' for n, label in enumerate(labels, 1))+'\n0. 離開')
                value = ask('選擇')
                if value == '0': break
                if not value.isdigit() or not 1 <= int(value) <= len(choices): continue
                try: execute(choices[int(value)-1])
                except KeyboardInterrupt: print('\n已返回選單。')
                except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error: print('未完成：'+str(error), file=sys.stderr)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        raise SystemExit('未完成：'+str(error))


if __name__ == '__main__':
    main()
