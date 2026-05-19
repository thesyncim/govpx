# 1280x720 realtime frames=1 vs frames=2 performance note

Date: 2026-05-14

## Where to run

`go run ./` with `-width`, `-height`, `-frames`, `-fps`, `-bitrate`, and `-mode` is the benchmark command under:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
```

From the repository root, the equivalent is:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Running the exact command from the repository root fails because the root package is a library:

```text
package github.com/thesyncim/govpx is not a main package
```

## Environment

```sh
go version
# go version go1.26.3 darwin/arm64

uname -a
# Darwin Marcelos-MacBook-Pro.local 25.3.0 Darwin Kernel Version 25.3.0: Wed Jan 28 20:51:28 PST 2026; root:xnu-12377.91.3~2/RELEASE_ARM64_T6041 arm64

git rev-parse --short HEAD
# e3b4cda
```

The worktree was already dirty before this investigation; I did not revert or edit those files.

## Commands run and observations

Exact 1-frame command:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
/usr/bin/time -p go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx ns/frame: 30.87 ms
govpx encode fps: 32.4
libvpx ns/frame: 20.29 ms
libvpx encode fps: 49.3
output bytes: 83.96 KiB
keyframe bytes: 83.96 KiB
real 1.38
user 1.57
sys 0.31
```

Exact 2-frame command:

```sh
cd /Users/thesyncim/GolandProjects/govpx/cmd/govpx-bench
/usr/bin/time -p go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Observed:

```text
govpx ns/frame: 20.20 ms
govpx encode fps: 49.5
libvpx ns/frame: 7.69 ms
libvpx encode fps: 130.0
govpx latency p50=10.84 ms p95=29.56 ms p99=29.56 ms
output bytes: 89.39 KiB
keyframe bytes: 83.96 KiB
avg interframe: 5.44 KiB
real 1.23
user 1.55
sys 0.32
```

I did not reproduce an absolute wall-clock collapse in this checkout. The 2-frame run was slightly faster overall than the 1-frame run, and govpx average encode time also improved because the second frame is much smaller than the keyframe. The relative gap to libvpx does widen in the 2-frame run: govpx is 1.52x libvpx at 1 frame and 2.63x libvpx at 2 frames.

Phase-timing command, with libvpx comparison and quality decode disabled so the numbers isolate govpx encode:

```sh
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -phase-timing -format json
```

Key observed fields:

```json
{
  "ns_per_frame": 20043500,
  "encode_fps": 49.89148601791104,
  "latency_ns": {
    "p50": 10589542,
    "p95": 29497459,
    "p99": 29497459
  },
  "phase_ns": {
    "inter_reconstruct_ns": 8487667,
    "key_reconstruct_ns": 16207750,
    "loop_filter_pick_ns": 6392126,
    "loop_filter_apply_ns": 1352875,
    "packet_write_ns": 6330542,
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

For comparison, the 1-frame phase-timing run:

```sh
go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -phase-timing -format json
```

Key observed fields:

```json
{
  "ns_per_frame": 29687750,
  "encode_fps": 33.68392687219476,
  "phase_ns": {
    "inter_reconstruct_ns": 0,
    "key_reconstruct_ns": 16091375,
    "loop_filter_pick_ns": 6068375,
    "packet_write_ns": 5937208,
    "inter_attempts": 0,
    "key_attempts": 1,
    "fullpel_sad_candidates": 0,
    "subpel_variance_calls": 0
  }
}
```

CPU profile command:

```sh
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime -auto-libvpx=false -encode-only -cpuprofile /tmp/govpx-1280x720-f2.cpu
go tool pprof -top /tmp/govpx-1280x720-f2.cpu
```

The profile was short, with 80 ms of samples, but the cumulative stack still pointed at the normal encode path:

```text
70ms  github.com/thesyncim/govpx.(*VP8Encoder).EncodeInto
50ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeKeyFrameAttempt
20ms  github.com/thesyncim/govpx.(*VP8Encoder).encodeInterFrameAttempt
10ms  github.com/thesyncim/govpx.(*VP8Encoder).buildReconstructingInterFrameCoefficientsWithSegmentation
10ms  github.com/thesyncim/govpx.(*VP8Encoder).pickLoopFilterLevel
```

## Root-cause hypothesis

The discontinuity at `frames=2` is explained by path selection: `frames=1` encodes only the initial keyframe, while `frames=2` adds the first inter frame.

The benchmark constructs all frames, performs a warmup encode loop, resets the encoder, then measures a second encode loop in `cmd/govpx-bench/benchcmd/encode.go:25`, `cmd/govpx-bench/benchcmd/encode.go:53`, and `cmd/govpx-bench/benchcmd/encode.go:63`. The first measured frame is a keyframe. On the second measured frame, `encodeSourceInto` switches into the inter path at `encoder_frame.go:483`, calling `encodeInterFrameWithQuantizerFeedback`.

That path reaches `encodeInterFrameAttempt` and then `buildReconstructingInterFrameCoefficients*` at `encoder_attempts.go:566` through `encoder_attempts.go:587`. The serial inter reconstruction loop walks every macroblock at `encoder_reconstruct.go:468` through `encoder_reconstruct.go:684`; on this 1280x720 input that is 80 x 45 = 3600 macroblocks for the first inter frame. Inside that loop, each macroblock asks `selectInterFrameModeDecision` to choose an inter/intra mode (`encoder_reconstruct.go:502`), which uses the realtime fast picker when RD mode is disabled (`encoder_inter_modes.go:82` through `encoder_inter_modes.go:96`).

The phase counters show that this first inter frame did substantial motion-search work: 39,871 full-pel candidates and 35,585 subpel variance calls. The `NewMV` branch in the fast picker invokes `interFrameMotionVectorSearch.selectFast()` at `encoder_inter_modes_fast.go:215` through `encoder_inter_modes_fast.go:244`; sub-pixel refinement then calls `dsp.SubpelVariance16x16` from `encoder_inter_motion_subpel.go:301` through `encoder_inter_motion_subpel.go:320`, repeatedly visited by the iterative half-/quarter-pel search at `encoder_inter_motion_subpel.go:328` through `encoder_inter_motion_subpel.go:443`.

So the likely cause of the perceived 1-frame vs 2-frame gap is not `go run` startup or benchmark harness overhead. It is that `frames=2` is the smallest input that exercises the inter-frame motion-search/reconstruction/packet path, while `frames=1` is keyframe-only. In my run that did not make total time collapse, but it did expose the first inter frame's extra motion-search counters and widened the relative gap to libvpx.
