#!/usr/bin/env python3
"""Compile the synthetic native ABI fixture; never opens an Apple runtime."""
from pathlib import Path
import os,subprocess,sys,tempfile
ROOT=Path(__file__).resolve().parents[1]
if sys.platform=='win32':raise SystemExit('Run the Go tests directly with AMCORE_MOCK_HELPER/AMCORE_MOCK_RUNTIME set to a native fixture build.')
with tempfile.TemporaryDirectory(prefix='amcore-synthetic-') as tmp:
    p=Path(tmp);runtime=p/'runtime';runtime.mkdir();helper=p/'native-helper'
    def run(args,**kwargs):subprocess.run([str(s) for s in args],cwd=ROOT,check=True,**kwargs)
    run(['cc','-O2','-c','third_party/cJSON.c','-o',p/'cjson.o'])
    run(['c++','-O2','-std=c++17','native/x64/native_helper.cpp',p/'cjson.o','-o',helper]+(['-ldl','-lpthread'] if sys.platform.startswith('linux') else []))
    run(['cc','-O2','-dynamiclib' if sys.platform=='darwin' else '-shared','-fPIC','native/x64/mock_sdk.c','-o',runtime/'libdrm_client.so'])
    env=os.environ.copy();env['AMCORE_MOCK_HELPER']=str(helper);env['AMCORE_MOCK_RUNTIME']=str(runtime)
    run(['go','test','-race','-tags=mygo_portable_test','./native'],env=env)
