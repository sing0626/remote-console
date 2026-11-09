"""Isolated lifecycle checks. No root, phone, network or live Docker required."""
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
import manage
import hashlib
import http.server
import threading


class FixtureManager(manage.Manager):
    def __init__(self, root):
        super().__init__(manage.Paths(root), self.download)
        self.calls = []
        self.health = [True]
        self.fail_build = False

    def download(self, destination, release):
        release = release or 'v0.2.1'
        metadata = {'release': release, 'commit': ('a' if release == 'v0.2.0' else 'b')*40}
        (destination/'scripts').mkdir()
        (destination/'scripts/manage.py').write_text('# synthetic fixture source\n')
        manage.mark(destination, **metadata)
        return metadata

    def compose(self, *args, source=None):
        self.calls.append((args, source or self.p.source))
        if args[0] == 'build' and self.fail_build:
            raise subprocess.CalledProcessError(1, ['docker', 'compose', 'build'])

    def healthy(self):
        return self.health.pop(0) if len(self.health) > 1 else self.health[0]


class Lifecycle(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='remote-console-manager-test-')
        self.root = Path(self.tmp.name).resolve()
        self.m = FixtureManager(self.root)
        p = self.m.p
        p.source.mkdir(parents=True); self.m.download(p.source, 'v0.2.0')
        p.data.mkdir(parents=True); manage.mark(p.data)
        (p.data/'controller-trust').mkdir()
        (p.data/'controller-trust/controllers.json').write_text('{"synthetic_credential":"preserve-test-value"}')
        (p.data/'phone-frpc.toml').write_text('synthetic-random-token\n')
        manage.write_json(p.config, dict(managed_by=manage.PROJECT, schema=1, domain='control.example.com', public_ip='93.184.216.34', release='v0.2.0', commit='a'*40))
        p.launcher.parent.mkdir(parents=True)
        p.launcher.write_text('# managed by '+manage.PROJECT)
        self.original = self.snapshot()

    def tearDown(self):
        self.tmp.cleanup()

    def snapshot(self):
        return {str(p.relative_to(self.m.p.data)): p.read_bytes() for p in self.m.p.data.rglob('*') if p.is_file()}

    def test_update_and_rollback_preserve_identity_bytes(self):
        self.m.update()
        self.assertEqual(self.m.config()['release'], 'v0.2.1')
        self.assertEqual(self.snapshot(), self.original)
        self.m.rollback()
        self.assertEqual(self.m.config()['release'], 'v0.2.0')
        self.assertEqual(self.snapshot(), self.original)
        self.assertEqual(len(list(self.m.p.backups.glob('*.tar.gz'))), 2)

    def test_failed_update_restores_program_and_credentials(self):
        self.m.health = [False, True]
        with self.assertRaisesRegex(RuntimeError, '已恢復舊版'): self.m.update()
        self.assertEqual(self.m.config()['commit'], 'a'*40)
        self.assertEqual(self.snapshot(), self.original)
        self.assertTrue(any('--build' in args for args, _ in self.m.calls))

    def test_failed_build_keeps_current_program_running(self):
        self.m.fail_build = True
        with self.assertRaises(subprocess.CalledProcessError): self.m.update()
        self.assertEqual(self.m.config()['release'], 'v0.2.0')
        self.assertFalse(any(args[0] == 'up' for args, _ in self.m.calls))
        self.assertEqual(self.snapshot(), self.original)

    def test_default_uninstall_keeps_data_and_restart_command(self):
        self.m.uninstall()
        self.assertEqual(self.snapshot(), self.original)
        self.assertTrue(self.m.p.launcher.exists())
        self.m.config()
        self.assertEqual(self.m.calls[-1][0], ('down', '--remove-orphans'))

    def test_explicit_purge_preserves_backup_and_unrelated_files(self):
        unrelated = self.root/'opt/unrelated'; unrelated.mkdir(); (unrelated/'data').write_text('untouched')
        self.m.uninstall(purge=True)
        self.assertFalse(self.m.p.data.exists()); self.assertFalse(self.m.p.source.exists())
        self.assertTrue(list(self.m.p.backups.glob('*.tar.gz')))
        self.assertEqual((unrelated/'data').read_text(), 'untouched')
        self.assertFalse(self.m.p.launcher.exists())

    def test_unknown_directory_blocks_purge_before_stopping(self):
        self.m.p.previous.mkdir()
        with self.assertRaises(ValueError): self.m.uninstall(purge=True)
        self.assertEqual(self.m.calls, [])
        self.assertEqual(self.snapshot(), self.original)

    def test_backup_contains_current_controller_store(self):
        backup = self.m.backup()
        with tarfile.open(backup) as tar:
            key = str(Path('controller-trust/controllers.json'))
            self.assertEqual(tar.extractfile('data/controller-trust/controllers.json').read(), self.original[key])

    def test_repeated_update_is_noop(self):
        self.m.update('v0.2.0')
        self.assertEqual(self.m.calls, [])
        self.assertEqual(self.snapshot(), self.original)

    def test_download_failure_cannot_switch_an_existing_deployment(self):
        def failed_download(*args): raise ValueError('Synthetic download interruption')
        self.m.downloader = failed_download
        with self.assertRaises(ValueError): self.m.update()
        self.assertEqual(self.m.calls, [])
        self.assertEqual(self.snapshot(), self.original)
        self.assertEqual(self.m.config()['release'], 'v0.2.0')


class Archive(unittest.TestCase):
    def test_reject_traversal_and_links(self):
        with tempfile.TemporaryDirectory(prefix='remote-console-archive-test-') as temp:
            base = Path(temp).resolve()
            for name, kind in [('root/../../escape', tarfile.REGTYPE), ('root/link', tarfile.SYMTYPE), ('/absolute', tarfile.REGTYPE)]:
                archive = base/'bad.tar.gz'
                with tarfile.open(archive, 'w:gz') as tar:
                    entry = tarfile.TarInfo(name); entry.type = kind; entry.linkname = '/tmp/outside'
                    tar.addfile(entry, io.BytesIO(b''))
                with self.assertRaises(ValueError): manage.extract_source(archive, base/'out')

    def test_secondary_phone_compose_uses_external_data_and_staged_source(self):
        with tempfile.TemporaryDirectory(prefix='remote-console-compose-test-') as temp:
            data = Path(temp).resolve()
            manage.write_json(data/'devices.json', {'default_device': 'primary', 'devices': [{'id': 'primary', 'key': '/private/enrollment.pub'}, {'id': 'secondary'}]})
            manage.write_json(data/'compose.devices.generated.json', {'services': {}})
            source = data/'staged-source'
            doc = manage.compose_document(source, data)
            self.assertEqual(doc['name'], manage.PROJECT)
            self.assertEqual(doc['services']['signaling-secondary']['build']['context'], str(source/'deploy/remote'))
            self.assertIn(str(data)+'/controller-trust:/state', doc['services']['gateway']['volumes'])

    def test_legacy_default_id_and_key_mount_are_preserved(self):
        with tempfile.TemporaryDirectory(prefix='remote-console-legacy-compose-') as temp:
            data = Path(temp).resolve()
            registry = {'default_device': 'original-host', 'devices': [
                {'id': 'original-host', 'key': '/run/owner-enrollment.pub'}, {'id': 'second-host'}]}
            doc = manage.compose_document(data/'source', data, 'remote-console', registry)
            self.assertEqual(doc['name'], 'remote-console')
            self.assertIn('signaling-second-host', doc['services'])
            self.assertNotIn('signaling-original-host', doc['services'])
            self.assertIn(str(data)+'/enrollment.pub:/run/owner-enrollment.pub:ro', doc['services']['gateway']['volumes'])

    def test_reload_adopted_installation_keeps_legacy_data_and_project(self):
        with tempfile.TemporaryDirectory(prefix='remote-console-legacy-config-') as temp:
            paths = manage.Paths(Path(temp).resolve())
            manage.write_json(paths.config, {'managed_by': manage.PROJECT, 'layout': 'legacy-v0.1.14'})
            manager = manage.Manager(paths)
            self.assertEqual(manager.p.data, paths.legacy/'private')
            self.assertEqual(manager.project, 'remote-console')


class Downloads(unittest.TestCase):
    def test_interrupted_transfer_retries_and_digest_rejects_corruption(self):
        payload = b'complete generic server fixture\n'*128
        class Handler(http.server.BaseHTTPRequestHandler):
            requests = 0
            def do_GET(self):
                Handler.requests += 1
                self.send_response(200); self.send_header('Content-Length', str(len(payload))); self.end_headers()
                self.wfile.write(payload[:13] if Handler.requests == 1 else payload)
                self.close_connection = True
            def log_message(self, *args): pass
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            with tempfile.TemporaryDirectory(prefix='remote-console-download-test-') as temp:
                target = Path(temp).resolve()/'source.tar.gz'
                url = f'http://127.0.0.1:{server.server_port}/fixture'
                manage.download_archive(url, target, 'sha256:'+hashlib.sha256(payload).hexdigest())
                self.assertGreaterEqual(Handler.requests, 2)
                self.assertEqual(target.read_bytes(), payload)
                with self.assertRaisesRegex(ValueError, 'SHA-256'):
                    manage.download_archive(url, target, 'sha256:'+'0'*64)
        finally:
            server.shutdown(); server.server_close(); thread.join(timeout=2)


if __name__ == '__main__': unittest.main()
