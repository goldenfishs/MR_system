#!/usr/bin/env python3
"""Build a data-free Docker installation bundle from an explicit allowlist."""
import argparse
import hashlib
import io
from pathlib import Path
import re
import tarfile

parser = argparse.ArgumentParser()
parser.add_argument('--version', required=True)
parser.add_argument('--image', required=True, help='Published image pinned to sha256 digest')
parser.add_argument('--commit', required=True)
parser.add_argument('--output', default='dist-release')
args = parser.parse_args()
if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?', args.version):
    parser.error('version must be vMAJOR.MINOR.PATCH[-prerelease]')
if not re.fullmatch(r'ghcr\.io/[a-z0-9_.-]+/[a-z0-9_.-]+@sha256:[0-9a-f]{64}', args.image):
    parser.error('image must be a GHCR image pinned to its manifest digest')
if not re.fullmatch(r'[0-9a-f]{40}', args.commit):
    parser.error('commit must be a full Git commit SHA')
root = Path(__file__).resolve().parent.parent
out = Path(args.output).resolve()
out.mkdir(parents=True, exist_ok=True)
prefix = f'mr-system-{args.version}'
archive = out / f'{prefix}-docker.tar.gz'
# Never traverse the workspace or include .env, databases, uploads, backups, or keys.
allowed = ('compose.yaml', '.env.example', 'install.sh', 'README.md')
with tarfile.open(archive, 'w:gz') as tar:
    for name in allowed:
        content = (root / 'release' / name).read_text()
        if name in ('.env.example', 'README.md'):
            content = content.replace('@IMAGE_REFERENCE@', args.image).replace('@VERSION@', args.version)
        data = content.encode()
        info = tarfile.TarInfo(f'{prefix}/{name}')
        info.size = len(data)
        info.mode = 0o755 if name == 'install.sh' else 0o644
        info.mtime = 0
        tar.addfile(info, io.BytesIO(data))
    provenance = f'version={args.version}\ncommit={args.commit}\nimage={args.image}\n'.encode()
    info = tarfile.TarInfo(f'{prefix}/SOURCE.txt')
    info.size = len(provenance)
    info.mode = 0o644
    tar.addfile(info, io.BytesIO(provenance))
checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
(out / 'SHA256SUMS').write_text(f'{checksum}  {archive.name}\n')
print(archive)
