#!/usr/bin/env python3
"""Compile only the pinned libxaac decoder with the core's native C toolchain."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import argparse
import hashlib
import json
import os
import re
import shlex
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / 'build/aac'
SOURCE = BUILD / 'src'
LOCK = ROOT / 'third_party/aac-lock.json'
LIB = BUILD / 'libxaacdec.a'


def go_environment(env):
    identity = (BUILD / 'build-id').read_text()
    if not LIB.is_file() or not re.fullmatch(r'[a-f0-9]{64}', identity):
        raise RuntimeError('Build the AAC decoder before compiling the core')
    # cgo does not include external static libraries when hashing its cache.
    flags = env.get('CGO_CFLAGS', '-O2 -g')
    return {**env, 'CGO_CFLAGS': f'{flags} -DAMCORE_AAC_BUILD_ID=0x{identity[:16]}'}


def checked_source(local=None):
    lock = json.loads(LOCK.read_text())
    revision = lock['revision']
    if not re.fullmatch(r'[a-f0-9]{40}', revision):
        raise RuntimeError('Invalid libxaac revision')
    if not SOURCE.exists():
        BUILD.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='sources-', dir=BUILD) as temp:
            repo = Path(temp) / 'src'
            if local:
                subprocess.run(['git', 'clone', '--no-hardlinks', '--no-tags', str(local.resolve()), str(repo)], check=True)
            else:
                subprocess.run(['git', 'init', '-q', str(repo)], check=True)
                subprocess.run(['git', '-C', str(repo), 'fetch', '-q', '--depth=1', '--no-tags', lock['repository'], revision], check=True)
            subprocess.run(['git', '-C', str(repo), 'checkout', '-q', '--detach', revision], check=True)
            repo.rename(SOURCE)
    def git(*args):
        return subprocess.check_output(['git', '-C', str(SOURCE), *args], text=True).strip()
    if git('rev-parse', 'HEAD') != revision or git('status', '--porcelain', '--untracked-files=all'):
        raise RuntimeError('libxaac sources must be the clean pinned revision')
    return revision


def build(local=None):
    revision = checked_source(local)
    cc = shlex.split(os.environ.get('CC', 'cc'))
    ar = shlex.split(os.environ.get('AR', 'ar'))
    version = subprocess.check_output(cc + ['--version'])
    stamp = BUILD / 'build-id'
    identity = hashlib.sha256(revision.encode() + version + repr((cc, ar)).encode() + Path(__file__).read_bytes()).hexdigest()
    if LIB.is_file() and stamp.is_file() and stamp.read_text() == identity:
        return
    # Upstream's x86 recipe uses the portable C selectors, with no assembly.
    recipes = ['common/common.cmake', 'decoder/libxaacdec.cmake', 'decoder/drc_src/libxaacdec_drc.cmake', 'decoder/x86/libxaacdec_x86.cmake']
    srcs = [name for recipe in recipes for name in re.findall(r'"\$\{XAAC_ROOT\}/([\w/]+\.c)"', (SOURCE / recipe).read_text())]
    if len(srcs) < 100 or len(srcs) != len(set(srcs)):
        raise RuntimeError('Unexpected libxaac source set')
    flags = ['-O2', '-std=gnu99', '-fwrapv', '-U__ARM_NEON__']
    if os.name != 'nt':
        flags += ['-fPIC']
    includes = [f'-I{SOURCE / name}' for name in ['common', 'decoder', 'decoder/drc_src']]
    with tempfile.TemporaryDirectory(prefix='objects-', dir=BUILD) as temp:
        def compile_one(pair):
            index, name = pair
            obj = Path(temp) / f'{index}.o'
            subprocess.run(cc + flags + includes + ['-c', str(SOURCE / name), '-o', str(obj)], check=True)
            return str(obj)
        with ThreadPoolExecutor(max_workers=min(8, os.cpu_count() or 1)) as pool:
            objects = list(pool.map(compile_one, enumerate(srcs)))
        binary = Path(temp) / LIB.name
        subprocess.run(ar + ['rcs', str(binary), *objects], check=True)
        binary.replace(LIB)
    stamp.write_text(identity)
    print(f'Built decoder-only libxaac {revision[:12]} ({LIB.stat().st_size} bytes)')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, help='reuse a local checkout containing the pinned revision')
    parser.add_argument('--test', action='store_true', help='run the race-enabled Go suite with AAC support')
    args = parser.parse_args()
    build(args.source)
    if args.test:
        subprocess.run(['go', 'test', '-race', '-tags', 'amcore_aac', './...'], cwd=ROOT, env=go_environment(os.environ), check=True)
