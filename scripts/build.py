#!/usr/bin/env python3
"""Build a native bundle on its target OS; no account or SDK files are needed."""
from pathlib import Path
import argparse, hashlib, json, os, platform, shutil, subprocess, sys
ROOT=Path(__file__).resolve().parents[1]
def run(args,**kwargs): subprocess.run([str(s) for s in args],cwd=ROOT,check=True,**kwargs)
def main():
    p=argparse.ArgumentParser();p.add_argument('--msys',default=r'C:\msys64');p.add_argument('--deps',type=Path,help='reuse an existing checkout of pinned macOS dependencies');args=p.parse_args()
    oskey={'darwin':'darwin','win32':'windows','linux':'linux'}.get(sys.platform)
    arch='arm64' if platform.machine().lower() in ['arm64','aarch64'] else 'amd64'
    if (oskey,arch) not in [('darwin','arm64'),('windows','amd64'),('linux','amd64')]:raise SystemExit('Supported native builds: macOS ARM64, Windows x64, Linux x64')
    key=f'{oskey}-{arch}';out=ROOT/'build/bundle';bundle=out/'native'/key;bundle.mkdir(parents=True,exist_ok=True)
    binary=out/('amcore.exe' if oskey=='windows' else 'amcore')
    env=os.environ.copy();env['CGO_ENABLED']='1'
    if oskey=='windows':
        env['PATH']=str(Path(args.msys)/'ucrt64/bin')+os.pathsep+env.get('PATH','');env['CC']='gcc';env['CXX']='g++'
    run(['go','build','-trimpath','-ldflags=-s -w','-o',binary,'./cmd/amcore'],env=env)
    if oskey=='darwin':
        if args.deps:
            lock=json.loads((ROOT/'native/macos/dependencies-lock.json').read_text())
            for name,info in lock.items():
                path=args.deps/name
                rev=subprocess.check_output(['git','-C',str(path),'rev-parse','HEAD'],text=True).strip()
                if rev!=info['revision']:raise RuntimeError('Unexpected dependency revision: '+name)
                dest=ROOT/'native'/name
                if not dest.exists():dest.symlink_to(path.resolve(),target_is_directory=True)
        else:run([sys.executable,'native/macos/bootstrap.py','--sources-only'])
        cmake=shutil.which('cmake')
        if not cmake:raise RuntimeError('Install CMake >= 3.20')
        run([cmake,'-S','native/macos','-B','build/macos','-DCMAKE_BUILD_TYPE=Release','-DCMAKE_OSX_DEPLOYMENT_TARGET=14.0','-DCMAKE_POLICY_VERSION_MINIMUM=3.5'])
        run([cmake,'--build','build/macos','--target','core-loader','-j','4'])
        shutil.copy2(ROOT/'build/macos/core-loader',bundle/'native-helper')
        run(['codesign','--force','--sign','-',bundle/'native-helper']);run(['codesign','--force','--sign','-',binary])
    else:
        runtime=ROOT/'runtime'/key;runtime.mkdir(parents=True,exist_ok=True)
        if oskey=='windows':
            run([sys.executable,'scripts/build_windows_core.py','--msys',args.msys])
            run([sys.executable,'scripts/build_windows_helper.py','--msys',args.msys])
            source=ROOT/'bin'/key
            for file in source.iterdir():
                if file.suffix.lower() in ['.exe','.dll']:shutil.copy2(file,bundle/file.name)
                if file.suffix.lower()=='.dll':shutil.copy2(file,out/file.name)
        else:
            run([sys.executable,'scripts/rebuild_linux_client.py'])
            build=ROOT/'build/linux-helper';build.mkdir(parents=True,exist_ok=True)
            cc=os.environ.get('CC','cc');cxx=os.environ.get('CXX','c++')
            run([cc,'-O2','-c','third_party/cJSON.c','-o',build/'cjson.o'])
            run([cxx,'-O2','-std=c++17','native/x64/native_helper.cpp',build/'cjson.o','-ldl','-lpthread','-o',bundle/'native-helper'])
        for file in runtime.rglob('*'):
            if file.is_file() and file.suffix in ['.dll','.so']:
                dest=bundle/file.relative_to(runtime);dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(file,dest)
        # Build tiny ELF dependency stubs from source, without Android libraries.
        stubdir=bundle/'rootfs/system/lib64';stubdir.mkdir(parents=True,exist_ok=True)
        cc=['cc'] if oskey=='linux' else [str(Path(args.msys)/'ucrt64/bin/clang.exe'),'--target=x86_64-linux-gnu','-fuse-ld=lld']
        for name in ['libandroid.so','libOpenSLES.so','libdl.so']:
            flags=['-shared','-fPIC','-nostdlib','-O2','-Wl,--hash-style=both','-Wl,-soname,'+name]
            if name=='libdl.so':flags+=['-Wl,--version-script=third_party/adam-drm/stubs/libdl.map']
            run(cc+flags+['third_party/adam-drm/stubs/empty_stub.c','-o',stubdir/name],env=env)
    files={str(f.relative_to(bundle)).replace('\\','/'):hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted(bundle.rglob('*')) if f.is_file() and f.name!='manifest.json'}
    (bundle/'manifest.json').write_text(json.dumps(files,indent=2)+'\n')
    print('Built '+str(out)+'; runtime setup and real subscriber playback are separate checks.')
if __name__=='__main__':main()
