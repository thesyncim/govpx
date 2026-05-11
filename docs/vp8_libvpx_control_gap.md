# VP8 libvpx Control Gap

Reference target: libvpx v1.16.0, matching `UpstreamLibvpxVersion`.

Primary sources:

- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8cx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8dx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vpx_encoder.h`
- `internal/coracle/build/libvpx-v1.16.0/vp8/vp8_cx_iface.c`
- `internal/coracle/build/libvpx-v1.16.0/vp8/vp8_dx_iface.c`

This document tracks VP8-relevant libvpx controls that are not exposed by
govpx yet, with an opinionated "sane to port" filter. It is not a plan to
recreate the libvpx C ABI. The intended shape is still small Go APIs that map
cleanly onto libvpx behavior where that behavior is useful.

## Summary

The highest-value missing controls are:

1. `VP8E_SET_ROI_MAP`
2. `VP8_SET_REFERENCE` / `VP8_COPY_REFERENCE` on encoder and decoder
3. Decoder metadata getters for last reference updates/uses and last quantizer
4. `VP8E_SET_TUNING` for PSNR vs SSIM tuning
5. VPX_Q constant-quality mode
6. Optional: spatial resampling / scale mode, output-partition packetization,
   PSNR packets, and `VP8E_SET_RTC_EXTERNAL_RATECTRL`

The controls that are probably not worth porting by default are encoder
preview postproc, decryptor callbacks, VP8 nonzero profiles for encode,
multi-resolution encode, VP9-only controls, and header-only fields that libvpx
does not actually wire into the VP8 encoder.

## Already Covered

| libvpx control or config | govpx surface | Notes |
| --- | --- | --- |
| `VP8E_SET_CPUUSED` | `EncoderOptions.CpuUsed`, `SetCPUUsed` | Includes VP8 `-16..16` range and realtime auto-speed behavior. |
| Encode deadline | `EncoderOptions.Deadline`, `SetDeadline` | Maps best/good/realtime behavior. |
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
| `VP8E_SET_SCREEN_CONTENT_MODE` | `EncoderOptions.ScreenContentMode`, `SetScreenContentMode` | Modes 0..2. |
| `VP8_SET_POSTPROC` on decoder | `DecoderOptions.PostProcess*` | Decoder postproc is exposed as Go flags. |
| `VP8D_GET_FRAME_CORRUPTED` | `FrameInfo.Corrupted` | Returned from decode paths, though not as a separate method. |
| Encoder common config: width, height, timebase, threads, bitrate, VBR/CBR/CQ, q range, buffer model, frame drop, lag/lookahead, two-pass, keyframe interval, temporal layers, error resilience | `EncoderOptions`, `RateControlConfig`, `TemporalScalabilityConfig` | Mostly covered with Go-style names. |

## Sane Missing Controls

### `VP8E_SET_ROI_MAP`

Status: missing. Priority: high.

What libvpx exposes:

- A per-macroblock segment map.
- Four VP8 segment IDs.
- Per-segment public quantizer deltas in `[-63, 63]`.
- Per-segment loop-filter deltas in `[-63, 63]`.
- Per-segment static breakout thresholds.
- Passing nil/no deltas disables segmentation.
- Enabling ROI disables cyclic refresh in libvpx.

Why it is sane:

- It is a real VP8 feature, not C API scaffolding.
- It is useful for screen sharing, active speaker regions, and ROI quality
  shaping.
- govpx already has segmentation machinery for cyclic refresh and alt-LF
  paths, so the port is mostly a public control plus plumbing and precedence.

Suggested Go API:

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

Port notes:

- Translate public q deltas through the same public-q to qindex table libvpx
  uses.
- Reject wrong macroblock dimensions.
- Define precedence against cyclic refresh explicitly. Matching libvpx means
  ROI turns cyclic refresh off while active.
- Gate with oracle tests for bitstream segmentation headers, per-MB segment
  IDs, q deltas, LF deltas, and static-threshold behavior.

### Reference Set/Copy

Status: missing. Priority: high for recovery/oracle/debug users, medium for
normal encode/decode.

libvpx controls:

- `VP8_SET_REFERENCE`
- `VP8_COPY_REFERENCE`

These controls are available on both the VP8 encoder and decoder interfaces.
They set or copy `LAST`, `GOLDEN`, or `ALTREF` reference buffers.

Why it is sane:

- External reference repair and recovery are legitimate VP8 workflows.
- It helps WebRTC-style applications and diagnostics.
- govpx already owns explicit `lastRef`, `goldenRef`, and `altRef` buffers.

Suggested Go API:

```go
type ReferenceFrame int

const (
    ReferenceLast ReferenceFrame = iota + 1
    ReferenceGolden
    ReferenceAltRef
)

func (e *VP8Encoder) SetReferenceFrame(ref ReferenceFrame, src Image) error
func (e *VP8Encoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error
func (d *VP8Decoder) SetReferenceFrame(ref ReferenceFrame, src Image) error
func (d *VP8Decoder) CopyReferenceFrame(ref ReferenceFrame, dst *Image) error
```

Port notes:

- Extend borders after setting a reference.
- On encoder reference replacement, invalidate or update state that assumes
  reference identity: alias flags, reference frame numbers, last-frame inter
  modes, source alt-ref lifecycle, and denoiser reference averages.
- Decoder set-reference should require an initialized decoder size, or accept
  the first set as initialization only if the semantics are documented.

### Decoder Last-Frame Metadata

Status: partially missing. Priority: medium.

libvpx controls:

- `VP8D_GET_LAST_REF_UPDATES`
- `VP8D_GET_LAST_REF_USED`
- `VPXD_GET_LAST_QUANTIZER`
- `VP8D_GET_FRAME_CORRUPTED`

govpx already returns corruption state through `FrameInfo.Corrupted`. The
remaining metadata is not exposed.

Why it is sane:

- This is cheap API surface.
- It helps RTP/WebRTC bookkeeping, diagnostics, and oracle parity.

Suggested Go shape:

```go
type ReferenceFlags uint8

const (
    ReferenceFlagLast ReferenceFlags = 1 << iota
    ReferenceFlagGolden
    ReferenceFlagAltRef
)

type FrameInfo struct {
    ...
    Quantizer int
    InternalQuantizer int
    RefUpdates ReferenceFlags
    RefUsed ReferenceFlags
}
```

Port notes:

- `RefUpdates` can come from the parsed refresh header.
- `RefUsed` can be derived while decoding inter modes.
- `InternalQuantizer` is the VP8 base qindex; `Quantizer` can use govpx's
  public 0..63 conversion.

### Encoder Last-Quantizer Getters

Status: partially covered. Priority: low.

libvpx controls:

- `VP8E_GET_LAST_QUANTIZER`
- `VP8E_GET_LAST_QUANTIZER_64`

govpx returns the public 0..63 quantizer in `EncodeResult.Quantizer`, but it
does not expose a standalone getter or the internal qindex of the last encoded
frame.

Suggested Go API:

```go
func (e *VP8Encoder) LastQuantizer() (public int, internal int, ok bool)
```

Alternatively add `InternalQuantizer` to `EncodeResult`.

### `VP8E_SET_TUNING`

Status: missing. Priority: medium.

What libvpx exposes:

- `VP8_TUNE_PSNR`
- `VP8_TUNE_SSIM`

Why it is sane:

- It is a real quality/decision knob.
- It affects RD behavior through activity masking and is visible in quality
  parity work.

Suggested Go API:

```go
type Tuning int

const (
    TunePSNR Tuning = iota
    TuneSSIM
)

// EncoderOptions.Tuning Tuning
func (e *VP8Encoder) SetTuning(t Tuning) error
```

Port notes:

- Default to PSNR to preserve existing behavior.
- SSIM tuning requires the libvpx activity-masking path around inter/key mode
  decision.
- Add oracle trace coverage for RD multiplier/activity mask deltas before
  broad quality gates.

### VPX_Q Constant-Quality Mode

Status: missing. Priority: low to medium.

libvpx has four rate-control modes: `VPX_VBR`, `VPX_CBR`, `VPX_CQ`, and
`VPX_Q`. govpx currently exposes VBR, CBR, and CQ.

Why it is sane:

- This is part of the common encoder config and VP8 implements it.
- It may be useful for callers that want the closest libvpx-style
  constant-quality behavior rather than the current constrained-quality floor.

Suggested Go API:

```go
const RateControlQ RateControlMode = ...
```

Port notes:

- First pin empirical behavior. libvpx maps `VPX_Q` to
  `USAGE_CONSTANT_QUALITY`, but the public VP8 config path still initializes
  `fixed_q` to `-1`, so do not assume this is a literal fixed-q mode without
  oracle tests.
- Make the relationship to `CQLevel`, min/max q, and target bitrate explicit.

## Maybe Sane, But Larger Or Niche

### Spatial Resampling And `VP8E_SET_SCALEMODE`

Status: missing. Priority: low to medium.

libvpx surfaces this through:

- `VP8E_SET_SCALEMODE`
- `rc_resize_allowed`
- `rc_resize_up_thresh`
- `rc_resize_down_thresh`

Why it may be sane:

- It is useful for low-bitrate realtime streams.
- It is a real VP8/libvpx behavior, not VP9-only scaffolding.

Why it is not a first pass:

- It changes coded dimensions and keyframe scale bits.
- It requires source scaling, reference scaling, reconstructed-frame
  bookkeeping, and decoder-output expectations.
- The current public `Image` and `EncodeResult` API assumes a stable encode
  size.

Port only after deciding the Go API for coded size vs display size.

### Output Partition Packets

Status: missing. Priority: low.

libvpx init flag:

- `VPX_CODEC_USE_OUTPUT_PARTITION`

This is not the same thing as `VP8E_SET_TOKEN_PARTITIONS`. govpx can encode
multi-token-partition frames, but it always returns one contiguous frame buffer.
libvpx can return one packet per partition with fragment flags.

Sane API only if callers need partition packetization:

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
packets. This is useful for diagnostics, but applications can also compute
PSNR externally from source and decoded output.

### `VP8E_SET_RTC_EXTERNAL_RATECTRL`

Status: missing. Priority: low unless matching libvpx RTC external rate-control
behavior is a target.

libvpx uses this to disable cyclic refresh and tweak realtime rate-control
behavior so its VP8 RTC rate-control wrapper can drive the encoder.

Potential API:

```go
// EncoderOptions.RTCExternalRateControl bool
func (e *VP8Encoder) SetRTCExternalRateControl(enabled bool) error
```

Only port with tests that pin cyclic-refresh disablement, correction-factor
updates, and overshoot-drop behavior.

### Input Fragments

Status: missing as a streaming decode mode. Priority: low.

libvpx's decoder can be initialized with `VPX_CODEC_USE_INPUT_FRAGMENTS`.
govpx expects complete VP8 frames. A fragment accumulator could be useful for
some RTP/container users, but it is transport framing rather than core VP8
decode.

## Probably Skip

| libvpx surface | Reason to skip by default |
| --- | --- |
| `VP8_SET_POSTPROC` on encoder preview | It only affects `vpx_codec_get_preview_frame`; govpx does not expose encoder preview frames, and decoder postproc already exists. |
| `VPXD_SET_DECRYPTOR` / `VP8D_SET_DECRYPTOR` | Decryption is transport/application policy. Prefer decrypting before `Decode`. |
| `g_profile` for VP8 encode | govpx always emits profile/version 0. Nonzero VP8 versions are mostly compatibility/special-path behavior and are rarely desirable for new encodes. Decoder already supports profiles 0..3. |
| `vpx_codec_enc_init_multi` / VP8 multi-resolution encode | This is a libvpx-specific multi-encoder API and not a small VP8 control. |
| VP9-prefixed controls in `vp8cx.h` / `vp8dx.h` | Out of scope. govpx is VP8-only. |
| `rc_scaled_width` / `rc_scaled_height` | Present in common config but not meaningfully wired for VP8 in `set_vp8e_config`; `VP8E_SET_SCALEMODE` and resize watermarks are the real VP8 path. |
| `rc_firstpass_mb_stats_in` / FPMB stats packets | Present in common headers, but no meaningful VP8 control path in the inspected v1.16.0 implementation. |
| Vizier/external experiment fields in `vpx_codec_enc_cfg_t` | VP8 validates denominators but does not wire them into the VP8 config in v1.16.0. |
| VP9 SVC/spatial-layer fields | Out of scope for VP8 govpx. Existing temporal layers cover the VP8-relevant part. |

## Suggested Port Order

1. Add low-risk metadata first: `FrameInfo` ref flags and quantizer, plus
   `LastQuantizer` on the encoder.
2. Add reference set/copy on decoder, then encoder. Keep the encoder state
   invalidation explicit and heavily tested.
3. Add ROI map, using existing segmentation machinery and oracle tests.
4. Add `TuneSSIM` only after tracing the libvpx activity-masking path.
5. Add `RateControlQ` if constant-quality callers need it.
6. Revisit spatial resampling only after deciding the coded-size/display-size
   API contract.
