# govpx API Draft

This is the intended public API shape for the repo tidy work. It describes the
clean surface that should remain at `github.com/thesyncim/govpx` after the root
package becomes a small wrapper over codec-owned internal packages.

The project is not released yet, so this document names the preferred final
API rather than preserving old spellings or internal staging shapes.

## Grounding And Style

This draft is grounded in the current govpx public surface and the pinned
libvpx v1.16.0 headers under `internal/coracle/build/libvpx-v1.16.0/`.

Checked libvpx sources:

| Source | Used for |
| --- | --- |
| `vpx/vpx_encoder.h` | encoder config fields such as dimensions, threads, timebase, lag, rate control, buffers, quantizers, two-pass, and keyframe intervals |
| `vpx/vp8cx.h` | VP8/VP9 encoder controls such as CPU-used, auto-alt-ref, token partitions, ARNR, CQ, ROI/active maps, VP9 tiles, AQ, SVC, row-MT, TPL, loop filter, and color/render metadata |
| `vpx/vp8dx.h` | decoder controls such as decryptor, VP9 byte alignment, skip loop filter, SVC spatial layer decode, row-MT, and loop-filter optimization |
| `vpx/vpx_decoder.h` and `vpx/vpx_frame_buffer.h` | decoder config, frame iteration lifetime, and external frame-buffer callbacks |

The Go API should use Go names and package-doc style, not C control names. C
names belong in comments only when they clarify libvpx parity. Prefer concise
type names, exported fields with direct behavior, and zero-value behavior that
is documented on the field or type. Avoid compatibility aliases until the first
release.

## Package Shape

The root package is the only public import path:

```go
import "github.com/thesyncim/govpx"
```

Root package files should stay small and user-facing:

| File | Public responsibility |
| --- | --- |
| `codec.go` | Codec identifiers and version/scope constants |
| `errors.go` | Sentinel errors for callers to compare with `errors.Is` |
| `image.go` | Caller-visible planar image/buffer types |
| `options.go` | Shared option structs embedded by codec-specific options |
| `vp8.go` | VP8 encoder and decoder facade |
| `vp9.go` | VP9 encoder and decoder facade |
| `rtp.go` | Shared RTP fragment/result types and public RTP contracts |

Codec algorithms, bitstream syntax, DSP kernels, probability models, oracle
harnesses, and diagnostics should live under `internal/`.

## Shared Types

The following option groups are candidate final names. Each field must be
backed by either the current govpx API or a checked libvpx config/control before
any code change lands. Use explicit shared option groups only when they
describe real cross-codec mechanics:

```go
type VideoOptions struct {
    Width  int
    Height int
}

type TimebaseOptions struct {
    FPS         int
    TimebaseNum int
    TimebaseDen int
}

type ThreadOptions struct {
    Threads int
}

type RateControlOptions struct {
    Mode              RateControlMode
    TargetBitrateKbps int
    MinBitrateKbps    int
    MaxBitrateKbps    int
    MinQuantizer      int
    MaxQuantizer      int
    CQLevel           int
    BufferSizeMs        int
    BufferInitialSizeMs int
    BufferOptimalSizeMs int
}

type RealtimeOptions struct {
    Deadline Deadline
    CpuUsed  int
    DropFrameAllowed   bool
    DropFrameWaterMark int
    MaxIntraBitratePct int
}

type PostProcessOptions struct {
    Flags      PostProcessFlag
    NoiseLevel int
}
```

Codec-specific options embed these groups and add only codec-specific fields.
Do not force VP8 and VP9 fields into a shared struct when the behavior differs
or when the field only exists to mirror one codec's libvpx control.

Current libvpx-backed option mapping:

| govpx concept | libvpx source |
| --- | --- |
| width, height | `vpx_codec_enc_cfg.g_w`, `g_h`; decoder config `w`, `h` |
| threads | `vpx_codec_enc_cfg.g_threads`; decoder config `threads` |
| FPS/timebase | `vpx_codec_enc_cfg.g_timebase` |
| lookahead | `vpx_codec_enc_cfg.g_lag_in_frames` |
| error resilience | `vpx_codec_enc_cfg.g_error_resilient` |
| rate-control mode | `vpx_codec_enc_cfg.rc_end_usage` |
| target bitrate | `vpx_codec_enc_cfg.rc_target_bitrate` |
| min/max quantizer | `vpx_codec_enc_cfg.rc_min_quantizer`, `rc_max_quantizer` |
| undershoot/overshoot | `vpx_codec_enc_cfg.rc_undershoot_pct`, `rc_overshoot_pct` |
| buffer model | `vpx_codec_enc_cfg.rc_buf_sz`, `rc_buf_initial_sz`, `rc_buf_optimal_sz` |
| two-pass VBR | `rc_twopass_stats_in`, `rc_2pass_vbr_*` fields |
| keyframe interval | `kf_min_dist`, `kf_max_dist`, `kf_mode` |
| VP8 token partitions | `VP8E_SET_TOKEN_PARTITIONS` |
| VP8/VP9 ARNR | `VP8E_SET_ARNR_*` and govpx's VP9 ARNR implementation backed by VP9 alt-ref paths |
| VP9 tile/row controls | `VP9E_SET_TILE_COLUMNS`, `VP9E_SET_TILE_ROWS`, `VP9E_SET_ROW_MT`, `VP9D_SET_ROW_MT` |
| VP9 SVC | `VP9E_SET_SVC*`, `VP9_DECODE_SVC_SPATIAL_LAYER` |
| VP9 color/render metadata | `VP9E_SET_COLOR_SPACE`, `VP9E_SET_COLOR_RANGE`, `VP9E_SET_RENDER_SIZE` |
| VP9 loop-filter controls | `VP9E_SET_DISABLE_LOOPFILTER`, `VP9_SET_SKIP_LOOP_FILTER`, `VP9D_SET_LOOP_FILTER_OPT` |

## Common Encode Path

VP8 and VP9 should share the same method meaning:

- `EncodeInto` writes encoded output into a caller-owned byte buffer.
- `FlushInto` drains delayed output into a caller-owned byte buffer.
- Result `Data` slices alias the caller-owned byte buffer.
- `ErrFrameNotReady` means the encoder accepted input but lookahead or
  delayed output has not emitted a packet yet.

```go
enc, err := govpx.NewVP8Encoder(govpx.VP8EncoderOptions{
    VideoOptions: govpx.VideoOptions{Width: 1280, Height: 720},
    TimebaseOptions: govpx.TimebaseOptions{FPS: 30},
    RateControlOptions: govpx.RateControlOptions{
        Mode:              govpx.RateControlCBR,
        TargetBitrateKbps: 2500,
        MinQuantizer:      2,
        MaxQuantizer:      56,
    },
    RealtimeOptions: govpx.RealtimeOptions{
        Deadline:         govpx.DeadlineRealtime,
        CpuUsed:          -6,
        DropFrameAllowed: true,
    },
})
if err != nil {
    return err
}
defer enc.Close()

packet := make([]byte, 256*1024)
res, err := enc.EncodeInto(packet, src, pts, duration, 0)
if err != nil {
    return err
}
if !res.Dropped {
    send(res.Data)
}
```

VP9 uses the same caller-owned-buffer shape and returns VP9 metadata:

```go
enc, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
    VideoOptions: govpx.VideoOptions{Width: 1280, Height: 720},
    TimebaseOptions: govpx.TimebaseOptions{FPS: 30},
    RateControlOptions: govpx.RateControlOptions{
        Mode:              govpx.RateControlCBR,
        TargetBitrateKbps: 2500,
    },
    RealtimeOptions: govpx.RealtimeOptions{
        Deadline: govpx.DeadlineRealtime,
        CpuUsed:  8,
    },
    RowMT: true,
})
if err != nil {
    return err
}
defer enc.Close()

res, err := enc.EncodeInto(packet, src, pts, duration, 0)
if err != nil {
    return err
}
_ = res.FrameInfo
```

Keep allocation-returning encode helpers out of the preferred API. Tests and
examples should use `EncodeInto`.

## Common Decode Path

Decoders support both convenience decode and caller-owned decode:

- `Decode` consumes a packet and publishes decoder-owned output through
  `NextFrame`.
- `DecodeInto` consumes a packet and writes the visible frame into
  caller-owned `Image` buffers.
- `DecodeIntoWithPTS` is the timestamp-carrying form.
- `LastFrameInfo` returns the most recent visible frame metadata where the
  codec can provide it.

```go
dec, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
    ThreadOptions: govpx.ThreadOptions{Threads: 4},
})
if err != nil {
    return err
}
defer dec.Close()

info, err := dec.DecodeInto(packet, &dst)
if err != nil {
    return err
}
_ = info
```

`NextFrame` returns decoder-owned storage that remains valid only until the
next `Decode`, `Reset`, or `Close`.

## VP8 Surface

VP8 keeps explicit codec names:

```go
type VP8EncoderOptions struct {
    VideoOptions
    TimebaseOptions
    ThreadOptions
    RateControlOptions
    RealtimeOptions

    TemporalScalability TemporalScalabilityConfig
    TokenPartitions int
    ErrorResilient bool
    ErrorResilientPartitions bool
    Sharpness int
    NoiseSensitivity int
    ARNRMaxFrames int
    ARNRStrength int
    ARNRType int
    LookaheadFrames int
    AutoAltRef bool
    AdaptiveKeyFrames bool
    TwoPassStats []FirstPassFrameStats
    ScreenContentMode int
    StaticThreshold int
}

type VP8DecoderOptions struct {
    ThreadOptions
    PostProcessOptions

    ErrorConcealment bool
    MaxWidth int
    MaxHeight int
    Decryptor VP8Decryptor
    DecryptorState any
    RejectResolutionChange bool
}
```

VP8-specific controls remain codec-specific: token partitions, temporal
layering, VP8 denoise/ARNR controls, VP8 active maps, ROI maps, and VP8
reference buffer controls.

## VP9 Surface

VP9 keeps codec-specific controls visible where they matter:

```go
type VP9EncoderOptions struct {
    VideoOptions
    TimebaseOptions
    ThreadOptions
    RateControlOptions
    RealtimeOptions

    RowMT bool
    Log2TileRows int8
    FrameParallelDecoding bool
    FrameParallelDecodingSet bool
    TemporalScalability TemporalScalabilityConfig
    SpatialScalability VP9SpatialScalabilityConfig
    Lossless bool
    AQMode VP9AQMode
    ColorSpace VP9ColorSpace
    ColorRange VP9ColorRange
    RenderWidth int
    RenderHeight int
    Segmentation VP9SegmentationOptions
    TwoPassStats []VP9FirstPassFrameStats
    EnableTPL bool
    EnableKeyFrameFiltering bool
}

type VP9DecoderOptions struct {
    ThreadOptions
    PostProcessOptions

    ByteAlignment int
    ErrorConcealment bool
    MaxWidth int
    MaxHeight int
    Decryptor VP9Decryptor
    DecryptorState any
    RejectResolutionChange bool
    GetFrameBuffer VP9GetFrameBufferFunc
    ReleaseFrameBuffer VP9ReleaseFrameBufferFunc
    SVCSpatialLayerSet bool
    SVCSpatialLayer uint8
    DecodeTileRowSet bool
    DecodeTileRow int
    DecodeTileColSet bool
    DecodeTileCol int
    DecoderRowMT bool
    DecoderLoopFilterOpt bool
    SkipLoopFilter bool
    InvertTileDecodeOrder bool
}
```

VP9-specific API remains explicit: superframes, spatial SVC, tile filters,
row-MT controls, external frame buffers, color/render metadata, segmentation,
lossless, AQ modes, TPL, frame parallel decode signaling, show-existing frames,
and intra-only packets.

## RTP Surface

RTP helpers remain public because packetizers and WebRTC integrations need
payload descriptor control. Shared mechanical types stay in root:

```go
type RTPPayloadFragment struct {
    Payload []byte
    Marker  bool
}
```

VP8 and VP9 descriptor types stay codec-specific:

```go
type VP8RTPPayloadDescriptor struct { ... }
type VP9RTPPayloadDescriptor struct { ... }
```

The public contract is:

- descriptor parsing and packing are codec-specific;
- packetization takes one encoded frame and an MTU and returns ordered payload
  fragments plus marker-bit state;
- assembly consumes ordered payload fragments and marker bits and returns one
  encoded frame;
- RTP headers, sequence/loss policy, jitter buffering, SRTP, SDP, and signaling
  stay caller-owned.

## Errors

Public sentinel errors live in root. Codec-specific internals should translate
to these sentinels before crossing the facade:

| Error | Meaning |
| --- | --- |
| `ErrInvalidData` | Malformed VP8 data or VP8 RTP payload |
| `ErrInvalidVP9Data` | Malformed VP9 data or VP9 RTP payload |
| `ErrVP9NotImplemented` | Valid VP9 packet outside Profile 0 scope |
| `ErrNeedKeyFrame` | Inter frame before reference state is initialized |
| `ErrFrameNotReady` | Delayed encoder accepted input without output |
| `ErrBufferTooSmall` | Caller-owned output buffer cannot hold data |
| `ErrFrameRejected` | Decoder rejected a frame due to configured limits |
| `ErrInvalidConfig` | Invalid options, image shape, or runtime control |
| `ErrInvalidBitrate` | Bitrate or buffer-model value outside range |
| `ErrInvalidQuantizer` | Public quantizer outside range |
| `ErrClosed` | Use after close or nil codec handle |

## Documentation Contract

`README.md` should stay short: install, quick decode, quick encode, RTP/WebRTC
summary, and links. Detailed behavior belongs in:

- `docs/api.md` for public API examples and option families;
- `docs/architecture.md` for package ownership and data flow;
- `docs/codec-status.md` for VP8/VP9 scope and unsupported features;
- `docs/validation.md` for local, CI, oracle, fuzz, and performance gates;
- `docs/migration.md` for internal cleanup coordination.
