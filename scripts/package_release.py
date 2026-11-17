#!/usr/bin/env python3
"""Create release ZIPs on the same runner that compiled the native bundle."""
from pathlib import Path
import argparse, hashlib, json, plistlib, re, stat, subprocess, zipfile

ROOT=Path(__file__).resolve().parents[1]
EXECUTABLES={'amcore','amcore.exe','amcore-demo','amcore-demo.exe','native-helper','native-helper.exe','account-ui','native-core-demo-ui'}
PRIVATE_FILES={'MUSIC_TOKEN','DEVELOPER_TOKEN','STOREFRONT_ID','device-id','playback-plan.json'}

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()

def archive(source, target, prefix):
    with zipfile.ZipFile(target,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
        for path in sorted(source.rglob('*')):
            if not path.is_file(): continue
            relative=path.relative_to(source)
            if path.is_symlink() or path.name in PRIVATE_FILES or path.suffix in {'.adi','.wvd','.sqlite','.db'}:
                raise RuntimeError('Unexpected private or linked file in release input')
            info=zipfile.ZipInfo.from_file(path,prefix+'/'+relative.as_posix())
            info.external_attr=(stat.S_IFREG|(0o755 if path.name in EXECUTABLES else 0o644))<<16
            z.writestr(info,path.read_bytes(),compress_type=zipfile.ZIP_DEFLATED,compresslevel=9)
    with zipfile.ZipFile(target) as z:
        if z.testzip() is not None: raise RuntimeError('Release ZIP integrity failed')

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--platform',required=True,choices=['darwin-arm64','windows-amd64','linux-amd64'])
    args=parser.parse_args()
    version=re.search(r'const Version = "([^"]+)"',(ROOT/'service/service.go').read_text())[1]
    source=ROOT/'build/bundle';info=json.loads((source/'build-info.json').read_text())
    revision=subprocess.check_output(['git','-C',str(ROOT),'rev-parse','HEAD'],text=True).strip()
    if info['commit']!=revision or info['sourceDirty']: raise RuntimeError('Bundle is not from this clean commit')
    for name,digest in info['sha256'].items():
        if sha(source/name)!=digest: raise RuntimeError('Bundle checksum mismatch')
    output=ROOT/'build/release';output.mkdir(parents=True,exist_ok=True)
    files=[]
    target=output/f'amcore-{version}-{args.platform}.zip'
    archive(source,target,'apple-music-native-core');files.append(target)
    if args.platform=='darwin-arm64':
        demo=ROOT/'build/demo';app=demo/'Native Core Demo.app/Contents'
        if 'LSEnvironment' in plistlib.loads((app/'Info.plist').read_bytes()): raise RuntimeError('Local QA configuration in demo')
        if (app/'MacOS/amcore-demo').read_bytes()!=(source/'amcore-demo').read_bytes(): raise RuntimeError('Demo executable differs from the core build')
        subprocess.run(['codesign','--verify','--deep','--strict',str(app.parent)],check=True)
        provenance={'repository':info['repository'],'commit':revision,'version':version,
                    'sha256':{p.relative_to(demo).as_posix():sha(p) for p in sorted(demo.rglob('*')) if p.is_file() and p.name!='build-info.json'}}
        (demo/'build-info.json').write_text(json.dumps(provenance,indent=2)+'\n')
        target=output/f'native-core-demo-{version}-darwin-arm64.zip'
        archive(demo,target,'native-core-demo');files.append(target)
    (output/f'SHA256SUMS-{args.platform}.txt').write_text(''.join(sha(p)+'  '+p.name+'\n' for p in files))
    print(json.dumps({'commit':revision,'platform':args.platform,'version':version,'archives':[p.name for p in files]}))

if __name__=='__main__': main()
