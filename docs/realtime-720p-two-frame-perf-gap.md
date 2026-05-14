# 1280x720 realtime frames=1 vs frames=2 performance gap

Date: 2026-05-14

## Summary

The literal command from the repo root fails because the root package is a library:

```text
go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
package github.com/thesyncim/govpx is not a main package
```

The equivalent benchmark entrypoint from the repo root is:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames N -fps 30 -bitrate 2000 -mode realtime
```

I did not reproduce a govpx-owned per-frame slowdown for `frames=2` in this checkout. The encode-only govpx average improved from about 29.9 ms/frame at `frames=1` to about 21.0 ms/frame at `frames=2`, because the two-frame run averages one keyframe plus one smaller inter frame.

There are still two real discontinuities at `frames=2`:

1. `frames=2` is the first run that exercises the inter-frame path, including macroblock mode selection, full-pel motion search, sub-pel refinement, inter reconstruction, loop-filter selection, and inter packet writing.
2. The default libvpx comparison changes timing source between these short runs on this machine: the one-frame run fell back to libvpx wall time, while the two-frame run used parsed `vpxenc` encode stats. That makes the printed govpx-vs-libvpx delta look much worse at `frames=2`, even though govpx's own average did not collapse.

Likely cause of the reported gap: the perceived jump is a mix of path selection and benchmark reporting. The first frame is keyframe-only. The second frame enters govpx's inter-frame motion-search path, where govpx is materially slower than libvpx for this content. In the default report, the comparison is amplified by libvpx timing source switching from wall time to `vpxenc-stats`.

## Environment

```text
go version go1.26.3 darwin/arm64
Darwin Marcelos-MacBook-Pro.local 25.3.0 Darwin Kernel Version 25.3.0: Wed Jan 28 20:51:28 PST 2026; root:xnu-12377.91.3~2/RELEASE_ARM64_T6041 arm64
git rev-parse --short HEAD: da0a100
```

The worktree already had unrelated untracked diagnostic files before this investigation. I did not edit or revert them.

## Timings

Default command, including libvpx comparison and quality metrics:

```sh
/usr/bin/time -p go run ./cmd/govpx-bench -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx ns/frame: 30.65 ms
libvpx ns/frame: 123.48 ms
libvpx timing source: wall
real: 0.29 s
```

```sh
/usr/bin/time -p go run ./cmd/govpx-bench -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx ns/frame: 20.38 ms
govpx latency: p50=10.65 ms, p95=30.11 ms, p99=30.11 ms
libvpx ns/frame: 8.11 ms
libvpx timing source: vpxenc-stats
libvpx wall/frame: 57.94 ms
libvpx subprocess overhead: 99.68 ms
real: 0.32 s
```

Encode-only phase timings, with libvpx and quality decode disabled:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -phase-timing -format json
```

Key fields:

```json
{
  "ns_per_frame": 29910625,
  "latency_ns": {"p50": 29910625, "p95": 29910625, "p99": 29910625},
  "phase_ns": {
    "inter_reconstruct_ns": 0,
    "key_reconstruct_ns": 16252458,
    "loop_filter_pick_ns": 6261416,
    "packet_write_ns": 5701167,
    "inter_attempts": 0,
    "key_attempts": 1,
    "fullpel_sad_candidates": 0,
    "subpel_variance_calls": 0
  }
}
```

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -phase-timing -format json
```

Key fields:

```json
{
  "ns_per_frame": 21028000,
  "latency_ns": {"p50": 11459333, "p95": 30596667, "p99": 30596667},
  "phase_ns": {
    "inter_reconstruct_ns": 9246875,
    "key_reconstruct_ns": 16402916,
    "loop_filter_pick_ns": 6708708,
    "packet_write_ns": 6619208,
    "inter_attempts": 1,
    "key_attempts": 1,
    "inter_coef_token_records": 64673,
    "fullpel_sad_calls": 18421,
    "fullpel_sad_candidates": 39871,
    "fullpel_batch_calls": 7150,
    "fullpel_early_breaks": 3755,
    "subpel_candidates": 35585,
    "subpel_variance_calls": 35585
  }
}
```

The phase counters are the clearest evidence: the one-frame run has no inter work, while the two-frame run adds one inter attempt and tens of thousands of motion-search candidate evaluations.

## Profile signal

For the exact two-frame encode-only profile:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -cpuprofile /tmp/govpx-720p-f2.cpu
go tool pprof -top /tmp/govpx-720p-f2.cpu
```

The profile was short, with only 80 ms of samples, but it still showed both paths:

```text
70ms  github.com/thesyncim/govpx.(*VP8Encoder).EncodeInto
40ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeKeyFrameAttempt
20ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
10ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
```

To get a less noisy view of where steady-state inter time goes, I also profiled 20 frames with the same options:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 20 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -cpuprofile /tmp/govpx-720p-f20.cpu
go tool pprof -top /tmp/govpx-720p-f20.cpu
```

Key cumulative samples:

```text
410ms  encodeInterFrameAttempt
270ms  buildReconstructingInterFrameCoefficientsWithSegmentation
190ms  selectFastInterFrameModeDecision
150ms  macroblockLumaMotionVarianceSSE
140ms  internal/vp8/dsp.varianceBlock16x16NEON
 90ms  pickLoopFilterLevel
 50ms  interFrameMotionVectorSearch.selectFast
 30ms  internal/vp8/dsp.SubpelVariance16x16
```

That profile supports the phase counters: once the run includes inter frames, the hot path is inter reconstruction and fast inter mode/motion scoring, with variance kernels taking the largest flat samples.

## Relevant code paths

Benchmark harness:

- `cmd/govpx-bench/benchcmd/encode.go:25` builds exactly `cfg.Frames` synthetic frames.
- `cmd/govpx-bench/benchcmd/encode.go:53` does a warmup encode pass.
- `cmd/govpx-bench/benchcmd/encode.go:59` resets the encoder.
- `cmd/govpx-bench/benchcmd/encode.go:63` starts the measured encode pass.
- `cmd/govpx-bench/benchcmd/encode.go:107` optionally runs quality decode/metrics after encode. `-skip-quality` removes this from focused timings.

Encoder path:

- `encoder_frame.go:483` enters `encodeInterFrameWithQuantizerFeedback` when the frame is not a keyframe.
- `encoder_attempts.go:566` starts the measured inter reconstruction phase.
- `encoder_attempts.go:576` to `encoder_attempts.go:584` call the inter coefficient/reconstruction builders.
- `encoder_attempts.go:597` runs loop-filter level selection for the inter attempt.
- `encoder_reconstruct.go:468` starts the serial macroblock loop; 1280x720 is 80 columns by 45 rows, or 3600 macroblocks.
- `encoder_reconstruct.go:502` calls `selectInterFrameModeDecision` for each macroblock.
- `encoder_inter_modes.go:82` selects the realtime fast picker when RD mode decision is disabled.
- `encoder_inter_modes_fast.go:215` enters the `NewMV` branch and calls `interFrameMotionVectorSearch.selectFast`.
- `encoder_inter_motion_subpel.go:301` calls `dsp.SubpelVariance16x16` for quarter-pel candidates.
- `encoder_inter_motion_subpel.go:328` performs iterative half-pel then quarter-pel refinement.

Libvpx reporting path:

- `cmd/govpx-bench/benchcmd/libvpx.go:84` initializes libvpx reference timing from subprocess wall time.
- `cmd/govpx-bench/benchcmd/libvpx.go:90` switches to parsed `vpxenc` progress timing if available.
- `cmd/govpx-bench/benchcmd/libvpx.go:93` divides whichever timing source was selected by the frame count.
- `cmd/govpx-bench/benchcmd/report.go:76` prints the selected libvpx timing source and wall/subprocess details.

## Conclusion

The first-frame case is keyframe-only and does not exercise motion search. The two-frame case adds the first inter frame, which immediately performs 3600 macroblock mode decisions plus full-pel and sub-pel motion search. That is the likely real performance gap versus libvpx.

For this exact checkout and environment, govpx itself did not get slower on average at `frames=2`; it went from about 29.9-30.7 ms/frame to about 20.4-21.1 ms/frame. The larger visible comparison gap comes from default reporting: libvpx was timed by wall clock for `frames=1` but by parsed `vpxenc-stats` for `frames=2`, so the libvpx denominator becomes much smaller in the two-frame report.
