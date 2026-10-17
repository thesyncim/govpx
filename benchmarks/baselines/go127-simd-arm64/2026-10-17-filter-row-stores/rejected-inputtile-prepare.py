from pathlib import Path
p=Path('/private/tmp/govpx-arm-rowstore-probe')
s=Path('/Users/thesyncim/GolandProjects/govpx/internal/pixel/loopfilter_macro_internal_fused_arm64.go').read_text();a=s.index('func loopFilterMacroInternalFusedVertical');head=s[:a];v=s[a:]
old='p3, p2, p1, p0, q0, q1, q2, q3 := loadLFColumns[[8]byte](p, 0, outer, 16)';assert v.count(old)==1
v=v.replace(old,'var input [20]archsimd.Uint8x16\nloadLFInputColumns20(p,pitch,&input)\np3,p2,p1,p0,q0,q1,q2,q3:=input[0],input[1],input[2],input[3],input[4],input[5],input[6],input[7]')
marker='\n\t\tptr := unsafe.Add(unsafe.Pointer(unsafe.SliceData(p)), edge+4)'
assert v.count(marker)==2
for _ in range(2):
 a=v.index(marker);b=v.index('\n\t\tq0, q1, q2, q3 =',a);b=v.index('\n',b+1)
 v=v[:a]+'\nq0,q1,q2,q3=input[edge+4],input[edge+5],input[edge+6],input[edge+7]'+v[b:]
(p/'internal/pixel/loopfilter_macro_internal_fused_arm64.go').write_text(head+v)
lines=['//go:build goexperiment.simd && arm64 && !purego','package pixel','import("simd/archsimd";"unsafe")','// The caller proves sixteen disjoint rows of twenty readable bytes.','func loadLFInputColumns20(p []byte,pitch int,cols *[20]archsimd.Uint8x16) {','ptr:=unsafe.Pointer(unsafe.SliceData(p))']
for i in range(16):lines.append(f'r{i}:=archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(ptr,{i}*pitch+4)))')
groups=[[f'r{i}'] for i in range(16)]
for stage,(cast,reshape) in enumerate([('', ''),('.ReshapeToUint16s()','.ReshapeToUint8s()'),('.ReshapeToUint32s()','.ReshapeToUint8s()'),('.ReshapeToUint64s()','.ReshapeToUint8s()')]):
 new=[]
 for g in range(0,len(groups),2):
  out=[]
  for j,(a,b) in enumerate(zip(groups[g],groups[g+1])):
   for half in ['Lo','Hi']:
    n=f't{stage}_{g}_{j}_{half}';lines.append(f'{n}:={a}{cast}.Interleave{half}({b}{cast}){reshape}');out.append(n)
  new.append(out)
 groups=new
for i,n in enumerate(groups[0]):lines.append(f'cols[{i+4}]={n}')
for i in range(4):lines.append(f'a{i}:=archsimd.BroadcastUint64x2(uint64(*(*uint32)(unsafe.Add(ptr,{i}*pitch)))|uint64(*(*uint32)(unsafe.Add(ptr,{i+4}*pitch)))<<32).SetElem(1,uint64(*(*uint32)(unsafe.Add(ptr,{i+8}*pitch)))|uint64(*(*uint32)(unsafe.Add(ptr,{i+12}*pitch)))<<32).ReshapeToUint16s()')
lines+=['b0,b1:=a0.InterleaveEven(a2).ReshapeToUint8s(),a0.InterleaveOdd(a2).ReshapeToUint8s()','b2,b3:=a1.InterleaveEven(a3).ReshapeToUint8s(),a1.InterleaveOdd(a3).ReshapeToUint8s()','cols[0],cols[1],cols[2],cols[3]=b0.InterleaveEven(b2),b0.InterleaveOdd(b2),b1.InterleaveEven(b3),b1.InterleaveOdd(b3)','}']
(p/'internal/pixel/loopfilter_inputtile_arm64.go').write_text('\n'.join(lines)+'\n')
print('Prepared exact20x16 input tile transpose, source only')
