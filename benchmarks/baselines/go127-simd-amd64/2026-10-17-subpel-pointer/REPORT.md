# AMD subpixel row-load candidate, native performance pending

Parent c3930581; AMD production is identical to mergedmainf56c66d7. This targets
subpelStats16, sampled at0.22sflat/0.24scumulative in PR35's4.52s EPYC7763 VP9
profile. That short profile establishes liveness, not the cause of the measured
VP9 regressions. Existing VP9 kernel controls stayed approximately flat.

Keep all AVX2 sixteen-pixel arithmetic, two bilinear rounding steps, paired
dot-product accumulations and deferred upper-lane fence. Replace repeated row
slices by exact16-byte array loads only after an OR/shift guard proves every
geometry component nonnegative and below2^30. With public w,h positive and
w divisible16, source/ref endpoints and even final unused row increments stay
below2^60. The original two endpoint checks prove all reads, including the
extra bilinear row/column, without padding. Zero/overlap strides and mutually
aliased read-only inputs remain valid. The original checked-slice function is
kept byte-identical after renaming for unusualgeometry, preserving its previous
overflow/panic behavior. No pointer is stored or retained.

Independent review passed bounds, overflow, carries/lifetimes, copied fallback
arithmetic and fixture fairness. Initial wrapper added a hot call; its source
and codegen remain archived. Final selected fast function has no added hot
helper call. Diagnostic cross-codegen drops243 to191 instruction lines and12
to2 bounds-panic sites; only the entry checks remain. Counts are not native
latency or acceptance evidence. All existing qualifying kernels remain intact.

Rosetta correctness only: existing full signed/extreme SubpelSixteenLane tests;
12,288 new legal-phase/alias/zero-overlap-stride/minimal-window cases plus
unusual rs1<<30/MAXINT h1 fallback and short-span panic checks; final root AMD
checkptr2 run passes. Full public DSP all-phase/size tests pass, actual AVX/AVX2
off fallback passes, and all40 new fixtures pass for candidate, CPUoff and
pinned assembly. Emulated ns/op values in1x smoke logs are not speed evidence.

New B.Loop public fixtures:16x8/16x16/32x16/16x32, five phases(fullpel,
horizontal-only,vertical-only,half,both), minimal and unequal padded strides.
Sources start at unaligned3/5, visible spans are exact and backing buffers
include checked read-only canaries. Independent scalar per-pixel equations
retain both rounding stages; final variance and SSE, full input immutability
and observable sinks keep work live. Pinned assembly's phase shortcuts are
legitimate algorithms and remain in the comparison.

Native harness preserves all206 older cases and adds the identical40 fixture
source to ASM/main/current, for246 total, six balanced permutations at150ms.
It records seven benchmark/production hashes and requires selected/extreme
pixel tests, all public subpixel tests and targeted race/checkptr2 before any
timing. Existing35-minute allowance remains. Merge/performance selection waits
for exact-head native results; no current x64 speedup or regression fix claimed.
README files, dependencies, project assembly exclusion and generic fallbacks
remain unchanged. Root PGO/validation logs are preserved with their results.
