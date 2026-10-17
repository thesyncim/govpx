import os,subprocess
from pathlib import Path
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-full-denoise-kernels')
names=['assembly','parent','candidate'];bins=['/private/tmp/govpx-arm-denoise-asm-fixture.test','/private/tmp/govpx-arm-denoise-final-encoder.test','/private/tmp/govpx-arm-full-denoise-encoder.test'];orders=[[0,1,2],[1,2,0],[2,0,1],[0,2,1],[2,1,0],[1,0,2]]
for name in names:Path('/private/tmp/govpx-arm-full-denoise-kernels-'+name+'.txt').write_text('')
for r,order in enumerate(orders):
 for i in order:
  args=['/Users/thesyncim/.codex/bin/project-env',bins[i],'-test.run=^$','-test.bench=^BenchmarkDenoiserFilterYNative$','-test.benchtime=150ms','-test.count=1','-test.cpu=1','-test.benchmem']
  with Path('/private/tmp/govpx-arm-full-denoise-kernels-'+names[i]+'.txt').open('a') as f:
   f.write('# round '+str(r+1)+'\n');f.flush();subprocess.run(args,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 print('ARM complete denoiser kernel round',r+1,'done',flush=True)
