#!/usr/bin/env python3
"""Build the pinned process-scoped Android capture server and live bitrate protocol."""
import argparse,os,shutil,subprocess,zipfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
REV='a60891aea193d92e7e5c3942700eca63f9d19a5f'
def run(*cmd,cwd=None):subprocess.run(list(map(str,cmd)),cwd=cwd,check=True)
def main():
 p=argparse.ArgumentParser();p.add_argument('--source',type=Path,default=ROOT/'vendor-build/scrcpy');p.add_argument('--sdk',default=os.environ.get('ANDROID_HOME') or os.environ.get('ANDROID_SDK_ROOT'));p.add_argument('--gradle',default=shutil.which('gradle'));p.add_argument('--offline',action='store_true');args=p.parse_args()
 if not args.sdk or not args.gradle:p.error('Set ANDROID_HOME and --gradle to Gradle 8.13 (JDK 17 or 21).')
 src=args.source.resolve();src.parent.mkdir(parents=True,exist_ok=True)
 if not src.exists():run('git','clone','--depth','1','--branch','v5.0.1','https://github.com/Genymobile/scrcpy.git',src)
 if subprocess.check_output(['git','rev-parse','HEAD'],cwd=src,text=True).strip()!=REV:raise SystemExit('Unexpected scrcpy revision')
 for name in ['scrcpy-secure-capture.patch','scrcpy-live-bitrate.patch']:
  patch=ROOT/'patches'/name
  if subprocess.run(['git','apply','--reverse','--check',str(patch)],cwd=src,capture_output=True).returncode==0:continue
  run('git','apply','--check',patch,cwd=src);run('git','apply',patch,cwd=src)
 build=src/'build.gradle';text=build.read_text().replace("classpath 'com.android.tools.build:gradle:9.1.0'","classpath 'com.android.tools.build:gradle:8.5.2'")
 if 'resolutionStrategy.force' not in text:text=text.replace('    dependencies {',"    configurations.classpath.resolutionStrategy.force('org.jetbrains.kotlin:kotlin-reflect:2.0.21', 'org.jetbrains.kotlin:kotlin-stdlib:2.0.21')\n    dependencies {",1)
 build.write_text(text)
 check=src/'config/android-checkstyle.gradle';check.write_text(check.read_text().replace('android.sourceSets.each','project.extensions.getByName("android").sourceSets.each'))
 (src/'local.properties').write_text('sdk.dir='+str(Path(args.sdk).resolve()).replace('\\','/')+'\n')
 cmd=[args.gradle,'--no-daemon','--no-configuration-cache','-Pandroid.suppressUnsupportedCompileSdk=36',':server:assembleDebug','--console=plain']
 if args.offline:cmd.insert(1,'--offline')
 run(*cmd,cwd=src)
 dest=ROOT/'sdriver/scrcpy/bin/scrcpy-server-v5.0.1-marble-secure'
 with zipfile.ZipFile(src/'server/build/outputs/apk/debug/server-debug.apk') as source,zipfile.ZipFile(dest,'w',zipfile.ZIP_DEFLATED) as output:
  for entry in source.infolist():
   if not entry.filename.startswith('META-INF/'):output.writestr(entry,source.read(entry.filename))
 print(dest)
if __name__=='__main__':main()
