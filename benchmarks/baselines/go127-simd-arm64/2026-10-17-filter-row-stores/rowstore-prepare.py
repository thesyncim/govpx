from pathlib import Path
import shutil
p=Path('/private/tmp/govpx-arm-rowstore-probe')
if not p.exists():shutil.copytree('/private/tmp/govpx-arm-highzip-probe',p)
root=Path('/Users/thesyncim/GolandProjects/govpx')
f=p/'internal/pixel/loopfilter_macro_internal_fused_arm64.go'
s=(root/'internal/pixel/loopfilter_macro_internal_fused_arm64.go').read_text();at=s.index('func loopFilterMacroInternalFusedVertical');pre=s[:at];v=s[at:]
needle='p3, p2, p1, p0, q0, q1, q2, q3 := loadLFColumns[[8]byte](p, 0, outer, 16)'
assert v.count(needle)==1;v=v.replace(needle,needle+'\nvar output [16]archsimd.Uint8x16\noutput[0], output[1], output[2] = p2,p1,p0\nchanged:=false')
v=v.replace('\n\t\toff := 4 * step','',1)
# Both macro store halves are replaced by retained, finalized columns.
a=v.index('\n\t\t\t\t{\n\t\t\t\t\tptr :=')
def endbrace(s,a):
 start=s.index('{',a);depth=0
 for i in range(start,len(s)):
  if s[i]=='{':depth+=1
  elif s[i]=='}':
   depth-=1
   if depth==0:return i+1
b=endbrace(v,a);b=endbrace(v,b)
v=v[:a]+'\noutput[0],output[1],output[2]=np2,np1,np0\nchanged=true\n'+v[b:]
needle='for edge := 8; edge <= 16; edge += 4 {';assert v.count(needle)==1
v=v.replace(needle,needle+'\noutput[edge-5],output[edge-4],output[edge-3],output[edge-2]=p3,p2,p1,p0')
a=v.index('\n\t\t\t// The caller proves the complete 16x16 window.');b=v.index('\n\t\t\tq0, q1 = nq0, nq1',a)
v=v[:a]+'\noutput[edge-3],output[edge-2]=np1,np0\nchanged=true\n'+v[b:]
# Finish only after all dependent edge computations; disjoint rows are proven.
i=v.rfind('\n}')
v=v[:i]+'\nif changed {\noutput[15]=q0\nstoreLFRowVectors(p,pitch,&output,q1)\n}\n'+v[i:]
f.write_text(pre+v)
# Generate an unrolled 16-column transpose, storing exactly columns1..17.
lines=['//go:build goexperiment.simd && arm64 && !purego','package pixel','import("simd/archsimd";"unsafe")','func storeLFRowVectors(p []byte,pitch int,cols *[16]archsimd.Uint8x16,tail archsimd.Uint8x16) {']
groups=[[f'cols[{i}]'] for i in range(16)]
for stage,(cast,reshape) in enumerate([('', ''),('.ReshapeToUint16s()','.ReshapeToUint8s()'),('.ReshapeToUint32s()','.ReshapeToUint8s()'),('.ReshapeToUint64s()','.ReshapeToUint8s()')]):
 new=[]
 for g in range(0,len(groups),2):
  out=[]
  for j,(a,b) in enumerate(zip(groups[g],groups[g+1])):
   for half in ['Lo','Hi']:
    n=f't{stage}_{g}_{j}_{half}'
    lines.append(f'{n}:={a}{cast}.Interleave{half}({b}{cast}){reshape}')
    out.append(n)
  new.append(out)
 groups=new
assert len(groups)==1 and len(groups[0])==16
lines.append('ptr:=unsafe.Pointer(unsafe.SliceData(p))')
for i,n in enumerate(groups[0]):lines.append(f'{n}.StoreArray((*[16]byte)(unsafe.Add(ptr,{i}*pitch+1)))\np[{i}*pitch+17]=tail.GetElem({i})')
lines.append('}')
(p/'internal/pixel/loopfilter_rowstore_arm64.go').write_text('\n'.join(lines)+'\n')
print('Prepared complete-row store probe')
