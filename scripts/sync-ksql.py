#!/usr/bin/env python3
"""Refresh the portable compiler snapshot from the companion source repository."""
import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('source', nargs='?', default='../ksql')
args = parser.parse_args()
source = pathlib.Path(args.source).resolve()
project = pathlib.Path(__file__).resolve().parents[1]
target = project / 'third_party/ksql'
if source == target or not (source / 'go.mod').is_file():
    raise SystemExit('Provide the companion KSQL repository directory.')
if 'module github.com/kawijayaa/ksql' not in (source / 'go.mod').read_text():
    raise SystemExit('Source module is not github.com/kawijayaa/ksql.')
paths = set(subprocess.check_output(['git', '-C', str(source), 'ls-files', '-z'], text=True).split('\0'))
# Include new source/tests that have not yet been committed in the companion repo.
paths.update(str(path.relative_to(source)) for path in source.rglob('*.go') if '.git' not in path.parts)
paths = sorted(path for path in paths if path and not path.startswith('.') and (source/path).is_file())
previous = target / 'SNAPSHOT.json'
if previous.exists():
    # Delete only files recorded as belonging to the previous generated snapshot.
    for path in json.loads(previous.read_text())['files']:
        relative = pathlib.PurePosixPath(path)
        if relative.is_absolute() or '..' in relative.parts:
            raise SystemExit('Unsafe path in existing snapshot manifest.')
        if path not in paths:
            (target/path).unlink(missing_ok=True)
files = {}
for path in paths:
    destination = target/path
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source/path, destination)
    files[path] = hashlib.sha256(destination.read_bytes()).hexdigest()
manifest = {'repository': 'https://github.com/kawijayaa/ksql',
            'baseCommit': subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip(),
            'includesUncommittedChanges': bool(subprocess.check_output(['git', '-C', str(source), 'status', '--porcelain'], text=True).strip()),
            'files': files}
previous.write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
print(f'Synced {len(files)} files into {target}')
