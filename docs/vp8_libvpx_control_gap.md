# VP8 libvpx Control Gap

Reference target: libvpx v1.16.0, matching `UpstreamLibvpxVersion`.

Primary sources:

- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8cx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8dx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vpx_encoder.h`
- `internal/coracle/build/libvpx-v1.16.0/vp8/vp8_cx_iface.c`
- `internal/coracle/build/libvpx-v1.16.0/vp8/vp8_dx_iface.c`

This document tracks VP8-relevant libvpx controls and the govpx APIs that cover
them. It is not a libvpx C ABI roadmap and does not define VP9 scope; use
`UPSTREAM.md` for VP9 Profile 0, RTP/WebRTC payload compatibility, and
non-Profile-0 behavior.

## Summary

The high-value VP8 encoder controls are covered by Go APIs. Remaining uncovered
items are optional C API surfaces outside the codec-payload contract:
spatial-resampler scale mode (the caller-driven resolution change side of
`vpx_codec_enc_config_set` is covered), output-partition packetization, PSNR
packets, and input-fragment decode plumbing.

Skipped controls: encoder preview postproc, decryptor callbacks, VP8 nonzero
profiles for encode, multi-resolution encode, VP9-only controls, and
header-only fields that libvpx does not wire into the VP8 encoder.

## Already Covered

| libvpx control or config | govpx surface | Notes |
| --- | --- | --- |
| `VP8E_SET_CPUUSED` | `EncoderOptions.CpuUsed`, `SetCPUUsed` | Includes VP8 `-16..16` range and realtime auto-speed behavior. |
| Encode deadline | `EncoderOptions.Deadline`, `SetDeadline` | Maps best/good/realtime behavior. |
| `VP8E_SET_TUNING` | `EncoderOptions.Tuning`, `SetTuning` | Covers PSNR default and SSIM activity masking for encoder RD decisions. |
| `VP8E_SET_ENABLEAUTOALTREF` | `EncoderOptions.AutoAltRef` | Hidden ARF scheduling exists behind lookahead. |
| `VP8E_SET_NOISE_SENSITIVITY` | `EncoderOptions.NoiseSensitivity`, `SetNoiseSensitivity` | Denoiser state is present. |
| `VP8E_SET_SHARPNESS` | `EncoderOptions.Sharpness`, `SetSharpness` | Runtime setter exists. |
| `VP8E_SET_STATIC_THRESHOLD` | `EncoderOptions.StaticThreshold`, `SetStaticThreshold` | Used by first pass and inter static skips. |
| `VP8E_SET_TOKEN_PARTITIONS` | `EncoderOptions.TokenPartitions`, `SetTokenPartitions` | Encodes VP8 1/2/4/8 token partitions. |
| `VP8E_SET_ARNR_MAXFRAMES`, `VP8E_SET_ARNR_STRENGTH`, `VP8E_SET_ARNR_TYPE` | `EncoderOptions.ARNR*`, `SetARNR` | ARNR type defaults to centered, matching libvpx. |
| `VP8E_SET_CQ_LEVEL` | `EncoderOptions.CQLevel`, `SetCQLevel` | Covered for constrained quality mode. |
| `VP8E_SET_MAX_INTRA_BITRATE_PCT` | `EncoderOptions.MaxIntraBitratePct`, `SetMaxIntraBitratePct` | Runtime setter exists. |
| `VP8E_SET_GF_CBR_BOOST_PCT` | `EncoderOptions.GFCBRBoostPct`, `SetGFCBRBoostPct` | Despite enum placement near VP9 controls, libvpx maps this for VP8. |
| `VP8E_SET_FRAME_FLAGS` and VP8 `EFLAG_*` | `EncodeFlags` on `EncodeInto` | More Go-native than a sticky control. Covers ref/use/update/entropy/golden/altref/keyframe flags. |
| `VP8E_SET_TEMPORAL_LAYER_ID` | `SetTemporalLayerID` | Also has `TemporalScalabilityConfig` for built-in patterns. |
| `VP8E_SET_ACTIVEMAP` | `SetActiveMap` | Per-MB active/inactive map exists. |
| `VP8E_SET_ROI_MAP` | `ROIMap`, `SetROIMap` | Per-MB VP8 segment map with public quantizer deltas, loop-filter deltas, and per-segment static thresholds. ROI disables cyclic refresh while active, matching libvpx. |
| `VP8E_SET_SCREEN_CONTENT_MODE` | `EncoderOptions.ScreenContentMode`, `SetScreenContentMode` | Modes 0..2. |
| `VP8E_SET_RTC_EXTERNAL_RATECTRL` | `EncoderOptions.RTCExternalRateControl`, `SetRTCExternalRateControl` | VP8 behavior: disables cyclic refresh and overshoot recode while always updating correction factors. |
| `VP8_SET_REFERENCE`, `VP8_COPY_REFERENCE` on encoder | `VP8Encoder.SetReferenceFrame`, `VP8Encoder.CopyReferenceFrame` | Reference selectors use LAST/GOLDEN/ALTREF. Setting a reference extends borders and invalidates encoder state tied to old reference identity. |
| `VP8_SET_POSTPROC` on decoder | `DecoderOptions.PostProcessFlags`, `DecoderOptions.PostProcessNoiseLevel` | Decoder postproc is exposed as Go flags. |
| `VP8_SET_REFERENCE`, `VP8_COPY_REFERENCE` on decoder | `VP8Decoder.SetReferenceFrame`, `VP8Decoder.CopyReferenceFrame` | Requires a decoded key frame to establish dimensions. Reference selectors use the same LAST/GOLDEN/ALTREF values as `ReferenceFlags`. |
| `VP8D_GET_LAST_REF_UPDATES`, `VP8D_GET_LAST_REF_USED`, `VPXD_GET_LAST_QUANTIZER`, `VP8D_GET_FRAME_CORRUPTED` | `FrameInfo.RefUpdates`, `FrameInfo.RefUsed`, `FrameInfo.InternalQuantizer`, `FrameInfo.Quantizer`, `FrameInfo.Corrupted`, `VP8Decoder.LastFrameInfo` | Ref flags use Go bit flags matching libvpx's LAST/GOLDEN/ALTREF bit values. `InternalQuantizer` is VP8 base qindex; `Quantizer` is govpx's public 0..63 mapping. |
| `VP8E_GET_LAST_QUANTIZER`, `VP8E_GET_LAST_QUANTIZER_64` | `EncodeResult.InternalQuantizer`, `EncodeResult.Quantizer`, `VP8Encoder.LastQuantizer` | Exposes both libvpx's internal qindex and the public 0..63 mapping. |
| Encoder common config: width, height, timebase, threads, bitrate, VBR/CBR/CQ/Q, q range, buffer model, frame drop, lag/lookahead, two-pass, keyframe interval, temporal layers, error resilience | `EncoderOptions`, `RateControlConfig`, `TemporalScalabilityConfig` | Mostly covered with Go-style names. |
| `vpx_codec_enc_config_set` with new width / height (caller-driven runtime resolution change) | `VP8Encoder.SetRealtimeTarget` with `Width` / `Height` set | Rebuilds size-dependent buffers in place, invalidates references, and forces the next frame to be a key frame at the new size. The spatial-resampler side (`VP8E_SET_SCALEMODE`, `rc_resize_*`) is still tracked under "Spatial Resampling And `VP8E_SET_SCALEMODE`". |

## Detailed Control Notes

### `VP8E_SET_ROI_MAP`

Status: covered. Priority: high.

What libvpx exposes:

- A per-macroblock segment map.
- Four VP8 segment IDs.
- Per-segment public quantizer deltas in `[-63, 63]`.
- Per-segment loop-filter deltas in `[-63, 63]`.
- Per-segment static breakout thresholds.
- Passing nil/no deltas disables segmentation.
- Enabling ROI disables cyclic refresh in libvpx.

Why it is covered:

- It is a real VP8 feature, not C API scaffolding.
- It supports screen sharing, active speaker regions, and ROI quality shaping.
- govpx already has segmentation machinery for cyclic refresh and alt-LF
  paths, so the port is mostly a public control plus plumbing and precedence.

Current Go API:

```go
type ROIMap struct {
    Enabled bool
    Rows int
    Cols int
    SegmentID []uint8
    DeltaQuantizer [4]int
    DeltaLoopFilter [4]int
    StaticThreshold [4]int
}

func (e *VP8Encoder) SetROIMap(m *ROIMap) error
```

Implementation notes:

- Translate public q deltas through the same public-q to qindex table libvpx
  uses.
- Reject wrong macroblock dimensions.
- Define precedence against cyclic refresh explicitly. Matching libvpx means
  ROI turns cyclic refresh off while active.
- Tests cover bitstream segmentation headers, per-MB segment IDs, q deltas, LF
  deltas, validation, disable semantics, and static-threshold selection.

### Reference Set/Copy

Status: covered for encoder and decoder. Priority: high for
recovery/oracle/debug users, medium for normal encode/decode.

libvpx controls:

- `VP8_SET_REFERENCE`
- `VP8_COPY_REFERENCE`

These controls are available on both the VP8 encoder and decoder interfaces.
They set or copy `LAST`, `GOLDEN`, or `ALTREF` reference buffers.

Why it is covered:

- External reference repair and recovery are legitimate VP8 workflows.
- It helps WebRTC-style applications and diagnostics.
- govpx already owns explicit `lastRef`, `goldenRef`, and `altRef` buffers.

Current Go API:

```go
type ReferenceFrame ReferenceFlags

const (
    ReferenceLast   ReferenceFrame = ReferenceFrame(ReferenceFlagLast)
    ReferenceGolden ReferenceFrame = ReferenceFrame(ReferenceFlagGolden)
    ReferenceAltRef ReferenceFrame = ReferenceFrame(ReferenceFlagAltRef)
)

func (e *VP8Encoder) SetReferenceFrame(ref ReferenceFrame, src Image) error
func (e *VP8Encoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error
func (d *VP8Decoder) SetReferenceFrame(ref ReferenceFrame, src Image) error
func (d *VP8Decoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error
```

Implemented notes:

- Extend borders after setting a reference.
- On encoder reference replacement, invalidate or update state that assumes
  reference identity: alias flags, reference frame numbers, last-frame inter
  modes, source alt-ref lifecycle, and denoiser reference averages.
- Decoder set-reference requires an initialized decoder size and extends
  borders after copying visible samples.
- Decoder runtime control parity is guarded by
  `TestOracleLibvpxDecoderReferenceControls`, which drives libvpx
  `VP8_SET_REFERENCE` / `VP8_COPY_REFERENCE` across LAST/GOLDEN/ALTREF,
  postprocess, error concealment, threaded decode, and resolution change.

### `VP8E_SET_TUNING`

Status: covered. Priority: medium.

What libvpx exposes:

- `VP8_TUNE_PSNR`
- `VP8_TUNE_SSIM`

Why it is covered:

- It is a real quality/decision knob.
- It affects RD behavior through activity masking and is visible in quality
  validation work.

Current Go API:

```go
type Tuning int

const (
    TunePSNR Tuning = iota
    TuneSSIM
)

// EncoderOptions.Tuning Tuning
func (e *VP8Encoder) SetTuning(t Tuning) error
```

Implemented notes:

- `TunePSNR` is the default and does not allocate an activity map.
- `TuneSSIM` builds a per-frame macroblock luma activity map and applies the
  libvpx-style activity mask to RD multipliers and zbin-over-quant adjustments
  used by inter-frame RD candidate scoring and accepted residual quantization.
- `SetTuning` validates the enum and invalidates the cached activity map so
  the next encode starts from the selected quality model.
- Tests cover constructor and runtime validation, no-allocation PSNR setup,
  SSIM activity-map construction, RD/zbin adjustment, and encode smoke.

### VPX_Q Constant-Quality Mode

Status: covered. Priority: low to medium.

libvpx has four rate-control modes: `VPX_VBR`, `VPX_CBR`, `VPX_CQ`, and
`VPX_Q`. govpx exposes these as `RateControlVBR`, `RateControlCBR`,
`RateControlCQ`, and `RateControlQ`.

Why it is covered:

- This is part of the common encoder config and VP8 implements it.
- It gives callers libvpx-style constant-quality behavior instead of the
  constrained-quality floor.

Current Go API:

```go
const RateControlQ RateControlMode = ...
```

Implemented notes:

- `RateControlQ` is accepted by `EncoderOptions.RateControlMode`,
  `RateControlConfig.Mode`, and `SetRateControl`.
- Like libvpx, `RateControlQ` validates `CQLevel` against the active
  min/max public quantizer range. The level is stored, but the mode does not
  apply the constrained-quality floor used by `RateControlCQ`.
- `RateControlQ` uses the normal public buffer settings rather than the
  relaxed VPX_VBR local-playback buffer override.
- Tests pin the non-CQ floor behavior for frame-start quantizer selection,
  active quantizer bounds, frame-size bounds, runtime `SetCQLevel`, and
  constructor/config validation.

### `VP8E_SET_RTC_EXTERNAL_RATECTRL`

Status: covered. Priority: low to medium.

libvpx uses this VP8 control to support RTC wrappers that drive external
rate-control decisions. In VP8 itself, enabling the control disables cyclic
refresh, keeps realtime correction-factor updates active, and disables
post-encode overshoot recode/drop behavior.

Current Go API:

```go
// EncoderOptions.RTCExternalRateControl bool
func (e *VP8Encoder) SetRTCExternalRateControl(enabled bool) error
```

Implemented notes:

- Default false has no allocation and only adds checks at existing
  cyclic-refresh, overshoot-drop, and post-encode rate-correction decision
  points.
- Tests pin cyclic-refresh disablement, correction-factor updates when
  `activeWorstQChanged` is set, overshoot-drop disablement, runtime setter
  validation, and zero-allocation runtime control behavior.

## Deferred

### Spatial Resampling And `VP8E_SET_SCALEMODE`

Status: partially covered. Priority: low to medium.

libvpx surfaces this through:

- `vpx_codec_enc_config_set` with a new width / height (caller-driven) —
  **covered** by `VP8Encoder.SetRealtimeTarget` with `Width` / `Height`.
  The encoder rebuilds size-dependent state in place, invalidates the
  LAST / GOLDEN / ALTREF references, and forces the next frame to be a
  key frame at the new size.
- `VP8E_SET_SCALEMODE` — **missing**.
- `rc_resize_allowed` — **missing**.
- `rc_resize_up_thresh` — **missing**.
- `rc_resize_down_thresh` — **missing**.

Why it is still deferred:

- It is a real VP8/libvpx behavior, not VP9-only scaffolding.
- It would scale the source inside the encoder, separate display size
  from coded size, and change keyframe scale bits in the bitstream
  header.
- It requires source scaling, reference scaling, reconstructed-frame
  bookkeeping, and decoder-output expectations beyond the caller-driven
  resize already covered.
- The current public `Image` and `EncodeResult` API assumes the caller
  drives the coded size, which is the contract the implemented
  resolution-change path keeps.

Port the spatial resampler only after deciding the Go API for coded size vs
display size.

### Output Partition Packets

Status: missing. Priority: low.

libvpx init flag:

- `VPX_CODEC_USE_OUTPUT_PARTITION`

This is not the same thing as `VP8E_SET_TOKEN_PARTITIONS`. govpx can encode
multi-token-partition frames, but it always returns one contiguous frame buffer.
libvpx can return one packet per partition with fragment flags.

Possible API if callers need partition packetization:

```go
type EncodedPartition struct {
    Data []byte
    PartitionID int
    Last bool
}
```

### PSNR Packet / Per-Frame PSNR Flag

Status: missing. Priority: low.

libvpx surfaces this through:

- `VPX_CODEC_USE_PSNR`
- `VPX_EFLAG_CALCULATE_PSNR`
- `VPX_CODEC_PSNR_PKT`

govpx has `EncodeResult.PSNRHint`, but it is not equivalent to libvpx PSNR
packets. Applications can also compute PSNR externally from source and decoded
output.

### Input Fragments

Status: missing as a streaming decode mode. Priority: low.

libvpx's decoder can be initialized with `VPX_CODEC_USE_INPUT_FRAGMENTS`.
govpx expects complete VP8 frame payloads at decode time; RTP/WebRTC sequence,
loss, and jitter-buffer policy stay caller-owned. VP8 RTP payload-descriptor
helpers plus MTU-aware packetizers and ordered frame assemblers live in the core
API; a stateful fragment accumulator is optional, not required for
codec-payload decode.

## Probably Skip

| libvpx surface | Reason to skip by default |
| --- | --- |
| `VP8_SET_POSTPROC` on encoder preview | It only affects `vpx_codec_get_preview_frame`; govpx does not expose encoder preview frames, and decoder postproc already exists. |
| `VPXD_SET_DECRYPTOR` / `VP8D_SET_DECRYPTOR` | Decryption is transport/application policy. Prefer decrypting before `Decode`. |
| `g_profile` for VP8 encode | govpx always emits profile/version 0. Nonzero VP8 versions are mostly compatibility/special-path behavior and are rarely desirable for new encodes. Decoder already supports profiles 0..3. |
| `vpx_codec_enc_init_multi` / VP8 multi-resolution encode | This is a libvpx-specific multi-encoder API and not a small VP8 control. |
| VP9-prefixed controls in `vp8cx.h` / `vp8dx.h` | Out of scope for this VP8 control-gap document. |
| `rc_scaled_width` / `rc_scaled_height` | Present in common config but not meaningfully wired for VP8 in `set_vp8e_config`; `VP8E_SET_SCALEMODE` and resize watermarks are the real VP8 path. |
| `rc_firstpass_mb_stats_in` / FPMB stats packets | Present in common headers, but no meaningful VP8 control path in the inspected v1.16.0 implementation. |
| Vizier/external experiment fields in `vpx_codec_enc_cfg_t` | VP8 validates denominators but does not wire them into the VP8 config in v1.16.0. |
| VP9 SVC/spatial-layer fields | Out of scope for VP8 govpx. Existing temporal layers cover the VP8-relevant part. |

## Suggested Port Order

1. Revisit spatial resampling only after deciding the coded-size/display-size
   API contract.
2. Add output-partition packetization only if a caller needs partition packets
   instead of contiguous frame payloads.
3. Add PSNR packets only if callers need libvpx-style per-frame diagnostic
   packets rather than external PSNR tooling.
