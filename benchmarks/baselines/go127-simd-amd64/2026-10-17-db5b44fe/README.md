# Native EPYC 9V45 control at db5b44fe

Run: https://github.com/thesyncim/govpx/actions/runs/39032233232
Three-way artifact: https://github.com/thesyncim/govpx/actions/runs/39032233232/artifacts/12100668761
Exact measured head: db5b44fe245072525cc0dbc39285d5bcf56c0f3a.
Hosted CI and both native jobs pass on this exact head; checks.json records their status.

All 85 raw three-way files are copied unchanged. summary.json,
complete-parity.json and checks.json add analysis/status. Native EPYC 9V45,
Go 1.27.2, SIMD/AVX2, GOAMD64=v1, GOMAXPROCS=1, PGO off. Original ASM
691cd051, parent 9a442f66 and current db5b44fe cover all six variant permutations.
The separate optimized-C job used EPYC 7763, so its absolute times are not
compared to these 9V45 measurements.

| Workload | ASM ms/input | Parent ms/input | Current ms/input | Current vs parent | Current vs ASM |
| --- | ---: | ---: | ---: | ---: | ---: |
| VP9 320p | 4.417586 | 2.502118 | 2.461486 | -1.624% | -44.280% |
| VP8 720p | 11.386280 | 14.331529 | 13.866399 | -3.246% | +21.782% |
| VP9 720p | 37.372808 | 18.952348 | 19.174889 | +1.174% | -48.693% |

All parent/current paired ranges cross zero. Individual control outliers
reach -13.895% / +18.835%, so no x64 change is attributed to the ARM-only
optimization. VP8 remains slower than old ASM in every pair, by 13.646–28.404%.
This measures the final residual-gather implementation on 9V45 and confirms
the CPU-specific VP8 gap remains; it is no longer an unmeasured CPU case.
Both VP9 workloads are faster than ASM in every pair. Results from EPYC
7763 do not establish performance parity on this CPU.

All 22 non-timing semantic/configuration fields agree across all 18 samples
per public workload and corresponding profile runs. Parent/current allocation
samples all match: VP9 320p 0.616667, VP8 zero, VP9 720p 1.25 per input.
ASM VP9 720p round 5 has one extra allocation per 60-frame run (1.266667);
that outlier is preserved. All 75 kernels have six samples per variant and
zero bytes/allocations per call. No timing or allocation sample is discarded.

The quantizer single-block fixture measures Go 4.304 ns versus ASM 3.5485 ns
(+21.291%), slower in all six pairs (16.333–28.001%), with zero allocations.
It uses legacy b.N and does not measure batching. Existing Batch25 / PerBlock25
benchmarks are not selected by this matrix; no direct batch speed ratio is
inferred from the single-block case.

Separate 120-frame profiles confirm Go fastQuantizeBlockBatchSIMD is live:
0.03 s flat / 0.06 s cumulative (3.70% of 1.62 s CPU), including per-block
work. Original fastQuantizeBlockBatchSSE2 is a 0.02 s leaf (1.47% of 1.36 s).
These short independent samples justify investigation, not a precise 3x
batch-speed claim. The current dispatcher loops and reloads tables per block;
identical observable B.Loop batch fixtures are needed for a direct comparison.
