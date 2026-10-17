import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env';env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-rowstore-confirm')
names=['assembly','parent','candidate'];bins=['/private/tmp/govpx-asm-bench','/private/tmp/govpx-quant-final-bench','/private/tmp/govpx-arm-rowstore-bench'];orders=[[0,1,2],[1,2,0],[2,0,1],[0,2,1],[2,1,0],[1,0,2]];rows=[]
for frames in [60,300]:
 for r,order in enumerate(orders):
  for i in order:
   args=[w,bins[i],'-codec=vp8','-width=1280','-height=720','-frames='+str(frames),'-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
   rows.append(dict(frames=frames,round=r,variant=names[i],result=json.loads(subprocess.check_output(args,env=env,text=True))))
  Path('/private/tmp/govpx-arm-rowstore-confirm.json').write_text(json.dumps({'samples':rows},indent=2)+'\n');print('ARM row-store confirmation',frames,'round',r+1,'done',flush=True)
