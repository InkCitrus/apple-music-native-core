#!/usr/bin/env python3
"""Fetch pinned build sources and checksum-verified trial runtime into this folder."""
from pathlib import Path
import sys
import concurrent.futures, hashlib, json, subprocess, urllib.request, zipfile
here = Path(__file__).resolve().parent
for folder, item in json.loads((here/'dependencies-lock.json').read_text()).items():
    dest = here.parent/folder
    if dest.exists():
        actual = subprocess.check_output(['git','-C',str(dest),'rev-parse','HEAD'],text=True).strip()
        if actual != item['revision']: raise RuntimeError(f'{folder}: unexpected source revision')
    else:
        subprocess.run(['git','init',str(dest)],check=True,stdout=subprocess.DEVNULL)
        subprocess.run(['git','-C',str(dest),'remote','add','origin','https://github.com/'+item['repository']+'.git'],check=True)
        subprocess.run(['git','-C',str(dest),'fetch','--depth','1','origin',item['revision']],check=True)
        subprocess.run(['git','-C',str(dest),'checkout','--detach','FETCH_HEAD'],check=True)
    if folder == 'android-linker': subprocess.run(['git','-C',str(dest),'submodule','update','--init','--depth','1'],check=True)
if '--sources-only' in sys.argv: sys.exit(0)
lock = json.loads((here/'runtime-lock.json').read_text())
runtime = here/'android-arm64'; runtime.mkdir(exist_ok=True)
def fetch(url):
    with urllib.request.urlopen(url,timeout=90) as r:
        data = r.read(70*1024*1024+1)
    if len(data)>70*1024*1024: raise RuntimeError('oversized runtime download')
    return data
def library(item):
    p=runtime/item['name']
    if p.exists() and hashlib.sha256(p.read_bytes()).hexdigest()==item['sha256']: return
    data=fetch('https://raw.githubusercontent.com/WorldObservationLog/wrapper/'+lock['sourceCommit']+'/rootfs/system/lib64/'+item['name'])
    if hashlib.sha256(data).hexdigest()!=item['sha256']: raise RuntimeError('runtime checksum mismatch: '+item['name'])
    p.write_bytes(data)
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool: list(pool.map(library,lock['libraries']))
p=runtime/'wrapper-main'; expected=lock['wrapper']['sha256']
if not p.exists() or hashlib.sha256(p.read_bytes()).hexdigest()!=expected:
    import io
    with zipfile.ZipFile(io.BytesIO(fetch(lock['wrapper']['source']))) as z:
        names=[n for n in z.namelist() if n=='main' or n.endswith('/main')]
        if len(names)!=1: raise RuntimeError('upstream core archive layout changed')
        data=z.read(names[0])
    if hashlib.sha256(data).hexdigest()!=expected: raise RuntimeError('upstream core changed; offsets must be revalidated')
    p.write_bytes(data)
print('Pinned sources and runtime verified. Account sessions are never downloaded.')
