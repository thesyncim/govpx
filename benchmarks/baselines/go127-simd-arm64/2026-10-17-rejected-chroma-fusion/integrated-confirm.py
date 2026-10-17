import os,json,subprocess
from pathlib import Path
env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-fusion-confirm');w='/Users/thesyncim/.codex/bin/project-env';rows=[]
for frames in [60,300]:
 for r in range(6):
  for n in (['parent','current'] if r%2==0 else ['current','parent']):
   b='govpx-fusion-root-bench' if n=='parent' else 'govpx-uv-root-bench'
   args=[w,'/private/tmp/'+b,'-codec=vp8','-width=1280','-height=720','-frames='+str(frames),'-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
   rows.append(dict(frames=frames,round=r,variant=n,result=json.loads(subprocess.check_output(args,env=env,text=True))))
  Path('/private/tmp/govpx-uv-root-confirm.json').write_text(json.dumps(dict(samples=rows),indent=2)+'\n');print(frames,'frames final chroma confirmation round',r+1,'done',flush=True)
