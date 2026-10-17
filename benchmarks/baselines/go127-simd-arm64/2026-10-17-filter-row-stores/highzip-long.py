import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env'
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-highzip')
rows=[];bins={'parent':'/private/tmp/govpx-quant-final-bench','candidate':'/private/tmp/govpx-arm-highzip-bench'}
for r in range(6):
 for n in (['parent','candidate'] if r%2==0 else ['candidate','parent']):
  args=[w,bins[n],'-codec=vp8','-width=1280','-height=720','-frames=300','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
  rows.append(dict(round=r,variant=n,result=json.loads(subprocess.check_output(args,env=env,text=True))))
 Path('/private/tmp/govpx-arm-highzip-long.json').write_text(json.dumps({'samples':rows},indent=2)+'\n')
 print('ARM high-half interleave round',r+1,'done',flush=True)
