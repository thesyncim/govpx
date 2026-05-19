# Worker report: 1280x720 realtime frames=1 vs frames=2

Date: 2026-05-14

## Summary

The exact root command from the task does not run in this checkout because the
repository root is a library package:

```sh
cd /Users/thesyncim/GolandProjects/govpx
/usr/bin/time -p go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
package github.com/thesyncim/govpx is not a main package
real 0.03
user 0.02
sys 0.02
```

The same is true for `-frames 2` from the repository root:

```text
package github.com/thesyncim/govpx is not a main package
real 0.03
user 0.01
sys 0.04
```

The equivalent executable benchmark is under `cmd/govpx-bench`, where `go run ./`
accepts the same flags.

In this checkout I did not reproduce an absolute govpx wall-time collapse at
`frames=2`. The two-frame run averaged less time per frame because it is one
large keyframe plus one much smaller inter frame. The real discontinuity is that
`frames=2` is the smallest run that enters the inter-frame path. That path adds
per-macroblock mode decisions, full-pel search, sub-pel refinement, inter
reconstruction, loop-filter selection, and inter packet writing.

The visible comparison gap also widens because the reference timing source
changes between the short runs: `frames=1` uses wall timing, while `frames=2`
uses parsed encoder stats.

## Environment

```text
go version go1.26.3 darwin/arm64
Darwin Marcelos-MacBook-Pro.local 25.3.0 Darwin Kernel Version 25.3.0: Wed Jan 28 20:51:28 PST 2026; root:xnu-12377.91.3~2/RELEASE_ARM64_T6041 arm64
git rev-parse --short HEAD: dfecf3a
```

The worktree was already dirty before this investigation. I did not edit or
revert production Go code.

## Direct timings

Command:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
/usr/bin/time -p go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx-bench  encode  realtime  1280x720 @30fps  target=2000 kbps  frames=1

metric          govpx      libvpx     delta
------          -----      ------     -----
ns/frame        29.25 ms   20.07 ms   1.46x
encode fps      34.2       49.8       0.69x
output bytes    83.96 KiB  83.96 KiB  1.00x
keyframe bytes  83.96 KiB  83.96 KiB  1.00x
avg interframe  0 B        0 B        -

quantizers      min=49 max=49 mean=49.00  (encoded=1 dropped=0)
govpx latency   p50=29.25 ms  p95=29.25 ms  p99=29.25 ms
libvpx timing   source=wall  wall/frame=20.07 ms  subprocess=-
real 1.38
user 1.55
sys 0.27
```

Command:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
/usr/bin/time -p go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx-bench  encode  realtime  1280x720 @30fps  target=2000 kbps  frames=2

metric          govpx      libvpx     delta
------          -----      ------     -----
ns/frame        20.06 ms   8.17 ms    2.46x
encode fps      49.9       122.4      0.41x
output bytes    89.39 KiB  87.89 KiB  1.02x
keyframe bytes  83.96 KiB  83.96 KiB  1.00x
avg interframe  5.44 KiB   3.93 KiB   1.38x

quantizers      min=49 max=56 mean=52.50  (encoded=2 dropped=0)
govpx latency   p50=10.63 ms  p95=29.49 ms  p99=29.49 ms
libvpx timing   source=vpxenc-stats  wall/frame=14.21 ms  subprocess=12.09 ms
real 1.22
user 1.60
sys 0.30
```

Important interpretation: govpx's average went from 29.25 ms/frame to
20.06 ms/frame. The widened relative delta is real versus the reference encode
number, but the govpx average did not collapse in this run.

## Focused phase timings

To isolate govpx encode work from reference comparison and quality decode:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -phase-timing -format json
```

Key fields:

```json
{
  "ns_per_frame": 30086250,
  "latency_ns": {
    "p50": 30086250,
    "p95": 30086250,
    "p99": 30086250
  },
  "phase_ns": {
    "inter_reconstruct_ns": 0,
    "key_reconstruct_ns": 16513125,
    "loop_filter_pick_ns": 6108416,
    "loop_filter_apply_ns": 629542,
    "packet_write_ns": 5816833,
    "inter_attempts": 0,
    "key_attempts": 1,
    "inter_coef_token_records": 0,
    "fullpel_sad_calls": 0,
    "fullpel_sad_candidates": 0,
    "subpel_candidates": 0,
    "subpel_variance_calls": 0
  }
}
```

Command:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -phase-timing -format json
```

Key fields:

```json
{
  "ns_per_frame": 20140062,
  "latency_ns": {
    "p50": 10590958,
    "p95": 29689166,
    "p99": 29689166
  },
  "phase_ns": {
    "inter_reconstruct_ns": 8448417,
    "key_reconstruct_ns": 16182625,
    "loop_filter_pick_ns": 6383292,
    "loop_filter_apply_ns": 1357459,
    "packet_write_ns": 6540167,
    "inter_attempts": 1,
    "key_attempts": 1,
    "loop_filter_trials": 17,
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

These counters are the main evidence. `frames=1` does no inter work. `frames=2`
adds one inter attempt with 64,673 coefficient token records, 39,871 full-pel
SAD candidates, and 35,585 sub-pel variance calls.

## CPU profile signal

Short two-frame profile:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -cpuprofile /tmp/govpx-1280x720-f2-current.cpu
go tool pprof -top /tmp/govpx-1280x720-f2-current.cpu
```

The two-frame profile had only 80 ms of samples, so it is noisy. It still shows
the measured encode split:

```text
80ms  github.com/thesyncim/govpx.(*VP8Encoder).EncodeInto
60ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeKeyFrameAttempt
20ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
10ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
10ms  github.com/thesyncim/govpx.(*VP8Encoder).pickLoopFilterLevel
```

Longer 20-frame profile with the same options gives a clearer steady-state
inter-frame view:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
go run ./ -width 1280 -height 720 -frames 20 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -cpuprofile /tmp/govpx-1280x720-f20-current.cpu
go tool pprof -top /tmp/govpx-1280x720-f20-current.cpu
```

Key cumulative samples:

```text
430ms  github.com/thesyncim/govpx.(*VP8Encoder).EncodeInto
360ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
140ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
140ms  github.com/thesyncim/govpx.(*VP8Encoder).pickLoopFilterLevel
 80ms  github.com/thesyncim/govpx.(*VP8Encoder).selectFastInterFrameModeDecision
 40ms  github.com/thesyncim/govpx.interFrameMotionVectorSearch.selectFast
 20ms  github.com/thesyncim/govpx.(*interFrameSubpixelSearch).iterative
 20ms  github.com/thesyncim/govpx/internal/vp8/dsp.SubpelVariance16x16
```

## Relevant code paths

Benchmark harness:

- `cmd/govpx-bench/benchcmd/encode.go:25` builds exactly `cfg.Frames` synthetic frames.
- `cmd/govpx-bench/benchcmd/encode.go:53` runs a warmup encode over all frames.
- `cmd/govpx-bench/benchcmd/encode.go:59` resets the encoder before measurement.
- `cmd/govpx-bench/benchcmd/encode.go:63` starts the measured encode loop.
- `cmd/govpx-bench/benchcmd/encode.go:107` runs quality decode/metrics unless `-encode-only` is set.

Encoder path:

- `vp8_encoder_frame.go:483` enters `encodeInterFrameWithQuantizerFeedback` when the frame is not a keyframe.
- `vp8_encoder_attempts.go:566` starts the measured inter reconstruction phase.
- `vp8_encoder_attempts.go:576` to `vp8_encoder_attempts.go:584` build inter coefficients and reconstruction.
- `vp8_encoder_attempts.go:597` runs inter-frame loop-filter level selection.
- `vp8_encoder_reconstruct.go:468` starts the per-macroblock inter reconstruction loop.
- `vp8_encoder_reconstruct.go:502` calls `selectInterFrameModeDecision` for each macroblock.
- `vp8_encoder_inter_modes.go:82` chooses the realtime fast picker when RD mode decision is disabled.
- `vp8_encoder_inter_modes_fast.go:215` enters the `NewMV` branch and calls `interFrameMotionVectorSearch.selectFast`.
- `vp8_encoder_inter_motion_subpel.go:301` calls `dsp.SubpelVariance16x16`.
- `vp8_encoder_inter_motion_subpel.go:328` starts iterative half-pel and quarter-pel refinement.

Reference timing path:

- `cmd/govpx-bench/benchcmd/libvpx.go:86` records reference subprocess wall time.
- `cmd/govpx-bench/benchcmd/libvpx.go:89` initially sets timing source to `wall`.
- `cmd/govpx-bench/benchcmd/libvpx.go:90` switches to parsed encoder stats when available.
- `cmd/govpx-bench/benchcmd/libvpx.go:94` divides the selected timing source by frame count.

## Likely cause

The first frame is keyframe-only. At 1280x720, the second frame is the first
inter frame and therefore walks 80 x 45 = 3600 macroblocks through the realtime
inter picker and motion-search/reconstruction path. The phase counters show the
extra work directly: the one-frame run has zero inter attempts and zero motion
search counters; the two-frame run has one inter attempt, 39,871 full-pel SAD
candidates, and 35,585 sub-pel variance calls.

So the likely cause of the perceived gap is not `go run` startup or the
benchmark harness. It is the path change from keyframe-only to keyframe plus
inter frame, combined with the benchmark's reference timing source switching
from wall time in the one-frame report to parsed encoder stats in the two-frame
report. In this run, govpx's own average encode time did not collapse; the
relative comparison gap widened because the second command exposes the inter
path and the reference denominator gets much smaller.
