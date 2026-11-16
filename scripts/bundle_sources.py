#!/usr/bin/env python3
"""Attach public corresponding sources, notices and exact build provenance."""
from pathlib import Path
import argparse, hashlib, io, json, shutil, subprocess, tarfile

ROOT = Path(__file__).resolve().parents[1]

def git(directory, *args):
    return subprocess.check_output(['git', '-C', str(directory), *args])

def add_repository(archive, directory, prefix, revision, dependency=False):
    data = git(directory, 'archive', '--format=tar', '--prefix='+prefix+'/', revision)
    with tarfile.open(fileobj=io.BytesIO(data)) as source:
        for member in source:
            if dependency:
                parts = Path(member.name).parts
                if any(part in {'tests', 'test', 'testdata', 'benchmarks', 'prebuilt', 'prebuilts', 'fuzz', 'fuzzers'} for part in parts): continue
                if Path(member.name).suffix.lower() in {'.apk', '.so', '.dll', '.exe', '.o', '.a', '.zip',
                    '.pem', '.pk8', '.p12', '.pfx', '.key', '.jks', '.keystore', '.avbpubkey'}: continue
            archive.addfile(member, source.extractfile(member) if member.isfile() else None)
    # git archive omits submodule contents. Include their exact locked sources.
    for entry in git(directory, 'ls-tree', '-r', '-z', revision).split(b'\0'):
        if not entry: continue
        metadata, path = entry.split(b'\t', 1)
        mode, _, commit = metadata.decode().split()
        if mode == '160000':
            name = path.decode()
            add_repository(archive, directory/name, prefix+'/'+name, commit, dependency)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('bundle', type=Path, nargs='?', default=ROOT/'build/bundle')
    parser.add_argument('--msys', type=Path)
    args = parser.parse_args()
    bundle = args.bundle.resolve()
    revision = git(ROOT, 'rev-parse', 'HEAD').decode().strip()
    licenses = bundle/'licenses'
    licenses.mkdir(parents=True, exist_ok=True)
    for relative in ['LICENSE', 'THIRD_PARTY.md', 'codec/LICENSE.Apple-ALAC',
                     'audio/LICENSE.miniaudio', 'third_party/LICENSE.mp4ff',
                     'third_party/LICENSE.Adam']:
        shutil.copy2(ROOT/relative, licenses/Path(relative).name)
    for path in (ROOT/'third_party/libhybris-win').glob('LICENSE.*'):
        shutil.copy2(path, licenses/path.name)
    shutil.copytree(ROOT/'third_party/macos-notices', licenses/'macos', dirs_exist_ok=True)
    if args.msys:
        upstream = args.msys/'ucrt64/share/licenses'
        matches = [p for p in upstream.iterdir() if any(name in p.name.lower() for name in ['gcc', 'stdc++', 'winpthread'])]
        if not matches: raise RuntimeError('Missing GCC/MinGW runtime notices')
        for path in matches:
            if path.is_dir(): shutil.copytree(path, licenses/'windows'/path.name, dirs_exist_ok=True)
            else:
                (licenses/'windows').mkdir(exist_ok=True)
                shutil.copy2(path, licenses/'windows'/path.name)
    dependencies = {}
    with tarfile.open(bundle/'corresponding-source.tar.gz', 'w:gz') as archive:
        add_repository(archive, ROOT, 'apple-music-native-core', revision)
        for name, info in json.loads((ROOT/'native/macos/dependencies-lock.json').read_text()).items():
            directory = ROOT/'native'/name
            if not directory.exists(): continue
            actual = git(directory, 'rev-parse', 'HEAD').decode().strip()
            if actual != info['revision']: raise RuntimeError('Unexpected dependency revision: '+name)
            add_repository(archive, directory, 'dependency-sources/'+name, actual, True)
            dependencies[name] = actual
    for name in ['THIRD_PARTY.md', 'LICENSE']:
        shutil.copy2(ROOT/name, bundle/name)
    files = {str(p.relative_to(bundle)).replace('\\', '/'):hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(bundle.rglob('*')) if p.is_file() and p.name != 'build-info.json'}
    dirty = bool(git(ROOT, 'diff', 'HEAD', '--name-only').strip())
    (bundle/'build-info.json').write_text(json.dumps({'repository':'https://github.com/InkCitrus/apple-music-native-core',
        'commit':revision, 'sourceDirty':dirty, 'protocol':1, 'dependencySources':dependencies,
        'dependencySourceFilter':'upstream tests, benchmarks, prebuilt binaries and unused signing material omitted', 'sha256':files}, indent=2)+'\n')
    print('Attached corresponding public source, license texts and build commit '+revision)

if __name__ == '__main__': main()
