"""Package only generic VPS runtime sources, not mobile SDKs, fonts or credentials."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import subprocess
import tarfile

root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--output', type=Path, default=root/'dist')
args = p.parse_args()
release = json.loads((root/'release.json').read_text())
version = 'v'+release['version']
tracked = subprocess.check_output(['git', '-c', 'safe.directory='+str(root), 'ls-files', '-z'], cwd=root).decode().split('\0')
allowed = {'scripts/manage.py', 'release.json', 'install.sh', 'LICENSE', 'NOTICE.md', 'README.md'}
files = sorted(name for name in tracked if name and (name in allowed or name.startswith('deploy/remote/')))
for name in files:
    path = Path(name)
    if 'private' in path.parts or '.generated.' in name or path.suffix in {'.pem', '.key', '.pub', '.crt', '.p12', '.apk', '.ipa', '.log'} or (root/path).is_symlink():
        raise SystemExit('Runtime or private file rejected: '+name)
args.output.mkdir(parents=True, exist_ok=True)
target = args.output/f'remote-console-server-{version}.tar.gz'
with target.open('wb') as output, gzip.GzipFile(filename='', mode='wb', fileobj=output, mtime=0) as compressed, tarfile.open(fileobj=compressed, mode='w') as archive:
    for name in files:
        path = root/name
        info = archive.gettarinfo(str(path), arcname=f'remote-console-{version}/'+name)
        info.uid = info.gid = info.mtime = 0; info.uname = info.gname = ''
        with path.open('rb') as source: archive.addfile(info, source)
digest = hashlib.sha256(target.read_bytes()).hexdigest()
target.with_suffix('.sha256').write_text(digest+'  '+target.name+'\n')
print(f'Generic server bundle: {target.name}; {target.stat().st_size} bytes; sha256:{digest}')
