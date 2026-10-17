# Rejected paired-chroma edge fusion

Apple M4 Max, Go 1.27.2, SIMD, PGO off, one Go/encoder thread, same 720p
realtime inputs, no competing local Go jobs. Baseline codec is db5b44fe.

The candidate fused the UV macroblock and internal edges, with disjoint-plane
guards, exact 12x8 bounds, inlined four/six-byte stores and no extra dependency.
Scalar 9,216 paired cases, int16 SIMD 4,608 paired cases, alias/short-window/threshold
checks and package tests passed, including race detection/checkptr.

Four designs and fresh prototype checks favored inlined stores, by 0.80%
for 60 frames and 1.11% for 300 frames, all 12 prototype pairs faster. The integrated
source did not reproduce reliable short-sequence gains: initial 60-frame
three-way run -0.084% with mixed pairs; fresh 60-frame run +0.045% with mixed
pairs. The 300-frame integrated run improves 0.748%, also with mixed pairs.
Outputs, quantizer metadata and allocations match in every final pair.

The integrated API is instruction-identical to the prototype; hot kernels
differ only in relocated constant addresses. The x64 path compiles out; its
instruction count, dispatch and bounds checks match except equivalent
independent instruction ordering. No general performance benefit is claimed.

This candidate is excluded from production because the integrated public
operation did not sustain a clear benefit across both workloads. All raw
results, codegen, validation and a reproducible source patch are retained.
