import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env';env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-correction-measure')
rows=[];names=['parent','factor','parallel'];orders=[[0,1,2],[1,2,0],[2,0,1],[0,2,1],[2,1,0],[1,0,2]]
for r,order in enumerate(orders):
 for i in order:
  v=names[i];b='/private/tmp/govpx-quant-'+('final' if v=='parent' else 'correction-'+v)+'-bench'
  args=[w,b,'-codec=vp8','-width=1280','-height=720','-frames=60','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
  rows.append(dict(frames=60,round=r,variant=v,result=json.loads(subprocess.check_output(args,env=env,text=True))))
 Path('/private/tmp/govpx-quant-correction-public.json').write_text(json.dumps({'samples':rows},indent=2)+'\n');print('correction round',r+1,'done',flush=True)
