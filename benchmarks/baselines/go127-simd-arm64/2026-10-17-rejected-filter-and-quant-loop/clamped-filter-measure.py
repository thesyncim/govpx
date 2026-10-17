import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env';env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-inline-measure')
rows=[]
for frames in [60,300]:
 for r in range(6):
  for v in (['parent','clamped'] if r%2==0 else ['clamped','parent']):
   b='/private/tmp/govpx-quant-'+('final' if v=='parent' else 'clamped-filter')+'-bench'
   args=[w,b,'-codec=vp8','-width=1280','-height=720','-frames='+str(frames),'-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json']
   rows.append(dict(frames=frames,round=r,variant=v,result=json.loads(subprocess.check_output(args,env=env,text=True))))
  Path('/private/tmp/govpx-quant-clamped-public.json').write_text(json.dumps({'samples':rows},indent=2)+'\n')
  print(frames,'round',r+1,'done',flush=True)
