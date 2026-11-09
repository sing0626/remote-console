"""Real Docker lifecycle acceptance on a disposable GitHub Linux runner only."""
import hashlib
import json
from pathlib import Path
import secrets
import subprocess
import tempfile
import urllib.request
import manage

root = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='remote-console-docker-') as temp:
    base = Path(temp).resolve()
    archive = base/'source.tar.gz'
    subprocess.run(['git', 'archive', '--format=tar.gz', '--prefix=source/', '-o', str(archive), 'HEAD'], cwd=root, check=True)
    def download(destination, release):
        manage.extract_source(archive, destination)
        value = {'release': release or 'v0.2.1', 'commit': ('a' if release == 'v0.2.0' else 'b')*40}
        manage.mark(destination, **value)
        return value
    manager = manage.Manager(manage.Paths(base), download)
    key = base/'phone.key'
    public = base/'phone.pub'
    subprocess.run(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:3072', '-out', str(key)], check=True, capture_output=True)
    subprocess.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-out', str(public)], check=True, capture_output=True)
    try:
        manager.install('control.example.com', '93.184.216.34', public, 'v0.2.0')
        # A synthetic valid trusted controller proves the store survives code replacement.
        secret = secrets.token_urlsafe(32)
        manage.write_json(manager.p.data/'controller-trust/controllers.json', {'version': 1, 'controllers': [{
            'id': secrets.token_urlsafe(16), 'name': 'Disposable CI controller', 'platform': 'fixture',
            'created_at': 1, 'last_used_at': 1, 'credential_hash': hashlib.sha256(secret.encode()).hexdigest()}]})
        before = {str(p.relative_to(manager.p.data)): p.read_bytes() for p in manager.p.data.rglob('*')
                  if p.is_file() and '.generated.' not in p.name}
        manager.update('v0.2.1')
        assert manager.config()['release'] == 'v0.2.1'
        manager.rollback()
        assert manager.config()['release'] == 'v0.2.0'
        after = {str(p.relative_to(manager.p.data)): p.read_bytes() for p in manager.p.data.rglob('*')
                 if p.is_file() and '.generated.' not in p.name}
        assert before == after, 'Identity or trust data changed during update/rollback'
        with urllib.request.urlopen('http://127.0.0.1:18079/_remoteconsole/status') as response:
            assert json.load(response)['gateway_ready'] is True
        manager.uninstall()
        assert manager.p.data.exists() and manager.p.config.exists()
        manager.compose('up', '-d', '--build')
        assert manager.healthy(), 'Retained installation could not restart'
        manager.uninstall(purge=True)
        assert not manager.p.data.exists() and list(manager.p.backups.glob('*.tar.gz'))
        print('PASS: real install / update / rollback / controller-store preservation / uninstall / restart / purge')
    finally:
        if manager.p.config.exists() and manage.owned(manager.p.data):
            manager.compose('logs', '--tail', '40')
            manager.compose('down', '--remove-orphans')
