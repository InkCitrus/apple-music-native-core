"""Rebuild the thin DRM client with the target Linux compiler for diagnostics."""
from common import ROOT
from pathlib import Path
import os,subprocess,sys
if not sys.platform.startswith('linux'):raise SystemExit('This rebuild uses the target Linux C/C++ compiler.')
source=ROOT/'third_party/adam-drm';build=ROOT/'build/linux-client';build.mkdir(parents=True,exist_ok=True)
flags=['-O2','-fPIC','-DMyRelease','-D_GNU_SOURCE','-DDRM_LIB_BUILD','-DMYGO_EXTERNAL_HYBRIS','-include','sys/time.h','-I'+str(source),'-I'+str(source/'native')];objects=[]
for name in ['drm_linux.c','drm_hybris.c','embedded_loader.c','native/main.c','native/cmdline.c','native/hybris_stubs.c','native/drm_lib.c','native/cJSON.c','native/android_shim.c','native/main.cpp','native/guard.cpp']:
    cpp=name.endswith('.cpp');compiler=os.environ.get('CXX','c++') if cpp else os.environ.get('CC','cc');obj=build/(name.replace('/','_')+'.o');args=[compiler]+flags+(['-std=c++17'] if cpp else ['-std=gnu11'])
    if name=='native/hybris_stubs.c':args+=['-Dandroid_dlopen=aml_android_dlopen','-Dandroid_dlsym=aml_android_dlsym','-Dandroid_dlerror=aml_android_dlerror','-Dandroid_update_LD_LIBRARY_PATH=aml_android_update_LD_LIBRARY_PATH']
    subprocess.run(args+['-c',str(source/name),'-o',str(obj)],check=True);objects.append(str(obj))
runtime=ROOT/'runtime/linux-amd64';runtime.mkdir(parents=True,exist_ok=True);subprocess.run([os.environ.get('CXX','c++'),'-shared','-o',str(runtime/'libdrm_client.so')]+objects+['-lpthread','-ldl','-lm'],check=True)
print('Native Linux DRM client rebuilt. Run probe and the real tests; compilation is not a playback result.')
