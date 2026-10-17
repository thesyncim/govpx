# ARM vertical filter wide row loads

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, GOMAXPROCS1.
Parent PR35 exact34b00fb4, pinned old assembly691cd051. Fixed internal edges
8/12/16 retain all dependent arithmetic/carries and exact final packed stores.
Initial column transpose uses low8 bytes of16-byte vector row loads; incoming
packets select words1/2/3 from vector loads at column4. Existing wrapper proves
all20 columns of16 disjoint rows. Every read remains within that exact window,
including pitch20, minimal slices and unaligned starts. No padding assumption.

Fresh six balanced parent/ASM/current permutations, with unrolled narrow-load
variant first/last as a fourth control;150ms per10 public Picker fixtures.
All60 paired samples improve versus parent, zero B/op and allocs/op. Fixtures
reset the entire union of read windows within B.Loop, compare full buffers with
the separate public edge operations, and retain observable output; independent
pixel tests use scalar filter equations.

* BenchmarkVP8PickerFilterNative/pitch32/flat: 1.0989x parent, 1.0544x unrolled narrow loads, 1.7548x assembly; 6/6 faster parent and 6/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch32/ramp: 1.1027x parent, 1.0511x unrolled narrow loads, 1.7559x assembly; 6/6 faster parent and 6/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch32/smooth: 1.0276x parent, 1.0150x unrolled narrow loads, 0.9159x assembly; 6/6 faster parent and 0/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch32/mixed: 1.0233x parent, 1.0123x unrolled narrow loads, 0.9166x assembly; 6/6 faster parent and 0/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch32/noop: 1.1117x parent, 1.0713x unrolled narrow loads, 1.9994x assembly; 6/6 faster parent and 6/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch1312/flat: 1.0932x parent, 1.0522x unrolled narrow loads, 1.7670x assembly; 6/6 faster parent and 6/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch1312/ramp: 1.1017x parent, 1.0570x unrolled narrow loads, 1.7656x assembly; 6/6 faster parent and 6/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch1312/smooth: 1.0300x parent, 1.0156x unrolled narrow loads, 0.9244x assembly; 6/6 faster parent and 0/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch1312/mixed: 1.0230x parent, 1.0107x unrolled narrow loads, 0.9168x assembly; 6/6 faster parent and 0/6 faster assembly.
* BenchmarkVP8PickerFilterNative/pitch1312/noop: 1.1211x parent, 1.0762x unrolled narrow loads, 1.9943x assembly; 6/6 faster parent and 6/6 faster assembly.

This is a smaller consistent component improvement, not a new1.5x gain.
Previously accepted >=1.5x gather and other component wins remain intact,
independently of whole-encoder timing. Active textured filters still trail
assembly; flat/ramp/noop controls beat assembly. Do not generalize their ratio
to textured work or the complete codec.

Whole synthetic720p VP8, realtimeCPU8,2500kbps,one thread, six fresh balanced
three-way permutations, all23 non-timing result fields exact and zero allocations:

* 300 frames: -0.5704% encode time versus parent (5/6 faster), +9.6678% versus assembly (0/6 faster).

* 60 frames: -0.6884% encode time versus parent (4/6 faster), +10.6055% versus assembly (0/6 faster).

Validation: complete native ./... PASS; existing7200 fusion cases plus640
zero-correction/later-edge cases, decline guards, race/checkptr2 PASS; six
fixed-speed packet/drop digests exactly match the previously validated PR35
results. Final root refreshed PGO, noasm/build-selection/precommit and full
suite logs are included. Validation and raw logs are included. The regenerated vertical hot body exactly
matches all698 timed instruction words and self-relative rendered branches.
Calls to initial-load/final-store helpers and cold stack growth remain; counts
are diagnostic rather than speed/acceptance rules. AMD generated source and
ARM horizontal function remain byte-identical to parent. Independent review
symbolically checked every lane of both transpose schedules and carried values.

Rejected probes and qualitative profile remain preserved in this archive:
direct signed widening multiply for macro coefficients9/18/27, with generic
int16 fallback for full helper contract, passed all256signed bytes across13
coefficients/extreme ranges. Two balanced100ms diagnostics show no useful
active-filter gain; no production promotion. Fixed-edge narrow-load unroll
alone gives about1–5% diagnostically; wide-load winner is confirmed freshly.
A separate horizontal fixed-edge unroll, based on the accepted vertical variant,
shows only mixed/small active-work differences in two diagnostic rounds
(0.991–1.015x textured,1.013–1.039x identity controls). It is preserved without
production promotion or a claim of a new1.5x gain.
The first wide-load compile had an unused first-edge variable; fixed retry is
retained alongside the failure. Every timing sample and outlier is included.

The post-gather1800-frame profile is qualitative:12.99s samples/13.42s duration,
vertical fusion12.24% cumulative, horizontal6.08%, token writer6.85%, DCT4.54%.
Profile percentages do not establish causal or additive speedups. No concurrent
builds/profiles during timings; independent C/native x64 CI runs on other hosts.
No README, dependency, handwritten assembly or production cgo changes.
