"""Synthetic v0.1.14 acceptance. Never contact an existing deployment or phone."""
import hashlib
import json
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
import urllib.request
import manage

root = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='remote-console-legacy-docker-') as temp:
    base = Path(temp).resolve()
    paths = manage.Paths(base)
    legacy = paths.legacy
    legacy.mkdir(parents=True)
    for name in ['gateway', 'gateway.Dockerfile', 'frps.Dockerfile', '.dockerignore']:
        source, target = root/'deploy/remote'/name, legacy/name
        if source.is_dir(): shutil.copytree(source, target)
        else: shutil.copy2(source, target)
    (legacy/'installer.version').write_text('v0.1.14\n')
    public = legacy/'owner-enrollment.pub'
    def create_key(name, target):
        key = base/(name+'.key')
        subprocess.run(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:3072', '-out', str(key)], check=True, capture_output=True)
        subprocess.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-out', str(target)], check=True, capture_output=True)
        return target
    create_key('first', public)
    second = create_key('second', base/'second.pub')
    third = create_key('third', base/'third.pub')
    data = legacy/'private'
    extra = legacy/'compose.devices.json'
    subprocess.run([sys.executable, str(root/'deploy/remote/initialize.py'), '--domain', 'control.example.com',
                    '--public-ip', '93.184.216.34', '--owner-key', str(public), '--output', str(data), '--compose', str(extra)], check=True)
    subprocess.run([sys.executable, str(root/'deploy/remote/register_device.py'), '--id', 'existing-second', '--name', 'Synthetic second phone',
                    '--public-key', str(second), '--private', str(data), '--primary-key', str(public), '--compose', str(extra)], check=True)
    registry = json.loads((data/'devices.json').read_text())
    registry['default_device'] = 'existing-first'
    registry['devices'][0].update(id='existing-first', key='/run/owner-enrollment.pub')
    manage.write_json(data/'devices.json', registry)
    # These paths belong to receiver installations and must never be rewritten by migration.
    for path in data.rglob('phone-frpc.toml'):
        path.write_text(path.read_text().replace('/data/local/tmp/remote-console/', '/data/local/tmp/old-receiver/'))
    document = manage.compose_document(root, data, 'remote-console', registry)
    document['services']['gateway']['volumes'][-1] = str(public)+':/run/owner-enrollment.pub:ro'
    for service in document['services'].values():
        if 'build' in service: service['build']['context'] = str(legacy)
    manage.write_json(legacy/'compose.yml', document)
    (data/'enrollment.pub').unlink()
    ident, secret = secrets.token_urlsafe(16), secrets.token_urlsafe(32)
    manage.write_json(data/'controller-trust/controllers.json', {'version': 1, 'controllers': [{
        'id': ident, 'name': 'Trusted before upgrade', 'platform': 'native', 'created_at': 1,
        'last_used_at': 1, 'credential_hash': hashlib.sha256(secret.encode()).hexdigest()}]})
    def old_compose(*args):
        subprocess.run(['docker', 'compose', '-p', 'remote-console', '-f', str(legacy/'compose.yml'), *args], cwd=legacy, check=True)
    def authenticated():
        request = urllib.request.Request('http://127.0.0.1:18079/_remoteconsole/trust/session',
                                         data=json.dumps({'credential': ident+'.'+secret}).encode(), headers={'Content-Type': 'application/json'})
        with urllib.request.urlopen(request, timeout=10) as response:
            assert json.load(response)['authenticated'] is True
    subprocess.run([sys.executable, str(root/'scripts/package_server.py'), '--output', str(base)], check=True)
    archive = next(base.glob('remote-console-server-*.tar.gz'))
    def download(destination, release):
        manage.extract_source(archive, destination)
        value = {'release': release or 'v0.2.1', 'commit': ('a' if release != 'v0.2.2' else 'b')*40}
        manage.mark(destination, **value)
        return value
    manager = manage.Manager(manage.Paths(base), download)
    try:
        old_compose('up', '-d', '--build')
        for attempt in range(30):
            try: authenticated(); break
            except OSError:
                if attempt == 29: raise
                import time
                time.sleep(1)
        original = manage.identity_snapshot(data)
        # A failed first attempt must restore the original service and leave its credential valid.
        real_health = manager.healthy
        manager.healthy = lambda: False
        try:
            manager.migrate('v0.2.1')
            raise AssertionError('Expected the injected health failure')
        except RuntimeError as error:
            assert '已恢復原有服務' in str(error), error
        authenticated()
        assert manage.identity_snapshot(data) == original
        assert not paths.config.exists() and not paths.source.exists()
        manager.healthy = real_health
        manager.migrate('v0.2.1')
        authenticated()
        assert manage.identity_snapshot(data) == original
        assert json.loads((data/'devices.json').read_text()) == registry
        # A new CLI process must still target the original project and in-place data.
        manager = manage.Manager(manage.Paths(base), download)
        assert manager.project == 'remote-console' and manager.p.data == data
        manager.update('v0.2.2')
        authenticated()
        manager.rollback()
        authenticated()
        assert manage.identity_snapshot(data) == original
        manager.add_device('new-third', 'Synthetic new phone', third)
        assert manager.healthy()
        authenticated()
        with urllib.request.urlopen('http://127.0.0.1:18079/_remoteconsole/devices') as response:
            listing = json.load(response)
            assert listing['default_device'] == 'existing-first'
            assert {phone['id'] for phone in listing['devices']} == {'existing-first', 'existing-second', 'new-third'}
        assert json.loads((data/'devices.json').read_text())['default_device'] == 'existing-first'
        for relative, digest in original.items():
            if relative != 'devices.json': assert manage.identity_snapshot(data)[relative] == digest
        manager.uninstall()
        assert data.exists()
        print('PASS: failed migration recovery, original credential login, stable phone IDs/config, update/rollback, add-phone, retained uninstall')
    finally:
        old_compose('logs', '--tail', '20')
        old_compose('down', '--remove-orphans')
