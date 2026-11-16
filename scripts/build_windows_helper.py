"""Build the Windows host for ELF guests that do not emit Windows stack probes."""
from common import ROOT, reports
from pathlib import Path
import argparse,hashlib,json,os,shutil,struct,subprocess,sys

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--msys',default=r'C:\msys64')
    args=parser.parse_args()
    if sys.platform!='win32':raise RuntimeError('Use the native Windows toolchain.')
    compiler=Path(args.msys)/'ucrt64/bin'
    work=ROOT/'build/windows-helper';work.mkdir(parents=True,exist_ok=True)
    target=ROOT/'bin/windows-amd64/native-helper.exe';target.parent.mkdir(parents=True,exist_ok=True)
    env=os.environ.copy();env['PATH']=str(compiler)+os.pathsep+env.get('PATH','')
    subprocess.run([str(compiler/'gcc.exe'),'-O2','-c',str(ROOT/'third_party/cJSON.c'),'-o',str(work/'cjson.o')],env=env,check=True)
    # Android SysV functions can allocate large frames without __chkstk. A
    # fully committed 8 MiB working range avoids skipping Windows guard pages.
    # Retain the shipped host's 16 MiB reserve; code below that range still has
    # ordinary Windows guard/overflow behavior. No SDK instructions are patched.
    command=[str(compiler/'g++.exe'),'-std=c++17','-O2',str(ROOT/'native/x64/native_helper.cpp'),str(work/'cjson.o'),'-static-libgcc','-static-libstdc++','-Xlinker','--stack','-Xlinker','16777216,8388608','-o',str(target)]
    subprocess.run(command,env=env,check=True)
    # GCC's std::thread uses winpthreads even with libstdc++ linked statically.
    dependency=compiler/'libwinpthread-1.dll'
    if dependency.exists():shutil.copy2(dependency,target.parent/dependency.name)
    data=target.read_bytes();pe=struct.unpack_from('<I',data,0x3c)[0];optional=pe+24
    reserve,commit=struct.unpack_from('<QQ',data,optional+72)
    assert reserve==16777216 and commit==8388608
    result={'windowsHelperBuilt':True,'sdkModified':False,'credentialsUsed':False,'stackReserveBytes':reserve,'stackCommitBytes':commit,'sha256':hashlib.sha256(data).hexdigest(),'compiler':subprocess.check_output([str(compiler/'g++.exe'),'--version'],text=True,env=env).splitlines()[0]}
    (reports()/'windows-helper-build.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))

if __name__=='__main__':main()
