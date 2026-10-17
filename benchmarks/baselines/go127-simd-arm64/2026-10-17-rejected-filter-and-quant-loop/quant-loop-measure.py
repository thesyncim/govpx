import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env';env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-quant-loop-measure')
rows=[];names=['parent','bic','blockptr','combined'];orders=[[0,1,2,3],[1,3,0,2],[2,0,3,1],[3,2,1,0],[0,2,1,3],[3,1,2,0],[1,0,3,2],[2,3,0,1]]
for r,order in enumerate(orders):
 for i in order:
  v=names[i];b='/private/tmp/govpx-quant-'+('final' if v=='parent' else 'loop-'+v)+'-bench'
  args=[w,b,'-codec=vp8','-width=1280','-height=720','-frames=60','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
  rows.append(dict(frames=60,round=r,variant=v,result=json.loads(subprocess.check_output(args,env=env,text=True))))
 Path('/private/tmp/govpx-quant-loop-public.json').write_text(json.dumps({'samples':rows},indent=2)+'\n');print('quant loop round',r+1,'done',flush=True)
