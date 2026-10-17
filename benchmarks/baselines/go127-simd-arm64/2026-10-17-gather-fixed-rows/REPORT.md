# Fixed-row pointer SIMD residual gather

Native Apple M4 Max,Go1.27.2 archsimd,PGOoff,GOMAXPROCS1. Parent03afac7c
merged as9f333e01 with identical tree; pinned assembly691cd051. The looped
pointer predecessor already gave1.5x representative gains and remains fully
preserved in the separate looped-gather archive. Fixed row groups remove its
loop address setup while retaining exact paired-row arithmetic/store order.
Guards, input/output bounds and aliases keep the same proven pointer contract.

Fresh six balanced main/ASM/current permutations, with the looped predecessor
as an additional first/last alternating control. All four binaries use exactly
the same18 public-operation fixtures: original two, eight unaligned/minimal
row windows with unequal padded strides, and eight tightly packed predictors;
random and extreme values. New fixtures check all coefficients with independent
scalar equations, full input immutability and output canaries after B.Loop.
150ms per fixture, zero bytes and allocations in every sample.

8x8: 1.9598–2.1740x main; 1.1035–1.3026x assembly; 1.2858–1.4137x the looped pointer version.

16x16: 1.6631–1.8438x main; 1.5232–1.5883x assembly; 1.0670–1.1706x the looped pointer version.

All108 paired samples beat main by at least 1.630x and beat assembly. Keep these verified component wins independently of overall codec results under AGENTS.md. Every raw sample/outlier and the earlier diagnostic remain archived.

Whole synthetic720p VP8,realtimeCPU8,2500kbps,one thread,six fresh balanced three-way permutations using the final root binary:

* 300frames: -0.790% encode time versus main (6/6 faster), +9.600% versus assembly (0/6 faster).

* 60frames: -0.936% encode time versus main (5/6 faster), +12.068% versus assembly (0/6 faster).

All23 non-timing result fields match and allocations stay zero. Whole and
component results remain separate; the overall ARM assembly gap is still open.

Validation: full native ./...,336 public repeated/overlapping-row, alignment
and extreme cases plus existing randomized/minimal-window oracles; explicit
successful helper assertions with mutually aliased read-only inputs; nil,
stride1<<48/negative and disabled declines; race/checkptr2; six fixed-speed
packet bytes/drop-position digests exactly matching parent; refreshed PGO and
fingerprint/noasm/build-selection/precommit. The first fixed-packet command
matched no tests (plural typo); its log is retained and the corrected singular
run executes all six cases. No empty-run claim is used.

Final ARM hot instruction words exactly match the timed prototype:108 lines
for8x8 and282 for16x16. Only leading addresses and in-function absolute branch
renderings are normalized; raw instruction words also compare exactly.
Both ARM functions are leaf code without bounds-panic calls. AMD cross-codegen
uses146/399 instruction lines, no bounds-panic or arithmetic calls;16x16 has a
cold stack-growth call and stack scratch for row addresses. These counts are
diagnostics, not acceptance criteria or native x64 performance claims.
AMD selected/actualAVX-off arithmetic and all18 fixture checks pass under
Rosetta for correctness only. Native x64 CI measures the final production
candidate before merge. No README or dependency changes.
