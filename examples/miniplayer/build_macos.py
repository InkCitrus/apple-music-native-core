#!/usr/bin/env python3
"""Build a small native control window around the direct Go-library example."""
from pathlib import Path
import argparse,hashlib,json,plistlib,shutil,subprocess,sys
ROOT=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--bundle',type=Path,default=ROOT/'build/bundle');p.add_argument('--output',type=Path,default=ROOT/'build/demo/Native Core Demo.app');a=p.parse_args()
if sys.platform!='darwin':raise SystemExit('The graphical example uses macOS AppKit; the Go example works on all supported desktops.')
bundle=a.bundle.resolve();app=a.output.resolve();contents=app/'Contents';binary=contents/'MacOS';binary.mkdir(parents=True,exist_ok=True)
shutil.copy2(bundle/'amcore-demo',binary/'amcore-demo')
resources=contents/'Resources';resources.mkdir(exist_ok=True)
shutil.copytree(ROOT/'auth/locales',resources/'Localizations',dirs_exist_ok=True)
if (binary/'native').exists():shutil.rmtree(binary/'native')
shutil.copytree(bundle/'native/darwin-arm64',resources/'native/darwin-arm64',dirs_exist_ok=True)
info={'CFBundleIdentifier':'dev.inkcitrus.nativecore.demo','CFBundleName':'Native Core Demo','CFBundleDisplayName':'Native Core Demo','CFBundleExecutable':'native-core-demo-ui','CFBundlePackageType':'APPL','CFBundleShortVersionString':'0.2.0','CFBundleVersion':'5','LSMinimumSystemVersion':'14.0','NSHighResolutionCapable':True,'CFBundleDevelopmentRegion':'en','CFBundleLocalizations':['en','zh-Hans']}
(contents/'Info.plist').write_bytes(plistlib.dumps(info))
subprocess.run(['swiftc','-O','-parse-as-library','-target','arm64-apple-macos14.0',str(ROOT/'native/macos/EditingMenu.swift'),str(ROOT/'examples/miniplayer/macos/PlayerWindow.swift'),'-o',str(binary/'native-core-demo-ui')],check=True)
subprocess.run(['codesign','--force','--sign','-',str(app)],check=True)
for name,sha in json.loads((bundle/'native/darwin-arm64/manifest.json').read_text()).items():
    assert hashlib.sha256((resources/'native/darwin-arm64'/name).read_bytes()).hexdigest()==sha, 'Native bundle changed while signing the demo'
print('Built '+str(app))
