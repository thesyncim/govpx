# Looped pointer-window VP8 residual gather

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, one thread/GOMAXPROCS1.
Parent03afac7c (PR34), pinned assembly691cd051. This looped predecessor
is preserved independently of the subsequent fixed-row follow-up. Keep the existing paired-row
4x4 block-scan arithmetic; use exact pointer row loads and coefficient stores
in the existing public pointer API instead of rebuilding dynamic slices and
checking each SIMD window. Non-nil pointers and0<=strides<2^48 are checked before
unsafe access; max input offset is15*stride+15<2^52. Complete allocations and
nonoverlapping output remain existing PtrFast caller preconditions. Read-only
rows and source/predictor windows may overlap. Declines retain old fallback.

Two fresh six-permutation three-way studies,150ms B.Loop: original two public
fixtures, eight unaligned/minimal-row fixtures with unequal padded strides,
then eight with tightly packed predictors. All18 medians beat parent by1.5x.
Fixture source is identical across parent/ASM/current. New fixtures check every
coefficient against independent scalar math, both input buffers and all output
canaries after B.Loop; global sinks retain work. Every sample is zero B/op and
zero allocs/op. Initial two-round diagnostics are separately preserved.

Median parent/current range 1.5138–1.5614x. 104/108 individual pairs reach1.5x; minimum pair 1.3781x. Preserve all slower/outlier samples; do not claim every pair reaches1.5x.

16x16: 1.3338–1.4237x assembly.

8x8: 0.8590–0.9217x assembly.

Retain correctness-verified component gains under AGENTS.md independently of whole codec. Whole synthetic720p VP8,realtime CPU8,2500kbps,six fresh balanced permutations:

* 300frames: -0.516% encode time versus parent (5/6 faster), +10.570% versus ASM (0/6 faster).

* 60frames: -0.175% encode time versus parent (4/6 faster), +10.111% versus ASM (0/6 faster).

All23 non-timing fields match, allocations remain zero. Kernel and encoder
figures stay separate; no closure of the overall ARM assembly gap is inferred.

Correctness: full native ./..., existing randomized/extreme residual oracles,
336 new public row/alignment/extreme cases, direct selected-path scalar checks
with mutually aliased inputs, exact stride-boundary/nil/disabled declines,
race/checkptr2. AMD selected and actualAVX/AVX2-off correctness runs use Rosetta,
never its timings. Initial test-only compile error attempted to mutate ARM's
constant Enabled flag; the runtime-disabled test now has AMD-only build tags.
Both failure and passing retry remain raw evidence.

Generated code: both sizes are leaf functions without panic/bounds calls on
ARM and AMD. ARM has82/94 instruction lines; AMD128/163, all counts nonzero.
Those are diagnostics, not x64 native speed evidence. New native CI fixtures
and required direct-path/public-row tests measure actual x64 next. No README
or dependency changes. An independent read-only review verifies bounds,
store coverage, lifetimes, fallback and benchmark contracts.
