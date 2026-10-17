import os,json,subprocess
from pathlib import Path
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-final-measure');w='/Users/thesyncim/.codex/bin/project-env'
names=['assembly','parent','current'];bins={'assembly':'govpx-asm-bench','parent':'govpx-fusion-root-bench','current':'govpx-uv-root-bench'}
orders=[[0,1,2],[1,2,0],[2,0,1],[0,2,1],[2,1,0],[1,0,2]];rows=[]
for codec in ['vp8','vp9']:
 for r,order in enumerate(orders):
  for i in order:
   n=names[i]
   if codec=='vp9' and n=='assembly':continue
   args=[w,'/private/tmp/'+bins[n],'-codec='+codec,'-width=1280','-height=720','-frames=60','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
   rows.append(dict(codec=codec,round=r,variant=n,result=json.loads(subprocess.check_output(args,env=env,text=True))))
  Path('/private/tmp/govpx-uv-root-public.json').write_text(json.dumps(dict(samples=rows),indent=2)+'\n');print(codec,'chroma root public round',r+1,'done',flush=True)
