from pathlib import Path
import os,subprocess
names=['assembly','main'];bins=['/private/tmp/govpx-picker-arm-'+n+'.test' for n in names]
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-picker-native')
for n in names:Path('/private/tmp/govpx-picker-arm-'+n+'.txt').write_text('')
for r in range(6):
 for i in ([0,1] if r%2==0 else [1,0]):
  with Path('/private/tmp/govpx-picker-arm-'+names[i]+'.txt').open('a') as f:
   f.write('# round '+str(r+1)+'\n');f.flush()
   subprocess.run(['/Users/thesyncim/.codex/bin/project-env',bins[i],'-test.run=^$','-test.bench=^BenchmarkVP8PickerFilterNative$','-test.benchtime=150ms','-test.cpu=1','-test.count=1','-test.benchmem'],env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 print('ARM picker component round',r+1,'done',flush=True)
