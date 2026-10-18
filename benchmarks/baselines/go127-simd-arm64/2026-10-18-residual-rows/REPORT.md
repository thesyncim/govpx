# Bounded SIMD residual subtraction

Native Apple M4 Max, Go 1.27.2 darwin/arm64 archsimd, PGO off, GOMAXPROCS=1.
Immediate parent 6a7b1444765e30fcbcc4a5aca8abd1af73dd9013 includes the repaired
AMD vertical carrier from PR #37 and the SSE winner from PR #38. Old assembly
reference 691cd0512c4829d9ee40d483205dd223f9eb028d. No cross-CPU attribution.

Adapt the sixteen-column residual layout from goav1 residualBlockSIMD at
0f40b2bd848d8c5c7bdbaa016f2ced3085f7fc7a, with no module dependency. ARM loads
16 pixels and stores two eight-coefficient packets. AMD widens to AVX2, stores
16 coefficients and defers upper-state cleanup. Dedicated four/eight-pixel
kernels read exactly 4/8 bytes, compute signed int16 residuals and store exactly
4/8 coefficients. Four-pixel unused SIMD lanes remain zero. Fused OR over all
computed residuals preserves nonzero even if later output rows overlap earlier.

The dispatcher proves widths 4/8 or 16-multiples through 64, h=1..64 and nonnegative
strides<2^30. Complete visible input/output lengths are checked before unsafe
access. Their row offsets fit below 2^36, output byte offsets below 2^37. Typed
row pointers advance only when another row remains, never forming an unused
out-of-object final pointer. Zero/overlapping strides and read-only input
aliases remain valid; output/input overlap conservatively declines BEFORE
stores, preserving the original eight-lane/scalar partial-write ordering.
Short windows decline to the original checked fallback, preserving its panic
and whole-buffer post-panic state. Larger/unsupported geometry and CPU-off keep
the parent behavior. No handwritten assembly, production cgo or new dependency.

Fresh six balanced parent/assembly/current orders, 100ms per 48 identical public
B.Loop fixtures: eight shapes, two stride/output layouts and random/max-error/
identical patterns. Origins 3/5 and coefficient origin 1, exact visible windows,
independent per-coefficient and boolean oracles before/after timing, complete
input and output backing canaries, and observable outputs. Every kernel sample
has zero B/op and allocations. Ranges cover the three pixel patterns.

| Shape / padded output | Parent/current time | Old-reference/current public time | Minimum paired parent/current |
|---|---:|---:|---:|
| 4x4 / false | 1.7523–1.8500x | 0.8543–0.8722x | 1.6549x |
| 4x4 / true | 1.7127–1.7593x | 1.7472–1.8283x | 1.6500x |
| 8x8 / false | 1.9575–1.9798x | 0.8544–0.8649x | 1.9019x |
| 8x8 / true | 1.9553–1.9709x | 4.0927–4.1167x | 1.9165x |
| 16x8 / false | 1.9493–1.9972x | 0.8406–0.8547x | 1.9011x |
| 16x8 / true | 1.9596–1.9753x | 5.6800–5.7577x | 1.8843x |
| 16x16 / false | 2.3303–2.3489x | 0.9679–0.9695x | 2.2758x |
| 16x16 / true | 2.3022–2.3399x | 7.0503–7.0544x | 2.1890x |
| 32x16 / false | 2.8509–2.8791x | 1.2358–1.2446x | 2.8249x |
| 32x16 / true | 2.8732–2.8778x | 9.5630–9.6164x | 2.8185x |
| 32x32 / false | 3.0018–3.0183x | 1.2128–1.2208x | 2.9580x |
| 32x32 / true | 2.9780–3.0012x | 9.7693–9.8481x | 2.8851x |
| 64x32 / false | 2.9400–2.9565x | 1.4187–1.4240x | 2.8599x |
| 64x32 / true | 2.9390–2.9607x | 10.1137–10.1756x | 2.9119x |
| 64x64 / false | 2.6489–2.6586x | 1.3244–1.3399x | 2.6205x |
| 64x64 / true | 2.4551–2.5719x | 8.5205–8.8549x | 2.1827x |

All 48 cases qualify at 1.7127–3.0183x parent;
all 288 paired samples are at least 1.6500x. Preserve these verified
component wins independently of overall codec timing. Contiguous 32/64-column
cases beat the old NEON path, while smaller contiguous cases still trail it.
The old reference rejects outStride!=w and uses SCALAR for every padded-output
fixture; its larger padded ratios are public fallback wins, not claims of
beating active assembly instructions by that factor.

Whole codec, synthetic input, realtime CPU 8, one thread, PGO off, six balanced
rounds (positive means more time/slower):

* vp9-320p: -1.3368% versus parent (6/6 faster), +8.8426% versus old assembly (0/6 faster).

* vp9-720p: -2.9800% versus parent (6/6 faster), +35.2237% versus old assembly (0/6 faster).

* vp8-720p-control: +0.4937% versus parent (2/6 faster), +11.9029% versus old assembly (0/6 faster).

VP9 workloads are 320x180 / 120 frames / 500 kbps and 1280x720 / 60 frames / 2500 kbps;
VP8 control is 1280x720 / 60 frames / 2500 kbps. All 22 semantic fields match across
all variants and every allocation sample matches: existing VP9 allocations
are 0.6166667 and 1.25 per input respectively; VP8 allocates zero. Matching output
statistics do not assert bitstream identity. The whole ARM assembly gaps remain
open. Subtract has no VP8 production caller, so its mixed-pair +0.4937% VP8 result
is retained as a control, with no claimed benefit or causal attribution. These
Go-assembly comparisons do not establish equivalent libvpx C performance; C
has separate native CI jobs and output/quality scope.

Correctness: full native suite, ARM race/checkptr2, combined AMD residual/
subpixel/SSE checkptr under Rosetta, and actual AVX-off fallback pass. New tests
run 5,760 geometry/pattern/alias iterations (5,040 also call the selected bounded
helper), with os0/1/w-1/contiguous/padded, h65 fallback, minimum windows and
mutual read-only aliases. Sixty-six output/input overlap geometries compare
complete backing state to the copied parent kernel, and 45 short-window
geometries compare recovered-panic backing state. Both src/pred aliases and
exact adjacency are covered. Independent source/arithmetic/bounds/CI review
passes. Root files exactly match the final measured private candidate.

Saved optimized Darwin/amd64 code has VEX-only wide inner-loop arithmetic and
normal-return upper-state cleanup. Legacy prologue initialization occurs before
AVX use; no whole-function all-VEX claim. Generated code and Rosetta are not
native x64 speed evidence. Corrected AMD subpixel source remains unchanged at
SHA256 5b21747a9df17eb8957dc2424b64dacf2da9986621a90bc779428ea414638aa4.
Native x64 performance is pending this successor's exact-head CI.

PGO refreshed, fingerprint/noasm/SIMD-selection/pre-commit gates pass. Native
harness retains 282 previous controls and adds 48 fixtures for 330 total, copying
the same fixture to both references, recording 14 source hashes and requiring
all three new tests in normal/race preflight. Benchmark timeout grows 35→45 min
to accommodate the larger suite; no correctness threshold is relaxed. README
files remain untouched.

All raw samples, diagnostics, source variants and drivers are retained. The
initial wide-only source qualified at 1.9–3.0x but regressed the unchanged 4x4
control; moving row pointers removed integer/address pressure, then dedicated
small-width SIMD removed that regression. Final fresh runs confirm all 48 gains
independently. No competing local Go builds/tests/profiles ran during timing.
Shared-desktop results do not isolate small control shifts or support adding
gains from different checkpoints/CPUs.

The successor was rebased onto merged main 8ee816b14603fd34dccbcb629f021cf5ee57e396, whose tree exactly equals the measured parent6a7. The complete successor tree was unchanged by the rebase (proof in rebase-proof.json), and all six measured source SHA256 values still match. Only benchmark evidence and source-provenance documentation were added after the production validation. The original immediate-parent hashes above remain the measurement identifiers.
