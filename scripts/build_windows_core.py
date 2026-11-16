from common import ROOT
from pathlib import Path
import argparse, json, os, re, shutil, subprocess, sys
def main():
    a=argparse.ArgumentParser();a.add_argument('--msys',default=r'C:\msys64');a.add_argument('--toolchain',default=None);a.add_argument('--make',default='make');a.add_argument('--client-only',action='store_true');args=a.parse_args()
    if sys.platform!='win32':raise RuntimeError('Use the native Windows/MSYS2 GCC toolchain for this loader.')
    msys=Path(args.msys);bash=msys/'usr/bin/bash.exe';compiler_bin=Path(args.toolchain)/'bin' if args.toolchain else msys/'ucrt64/bin';compiler=compiler_bin/'g++.exe'
    if not bash.exists() or not compiler.exists():raise RuntimeError('Install MSYS2 UCRT64 GCC and make; see AGENT_WINDOWS.md.')
    runtime=ROOT/'runtime/windows-amd64';core=runtime/'libhybris-core.dll';linker=runtime/'hybris-linker/q.so'
    import hashlib
    if args.client_only and not all(p.is_file() for p in [core,linker]):raise RuntimeError('Client-only rebuild requires the existing native Windows core and linker.')
    original_loader_hashes={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [core,linker]} if args.client_only else {}
    env=os.environ.copy();env['MSYSTEM']='UCRT64';env['CHERE_INVOKING']='1'
    import shlex
    compiler_path=compiler_bin.resolve().as_posix();compiler_posix='/'+compiler_path[0].lower()+compiler_path[2:]
    path_setup='export PATH='+shlex.quote(compiler_posix)+':/usr/bin:$PATH; '
    if not args.client_only:
        source=ROOT/'third_party/libhybris-win';s=(source/'build-and-test-x86_64.sh').read_text(encoding='utf-8')
        marker=s.index('ls -lh /tmp/hybris-linker/q.so');start=s.index('\nif [[ "$TARGET_TRIPLE"',marker);end=s.index('# ── Step 2:',start);s=s[:start]+'\n'+s[end:]
        end=s.index('ls -lh "$BUILD/libhybris-core.so"');s=s[:end]+s[end:].split('\n',1)[0]+'\n'
        s=s.replace('BUILD="/tmp/hybris-x86_64-build"','BUILD="$ROOT/.mygo-build"')
        s=s.replace("-DLINKER_PLUGIN_DIR='\"/tmp/hybris-linker\"'","-DLINKER_PLUGIN_DIR='\".\"'")
        s=s.replace('/tmp/hybris-linker','$ROOT/.mygo-linker')
        script=source/'mygo-build-only.sh';script.write_text(s,newline='\n',encoding='utf-8')
        path=script.resolve().as_posix();posix='/'+path[0].lower()+path[2:]
        subprocess.run([str(bash),'-lc',path_setup+'bash '+shlex.quote(posix)],env=env,check=True)
        (runtime/'hybris-linker').mkdir(parents=True,exist_ok=True)
        shutil.copy2(source/'.mygo-build/libhybris-core.so',core);shutil.copy2(source/'.mygo-linker/q.so',linker)
    # Keep the DRM client and loader on the same native GCC/pthread runtime.
    drm=ROOT/'third_party/adam-drm';p=drm.resolve().as_posix();drm_posix='/'+p[0].lower()+p[2:]
    command=path_setup+shlex.quote(args.make)+' -B -C '+shlex.quote(drm_posix)+' MINGW_CC=gcc MINGW_CXX=g++ windows-build'
    subprocess.run([str(bash),'-lc',command],env=env,check=True)
    shutil.copy2(drm/'build-windows/libdrm_client.dll',runtime/'libdrm_client.dll')
    # Copy only non-system runtime DLLs actually imported by the native GCC build.
    objdump=compiler_bin/'objdump.exe';pending=[core,linker,runtime/'libdrm_client.dll'];seen=set()
    while pending:
        file=pending.pop();text=subprocess.check_output([str(objdump),'-p',str(file)],text=True,errors='replace')
        for name in re.findall(r'DLL Name:\s*(\S+)',text):
            if name.lower() in seen:continue
            seen.add(name.lower());dependency=compiler_bin/name
            if dependency.exists():
                target=runtime/name;shutil.copy2(dependency,target);pending.append(target)
    current_loader_hashes={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [core,linker]}
    if args.client_only and original_loader_hashes!=current_loader_hashes:raise RuntimeError('Client-only build unexpectedly changed the existing loader.')
    result={'windowsLoaderBuilt':not args.client_only,'nativeGCCClientBuilt':True,'clientOnly':args.client_only,'existingLoaderUnchanged':original_loader_hashes==current_loader_hashes if args.client_only else None,'androidNDKRequired':False,'credentialsUsed':False,'sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [core,linker,runtime/'libdrm_client.dll']}}
    (ROOT/'reports').mkdir(exist_ok=True);(ROOT/'reports/windows-native-build.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
if __name__=='__main__':main()
