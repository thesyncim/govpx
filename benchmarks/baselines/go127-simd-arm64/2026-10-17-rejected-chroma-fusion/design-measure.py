import os,json,subprocess,statistics
from pathlib import Path
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-next-measure');w='/Users/thesyncim/.codex/bin/project-env'
orders=[[0,1,2,3],[1,2,3,0],[2,3,0,1],[3,0,1,2],[3,2,1,0],[2,1,0,3],[1,0,3,2],[0,3,2,1]]
names=['current','uv','uvinline','uvflat'];rows=[]
for r,order in enumerate(orders):
 for i in order:
  n=names[i];b={'current':'govpx-fusion-root-bench'}.get(n,'govpx-fusion-'+n+'-bench')
  args=[w,'/private/tmp/'+b,'-codec=vp8','-width=1280','-height=720','-frames=60','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
  rows.append(dict(round=r,variant=n,result=json.loads(subprocess.check_output(args,env=env,text=True))))
 Path('/private/tmp/govpx-fusion-uv-design-public.json').write_text(json.dumps(dict(samples=rows),indent=2)+'\n');print('chroma fusion public round',r+1,'done',flush=True)
