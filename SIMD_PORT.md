# Go 1.27 native SIMD migration

The migration starts from `main` at `691cd051` and removes all 96 tracked
hand-written assembly files, their assembly declarations, and the custom
CPUID package. The module requires Go 1.27 and selects toolchain Go 1.27.2.
`make` and `scripts/go.sh` enable `GOEXPERIMENT=simd` by default. Applications
using govpx should also build with that experiment to select native kernels:

```sh
GOEXPERIMENT=simd go build ./...
```

ARM64 uses `simd/archsimd`; amd64 dispatch checks `archsimd.X86.AVX2()`.
`-tags purego`, `GOEXPERIMENT=`, unsupported architectures, and amd64 CPUs
without AVX2 retain scalar Go implementations. No module dependencies were
added. Compiler-generated machine code and standard-library implementations
are outside the repository's assembly exclusion check.

## Kernel coverage

| Family | Go implementation |
| --- | --- |
| Pixel search/statistics | SAD, row-limited SAD, four-reference SAD, variance/SSE, fused bilinear subpixel variance, projections and vector variance |
| Prediction/filtering | VP8 six-tap and bilinear, VP9 eight-tap and averaging, DC/TM/V/H and directional intra, VP8/VP9 loop filters |
| Residuals | Subtraction, nonzero detection, VP8 block-scan gather, coefficient error/energy/absolute sums |
| VP8 transforms | Forward/inverse DCT and Walsh, DC-only and paired reconstruction, dequantization and quantization |
| VP9 transforms | Forward DCT 4/8/16/32 and ADST 4/8/16, inverse DCT 4/8/16/32 and ADST hybrids, sparse/DC variants, lossless Walsh, Hadamard and quantization |
| Temporal processing | ARNR accumulation and VP8 denoiser first pass |
| Bitstream packing | Serial Go bit-packing replaces the former assembly helper; its bit-order contract remains independently tested |

The new 32-point inverse DCT and directional intra paths extend SIMD
coverage beyond the former assembly dispatch. Scaled-coordinate convolution
and other existing scalar operations retain
Go implementations.

The self-contained `internal/pixel` package holds copied goav1 SAD,
pixel-statistics, convolution, and widening/reduction kernels plus local
VP8/VP9 glue. [UPSTREAM.md](internal/pixel/UPSTREAM.md) pins the source commit
and adaptations; `third_party/goav1` retains its license and notices. The
available gopus audio kernels were inspected but are not required here.
Codec-specific rounding, clipping after each filter pass, scan orders, and
wrapping int16 narrowing remain govpx's responsibility.

`scripts/gensimd.go` generates four-lane VP9 transform networks from the
independent scalar functions and emits specialized DCT pipelines with
fixed-size scratch space. Regenerate with:

```sh
scripts/go.sh run scripts/gensimd.go
```

ARM64 vertical eight-tap filtering reuses seven rows in registers as the
window advances. Bounds-checked entry points cover exact final-row lengths;
unsafe loads read only four, eight, or sixteen bytes within the proven
source allocation. Pixel tests cover misalignment, minimal buffers, signed
filter lobes, coefficient extremes, tails, permitted aliases, and zero
allocations. Codec tests retain independent scalar/libvpx references.

## Validation

The validation environment is official Go 1.27.2, Apple M4 Max,
darwin/arm64, with `GOEXPERIMENT=simd`. AVX2 parity was also executed under
Rosetta with AVX advertisement enabled; that is correctness evidence, not
native x86 performance evidence. Runtime AVX2-disabled execution selects
the scalar backend separately from experiment-disabled builds.

The local checks include:

- Full native and purego package suites; experiment-disabled short suite.
- Native SIMD selection, six architecture/build-tag selection checks, and
  repository assembly exclusion.
- Race and checkptr=1 tests on pixel and both codecs' DSP/encoder packages.
- `go vet`, Linux amd64/arm64 builds, and a scalar Linux riscv64 build.
- Oracle trace compilation, decoder conformance, quality gates, libvpx DSP
  and codec oracles, pinned encoder byte parity, and parity regression report.
- Fresh VP8 encode/quality and VP9 realtime PGO profiles and source fingerprint.

Checkptr=2's numerical tests also pass. Its forced heap escapes change the
allocation assertions in pointer-heavy VP8 entry points; release and
checkptr=1 zero-allocation checks pass.

Use `scripts/go.sh` for Go commands: it invokes Codex's `project-env` wrapper
when installed, otherwise derives a project-wide cache from `git-common-dir`.
`CODEX_AGENT_ID` keeps concurrent agents' temporary build directories separate.

```sh
make verify-production
GOEXPERIMENT= scripts/go.sh test -short ./...
scripts/go.sh test -race ./internal/pixel ./internal/vp8/dsp ./internal/vp9/dsp ./internal/vp8/encoder ./internal/vp9/encoder
GOOS=linux GOARCH=amd64 scripts/go.sh build ./...
GOOS=linux GOARCH=arm64 scripts/go.sh build ./...
```

CI fixes preserve the existing quality thresholds: BD-rate now rejects
duplicates on its independent PSNR axis instead of rejecting valid equal
encoded rates; the ARNR fixture holds reference scheduling fixed while
toggling filter strength; the VP8 oracle trace helper disables libvpx's
interactive warning prompt. Both original quality failures were reproduced
on unchanged main with exactly the same curves.

Hosted CI also exposed adaptive-timing noise in VP8 oracle fixtures.
The threaded byte matrix now pins its realtime floor with `CpuUsed=-4`.
The inter-mode distribution report pins speed 8 with `CpuUsed=-8`, using
fresh expected curves generated from unchanged `691cd051` with the same
fixed-speed fixture. Its percentage-point, L1, and EOB tolerances remain
unchanged; the independent 720p baseline now has L1=0 and EOB ratio=1.
Adaptive-control coverage remains in the other oracle tests. CI retains
the complete parity log as an artifact for future failed assertions.

The [complete hosted CI run at `b9badbe3`](https://github.com/thesyncim/govpx/actions/runs/38950894545)
passes all production gates, including native/purego suites, decoder
conformance, encoder quality, libvpx oracles, exact encoder byte parity,
and the 18/18 parity regression report.

## Performance and remaining limits

The migration removes architecture-specific assembly maintenance; it does
not establish performance parity with the removed assembly. Common inverse
DCT setup and the rolling vertical filter are substantially faster than the
initial generic SIMD ports, and native SIMD beats scalar Go on the measured
hot kernels. Some kernels and the representative encoder workload remain
slower than the assembly baseline. The PR remains draft while that gap is
visible, and the historical README numbers should not be interpreted as
measurements of this implementation.

The initial migration measurements below compare `691cd051` with `d5374fe0` using the
same Go 1.27.2 compiler, experiment, CPU and `-pgo=off`. Kernel medians use
five runs with 150ms per benchmark. The public encoder comparison alternates
six baseline and six SIMD runs: VP9 realtime, 320x180, 120 frames, 30 fps,
500 kbps, CPU-used 8, one thread, encode-only, external comparison disabled.

| Operation | Assembly-era dispatch | Go SIMD dispatch |
| --- | ---: | ---: |
| VP8 SAD 16x16 | 7.96 ns | 9.69 ns |
| VP9 inverse DCT 4x4 | 31.26 ns | 15.05 ns |
| VP9 inverse DCT 8x8 | 16.71 ns | 104.60 ns |
| VP9 forward DCT 16x16 | 83.03 ns | 217.30 ns |
| VP9 horizontal filter 16x16 | 20.71 ns | 164.40 ns |
| VP9 vertical filter 16x16 | 22.01 ns | 112.60 ns |
| VP9 subpixel variance 16x16 | 26.36 ns | 86.32 ns |
| VP9 public encoder | 1.474 ms/frame | 2.013 ms/frame |

The public encoder is **36.6% slower** on this workload. All twelve runs
produce the same 246,893 output bytes, 117 encoded/3 dropped frames,
quantizer statistics, and 0.617 allocations per input frame. The measured
range is 1.466–1.482 ms/frame for assembly and 2.008–2.043 ms/frame for SIMD.
This is one CPU/workload, not a general throughput or statistical confidence
claim. The forward 16x16 scalar reference measures about 545 ns; the SIMD
port remains faster than scalar while falling behind the removed assembly.

Raw results and commands are retained in
[benchmarks/baselines/go127-simd-arm64](benchmarks/baselines/go127-simd-arm64).
The public workload's CPU profile confirms live calls through the pixel
kernels. Generated ARM64 code also contains widening vector multiplies,
vector additions, and byte narrowing in the selected filter body. Its
multiply/add instruction sequence and the four-lane transform layout leave
further tuning opportunities; no x86 performance claim is made from Rosetta.

### Subsequent optimization checkpoint

The next pass adds actual eight-lane AVX2 DCT8/16 pipelines, an ARM64
int16 inverse DCT8 pipeline, sixteen-pixel AVX2 convolution, exact narrowed
int16 convolution on both architectures, and fused FP quantization/EOB/token
classes. Four-pixel convolution now reaches the native backend. The wider
transforms include sparse and full signed-int16 parity tests; FP token-class
tests require the fused native path when SIMD is enabled.

On ARM64, inverse DCT8 now measures about 32 ns, forward DCT16 about 180 ns,
horizontal filtering about 98 ns and vertical filtering about 70 ns, with
zero allocations. Six new alternating public VP9 encoder pairs measure
1.889 ms/frame for SIMD versus 1.518 ms/frame for the assembly baseline
(24.4% more time); every run still emits the same 246,893 bytes. The initial
36.6% regression above is historical. ARM performance work remains open.

Native Linux/amd64 measurements on an AMD EPYC 7763, before these latest
wider kernels (commit `818e4630`), measured 5.408 ms/frame versus 6.995
ms/frame for the same assembly-era public VP9 workload: 22.7% less time.
All twelve runs agreed on bytes, encoded/dropped frame counts and allocations.
This result is specific to that CPU/workload; the old x64 dispatch had less
assembly coverage than ARM64. Horizontal convolution was still slower in
that checkpoint.

### Native amd64 measurements at `bb1a6ef3`

The next hosted run used an AMD EPYC 9V74, which is a different CPU from
the previous EPYC 7763 runner. Each comparison below uses baseline
`691cd051` and SIMD `bb1a6ef3` on the same runner, official Go 1.27.2,
`GOEXPERIMENT=simd`, `GOAMD64=v1`, `GOMAXPROCS=1`, and `-pgo=off`. Native
SIMD selection was asserted with `GOVPX_REQUIRE_SIMD=1`. Six pairs alternate
assembly/SIMD and SIMD/assembly order; kernel samples run for 150 ms.

| Operation | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| VP8 SAD 16x16 | 8.204 ns | 12.505 ns | +52.4% |
| VP9 inverse DCT 4x4 | 55.84 ns | 17.76 ns | -68.2% |
| VP9 inverse DCT 8x8 | 289.25 ns | 46.46 ns | -83.9% |
| VP9 forward DCT 16x16 | 1,074.5 ns | 187.90 ns | -82.5% |
| VP9 horizontal filter 16x16 | 233.10 ns | 87.38 ns | -62.5% |
| VP9 vertical filter 16x16 | 618.70 ns | 87.665 ns | -85.8% |
| VP9 subpixel variance 16x16 | 146.65 ns | 85.92 ns | -41.4% |
| VP9 realtime 320x180 | 5.498 ms/frame | 3.607 ms/frame | -34.4% |
| VP8 realtime 720p | 14.040 ms/frame | 45.187 ms/frame | +221.9% |
| VP9 realtime 720p | 47.558 ms/frame | 28.475 ms/frame | -40.1% |

Both 720p workloads encode 60 input frames at 2,500 kbps, CPU-used 8,
one thread, with encode-only mode and external comparison disabled. The
320x180 case retains the 120-frame, 500 kbps configuration described above.
All kernels report zero allocations. The forward DCT scalar control is
unchanged within these samples: 1,074.0 ns versus 1,073.5 ns.

The VP8 720p regression is substantial and unresolved at this checkpoint:
every paired SIMD run takes 220.8–223.1% more time. The VP9 paired time
changes are -34.1 to -34.7% at 320x180 and -39.8 to -40.6% at 720p.
These results support workload-specific VP9 gains; they do not establish
overall performance parity with the removed assembly.

Every run agrees on output byte counts, encoded/dropped frame counts,
quantizer statistics/histograms, and allocations within each workload.
These JSON reports retain byte counts, not bitstream hashes; exact codec
bytes are checked separately by the oracle parity gates. VP8 720p
produces 685,957 bytes, 58 encoded/2 dropped frames, and zero allocations
per input frame. VP9 720p produces 642,654 bytes, 48 encoded/12 dropped
frames, and 1.25 allocations per input frame. The 320x180 output remains
246,893 bytes, 117 encoded/3 dropped frames, and 0.617 allocations per
input frame.

[The native amd64 workflow run](https://github.com/thesyncim/govpx/actions/runs/38947952120)
retains raw samples, encoder JSON, CPU/compiler metadata, and the dispatch
assertion. Do not compare absolute timings between its EPYC 9V74 and the
earlier EPYC 7763 run. The workflow now captures separate VP8/VP9 720p CPU
profiles after all timed pairs, with raw profiles and flat/cumulative top
tables, to guide the remaining work without changing the timing samples.

### Loop-filter and metric follow-up

The next checkpoint replaces the generic narrow loop-filter arrays with
named registers, adds a sixteen-position AVX2 filter, and copies goav1's
ARM64 byte-domain filter with full VP8/VP9 masks and macroblock corrections.
Vertical loads/stores use register transposes and touch exactly the original
modified columns. Entry bounds and minimal/overlapping-layout tests cover
unsafe row I/O; the block-limit-255 case retains exact int16 arithmetic.

Local ARM64 comparisons reduced VP8 720p from 18.74 to 14.40 ms/frame with
the narrow-register filter, then to 13.71 ms/frame with the copied byte
pipeline before the final bounded row-I/O tuning. These are intermediate
comparisons; the assembly baseline remains faster. Variance64x64 improved from 508 to 309 ns,
and a fused SAD/sum/SSE pass reduced bottom-edge source metrics from 915
to 203 ns. All measured kernels retain zero allocations.

### Native amd64 measurements at `b9badbe3`

The next [native workflow run](https://github.com/thesyncim/govpx/actions/runs/38950894532)
used an AMD EPYC 7763 with AVX2 and no AVX512, rather than the EPYC 9V74
used at `bb1a6ef3`. Both variants in this run use the same compiler and
runner settings described above. Compare each SIMD result with its paired
assembly baseline; the different hosts prevent attributing changes between
these checkpoints solely to the code.

| Operation | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| VP8 loop filter, horizontal luma edges | 34.94 ns | 452.95 ns | +1,196.6% |
| VP8 loop filter, vertical luma edges | 116.90 ns | 1,257.00 ns | +975.3% |
| VP8 fast quantization | 6.864 ns | 39.955 ns | +482.1% |
| VP8 forward DCT 4x4 | 8.770 ns | 15.370 ns | +75.3% |
| VP9 inverse DCT 4x4 | 70.805 ns | 22.570 ns | -68.1% |
| VP9 inverse DCT 8x8 | 373.75 ns | 185.10 ns | -50.5% |
| VP9 forward DCT 16x16 | 1,281.50 ns | 242.25 ns | -81.1% |
| VP9 horizontal filter 16x16 | 287.60 ns | 98.555 ns | -65.7% |
| VP9 vertical filter 16x16 | 802.85 ns | 98.190 ns | -87.8% |
| VP9 realtime 320x180 | 7.112 ms/frame | 5.504 ms/frame | -22.6% |
| VP8 realtime 720p | 23.784 ms/frame | 121.698 ms/frame | +411.7% |
| VP9 realtime 720p | 60.948 ms/frame | 43.964 ms/frame | -27.9% |

All six VP8 encoder pairs take 409.6–418.9% more time with SIMD. The VP9
paired ranges are 22.0–23.1% less time at 320x180 and 27.3–29.2% less time
at 720p. VP8 remains a major unresolved performance gap. Every sample
retains the output byte counts, encoded/dropped frame counts, and quantizer
statistics/histograms listed above. VP8 and the smaller VP9 workload also
retain their allocation counts. One of six VP9 720p SIMD samples records
76 allocations over 60 input frames rather than the 75 in all other
SIMD and assembly samples; the median remains 1.25 allocations per frame.
All kernel samples report zero allocations.

Separate profiles, captured after the timing pairs, retain raw `.pgo` files
and flat/cumulative top tables for both codecs and variants. In the SIMD
VP8 profile, loop filtering accounts for 48.9% cumulative CPU time;
`packBytes` and `wideBytes` account for 18.9% and 13.1% flat time.
`storeNarrowEdge16` accounts for 28.8% cumulative time, including packing.
Fast quantization accounts for 9.0% cumulative time. These profiles point
to byte packing, edge I/O, and quantization for the next optimization pass;
their timings are excluded from the encoder comparison above.

### Native amd64 measurements at `6a7b0345`

The next checkpoint fuses VP8 quantization/EOB, specializes small statistics
and bilinear/subpixel shapes, and repairs AVX/legacy-SSE transitions around
wide kernels. The Go 1.27.2 compiler emits legacy 128-bit register moves in
some mixed-width code. Wide return boundaries now clear upper vector bits;
the loop-filter handoff stores its wide results before entering 128-bit
packing, with the clear before the callee's loads.

The [native run](https://github.com/thesyncim/govpx/actions/runs/38953587137)
uses an AMD EPYC 9V45 with AVX2 and AVX512, a different CPU from the preceding
EPYC 7763 run. It cannot isolate the transition repair on that older CPU.
Within-run baseline/compiler/settings and the six alternating pairs remain
the same as above.

| Operation | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| VP8 horizontal luma loop-filter edges | 28.325 ns | 58.755 ns | +107.4% |
| VP8 vertical luma loop-filter edges | 88.310 ns | 177.950 ns | +101.5% |
| VP8 fast quantization | 3.603 ns | 5.174 ns | +43.6% |
| VP9 inverse DCT 8x8 | 221.950 ns | 40.205 ns | -81.9% |
| VP9 forward DCT 16x16 | 806.950 ns | 150.800 ns | -81.3% |
| VP9 subpixel variance 16x16 | 114.500 ns | 54.475 ns | -52.4% |
| VP9 realtime 320x180 | 4.587 ms/frame | 2.656 ms/frame | -42.1% |
| VP8 realtime 720p | 12.271 ms/frame | 20.485 ms/frame | +66.9% |
| VP9 realtime 720p | 39.922 ms/frame | 21.715 ms/frame | -45.6% |

The VP8 encoder still takes 64.0–73.3% more time in every pair. The VP9
paired ranges are 39.7–43.3% less time at 320x180 and 44.9–47.2% less time
at 720p. Counts, encoded/dropped frames, and quantizer statistics agree in
every sample. VP8 and VP9 320x180 allocations agree in every sample; one
VP9 720p assembly sample records one extra allocation over 60 input frames,
while all six SIMD samples retain 1.25 allocations per input frame.
All kernel samples allocate zero bytes.

The separate VP8 profile attributes 33.1% cumulative time to loop filtering,
26.7% to the sixteen-position narrow filter, and 12.7% to statistics.
`packBytes` accounts for 2.1% flat time; `wideBytes` no longer appears in
the top 40. These profiles and the changed CPU do not establish how much
each individual optimization contributes. Full raw timing/profile evidence,
CPU metadata, a summary, and exact count/allocation details are retained in
[the dated checkpoint directory](benchmarks/baselines/go127-simd-amd64/2026-10-16-6a7b0345).

### Native amd64 byte-domain results at `ff9395d0`

This checkpoint extends the byte-domain loop filter to amd64, avoiding its
wide int16 packing path, and specializes full-height statistics. The
[native run](https://github.com/thesyncim/govpx/actions/runs/38955379654)
uses an EPYC 7763 with AVX2 and no AVX512. This is the same CPU model as
the `b9badbe3` run, but a different hosted VM; only the baseline/SIMD pairs
within each run share the same host. Multiple intervening changes prevent
isolating the transition repair from the byte-filter and statistics work.

| Operation | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| VP8 horizontal luma loop-filter edges | 34.875 ns | 92.345 ns | +164.8% |
| VP8 vertical luma loop-filter edges | 116.900 ns | 171.100 ns | +46.4% |
| VP8 fast quantization | 6.870 ns | 8.116 ns | +18.1% |
| VP9 inverse DCT 8x8 | 373.500 ns | 63.780 ns | -82.9% |
| VP9 subpixel variance 16x16 | 207.850 ns | 85.615 ns | -58.8% |
| VP9 realtime 320x180 | 6.996 ms/frame | 4.489 ms/frame | -35.8% |
| VP8 realtime 720p | 23.187 ms/frame | 28.039 ms/frame | +20.9% |
| VP9 realtime 720p | 60.011 ms/frame | 34.547 ms/frame | -42.4% |

VP8 remains 20.7–21.7% slower in every pair. VP9 takes 35.1–36.2% less
time at 320x180 and 41.8–43.2% less time at 720p. All twelve samples per
workload agree on output byte counts, encoded/dropped counts, quantizer
statistics/histograms, keyframe/interframe sizes, and allocations, with no
allocation outliers. The unchanged counts and allocations are 246,893
bytes, 117/3 frames, 0.616667 allocations per input for VP9 320x180;
685,957 bytes, 58/2 frames, zero allocations for VP8 720p; and 642,654
bytes, 48/12 frames, 1.25 allocations for VP9 720p. Exact bitstreams
remain covered separately by the oracle gates.

In the separate VP8 profile, loop filtering accounts for 23.7% cumulative
time, including 22.2% in the byte-wide path. `pixelStatsRows16` accounts
for 7.6%, `forward4Go` for 5.3%, and `Gather` for 4.7%. The VP8 gap is
substantially smaller than the earlier EPYC 7763 run's +411.7%, but this
is not a same-host measurement of any individual change. Raw paired
timings, semantic fields, profiles, CPU metadata, and summary are retained
in [the checkpoint directory](benchmarks/baselines/go127-simd-amd64/2026-10-16-ff9395d0).

### Native amd64 batch results at `3c94d2cc`

The next checkpoint processes two VP8 transform blocks together, with
eight-lane ARM64 arithmetic and a 128-bit amd64 dot-product pipeline.
The [native run](https://github.com/thesyncim/govpx/actions/runs/38956724630)
uses another EPYC 7763 VM, with the same compiler, settings, and six
alternating pairs. Only the new benchmark/test file is copied onto the
assembly baseline; its production source remains at `691cd051`.

The new residual-domain 25-block DCT benchmark measures 217.25 ns for
assembly and 216.15 ns for SIMD, with zero allocations. The -0.51% median
change and -0.87% to +0.55% paired range indicate parity within these
samples. This compares both implementations with identical [-255,255]
coefficients; the fixture does not modify either production implementation.

| Workload | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| VP9 realtime 320x180 | 6.966 ms/frame | 4.445 ms/frame | -36.2% |
| VP8 realtime 720p | 23.166 ms/frame | 27.470 ms/frame | +18.6% |
| VP9 realtime 720p | 59.909 ms/frame | 34.636 ms/frame | -42.2% |

Every VP8 pair remains slower, ranging from +17.4% to +34.7%; the fourth
SIMD sample is 30.982 ms/frame while the other five are 27.265–28.024 ms.
All samples are retained. VP9 paired ranges are 36.1–36.3% less time at
320x180 and 41.1–42.8% less time at 720p. All byte counts, encoded/dropped
counts, quantizer statistics/histograms, keyframe/interframe sizes, and
allocations remain identical across every sample, with no allocation
outliers. Different VMs prevent a controlled same-host comparison against
the preceding checkpoint, even when their CPU model matches.

The separate VP8 profile attributes 20.8% cumulative time to the byte-wide
filter, 13.1% to statistics rows, 6.1% to gathering, and 2.8% to the new
batched transform. The complete raw artifact and summary are preserved in
[the batch checkpoint directory](benchmarks/baselines/go127-simd-amd64/2026-10-16-3c94d2cc).

### Native amd64 SSE/Walsh results at `bd25795a`

This checkpoint adds squared-error-only scoring and permits the native
Walsh path across the full signed-int16 domain. The
[native benchmark](https://github.com/thesyncim/govpx/actions/runs/38958062568)
and [all hosted production gates](https://github.com/thesyncim/govpx/actions/runs/38958062570)
pass. The runner is an EPYC 9V74 with AVX2/AVX512, unlike the previous
EPYC 7763 run; absolute checkpoint timings do not isolate these changes.
Within each run, both variants share the compiler, settings, and host.

| Operation | Assembly-era dispatch | Go SIMD dispatch | Time change |
| --- | ---: | ---: | ---: |
| SSE16x720 strip | 862.35 ns | 1,435.00 ns | +66.4% |
| SSE16x16 | 12.60 ns | 33.585 ns | +166.5% |
| SSE8x8 | 9.834 ns | 15.420 ns | +56.8% |
| Residual DCT, 25 blocks | 163.70 ns | 162.95 ns | -0.5% |
| VP9 realtime 320x180 | 5.505 ms/frame | 3.380 ms/frame | -38.6% |
| VP8 realtime 720p | 13.981 ms/frame | 20.683 ms/frame | +47.9% |
| VP9 realtime 720p | 47.022 ms/frame | 25.998 ms/frame | -44.7% |

All six VP8 pairs take 47.7–48.8% more time, so its remaining performance
gap is still material on this CPU. VP9 takes 38.0–38.9% less time at
320x180 and 44.6–44.9% less time at 720p. Every semantic/size/quantizer
field and allocation count matches across all twelve samples per workload,
with no allocation outliers. Counts remain 246,893 bytes, 117/3 frames,
0.616667 allocations/input for VP9 320x180; 685,957 bytes, 58/2 frames,
zero allocations for VP8 720p; and 642,654 bytes, 48/12 frames,
1.25 allocations for VP9 720p. All kernel samples allocate zero bytes.

The independent VP8 profile attributes 30.9% cumulative time to the
byte-wide filter, 4.1% to SSE16, and 4.9% to gathering. Raw profiles,
timings, metadata, a summary, and test-only baseline overlay details are
preserved in [the SSE checkpoint directory](benchmarks/baselines/go127-simd-amd64/2026-10-16-bd25795a).

### SIMD width boundaries and VP8 quantization

The next optimization restores fused VP8 quantization/dequantization/EOB,
using unsigned high-half products for the full signed coefficient domain.
ARM64 full-block quantization measures 2.46 ns versus 16.11 ns for the
separate quantize/EOB walk. Full signed coefficient/table tests use the
independent scalar encoder; negative rounding/quant tables retain fallback.
VP8 720p improves from 14.225 to 12.820 ms/frame in six alternating pairs,
with matching counts, quantizer statistics and zero allocations. The
assembly baseline remains faster on ARM64.

Four-byte row packing improves variance4x4 from 8.12 to 6.04 ns and
variance4x8 from 15.62 to 9.15 ns. Eight-lane nonnegative two-tap prediction
improves 16x16 bilinear prediction from 328 to 105 ns. Shape specialization
removes four/eight-byte load branches from subpixel variance's inner loop,
uses stride increments and integer multiply-adds: 16x16 improves from 72.7
to 50.5 ns, 64x64 from 1,138 to 770 ns. All kernels report zero allocations.

The EPYC 7763 profile exposed legacy SSE register moves while AVX2 upper
halves remained live. Go 1.27.2's archsimd explicitly provides
ClearAVXUpperBits for this boundary. Wide kernels now clear on scalar/memory
return; loop-filter stores hand off six VEX memory stores to a 128-bit
callee which clears before loading them. Packing uses a VEX zero load.
Generated-code inspection confirms no legacy MOVUPS in the AVX2 arithmetic
body before that handoff. ARM64 and live AVX2 parity pass; native x64 timing
is required to establish the performance effect of this transition repair.


### Byte filters and full-height statistics

VP8 filter scoring compares full-height16-pixel strips. The old64-row
statistics dispatch limit sent those strips to a slower loop. Native
statistics now accept tall strips and batch at4096rows to bound signed
accumulators while preserving modulo 2^32 SSE. The generic tail also uses
native widening square/dot-product helpers. Extreme signed sums and SSE
wraparound are checked against the independent scalar loop.

Exact2/4/6-column stores keep transposed values in registers and avoid the
row array. Two/four-column filters use smaller transposes. The central
filter correction uses monotone saturated byte additions instead of widened
multiplication; all130,816 outer/delta combinations match scalar clamp
arithmetic. Rounded macroblock corrections fit int8 before narrowing.
The shared byte-domain path now also serves amd64, using128-bit vectors and
AVX2-safe shifts/narrowing, avoiding the AVX2 wide-vector memory handoff for
ordinary thresholds. The full int16 path retains the255-threshold boundary.
Native ARM and Rosetta AVX2 pixel/DSP/encoder suites pass; native x64 timing
of this alternative is pending.

Six fresh ARM encoder pairs improve VP8 720p from12.443 to11.202ms/frame
(-10.0%) versus6a7b0345. VP9 varies+0.8% at320x180 and+0.6% at720p; those
small differences do not establish an improvement. Counts, quantizers and
allocations match in every sample. The original ARM assembly baseline
remains faster. [Raw pairs and kernel evidence](benchmarks/baselines/go127-simd-arm64/2026-10-16-byte-filters)
retain the workload and compiler context.

### Batched VP8 forward transforms

The batched DCT now processes two independent 4x4 blocks across eight lanes
on ARM64 and amd64. ARM uses widening int16 products; x64 uses 128-bit
pairwise dot products, with no AVX512 requirement. Residuals outside
[-255,255] retain scalar arithmetic, and odd blocks retain the single-block
path. Independent scalar tests cover full signed inputs and in-place use.
ARM fixed register shifts also remove repeated scalar-to-vector shift setup.

Six alternating ARM kernel triples measure the 25-block residual workload
at about 221 ns before, 186 ns with fixed shifts, and 107 ns with batching;
all allocate zero. Six public VP8 720p encoder pairs improve 11.684 to
11.165 ms/frame (-4.44%), with identical counts, quantizers and allocations.
[Raw measurements](benchmarks/baselines/go127-simd-arm64/2026-10-16-batched-dct)
include both experiments. The original ARM encoder remains faster.
Rosetta verifies x64 correctness; native batch timings are pending. The
native harness adds only the shared benchmark test to the archived baseline,
leaving its production implementation unchanged.


### Squared-error-only scoring and DC Walsh coverage

VP8 SSE calls now use dedicated eight/sixteen-column kernels instead of
computing and discarding variance's signed sum. They reuse exact row packing
and native square/dot products, with two independent accumulators. Odd rows,
minimal/misaligned allocations, read-only aliases, and modulo 2^32 overflow
are checked against independent scalar arithmetic. ARM kernel comparisons
measure roughly 1,160→750 ns for 16x720,30→19 ns for 16x16,and11.9→8.2 ns for 8x8,
with zero allocations.

The Walsh transform's int32 sums/shifts are safe across all int16 inputs.
Its former pixel-residual guard unnecessarily sent larger DC/Y2 inputs to
scalar code. 6000 full-signed cases now require live native dispatch and
match the scalar transform on ARM64 and Rosetta amd64. DCT retains its
separate residual bound.

Six ARM encoder triples show small/noisy changes 10.783→10.697→10.640 ms/frame
for baseline, SSE-only, and SSE+Walsh, with every output/count/quantizer and
allocation field equal. These differences do not establish a public encoder
speedup. [Raw comparisons](benchmarks/baselines/go127-simd-arm64/2026-10-16-squared-error)
retain the evidence. A filter-kind specialization was rejected because its
horizontal-edge regression outweighed an uncertain vertical benefit.


### Fused internal edges and full-row residual gather

VP8's three internal luma edges share one validated 16x16 tile and native
byte-filter dispatch. Each edge still observes the preceding edge's writes.
The 255-threshold fallback and eight-lane layout retain their existing paths.
Gather now loads each sixteen-pixel row once before widening both halves;
exact four-coefficient stores retain block-scan order. Minimal allocations,
misalignment, read-only input aliases and untouched sentinels are tested.
The x64 high-byte widening uses a fixed shuffle instead of a loaded mask.

Six ARM pairs improve horizontal edges about 36→23 ns, vertical 99→77 ns,
and 16x16 gather 61→41 ns, with zero allocations. Six public VP8 quartets
improve 10.758→10.444 ms/frame (-2.92%) against bd25795a. Output statistics
and allocations match in every sample. Original assembly measures 7.330
ms/frame in the same run, so the new path still trails by 42.48%.
[Raw evidence](benchmarks/baselines/go127-simd-arm64/2026-10-16-fused-edges-gather)
retains all variants. ARM and Rosetta AVX2 pixel/DSP/encoder suites pass.
Native x64 CI now compares the parent commit in the same runner alongside
head and the original baseline, with balanced execution order.


### Same-runner parent comparison

The 5a0cc122 native x64 run on EPYC 7763 compares the parent bd25795a and
current kernels alongside original main. VP8 720p improves 27.003→25.051ms
(-7.23%), with all six pairs faster; assembly is 23.191ms (current +8.02%).
VP9 320 improves 0.33% and 720 varies+0.75%, small/noisy changes, while remaining
36.09% and41.73% faster than the original baseline. Every output/count,
quantizer and allocation field matches across all 18 samples per workload.
[Raw three-variant evidence](benchmarks/baselines/go127-simd-amd64/2026-10-16-5a0cc122)
includes balanced order and environment. Full local native/purego,
race/checkptr1, strict byte parity, 18/18 report and hosted CI all pass.

### Additional forward32 coverage and full-width x64 scoring

Both VP9 32x32 forward variants now select four-lane native SIMD on ARM64
and x64 for 8-bit residuals. The generated network retains the scalar
rounding stage; the RD form rounds after stage 2, while precision rounds at
the final store. Full-signed inputs retain the independent scalar function.
1424 native differential cases include every cosine-sign frequency pair,
minimal/misaligned buffers and exact in-place operation. Pinned libvpx
fixtures and codec caller tests pass. Conservative affine bounds including
each rounding error keep intermediate int32 arithmetic below its limit.

Six ARM pairs improve precise 32 from 4699.5→2084.5ns (-55.64%) and RD from
4709.5→1953.5ns (-58.52%), with zero allocations. Realtime public encoder
measurements do not establish a speedup from this added coverage.
Fixed four/eight-byte filter column tiles also remove repeated row-width
branches and unused simple-filter transpose outputs. Two independent six-pair
runs improve vertical internal edges by 2.40% and1.57%; public changes remain
noise. A fixed-shift32 variant was rejected for lack of a measured gain.
[Raw evidence and bounds](benchmarks/baselines/go127-simd-arm64/2026-10-16-dct32-filter-loads)
retain all accepted/rejected variants and public output statistics.

The x64 sixteen-pixel SSE kernel now widens each full row to AVX2 int16
lanes and uses a full-width pairwise dot product. Safe slice loads preserve
exact bounds, including overlapping read-only rows and zero strides.
AVX2 upper bits clear at return; generated-code inspection finds no legacy
XMM moves in the YMM loop. Full pixel/DSP/encoder suites pass on ARM and
Rosetta AVX2. Native x64 performance of this candidate is pending.


### Native x64 forward32 and wider SSE results

The 3eb51360 native run on EPYC 9V74 compares its parent 5a0cc122 on the
same CPU. SSE16x16 improves 33.61→27.585ns (-17.93%) and the 720 -row strip
1430.5→1154ns (-19.33%). Precise forward32 improves 7616.5→2717ns (-64.33%),
RD 7995→2726.5ns (-65.90%). Public parent comparisons remain within noise:
VP9 320 +0.013%, VP8 720 +0.516%, VP9 720 -0.030%. Against original assembly,
VP9 remains 39.77%/43.12% lower in time; VP8 remains 39.59% slower.
[All three variants](benchmarks/baselines/go127-simd-amd64/2026-10-16-3eb51360)
match output/count/quantizer/allocation fields across 18 samples per workload.
Full local and hosted native/purego, race/checkptr1, strict byte parity,
quality/oracle/conformance and 18/18 report gates pass.

### Paired chroma filtering and DC reconstruction

VP8 U/V edges now occupy all sixteen byte lanes in one filter network.
Generated arithmetic shares the canonical copied goav1/libvpx byte rules.
Aliased planes and the 255 threshold boundary retain sequential calls.
An overflow-safe complete 8x8 tile proof bounds every exact read/store.
Two adjacent DC-only 4x4 blocks similarly share one native packet: unsigned
saturated add/sub clips byte deltas without pixel widening. Safe slice loads
and stores handle two rows at once. Exact in-place/disjoint layouts run
natively; partial aliases and overlapping rows retain block order.

Independent scalar tests cover whole allocations, minimal/misaligned spans,
aliases, differing strides and oversized dimensions. All 130816 byte/delta
combinations match scalar clipping, including both packet halves. ARM code
inspection confirms live UQADD/UQSUB and zero public-call allocations.
Six ARM pairs improve chroma filter kernels 30–35% in the fresh run. Six DC
triples select byte saturation over int16: in-place 13.32→7.131ns (-46.46%),
disjoint 12.695→6.559ns (-48.33%), all zero allocations.

Ten balanced public quintets measure original assembly 6.952ms, parent
3eb51360 10.408ms, UV-only 10.246ms, DC-only 10.209ms and combined 10.000ms.
Combined VP8 time improves 3.92% versus parent, with all ten pairs faster
(-5.14..-2.32%) and every output statistic/allocation count equal across 50
samples. Original assembly still leads on ARM (new path +43.84% time).
[Raw designs, public comparisons and live profiles](benchmarks/baselines/go127-simd-arm64/2026-10-16-paired-chroma-dc)
retain the complete evidence. ARM and Rosetta AVX2 pixel/DSP/encoder suites
pass; native x64 timing of the paired kernels is pending.

The native ef35b4ba comparison resolves the paired-kernel timing on one
EPYC 7763 runner (Go 1.27.2, SIMD, one thread, PGO off; all six variant
orders). VP8 720p improves from parent 25.100→23.595ms (-5.99%), every pair
faster (-6.65..-4.99%); assembly is 23.064ms, leaving a 2.30% gap. Paired DC
improves 61.10% (56.89% exact in-place), and UV filters improve 42.70–55.29%,
all allocation-free. VP9 320p/720p changes +0.13%/+1.36% versus parent have
paired ranges crossing zero; versus assembly, time remains 35.93%/41.43%
lower. Every byte count, encoded/dropped count, quantizer statistic/histogram,
frame-size statistic and allocation count matches across all 18 samples
per workload. [Raw three-variant evidence and profiles](benchmarks/baselines/go127-simd-amd64/2026-10-16-ef35b4ba)
retain all samples; separate-host absolute timings do not isolate changes.
Full local native/purego, five-package race/checkptr1, strict byte parity,
18/18 report and source/selection/PGO gates pass; hosted CI and native
three-variant benchmarks are both green.

### Wider subpixel variance and non-flat VP9 packets

Subpixel variance now processes sixteen pixels per iteration: two NEON
int16 packets share complete row loads on ARM; AVX2 handles sixteen int16
lanes and pairwise signed/squared reductions on amd64. Horizontal rows stay
in registers. Safe slice reads preserve exact minimal bounds; four/eight
pixel widths retain their prior path. All bilinear phases and byte extrema,
zero/overlapping strides and misalignment match independent scalar results.
ARM16x16/32x32/64x64 microbenchmarks improve about20%, with zero allocations.
An unsafe ARM variant showed only0.64–1.15% extra median change and was not
retained. A separate uint64-broadcast load experiment regressed10–12% and
was discarded after disassembly showed additional VDUP instructions.

VP9 eight/sixteen-tap filter entry points can use the existing byte-domain
narrow arithmetic when no active position requires flat filtering. Any
flat lane rejects the untouched packet into the original7/15-tap path;
minimal windows, sparse-flat masks, overlap order and255 thresholds retain
parity. Generated arithmetic shares the canonical copied goav1 network.
Non-flat kernels improve43–53% in six initial ARM pairs; specializing the
eight-position packet saves another2.9–6.7%. True flat packets pay for the
extra test, measured explicitly in the attached final-code benchmark.

Ten balanced quintets compare assembly, parent ef35b4ba, each isolated
change and the final combination. ARM VP9 improves13.825→12.754ms/frame
(-7.745%), all ten pairs faster (-9.65..-5.87%). VP8 remains within noise
(+0.503% median, paired range -7.34..+11.74%). Every non-timing field and
allocation count matches across100 samples. Assembly still leads on ARM:
VP8 +43.33%, VP9 +46.86% time. These remaining gaps are unresolved.
[Raw public/kernel comparisons, rejected design, live profile and code](benchmarks/baselines/go127-simd-arm64/2026-10-16-wide-subpel-nonflat)
preserve the evidence. Five pixel/DSP/encoder packages pass natively and
under Rosetta AVX2; PGO is refreshed and pre-commit passes. Native x64
performance of the new wider/filter paths is pending.

### Native x64 wider subpixel and non-flat results

The 08c2b96e three-variant run is fully green on EPYC 7763. Same-run parent
comparisons improve VP9 320p 4.493→4.428ms (-1.44%) and 720p 35.497→34.590ms
(-2.55%), every pair faster. VP8's +0.42% parent change is within noise;
current 23.908ms versus assembly 23.568ms leaves a 1.44% gap on this runner.
Subpixel 16/32/64 improves 14.3–21.1%; non-flat filters 28.8–38.8%, all zero allocations.
All 18 public samples per workload match every recorded semantic/allocation
field. [Raw samples and live profiles](benchmarks/baselines/go127-simd-amd64/2026-10-16-08c2b96e)
retain same-run assembly and parent comparisons. Hosted CI, full local
native/purego, race/checkptr1, strict byte, 18/18 report and source/PGO gates
all pass.

### VP8 six-tap arithmetic and split-block dispatch

VP8's bounded six-tap weights now use int16 SIMD. Negative products are
accumulated first; their total weight is at most 128, giving a minimum
64-128*255=-32576. Subsequent positive saturation is exact after byte
rounding/clipping. Odd phases omit both zero end taps in a separate
four-tap helper. Unsupported filters retain the int32 path. All reads and
stores use safe slices. Initial six-round triples halve dense 16x16
prediction time; another six triples select separate helpers over a
naive kernel and generic specialization, avoiding extra small-block cost.

Both-axis fractional split-block variance now uses the existing fused
statistics kernel. Integer/one-axis cases retain their previous path.
Fresh six-pair B.Loop tests with observable outputs improve split shapes
69.65–83.59%, all zero allocations. Codec-table/extreme/random six-tap tests and
all-phase minimal/misaligned split tests match independent scalar results.
Five kernel/DSP/encoder packages pass on ARM and Rosetta AVX2.

Ten balanced public quintets compare assembly, parent 08c2b96e, each
isolated change and the combination. ARM VP8 720p improves 9.969→9.704ms
(-2.661%), all ten pairs faster (-5.11..-1.65%). Every non-timing field and
allocation count matches across 50 samples. Assembly still leads at 7.067ms,
leaving 37.31% extra time on ARM. Split-only public samples do not establish
an end-to-end improvement. Six control pairs show VP8 CPU-used4 320x180
-1.987% (every pair faster) and VP9 720p within noise (-0.447%, paired range
crossing zero); all 24 control samples preserve output/allocation fields.
The 720p profile confirms the six-tap kernel live at 4.23% cumulative time.
[Raw designs, comparisons, controls and profiles](benchmarks/baselines/go127-simd-arm64/2026-10-16-six-tap-split)
keep the evidence and rejected variants. Native x64 timing of this next
checkpoint is pending.

After integration, six fresh balanced parent/prototype/final triples measure
9.932/9.592/9.616ms. The final build improves 3.184% versus parent, all pairs
faster 2.32–4.12%; its 0.243% prototype difference is within noise. All 18
samples preserve recorded semantic/allocation fields. Final ARM/Rosetta
five-package suites, PGO refresh and pre-commit pass.

The first3792 hosted run rejected a stale PGO fingerprint caused solely
by two comment-space edits after refresh. A full refresh against the exact
committed source and pre-commit now pass with fingerprint b2f0317c. Native
benchmark concurrency preserves the active run while queueing the newest
checkpoint, allowing fixes to be pushed without discarding measurements.

### Native x64 six-tap and split-block result

The 3792c617 native EPYC 7763 comparison improves VP8 720p
23.716→23.047 ms (-2.82%) versus parent, every pair faster (2.08–4.45%).
It also beats original assembly 23.452 ms by 1.73%, every pair faster
(0.88–2.54%). VP9 changes from parent remain small/mixed (-0.25% 320p,
-0.50% 720p), while both workloads beat assembly in every pair. Six-tap
kernels improve 11.6–58.9% from parent and split-block statistics 72.9–85.3%,
all zero allocations. Some small four/eight-pixel six-tap phases still trail
assembly. All 18 public samples per workload retain every recorded semantic
and allocation field. [Raw native samples and profiles](benchmarks/baselines/go127-simd-amd64/2026-10-16-3792c617)
retain the same-run evidence. The ARM assembly gap remains unresolved.

### Fixed VP8 internal-edge packets

Generated sixteen-position/four-column packets remove dynamic kind/width
branches and general store dispatch from the live three-edge VP8 path.
Exact-width stores preserve minimal windows and overlapping logical-row
order. Divide-before-multiply checks reject short/overflow-sized spans.
Four designs compare fixed packets, direct stores and separate orientations;
complete encodes select the shared direct-store design despite a different
microbenchmark winner. Eight balanced public quartets improve ARM VP8 720p
2.58%; six fresh parent/prototype/final triples confirm 9.704→9.493 ms
(-2.176%), all six pairs faster (1.12–2.87%). Every recorded non-timing field
and allocation count matches across all 62 public/control samples.

Fresh kernel pairs improve horizontal 22.075→20.750 ns (-6.00%) and vertical
73.600→63.465 ns (-13.77%), zero allocations. Five pixel/DSP/encoder packages
pass on ARM and Rosetta AVX2. A live 600-frame profile and LLVM disassembly
confirm the specialized body executes and no longer calls the general store
helper. VP9 control variation is recorded without attribution to this VP8
change; native x64 timing and a fresh original-assembly ARM comparison remain
pending. [Raw designs, fresh comparisons, control, code and profile](benchmarks/baselines/go127-simd-arm64/2026-10-16-internal-filter)
keep the evidence and rejected variants.

The final internal-filter sources also pass a full PGO refresh and pre-commit;
the exact-source fingerprint is recorded with the profile. Broader final
local/hosted validation is in progress.

The 649237be internal-filter checkpoint passes full local native/purego,
five-package race/checkptr1, strict byte parity, the 18/18 report and all
source/architecture/PGO gates. Hosted CI is fully green as well. Native x64
timing of the internal-filter change is still running.

### Batched VP9 reductions and rolling VP8 prediction

Copied goav1 four-row column projections replace repeated generic Sum calls;
safe reads, VP9 normalization and remainder rows preserve exact int16
semantics. Local x64 adaptation uses PSADBW. Four rounded 8x8 averages share
sixteen-byte row loads: ARM accumulates four uint16 packets, x64 two PSADBW
packets. These kernels remain self-contained. VP8 vertical six/four-tap
prediction reuses five/three rows in registers; aliasing and overlapping
output layouts retain previous eight-pixel sequencing. Small four-pixel
blocks retain the existing path after an initial helper-call regression.

Fresh six-pair ARM DSP comparisons improve averages 82.99%, tall column
projections 68.63–76.87%, vertical 16x16 prediction 62.85%, both-axis dense
16x16 40.31% and odd-phase 35.66%, all zero allocations. Four-pixel cases
remain within roughly 1.2%. Ten balanced VP9 public quintets compare assembly,
parent, isolated changes and their combination; combined time improves
2.517%, nine of ten pairs faster. Six fresh final triples confirm
12.550→12.188 ms (-2.887%), every pair faster (2.36–7.20%). VP8's fresh final
-1.167% median has mixed paired results, so no stable whole-encoder VP8 gain
is claimed from that set. All 104 public samples retain every recorded
non-timing/allocation field within their codec/workload.

Original ARM assembly still leads: the same-run prototype comparisons show
30.76% extra VP8 time and 42.80% extra VP9 time. Profiles prove the new
projection/average/rolling kernels execute; ARM and Rosetta five-package
suites, three-package experiment-off and native-selection/span tests pass.
LLVM confirms native NEON and x64 PSADBW generation. Native x64 performance
of this checkpoint is pending. [Raw comparisons, rejected guard, final code,
profiles and disassembly](benchmarks/baselines/go127-simd-arm64/2026-10-16-projection-average-rolling)
retain the evidence.

The exact final sources pass a full PGO refresh and pre-commit. Full final
local/hosted validation is in progress; the native benchmark timeout now
allows 25 minutes for the expanded matrix without changing acceptance gates.

### Native x64 internal-filter result and architecture scope

Hosted and native 649237be runs are fully green. EPYC 7763 horizontal
internal filtering improves 51.540→44.795 ns (-13.09%, every pair faster),
while vertical filtering regresses 124.550→128.100 ns (+2.85%, every pair
slower). VP8 720p changes -0.61% with mixed pairs; VP9 +0.62% at 320p and
+0.35% at 720p are also mixed. No stable whole-encoder gain is established
by these samples. Recorded output/quantizer fields match. VP9 720p has one
extra-allocation sample in assembly and one in current (1.2667 versus usual
1.25 allocations/input); parent samples remain 1.25. These are disclosed
rather than described as exact allocation parity.
[Raw native comparison and profiles](benchmarks/baselines/go127-simd-amd64/2026-10-16-649237be)
keep the evidence. The next checkpoint scopes direct internal vertical
stores to ARM and restores the measured general vertical body on x64,
retaining x64's faster horizontal specialization. Its native timing is pending.

After scoping the x64 vertical path, five-package ARM/Rosetta tests and a
fresh PGO/pre-commit pass again. The complete ARM instruction region is
byte-identical before/after this architecture-only choice (excluding its
embedded 112-byte Go build-ID header), preserving the final ARM timing
comparison. Raw hashes and disassembly are retained in the reduction evidence.

### Three-edge ARM loop-filter reuse

The three internal VP8 edges now retain four neighboring rows/columns in
registers. New inputs are loaded after each sequential store, preserving
cross-row aliases at pitches 8/9/15. Input traffic falls 384→256 bytes per
tile. Generated code shares canonical goav1 byte arithmetic; an inline copy
of its four-column transpose avoids spilling retained vectors across loader
calls. Six design triples compare the previous path, reuse with calls and
reuse with the inline transpose. Eight balanced public quartets select
inline reuse: ARM VP8 720p improves 9.214→8.667 ms (-5.942%), every pair faster
(4.20–6.73%). Reuse with calls already improves 4.622%; inline reuse improves
another 1.384% against that stronger alternative.

Six fresh final triples confirm 9.192→8.629 ms (-6.124%), every pair faster
(4.22–6.69%). Final kernel pairs improve horizontal 7.19% and vertical
35.82%, all zero allocations. All 62 public/design/control samples preserve
every recorded non-timing/allocation field within their codec/workload.
VP9 control differences are mixed and establish no stable change. The same-run
original assembly comparison still leaves 27.31% extra ARM VP8 time.

Five-package ARM/Rosetta suites, 3456 additional exact-window cases against
the independent int16 network, PGO refresh and pre-commit pass. LLVM shows
fourteen static vector stack load/store instructions in the call design and
none in the selected vertical body; a live profile confirms both fused bodies.
Fusion is ARM-only. The x64 VP8 DSP test instruction region remains
byte-identical to dff92311 after excluding the Go build-ID header, preserving
its measured architecture-specific paths.
[Raw designs, final comparisons, controls, profiles and generated code evidence](benchmarks/baselines/go127-simd-arm64/2026-10-16-fused-internal)
retain the results. Final broader local checks pass; the native x64 control
run also passes. Hosted validation is tracked on the PR.

### Native x64 batched-reduction result

Hosted and native dff92311 runs both pass. On EPYC 7763, VP9 320p improves
4.389→4.324 ms (-1.481%) from parent, every pair faster (0.98–2.12%). VP9
720p improves 33.117→32.097 ms (-3.081%), every pair faster (2.46–3.43%),
and uses 46.62% less time than original assembly's 60.133 ms. VP8 changes
+0.373% from parent with mixed pairs; it uses 3.577% less time than assembly.
The restored vertical path improves 128.250→124.450 ns (-2.96%); horizontal
43.295 ns improves 3.33%. Quad averages improve 108.850→20.055 ns (-81.58%),
and projection 64x128 1879.500→588.400 ns (-68.69%), all zero allocations.

All recorded output and quantizer fields match. VP9 720p current round two
has one extra allocation total (1.2667 versus usual 1.25 allocations/input);
the other current samples remain 1.25. No exact allocation-parity claim is
made for that public workload. [Raw native samples and profiles](benchmarks/baselines/go127-simd-amd64/2026-10-16-dff92311)
preserve the evidence. ARM-only fusion keeps these selected x64 paths.

Final fused ARM full native, five-package race/checkptr1, strict byte parity,
18/18 report and source/architecture/fmt/PGO gates all pass. Prior full purego
coverage remains valid because its selected sources are unchanged; hosted CI
also reruns that suite. Latest upstream main and the PR base remain 691cd051,
and the draft PR is mergeable without conflicts.

### Empty projection contract and second x64 CPU

Final review reproduced a zero-row projection panic caused by evaluating
`ref[off:]` before the empty-batch check. The public entry now returns before
reading either buffer for zero/negative row counts. A regression test covers
nil sources, unused positive/negative/maximum offsets, and unchanged output.
Targeted native ARM, Rosetta AVX2 correctness, experiment-off and purego checks
pass. PGO was refreshed after the final source change, with fingerprint
`bd5031e5889589f04c4d01da296645535ee6d44b`; pre-commit passes.
[Reproduction and final validation](benchmarks/baselines/go127-simd-arm64/2026-10-16-empty-projection)
retain the logs. No extra performance gain is attributed to this fix.

The c49e9d60 native control run used EPYC 9V45 rather than the earlier EPYC
7763. Parent/current changes are mixed and cross zero, consistent with their
identical x64 VP8 DSP instruction bodies. Current VP9 uses 44.03% less time
at 320p and 50.64% less at 720p than same-run original assembly. VP8 uses
19.82% more time than assembly, with every pair slower (8.86–21.47%). All
18 recorded semantic/allocation fields match. This control establishes a
remaining x64 VP8 gap on that CPU, despite the earlier 7763 result. Raw
control artifacts are linked from the [native run](https://github.com/thesyncim/govpx/actions/runs/38976212853);
the PR remains draft, with ARM and CPU-specific x64 performance limits open.

The final empty-projection source also passes full native (45.181s), full
purego (112.845s), five-package race/checkptr1, strict encoder bytes (34.302s),
18/18 parity report, no-assembly audit, all six selection modes, formatting
and exact-source PGO checks. The preceding c49e9d60 hosted and native jobs
are both green.

### Paired residual packets and safe bounds elimination

VP8 gather now interleaves two four-coefficient rows per vector store and
executes two packets per four-row block group. This halves stores for both
luma and chroma, retaining exact scan order and overlapping read-row support.
Checking output size once with a bounded block index lets Go remove repeated
output bounds checks, confirmed by BCE diagnostics. No unsafe operations or
dependencies are added.

On native M4 Max, six fresh final triples improve 16x16 from 40.230 to 16.010 ns
(-60.20%) and 8x8 from 18.760 to 8.084 ns (-56.91%), all zero allocations.
VP8 720p improves 8.574611 to 8.406684 ms (-1.958%), every pair faster
(0.716–4.326%). The stronger four-row alternative already improves 1.273%;
the final design's additional 0.694% public change is mixed, while both kernels
improve clearly. All 84 public design/control samples preserve their full
non-timing/allocation fields. VP9 control changes are mixed and establish
no stable gain. Five ARM/Rosetta packages and 80 new repeated/overlap cases
pass; a live profile confirms Gather. Native x64 timing remains pending.
[Raw designs, fresh comparisons, codegen and parity evidence](benchmarks/baselines/go127-simd-arm64/2026-10-16-paired-gather)
retain the results; PGO was refreshed only after final source freeze.

Final gather gates pass full native (45.872s), focused purego pixel coverage,
five-package race/checkptr1, strict encoder bytes (34.267s), 18/18 report,
no-assembly audit, all six selection modes, formatting and exact-source PGO.
Production purego sources are unchanged from the preceding full passing suite.

A final six-pair native ARM assembly control measures original assembly
6.651471 ms versus current 8.420126 ms/input: 26.590% more time remains
(all six pairs slower, 19.265–27.910%). All 12 control samples preserve every
recorded non-timing/allocation field. This is the current measured ARM VP8
limit; the kernel/parent gains do not establish assembly parity.

## VP9 x64 class-index follow-up (2026-10-17)

The previous native EPYC7763 run left VP9 320p 0.48% slower than its
parent despite identical VP9 instruction bodies. A VP9-only linked control
is byte-identical; mixed-codec link layout is an investigation lead, not an
established cause. This follow-up improves actual encoder-reachable work:
the quantizer already proves token LUT indices are in [0,11], so x64 now
extracts their low bytes without the general pixel saturation clamps.
Go1.27.2 removes five redundant instructions per eight-coefficient class
packet. The ARM quantizer remains byte-identical. No ISA requirement,
public contract, dependency, or fallback changes.

The independent quantizer oracle and 144 mixed-lane index packets pass on
ARM and under Rosetta; experiment-off and purego package suites pass.
Native x64 performance is pending. Ten identical public B.Loop fixtures
measure quantization, EOB and every raster class on all three references;
the old assembly fixture includes its required class walk. Encoder timing
remains the acceptance criterion, since production callers can skip some
classes after EOB. No speedup or regression fix is claimed before the
fresh native run. See `benchmarks/baselines/go127-simd-amd64/2026-10-17-fp-class-indices`.

## ARM variance and transpose follow-up (2026-10-17)

Six fresh balanced native M4 Max comparisons against611f47fe improve
VP8 720p8.621926 ->8.320135ms/frame (-3.500%), every pair3.208–4.365%
faster. All non-timing fields match, including zero allocations. The same
run measures original assembly7.065953ms/frame: SIMD is still17.750%
slower, and every assembly/current pair remains slower. The parent is
22.021% above assembly in this run; the older26.590% gap is separate
evidence and its full change cannot be attributed to this optimization.
VP9 changes are mixed, so no stable VP9 encoder gain is claimed.

The ARM sixteen-column variance kernel keeps signed sums in int16 for
1..64rows under a32640-per-lane bound, removes per-row bounds checks only
after overflow-safe final-row proofs, and reduces in SIMD registers.
Matched public B.Loop h1..h64 cases improve24.9–45.5%; h16 improves40.24%.
The 16x8 loop-filter transpose pairs rows eight apart and uses24 TRN
instructions, replacing two8x8 transposes and eight joins. Reads remain
exactly eight bytes per row. Independent numeric/lane/minimal-buffer tests,
race/checkptr, native-required Rosetta correctness and fallback tests pass.
The selected x64 variance bodies are instruction-identical; the column
helper differs only in three reordered argument-save instructions, with
unchanged instruction count/arithmetic. Native x64 checks remain required.

Separate macro-filter specializations, a3.5x direct-kernel denoiser gain,
and two/four-accumulator full-plane SSE designs were rejected or left
experimental because encoder results were mixed. Fresh results, design
controls and exact generated-code evidence are under
`benchmarks/baselines/go127-simd-arm64/2026-10-17-variance-transpose`.


### Native FP follow-up at 611f47fe

[Hosted CI](https://github.com/thesyncim/govpx/actions/runs/39028739727)
and the [native three-way run](https://github.com/thesyncim/govpx/actions/runs/39028739721)
both pass on exact head `611f47fe`. The native EPYC 7763 run retains all six
variant permutations, PGO off and the same original ASM / parent `63c2f188`
controls. VP9 320p improves 4.427351 to 4.360532 ms (-1.509% median), with
five of six pairs faster (-2.510..+0.155% time). The paired mean is -1.270%
with a descriptive t95 interval [-2.253,-0.288]%; six balanced rounds are
limited evidence and do not establish a universal code-layout or CPU fix.
VP9 720p improves 2.192% by medians, but paired changes cross zero. VP8's
+0.854% median control change also has mixed pairs and is not attributed
to this VP9-only kernel change. Current takes 37.915% / 45.985% less VP9
320p / 720p time and 3.030% less VP8 time than same-run ASM, all six pairs.

Nine equivalent full-class-output composite fixtures improve 2.043–4.258%
over parent medians, while the no-class control changes -0.579% with mixed
pairs and still takes 53.477% more time than bare ASM. Full class output
can require more work than zero/EOB-limited production callers; the public
encoder measurements remain the acceptance evidence. Every one of the 75
kernel cases has six samples per variant with zero allocations. All 22
non-timing semantic/configuration fields and allocations match across all
18 samples per public workload and across corresponding profile runs.
There is no allocation outlier in this run. Separate short profiles confirm
the quantizer remains live; unchanged scalar-DCT controls show timing
variation, so a universal regression fix is not claimed. CPU-specific EPYC
9V45 VP8 and ARM ASM gaps remain open. All 85 raw files and complete analysis
are preserved in
[the 611f evidence folder](benchmarks/baselines/go127-simd-amd64/2026-10-17-611f47fe).


### Native x64 control at ARM checkpoint 211125e5

[Hosted CI](https://github.com/thesyncim/govpx/actions/runs/39029805350)
and the [native control](https://github.com/thesyncim/govpx/actions/runs/39029805450)
both pass on exact head `211125e5`. Native EPYC 7763, Go 1.27.2, AVX2,
PGO off and six balanced permutations compare ASM / parent `611f47fe` /
current. VP9 320p changes 4.344040 to 4.337680 ms (-0.146%), VP8 720p
22.197201 to 22.124264 ms (-0.329%), and VP9 720p 32.569408 to 32.311997 ms
(-0.790%). Every parent/current paired range crosses zero; no new x64 gain
is attributed to the ARM checkpoint. Same-run ASM comparisons use 38.480%
less VP9 320p time, 5.020% less VP8 time and 46.120% less VP9 720p time,
with every assembly/current pair faster. All 22 recorded non-timing
semantic/configuration fields and allocations agree across all 18 public
samples and corresponding profiles. All 75 kernel cases retain six samples
per variant with zero allocations. CPU-specific EPYC 9V45 VP8 and ARM gaps
remain open; cross-job absolute times do not isolate improvements.
[All 85 raw files and analysis](benchmarks/baselines/go127-simd-amd64/2026-10-17-211125e5)
are preserved with the passing exact-head status.


### Optimized upstream libvpx baseline

The benchmark now supports `-reference-first` and a six-round, alternating-order
comparison with pristine optimized libvpx v1.16.0, pinned to peeled commit
`1024874c5919305883187e2953de8fcb4c3d7fa6`. A separate native x64 CI job
builds upstream assembly/intrinsics and records source/configuration hashes,
CPU dispatch, codec-internal timing, wall overhead, controls and decoded quality.

On Apple M4 Max, codec checkpoint `211125e5` with the new order harness and PGO
off takes 8.471075 ms/input for adaptive VP8 720p versus C 5.115767 ms
(+65.59%), and 12.588807 ms for VP9 720p versus C 4.842000 ms (+159.99%).
All six paired comparisons show a remaining C gap. VP8 measured quality, payload
and frame/drop counts match; VP9 quality and output differ slightly, so equivalent
work is not assumed. Fixed-speed VP8 and VP9 320p controls, complete quality
results, timing contracts and raw samples are preserved in the
[ARM libvpx evidence](benchmarks/baselines/go127-simd-arm64/2026-10-17-libvpx).
These C results are distinct from comparisons against the removed Go assembly.


### Native x64 optimized libvpx at 9a442f66

The first [native libvpx job](https://github.com/thesyncim/govpx/actions/runs/39031352624/job/117392274827)
passes on exact `9a442f66`. Native EPYC 7763, Go 1.27.2, GCC 13.3 / NASM
2.16, one encoder thread, PGO off and six alternating Go/reference-first
runs compare identical generated inputs and corresponding controls against
pristine v1.16.0 (`1024874c`) with verified optimized upstream assembly.
Codec-internal timers are used throughout; subprocess/file overhead and
separate decoded quality passes remain visible.

| Workload | Go ms/input | C ms/input | Go time delta |
| --- | ---: | ---: | ---: |
| VP9 320p, cpu8 | 4.353493 | 1.428450 | +204.77% |
| VP9 720p, cpu8 | 32.668384 | 10.101667 | +223.40% |
| VP8 720p, adaptive cpu8 | 22.183473 | 10.259817 | +116.22% |
| VP8 720p, fixed cpu-8 | 18.320404 | 6.781175 | +170.17% |

Every paired Go/C delta is positive. The fixed-8 VP8 comparison matches
measured PSNR 27.157611 dB, SSIM 0.925927, 723593 payload bytes and 58/2
encoded/dropped frames; it still takes 170.17% more Go time. Both run at
2894.372 kbps, above the common 2500 kbps target. Adaptive VP8 is different
work: C improves PSNR 27.983951 to 29.176639 dB and SSIM 0.908504 to
0.933088 while reducing payload 685957 to 678205 bytes. It is not presented
as identical-quality throughput. VP9 output/quality also differs slightly,
with the same differences as the separate ARM comparison. All quality-pass
payload/count statistics match corresponding timing samples; no timed
bitstream identity is asserted. These are substantial remaining optimized-C
gaps, not a universal performance-parity claim.
[All 74 raw files, controls, configurations and explanation](benchmarks/baselines/go127-simd-amd64/2026-10-17-9a442f66-libvpx)
are retained without discarding any pair.


### ARM macroblock and internal edge fusion

The ARM VP8 luma path combines a macroblock boundary and its three internal
edges while preserving their store order and exact 20x16 window. Generated
arithmetic shares the canonical byte filter; the selected vertical layout
packs exact four-byte rows before eight column transposes. Non-ARM builds
compile out the entire fused call and its slice construction. The x64 decoder
hot body remains byte-identical to parent `9a442f66` (850 bytes).

Fresh 60-frame confirmation improves 8.351395 to 8.110483 ms/input (-2.885%),
and the 300-frame sequence improves 8.368106 to 8.105590 ms (-3.137%), all
twelve pairs faster with identical output metadata and allocations. An initial
three-way run shows a smaller, mixed -1.407% parent change; it retains same-run
old assembly at 7.283229 ms versus current 8.322421 ms, still +14.268%. The
older 26.6% and 17.75% gaps come from different runs and cannot be subtracted
to attribute a gain. The unchanged VP9 control changes +0.456% with mixed pairs.

9216 comparisons against the independent scalar libvpx port and 7200 against
the independent int16 SIMD network pass, including exact minimum windows,
unaligned addresses, sentinels, threshold extremes, race detection and checkptr.
Full native, fallback, strict byte parity, all 18 parity-report checks, PGO and
source/build-selection gates pass. Further helper expansion reduced instruction
count without establishing an additional reliable encoder gain and is excluded.
[Complete designs, raw measurements and validation](benchmarks/baselines/go127-simd-arm64/2026-10-17-macro-fusion)
are retained. The ARM assembly gap and optimized-C gaps remain open.


### Three-way x64 harness control at 9a442f66

All three remote jobs at `9a442f66` pass: hosted exhaustive checks, native
ASM/parent/current and optimized libvpx. The [three-way control](https://github.com/thesyncim/govpx/actions/runs/39031352624/artifacts/12100503682)
uses native EPYC 7763, Go 1.27.2, PGO off and all six variant permutations.
Against parent `211125e5`, VP9 320p changes 4.311819 to 4.318367 ms (+0.152%,
five of six pairs slower), VP8 720p 21.865938 to 21.938879 ms (+0.334%, all
six pairs slower, +0.017..+1.028%), and VP9 720p 32.083129 to 31.963131 ms
(-0.374%, all six pairs faster). This benchmark-harness checkpoint changes
no codec arithmetic; no optimization attribution or universal noise claim
is made for these small controls. Every current/ASM pair remains faster:
37.920% less VP9 320p time, 4.343% less VP8 time and 46.272% less VP9 720p time.
All 22 semantic/configuration fields and allocations match across all 18
samples and corresponding profile runs; all 75 kernel cases have six samples
per variant and zero allocations. The much larger optimized-C gaps remain
separate evidence. [All 85 raw files and analysis](benchmarks/baselines/go127-simd-amd64/2026-10-17-9a442f66)
are retained without discarding any pair.


### EPYC 9V45 control at db5b44fe

Both native jobs on `db5b44fe` pass. Its [three-way control](https://github.com/thesyncim/govpx/actions/runs/39032233232/artifacts/12100668761)
ran on EPYC 9V45; the separate optimized-C job ran on EPYC 7763. Same-run
9V45 ASM / parent `9a442f66` / current VP8 720p medians are 11.386280 /
14.331529 / 13.866399 ms: current remains 21.782% slower than old ASM,
every pair slower by 13.646–28.404%. The final gather is now measured on
this CPU and does not establish assembly parity. VP9 320p uses 44.280% less
ASM time, and VP9 720p uses 48.693% less, every pair faster. All parent/current
ranges cross zero, including large -13.895% / +18.835% control outliers;
no x64 change is attributed to ARM-only fusion. All 22 recorded semantic
fields agree. Parent/current allocations match; ASM VP9 round 5 retains one
extra allocation (1.266667 versus 1.25 per input). All 75 kernels have six
allocation-free samples per variant. Cross-CPU absolute times are not compared.

The single-block VP8 quant fixture is 4.304 ns versus old ASM 3.5485 ns
(+21.291%, all six slower); no batch benchmark was selected. Separate short
profiles show the current per-block batch dispatcher is live at 0.06 s
cumulative / 1.62 s total CPU, versus the old batch ASM leaf's 0.02 s /
1.36 s. This is not a precise direct batch ratio; table-hoist/full-block
batch candidates require new matched observable fixtures.
[All 85 raw files and analysis](benchmarks/baselines/go127-simd-amd64/2026-10-17-db5b44fe)
preserve the CPU-specific evidence and the outlier.

## Shared-table VP8 quantization follow-up

The batch path now retains shared quantization tables across blocks, using
128-bit ARM vectors and full-block 256-bit AVX2 vectors. Batches below eight
blocks retain the existing path. Conservative bounds/alias guards preserve
fallback behavior, including signed tables and overlapping block buffers.

On the M4 Max, six balanced 720p realtime comparisons reduce VP8 median encode
time from 7.963865 to 7.844487 ms/input (-1.499%, all six pairs faster). The
same-run old assembly median is 6.840786 ms: current SIMD remains +14.672%
slower, with every pair slower. Eight/16/25-block quantization medians improve
48.6%/52.8%/55.6% versus the parent, with zero allocations; they still trail
assembly. The unchanged VP9 control has mixed pairs and no attributed gain.

Full native tests, experiment-off/purego codec suites, race/checkptr scalar
oracles, strict byte parity, 18/18 parity cases and refreshed PGO/source gates
pass. Required-SIMD x64 differential tests pass under Rosetta; native AVX2
performance remains pending CI. This is not evidence of a native x64 speedup.
[Raw measurements, codegen and validation](benchmarks/baselines/go127-simd-arm64/2026-10-17-quant-batch)
record the remaining assembly gap. A separate
[rejected chroma fusion](benchmarks/baselines/go127-simd-arm64/2026-10-17-rejected-chroma-fusion)
retains its raw results and source patch; it is excluded from production
because integrated public comparisons did not sustain the prototype gain.

A fresh optimized libvpx comparison for this codec measures VP8 adaptive at
7.699581/4.730708 ms/input (Go/C, +62.76%) and fixed speed at 6.509613/3.011758
ms (+116.14%). Within each VP8 workload, measured PSNR, SSIM, payload size and
encoded/drop counts match. VP9 720p measures 12.061530/4.599933 ms (+162.21%),
with small documented output/quality differences. Six rounds alternate
execution order; codec-internal timers and separate quality passes exclude
subprocess/file wall time. Optimized C remains substantially faster.
[Current ARM Go/C raw evidence and provenance](benchmarks/baselines/go127-simd-arm64/2026-10-17-quant-batch-libvpx)
keep these results separate from the older codec's C comparison.

A fresh 300-input three-way comparison confirms the batch change at
7.993610 to 7.857803 ms/input (-1.699%, all six pairs faster). Same-run old
assembly is 7.150376 ms: current remains +9.894% slower, with every pair slower.
Non-timing outputs and zero allocations match across all variants. This longer
workload is distinct from the 60-input comparison above. Its raw reports and
driver are retained alongside the short-run batch evidence.

Further [filter and quantization-loop designs](benchmarks/baselines/go127-simd-arm64/2026-10-17-rejected-filter-and-quant-loop)
are excluded from production after mixed or slower integrated results. The
profile and reproducible patches are retained for future compiler/CPU work.

The first x64 batch checkpoint (`048a5f3e`) regresses on EPYC 7763: eight-block
quantization is +63.866% versus the base, and VP8 encode is +2.902%, all six
pairs slower. All 80 kernels have six allocation-free samples per variant;
all semantic fields match. [Raw failed candidate and analysis](benchmarks/baselines/go127-simd-amd64/2026-10-17-048a5f3e)
remain available. Generated code exposes legacy MOVUPS for the signed-table
zero comparison while YMM values are live; the profile concentrates in this
check. The follow-up uses the existing VEX-loaded zero table. Its required-SIMD
oracles, generated-code check, refreshed PGO and source gates pass. Native
`3d485291` validation on EPYC 7763 confirms the fix: quantization batches of
8/16/25 blocks improve 59.4%/65.9%/69.3% versus base, all six pairs faster.
Public VP8 improves 0.685% versus base and 4.905% versus old assembly, all six
pairs faster. Counts 1/2 regress 5.0%/6.9%; these remain recorded tradeoffs.
[Validation](benchmarks/baselines/go127-simd-amd64/2026-10-17-quant-vex-validation).

The next x64 candidate retains rolling rows in 128-bit vertical filters and
clears AVX upper bits at quantizer exits. Its isolated filter test on EPYC
9V74 improves public VP8 2.414%, all six pairs faster, but still trails old
assembly by 19.070%. Horizontal dot-product and wider rolling variants are
excluded because they do not improve the public workload. These independent
measurements are not additive. Combined `04c05786` native validation on EPYC
9V45 improves VP8 5.528% versus base (all six pairs faster), but still trails
old assembly by 13.026% (all six slower). All 80 kernels have six zero-allocation
samples per variant; all recorded semantic/configuration fields match. Its
separate optimized-C job on 9V45 measures fixed-8 VP8 at 10.723892/4.366983 ms
(Go/C, +145.568%) with matching measured output/quality; adaptive C output
varies across timed samples and is not a matched-quality throughput claim.
[Comparisons and integration checks](benchmarks/baselines/go127-simd-amd64/2026-10-17-rolling-integration).


ARM VP8 vertical filtering now retains finalized columns and transposes them
into exact 16-byte row stores plus one tail byte. The complete disjoint tile
is already proved by the entry point; all edge arithmetic still occurs in
its original order. The 720p 60-input confirmation improves 1.190% versus
parent (all six pairs faster); 300 inputs improve 1.558%, all six faster.
Current/assembly medians in that longer run are 7.820088/6.942207 ms/input:
the remaining assembly gap is 12.646%, all six pairs slower. All 23 non-timing
fields, including zero allocations, match. Short-run repeats include one
mixed control, retained alongside the two all-six wins; no universal CPU or
noise claim is made. Direct high-half interleaves, preloaded constants and
an inline row-store body are excluded after unconvincing fresh comparisons.
[Complete accepted/rejected measurements and reproduction](benchmarks/baselines/go127-simd-arm64/2026-10-17-filter-row-stores).

The refreshed optimized-libvpx comparison for `d87df7a7` measures VP8 adaptive
at 7.818055/4.910325 ms/input (Go/C, +59.217%) and fixed speed at
6.634663/3.088625 ms (+114.810%). Measured PSNR, SSIM, output sizes and frame/drop
counts match within each workload, including timing/quality passes; this does
not by itself prove bitstream identity. Six rounds alternate execution order,
with codec timers and separate quality passes. The optimized C build is the
same pinned v1.16.0 NEON binary as the prior comparison; all timing samples
are new. [Raw timings and controls](benchmarks/baselines/go127-simd-arm64/2026-10-17-filter-row-stores-libvpx).


The next x64 kernel processes a full 16-byte statistics row in one AVX2
packet, retaining exact int16 signed sums for at most 64 rows and int32
squared-error accumulators. Native `618ff11d` on EPYC 7763 reduces VP8 encode
time 3.363% versus its preceding SIMD source, all six pairs faster; it takes
11.662% less time than original assembly in that run, all six faster. Shared
statistics are also live in VP9: its 320p workload improves 2.967%, all six
pairs faster, while 720p improves 1.299% with mixed pairs. No cross-CPU gain or
closure of the prior 9V45 gap is inferred. All 81 kernel cases are allocation
free; two one-allocation public VP9 outliers and an unrelated quant8 control
regression remain in the [native evidence and raw archives](benchmarks/baselines/go127-simd-amd64/2026-10-17-stats16).


Combined `c065a265` validation on EPYC 7763 improves VP8 7.441% versus the
PR base and 12.409% versus original assembly, all six pairs faster. VP9
320p improves 1.894% versus base, all six faster; 720p improves 0.996% at the
median with mixed pairs. All 81 kernel cases have six allocation-free samples
per variant, and every recorded public semantic/configuration and allocation
field matches in this run. Quantization counts 1/2 still regress 3.308%/2.342%
versus base, all six slower. Hosted tests, native selected-backend/oracle tests
and the optimized-C job all pass. Its separate EPYC 9V45 C job measures
fixed-speed VP8 at 10.267339/4.104000 ms (Go/C, +150.179%) with matching measured
quality, payload and frame statistics. Adaptive output quality differs.
This does not supply a 9V45 assembly comparison for the newer variance source.
[Exact-head audit and complete raw archives](benchmarks/baselines/go127-simd-amd64/2026-10-17-c065a265-validation).

Further ARM tail extraction, bounded squared-error loads, mask reduction and
high-byte widening are excluded. Short comparisons have mixed pair signs;
fresh 300-input tail, bounds and mask comparisons measure -0.135%, +0.228%
and +0.448% versus the selected row-store implementation, with mixed pairs.
All 23 recorded non-timing fields and zero allocations match, and scalar
oracles pass; these changes do not establish a sustained encoder gain.
[Reproducible rejected probes, raw samples and generated code](benchmarks/baselines/go127-simd-arm64/2026-10-17-rejected-row-tail-and-metrics).


ARM full-width denoising now processes sixteen pixels in checked byte-vector
loads and stores. Signed per-column adjustment sums remain exact through the
scalar upper clamp; short spans, unsupported parameters and partial aliases
retain the existing path. The fresh 300-input six-pair confirmation improves
VP8 0.614% versus its preceding SIMD implementation, all six pairs faster,
with identical recorded non-timing fields and zero allocations. A separate
same-run assembly comparison leaves an 11.743% ARM VP8 gap. A pointer-load
variant is excluded because its incremental encoder gain does not survive
the direct confirmation. Full tests, race/checkptr, experiment-off, purego,
strict byte parity and refreshed PGO checks pass.
[Measurements, scalar oracles and generated-code proof](benchmarks/baselines/go127-simd-arm64/2026-10-17-byte-denoiser).

Temporary x64 probe sources are retained in a verified Git bundle before
remote branch cleanup. Accepted rolling and statistics implementations are
already integrated; rejected experiments remain evidence rather than merge
candidates. [Source archive and branch audit](benchmarks/baselines/go127-simd-amd64/2026-10-17-experiment-archive).


The selected ARM byte denoiser has a fresh optimized-libvpx comparison:
adaptive VP8 Go/C medians are 7.681322/4.866042 ms/input (+57.856%);
fixed-speed medians are 6.610026/3.064633 ms/input (+115.687%). Each has
six alternating-order rounds, separate decoded-quality verification, matching
measured quality/output/frame statistics and unchanged binary hashes.
[Complete controls, timings and quality](benchmarks/baselines/go127-simd-arm64/2026-10-17-byte-denoiser-libvpx).

The x64 byte-zip and mask probes remain excluded: byte-zip has mixed public
VP8 signs and a VP9 regression; mask reduction does not confirm its first
VP8 gain and regresses VP9 in its second run. The same-run preceding selected
SIMD source on EPYC 9V74 still trails assembly by about 16.1% in VP8. Wins
on EPYC 7763 and Xeon 8573C therefore do not establish an all-x64 win.
[Both rejected designs, complete native samples and control drift](benchmarks/baselines/go127-simd-amd64/2026-10-17-byte-zip-and-mask-rejections).


A width-gated x64 horizontal dot-product probe improves VP8 2.999% versus
its pinned SIMD parent on EPYC 7763, all six pairs faster. It is held out
of production pending confirmation: VP9 320p regresses 0.754% in five pairs,
720p is mixed, and unrelated controls also drift. The dot helper is live
in VP8; VP9 supplies full eight-tap arrays and cannot select this six-tap
helper, so the VP9 shift is not direct evidence of dot arithmetic cost.
All 81 kernels have six zero-allocation samples; scalar/alias oracles pass.
[Full results, controls and dispatch evidence](benchmarks/baselines/go127-simd-amd64/2026-10-17-gated-dot-hold).


The complete luma denoiser now retains the first pass's clamped columns and
uses SIMD for the weaker correction and final signal copy, avoiding a repeated
scalar first pass. On M4 Max, six-round public DenoiserFilterY medians are
576.2→54.4 ns for mixed blocks (10.59×) and 320.6→33.65 ns for saturating
blocks (9.53×), all allocation-free with scalar-checked outputs. These are
10.18×/9.20× faster than old assembly for those cases; smooth 43.42 ns still
trails assembly 33.385 ns. Default whole-encoder 300-input median is +0.198%
with mixed signs and identical 23 non-timing fields. The kernel gain is kept
under the user's explicit 1.5× acceptance rule; no whole-encoder improvement
is claimed. The corresponding x64 byte and correction paths have required
SIMD scalar/alias correctness coverage; native performance is measured in
the follow-up PR. [Raw measurements, proofs and reproduction](benchmarks/baselines/go127-simd-arm64/2026-10-17-complete-denoiser).

Two-block ARM quantization unrolling is excluded: 300-input median regresses
0.290%, five of six pairs slower, while short runs are mixed. All 23 non-timing
and allocation fields match. [Rejected source and complete results](benchmarks/baselines/go127-simd-arm64/2026-10-17-rejected-quant-pair).


Fresh integrated-source M4 Max fixed-speed VP8 Go/libvpx medians are
6.906217/3.226242 ms/input (+114.064%), with matching measured quality,
payload and frame counts, including every timing/quality pass. Adaptive
medians are 8.029216/5.547350 ms (+44.740%), but libvpx's timed output varies;
its separate quality pass therefore does not establish matched work for all
adaptive timings. Six rounds alternate execution order; codec-internal timers
are separate from subprocess wall time. [Raw controls, samples and quality](benchmarks/baselines/go127-simd-arm64/2026-10-17-complete-denoiser-libvpx).

[The merged checkpoint audit](benchmarks/baselines/go127-simd-amd64/2026-10-17-merged-checkpoint) retains final main-equivalent native/C results, CPU-specific gaps,
small-count quantization regressions and the isolated VP9 allocation outlier.
[The new x64 port's correctness evidence](benchmarks/baselines/go127-simd-amd64/2026-10-17-complete-denoiser-correctness) includes required-SIMD ordinary tests and checkptr oracles. Three unrelated
allocation assertions under checkptr instrumentation fail identically on clean
main; ordinary baseline and new-source allocation tests pass unchanged.
