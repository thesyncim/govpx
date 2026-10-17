# ARM rounded-zero VP8 filtering

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, GOMAXPROCS=1.
Parent main20bc81cbbf68985f4619b6d69f3f3d60566ba013; pinned assembly691cd051.
Skip identity corrections while retaining vectors for dependent edges. Macro
|f|<=2 makes all (weight*f+63)>>7 zero for weights9/18/27 and both HEV shifts
zero; internal |f|<=3 makes both primary shifts and the secondary correction
zero. ABS reinterpreted as unsigned preserves -128 as128, so it stays active.
The AST generator checks exactly two macro and two internal blocks. AMD
production output stays byte-identical. Larger corrections keep all original
arithmetic and store order; an earlier vertical changed flag stays set.

Six fresh permutations of main/assembly/current with zero-only as an additional
first/last control,250ms B.Loop,ten identical public Picker cases in all four
snapshots. Each operation restores the read window, applies both orientations,
checks the full allocation against the original separate-edge operations,
and sinks output. All samples are zero B/op and zero allocs/op.

| Tile | Main/current | ASM/current | Minimum paired main/current |
| --- | ---: | ---: | ---: |
| pitch32/flat | 1.843x | 1.658x | 1.824x |
| pitch32/ramp | 1.806x | 1.637x | 1.783x |
| pitch1312/flat | 1.822x | 1.644x | 1.814x |
| pitch1312/ramp | 1.820x | 1.645x | 1.771x |

Every one of these24 flat/gradient pairs beats main by >=1.5x and beats ASM.
Gradient cases also improve1.605–1.610x over the accepted zero-only design
(all12 pairs >=1.562x).
Retain these verified representative component wins under AGENTS.md even if
whole-codec results stay flat. Six other controls range0.991–1.007x main;
pitch1312/noop is0.991x, faster in only1/6pairs. Smooth/mixed still take about
9.7–10.6% more time than ASM. Noop's existing1.84x ASM speed is not a new gain.
All smaller gains, losses and outliers remain in raw samples.

Whole VP8 synthetic1280x720,realtime CPU8,2500kbps,one thread, six balanced
permutations against the same parent and pinned assembly:

* 300frames: +0.606% time versus main (2/6 faster), +8.354% versus ASM (0/6 faster).

* 60frames: +1.042% time versus main (1/6 faster), +12.340% versus ASM (0/6 faster).

All23 non-timing fields match in every whole sample, including output/quality,
quantizers, frame counts and zero allocations. These measurements do not prove
the overall ARM assembly gap is closed. Kernel and whole results stay separate.

Validation: full native ./...; original scalar filter oracles;640 explicit
byte0/127/128/255, cancelling ramp, +/-1 steps, +/-2 nonzero steps and late-only
last-lane changes across pitches20/21/32/1312 and alignments0..7; race/checkptr2;
PGO refresh and fingerprint/noasm/build-selection/precommit. Six fixed-speed
streams match main packet bytes AND drop positions, including partial17x16,
flat/patterned64x64,320x180 and720p/60 and/300. Independent review verifies
rounding boundaries and generator/state behavior. Disassembly shows ABS plus
unsigned UMAXV and branches in both live fused bodies; instruction counts
are diagnostic only. Final generated hot source matches the timed private
candidate byte-for-byte. Timed fixtures are preserved byte-for-byte, alongside
the final gofmt'd fixture/generator/tests. No performance data goes in README.
