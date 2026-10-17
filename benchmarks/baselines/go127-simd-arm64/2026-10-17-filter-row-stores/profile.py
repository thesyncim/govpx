import os,json,subprocess
from pathlib import Path
w='/Users/thesyncim/.codex/bin/project-env';env=dict(os.environ,GOMAXPROCS='1',CODEX_PROJECT_CACHE_ROOT='/private/tmp/codex-projects',CODEX_AGENT_ID='root-arm-rowstore-profile')
for n,b in [('parent','govpx-quant-final-bench'),('current','govpx-arm-rowstore-final-bench')]:
 args=[w,'/private/tmp/'+b,'-codec=vp8','-width=1280','-height=720','-frames=600','-fps=30','-bitrate=2500','-mode=realtime','-cpu-used=8','-threads=1','-encode-only','-auto-libvpx=false','-format=json','-cpuprofile=/private/tmp/govpx-arm-rowstore-'+n+'-profile.pgo']
 Path('/private/tmp/govpx-arm-rowstore-'+n+'-profile.json').write_text(subprocess.check_output(args,env=env,text=True));print(n,'ARM profile captured',flush=True)
