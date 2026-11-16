"""Optional native build for distributions older than the pinned GLIBC 2.38 loader."""
from common import ROOT
import os,shutil,subprocess,sys
if not sys.platform.startswith('linux'):raise SystemExit('Run this optional build on Linux with GCC/G++ installed.')
source=ROOT/'third_party/libhybris-win';s=(source/'build-and-test-x86_64.sh').read_text(encoding='utf-8');marker=s.index('ls -lh /tmp/hybris-linker/q.so');start=s.index('\nif [[ "$TARGET_TRIPLE"',marker);end=s.index('# ── Step 2:',start);s=s[:start]+'\n'+s[end:];end=s.index('ls -lh "$BUILD/libhybris-core.so"');s=s[:end]+s[end:].split('\n',1)[0]+'\n';s=s.replace('BUILD="/tmp/hybris-x86_64-build"','BUILD="$ROOT/.mygo-build"').replace("-DLINKER_PLUGIN_DIR='\"/tmp/hybris-linker\"'","-DLINKER_PLUGIN_DIR='\".\"'").replace('/tmp/hybris-linker','$ROOT/.mygo-linker');script=source/'mygo-build-only.sh';script.write_text(s,encoding='utf-8');subprocess.run(['bash',str(script)],check=True)
runtime=ROOT/'runtime/linux-amd64';(runtime/'hybris-linker').mkdir(parents=True,exist_ok=True);shutil.copy2(source/'.mygo-build/libhybris-core.so',runtime/'libhybris-core.so');shutil.copy2(source/'.mygo-linker/q.so',runtime/'hybris-linker/q.so')
print('Rebuilt Linux loader from included source. Re-run probe; do not treat compilation as playback verification.')
