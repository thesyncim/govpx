# 1280x720 Realtime Frames 1 vs 2 Investigation

Date: 2026-05-14

## Commands

Run from `/Users/thesyncim/GolandProjects/govpx`.

The two requested root commands are not executable in this checkout because the
repository root is a library package:

```sh
go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
package github.com/thesyncim/govpx is not a main package
```

Timed:

```text
real 0.03
user 0.02
sys 0.03
```

```sh
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
package github.com/thesyncim/govpx is not a main package
```

Timed:

```text
real 0.03
user 0.02
sys 0.03
```

The matching benchmark entrypoint from the repo root is `./cmd/govpx-bench`.
Environment for the successful benchmark runs:

```text
go version go1.26.3 darwin/arm64
git rev-parse --short HEAD: 48e03d7
```

## Observed Timings

```sh
/usr/bin/time -p go run ./cmd/govpx-bench -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Key output:

```text
ns/frame        30.12 ms   21.86 ms   1.38x
encode fps      33.2       45.7       0.73x
output bytes    83.96 KiB  83.96 KiB  1.00x
keyframe bytes  83.96 KiB  83.96 KiB  1.00x
avg interframe  0 B        0 B        -
quantizers      min=49 max=49 mean=49.00  (encoded=1 dropped=0)
govpx latency   p50=30.12 ms  p95=30.12 ms  p99=30.12 ms
libvpx timing   source=wall  wall/frame=21.86 ms  subprocess=-
real 0.22
user 0.15
sys 0.15
```

```sh
/usr/bin/time -p go run ./cmd/govpx-bench -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Key output:

```text
ns/frame        20.74 ms   8.35 ms    2.48x
encode fps      48.2       119.7      0.40x
output bytes    89.39 KiB  87.89 KiB  1.02x
keyframe bytes  83.96 KiB  83.96 KiB  1.00x
avg interframe  5.44 KiB   3.93 KiB   1.38x
quantizers      min=49 max=56 mean=52.50  (encoded=2 dropped=0)
govpx latency   p50=10.99 ms  p95=30.50 ms  p99=30.50 ms
libvpx timing   source=vpxenc-stats  wall/frame=15.18 ms  subprocess=13.66 ms
real 0.21
user 0.19
sys 0.15
```

Focused govpx-only phase timing, with libvpx comparison and quality decode
disabled:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -phase-timing -format json
```

Key fields:

```json
{
  "ns_per_frame": 31153375,
  "latency_ns": {"p50": 31153375, "p95": 31153375, "p99": 31153375},
  "phase_ns": {
    "inter_reconstruct_ns": 0,
    "key_reconstruct_ns": 16915292,
    "loop_filter_pick_ns": 6400792,
    "packet_write_ns": 6088583,
    "inter_attempts": 0,
    "key_attempts": 1,
    "inter_coef_token_records": 0,
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
  "ns_per_frame": 21286021,
  "latency_ns": {"p50": 11238708, "p95": 31333334, "p99": 31333334},
  "phase_ns": {
    "inter_reconstruct_ns": 8931709,
    "key_reconstruct_ns": 16849959,
    "loop_filter_pick_ns": 6739292,
    "packet_write_ns": 7024542,
    "inter_attempts": 1,
    "key_attempts": 1,
    "inter_coef_token_records": 64673,
    "fullpel_sad_candidates": 39871,
    "subpel_candidates": 35585,
    "subpel_variance_calls": 35585
  }
}
```

The two-frame CPU profile is too short for a stable flat profile, but it still
shows one keyframe attempt and one inter-frame attempt:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -skip-quality -cpuprofile /tmp/govpx-realtime-720p-f2-20260514.cpu
go tool pprof -top /tmp/govpx-realtime-720p-f2-20260514.cpu
```

Relevant cumulative samples:

```text
60ms  github.com/thesyncim/govpx.(*VP8Encoder).EncodeInto
40ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeKeyFrameAttempt
20ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
10ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
```

A longer same-config 20-frame profile gives a clearer inter-frame signal:

```text
390ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
220ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
140ms  github.com/thesyncim/govpx.(*VP8Encoder).selectFastInterFrameModeDecision
130ms  github.com/thesyncim/govpx.(*VP8Encoder).pickLoopFilterLevel
 50ms  github.com/thesyncim/govpx.interFrameMotionVectorSearch.selectFast
```

## Root Cause

There is no absolute govpx realtime collapse in the equivalent benchmark on
this checkout: the measured average improves from 30.12 ms/frame at one frame
to 20.74 ms/frame at two frames. The visible regression is the relative
comparison to libvpx, which widens from 1.38x to 2.48x.

Two measured facts explain the discontinuity:

1. The one-frame run is keyframe-only. `shouldEncodeKeyFrame` returns true when
   `e.frameCount == 0`, so `frames=1` never enters inter-frame analysis.
2. The two-frame run is the first run that encodes an inter frame. Phase
   counters show one inter attempt, 64,673 inter coefficient token records,
   39,871 full-pel SAD candidates, and 35,585 sub-pel variance calls.

The relative libvpx comparison is also affected by a benchmark reporting
switch. The one-frame reference uses wall timing, while the two-frame reference
uses parsed `vpxenc` encode stats. That makes the printed denominator much
smaller for `frames=2` even though the wall times are similar.

## Source References

- `cmd/govpx-bench/benchcmd/encode.go:25` creates exactly `cfg.Frames`
  synthetic frames.
- `cmd/govpx-bench/benchcmd/encode.go:53` to `cmd/govpx-bench/benchcmd/encode.go:63`
  warm up the encoder, reset it, then measure the encode loop.
- `cmd/govpx-bench/benchcmd/libvpx.go:86` to `cmd/govpx-bench/benchcmd/libvpx.go:94`
  choose wall timing unless parsed `vpxenc` timing is available.
- `encoder_reference_decisions.go:405` to `encoder_reference_decisions.go:420`
  make frame zero a keyframe.
- `encoder_frame.go:498` to `encoder_frame.go:500` enter
  `encodeInterFrameWithQuantizerFeedback` for non-keyframes.
- `encoder_attempts.go:566` to `encoder_attempts.go:587` time and run inter
  reconstruction/coefficient building.
- `encoder_attempts.go:591` to `encoder_attempts.go:598` time loop-filter
  level selection.
- `encoder_reconstruct.go:468` to `encoder_reconstruct.go:502` walk the
  inter-frame macroblock grid and call `selectInterFrameModeDecision`. For
  1280x720 this is 80 by 45 macroblocks, or 3,600 decisions.
- `encoder_inter_modes.go:82` to `encoder_inter_modes.go:96` select the
  realtime fast inter-mode picker when RD mode decision is disabled.
- `encoder_inter_modes_fast.go:215` to `encoder_inter_modes_fast.go:244`
  invoke `interFrameMotionVectorSearch.selectFast()` for `NewMV` candidates.
- `encoder_inter_motion_subpel.go:301` to
  `encoder_inter_motion_subpel.go:320` call `dsp.SubpelVariance16x16` for
  sub-pixel candidates.
- `encoder_inter_motion_subpel.go:328` starts the iterative half-pel and
  quarter-pel refinement path.

## Suggested Fixes

1. Fix the invocation mismatch: either document `go run ./cmd/govpx-bench ...`
   as the root command or add a root `main` package if the requested `go run ./`
   interface is intended.
2. Make short-run reports compare like with like. For example, keep libvpx
   wall time as the primary number for one- and two-frame runs, or print the
   `vpxenc-stats` delta separately so the timing-source switch is not read as
   an encoder collapse.
3. Optimize only after targeting the measured inter-frame work. The first
   candidates are the realtime `NewMV` path, sub-pixel refinement count, and
   loop-filter search. Validate changes with `-phase-timing` so reductions show
   up in `fullpel_sad_candidates`, `subpel_variance_calls`, and
   `loop_filter_pick_ns`.
