# Realtime frames=1 vs frames=2 performance gap

Date: 2026-05-14

## Summary

The literal command from the repository root does not run in this checkout:

```sh
go run ./ -width 1280 -height 720 -frames 1 -fps 30 -bitrate 2000 -mode realtime
go run ./ -width 1280 -height 720 -frames 2 -fps 30 -bitrate 2000 -mode realtime
```

Both fail with:

```text
package github.com/thesyncim/govpx is not a main package
```

The equivalent local benchmark entrypoint is:

```sh
go run ./cmd/govpx-bench -width 1280 -height 720 -frames N -fps 30 -bitrate 2000 -mode realtime
```

The cause of the gap is the frame-type boundary. `frames=1` only encodes frame
zero, which is always a keyframe. `frames=2` adds the first inter frame, and
that immediately enters the realtime inter-mode and motion-search path. The
extra work maps directly to libvpx behavior: libvpx also forces
`current_video_frame == 0` to `KEY_FRAME`, while later non-keyframes enter
`vp8cx_encode_inter_macroblock`, `vp8_pick_inter_mode`, full-pel diamond/hex
search, and sub-pixel refinement.

## Measurements

Environment:

```text
go version go1.26.3 darwin/arm64
git rev-parse --short HEAD: d5c5f47
```

Focused govpx-only measurements used `-auto-libvpx=false -skip-quality
-phase-timing -format json` so the counters describe the encoder path rather
than libvpx comparison and quality decode work.

For `frames=1`, the representative run was keyframe-only:

```json
{
  "ns_per_frame": 37527916,
  "phase_ns": {
    "inter_reconstruct_ns": 0,
    "key_reconstruct_ns": 20228666,
    "inter_attempts": 0,
    "key_attempts": 1,
    "inter_coef_token_records": 0,
    "fullpel_sad_candidates": 0,
    "subpel_variance_calls": 0
  }
}
```

For `frames=2`, timings were noisy, but the structural counters were stable:

```json
{
  "phase_ns": {
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

The important signal is not a specific wall-clock number: it is that the
one-frame command has no inter-frame work, while the two-frame command adds one
complete inter-frame encode. At 1280x720, the inter-frame path walks an 80x45
macroblock grid, or 3,600 macroblock decisions.

## govpx source path

- `encoder_reference_decisions.go:405` makes `e.frameCount == 0` a keyframe.
- `encoder_frame.go:518` sends non-keyframes to
  `encodeInterFrameWithQuantizerFeedback`.
- `encoder_reconstruct.go:480` to `encoder_reconstruct.go:502` walks the
  inter-frame macroblock grid and begins a mode decision for each macroblock.
- `encoder_inter_modes.go:82` to `encoder_inter_modes.go:96` selects the
  realtime fast inter-mode picker when RD mode decision is disabled.
- `encoder_inter_modes_fast.go:215` to `encoder_inter_modes_fast.go:244`
  evaluates `NEWMV` by calling `interFrameMotionVectorSearch.selectFast()`.
- `encoder_inter_motion_subpel.go:301` to
  `encoder_inter_motion_subpel.go:320` calls `dsp.SubpelVariance16x16` for
  quarter-pel candidates.
- `encoder_inter_motion_subpel.go:323` to
  `encoder_inter_motion_subpel.go:328` documents the half-pel then quarter-pel
  refinement as the libvpx iterative sub-pixel path.

## libvpx source mapping

Vendored source inspected: `internal/coracle/build/libvpx-v1.16.0`.

- `vp8/encoder/onyx_if.c:3403` to `vp8/encoder/onyx_if.c:3412` sets
  `cm->frame_type = KEY_FRAME` when `cm->current_video_frame == 0`.
- `vp8/encoder/encodeframe.c:446` to `vp8/encoder/encodeframe.c:457` branches
  per macroblock: keyframes call `vp8cx_encode_intra_macroblock`, inter frames
  call `vp8cx_encode_inter_macroblock`.
- `vp8/encoder/encodeframe.c:1161` to `vp8/encoder/encodeframe.c:1185` uses
  `vp8_pick_inter_mode` for the non-RD realtime picker.
- `vp8/encoder/pickinter.c:563` starts `vp8_pick_inter_mode`.
- `vp8/encoder/pickinter.c:1025` to `vp8/encoder/pickinter.c:1032` performs
  hex or diamond full-pel search for `NEWMV`.
- `vp8/encoder/mcomp.c:225` starts
  `vp8_find_best_sub_pixel_step_iteratively`.
- `vp8/encoder/mcomp.c:301` to `vp8/encoder/mcomp.c:329` performs the half-pel
  then quarter-pel refinement loop.

## Conclusion

The `frames=1` case is fast because it stops after the first keyframe and never
enters inter prediction. The `frames=2` case is the first run that exercises
inter prediction: per-macroblock mode selection, full-pel motion search,
sub-pel refinement, residual coding, reconstruction, loop-filter decisions, and
inter packet writing.

This is not a mysterious two-frame special case. It is the first-inter-frame
cost becoming visible, and that behavior matches libvpx's source-level control
flow.
