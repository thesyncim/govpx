# Reuse SIMD statistics for identity subpixel phases

Native Apple M4 Max, Go1.27.2 archsimd, PGOoff,GOMAXPROCS1. Parent a5401799
and pinned assembly691cd051. Exact fx=fy={128,0} evaluates each bilinear stage
to the input byte:128*p+64 fits signedint16 (64..32704), shifted7 equals p.
For w,h<=64, reuse the existing SIMD Stats kernel rather than multiply identity
weights and load irrelevant neighbor values. Keep BOTH original bilinear
endpoint checks first, including the otherwise unnecessary row/column. Positive
dimensions/nonnegative strides follow original dispatch; stride<2^30 proves
endpoint arithmetic cannot overflow. Other coefficients/geometry/CPUoff retain
unchanged paths. No input writes or retained pointers; read-only aliases valid.

Fresh six balanced parent/assembly/current permutations,100ms per40 public
fixtures, all zero B/op and allocations. Identical B.Loop sources and scalar
variance/SSE equations, minimum visible spans, unaligned3/5 origins, minimal
and unequal padded strides, full immutable backing-buffer checks and sinks.
All oldASM phase-specific algorithms remain legitimate strong controls.

| Full-pixel public fixture | Parent/current time ratio | Assembly/current time ratio | Minimum paired parent/current |
|---|---:|---:|---:|
| 16x8/phase0-0/paddedfalse | 1.7820x | 1.0377x | 1.7655x |
| 16x8/phase0-0/paddedtrue | 1.7711x | 1.0680x | 1.7453x |
| 16x16/phase0-0/paddedfalse | 2.3114x | 0.7057x | 2.3015x |
| 16x16/phase0-0/paddedtrue | 2.3021x | 0.7076x | 2.2685x |
| 32x16/phase0-0/paddedfalse | 1.7042x | 0.4809x | 1.6892x |
| 32x16/phase0-0/paddedtrue | 1.6954x | 0.4898x | 1.6534x |
| 16x32/phase0-0/paddedfalse | 2.3249x | 0.6743x | 2.3062x |
| 16x32/phase0-0/paddedtrue | 2.3340x | 0.6766x | 2.3138x |

All48 identity pairs exceed 1.6534x parent.
Keep this correctness-verified >=1.5x component win independently of overall
codec performance under AGENTS.md. Other32 phase controls range
0.9832–1.0037x parent; retain every slower control
and all raw samples/outliers. Only16x8 beats assembly here; other measured sizes
still trail assembly. This is not blanket assembly parity or a general2x claim.

Whole-codec effect of this last shared shortcut is pending and is not inferred
from kernel results. Earlier vertical-filter-only whole measurements remain
separately scoped. Native x64 performance waits for the exact-head246-case CI
comparison; Rosetta measurements are never speed evidence.

Independent review passed exact arithmetic, numeric ranges, endpoint/panic
contracts, aliases, fallbacks and existing Stats AVX cleanup. |sum|<=1,044,480
andSSE<=266,342,400; ARM16 narrow paired bound32640 and AMD16 bound16320 fit
int16. Width4/quads,8/paired,24/generic and32/64 row reductions remain exact.
Existing extreme and64-phase oracles pass;72 new identity alias/stride/shape
cases and40 short-window panic cases pass. Native harness requires identity
tests in selected/scalar/race/checkptr coverage; same40 fixture and all206older
controls retained, eight benchmark/production hashes recorded.

Initial two-round40case diagnostic, freshsix confirmation, full source/driver
snapshots and correctness/codegen logs are preserved. No concurrent local
builds/profiles during timing. README files and dependencies remain unchanged.
