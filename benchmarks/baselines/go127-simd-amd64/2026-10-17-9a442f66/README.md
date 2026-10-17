# Native AMD 9a442f66 benchmark-harness control

Hosted CI: https://github.com/thesyncim/govpx/actions/runs/39031352613
Native workflow: https://github.com/thesyncim/govpx/actions/runs/39031352624
Three-way artifact: https://github.com/thesyncim/govpx/actions/runs/39031352624/artifacts/12100503682
All three jobs pass on exact head 9a442f6635fa1d430dbb1f58d95c038bf21de83f:
hosted exhaustive checks, native ASM/parent/current and optimized libvpx.
The separate libvpx artifact is in the sibling 2026-10-17-9a442f66-libvpx folder.

All 85 three-way raw files are preserved unchanged; summary.json,
complete-parity.json and checks.json retain analysis and exact-head status.
Native EPYC 7763, Go 1.27.2, AVX2/SIMD, GOAMD64=v1, GOMAXPROCS=1, PGO off.
Original ASM 691cd051, parent 211125e5 and current 9a442f66 run in six rounds
covering every variant permutation. This checkpoint changes the benchmark
harness, not codec arithmetic. Small changes are not attributed to a codec
optimization or dismissed as universal noise.

| Workload | ASM ms/input | Parent ms/input | Current ms/input | Current vs parent | Current vs ASM |
| --- | ---: | ---: | ---: | ---: | ---: |
| VP9 320p | 6.956120 | 4.311819 | 4.318367 | +0.152% | -37.920% |
| VP8 720p | 22.935057 | 21.865938 | 21.938879 | +0.334% | -4.343% |
| VP9 720p | 59.490460 | 32.083129 | 31.963131 | -0.374% | -46.272% |

VP9 320p is slower in 5/6 parent pairs (-0.080..+0.569% time),
VP8 is slower in all 6 pairs (+0.017..+1.028%), and VP9 720p is faster
in all 6 (-0.879..-0.146%). All current/ASM pairs are faster for each public
workload. These scoped harness controls remain in the evidence; different
absolute times from another job do not isolate code changes.

All 22 non-timing semantic/configuration fields and allocation samples match
across all 18 samples per workload and across corresponding profile runs.
Allocations are VP9 320p 0.616667, VP8 zero and VP9 720p 1.25 per input;
profile samples are 0.625 / 0 for VP9 / VP8. All 75 kernel cases retain
six samples per variant and zero bytes/allocations per call. No pair or
allocation outlier is discarded. Hosted independent byte/oracle checks pass.

This comparison uses the old Go ASM baseline. The separate optimized-C
comparison exposes substantial additional gaps, and its differing quality
cases are not equivalent work. EPYC 9V45 VP8 and native ARM gaps remain open.
