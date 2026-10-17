# UV edge-fusion research checkpoint

Native Apple M4 Max,Go1.27.2 SIMD,PGOoff,GOMAXPROCS1. Parent03afac7c,
pinned assembly691cd051. Prototype combines macro/internal chroma filtering
on disjoint planes and carries updated neighboring columns through12x8 tiles.
Only the chroma-only encoder path is routed; other interleaving stays unchanged.

Initial3200 scalar-oracle cases plus existing pixel/decoder filter tests pass.
Identical public chroma caller fixtures with scalar-verified lower-level ops,
full-buffer comparisons and B.Loop give1.179–1.303x parent in two balanced100ms
rounds, all8cases faster and zero allocations. This is a diagnostic, not fresh
component acceptance. No new1.5x component gain was found in these fixtures.

Fresh six-permutation whole synthetic720p,realtimeCPU8,2500kbps,one thread:
300frames−0.418% encode time versus parent (3/6 faster), +6.760% versus ASM;
60frames−0.900% versus parent (5/6 faster), +11.473% versus ASM. All23
non-timing fields match and allocations stay zero. These modest/mixed whole
results warrant follow-up, not an asserted closure of the ARM assembly gap.
The current production change keeps the independently confirmed1.5x pointer
gather wins; this smaller fusion prototype remains unselected at this checkpoint.
Its complete code and raw evidence are preserved here for continuation.
Initial compile failures were a generated unused z and a test-edit stray plus;
passing retries remain archived alongside them. No README or dependency edits.
