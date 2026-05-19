# govpx Internal Migration Map

This document is for repo cleanup coordination before the first release. It is
not an external compatibility promise. Do not add deprecated aliases or
compatibility wrappers from this map unless a temporary same-wave move cannot
compile without them, and remove temporary aliases before the wave ends.

## Sources Checked

API moves must stay grounded in the current implementation and the pinned
libvpx v1.16.0 baseline.

Checked local libvpx files:

- `internal/coracle/build/libvpx-v1.16.0/vpx/vpx_encoder.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8cx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vp8dx.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vpx_decoder.h`
- `internal/coracle/build/libvpx-v1.16.0/vpx/vpx_frame_buffer.h`

Current govpx public docs checked:

- `README.md`
- `doc.go`
- `examples/webrtc-vp8`
- `examples/webrtc-vp9`
- `docs/vp8_libvpx_control_gap.md`
- `docs/vp8_controls_audit.md`
- `docs/vp9_parity_guidelines.md`

## Naming Policy

Use Go-style names in the public API. Keep libvpx C names in comments only
when the mapping is useful for parity review.

Preferred style:

- Constructors are explicit: `NewVP8Encoder`, `NewVP9Encoder`,
  `NewVP8Decoder`, `NewVP9Decoder`.
- Option structs are nouns ending in `Options`.
- Shared embedded groups may use `Options` when they configure behavior:
  `VideoOptions`, `TimebaseOptions`, `ThreadOptions`,
  `RateControlOptions`, `RealtimeOptions`, `PostProcessOptions`.
- Method names describe ownership: `EncodeInto`, `DecodeInto`, and
  `FlushInto` always write into caller-owned buffers.
- Codec-specific concepts keep codec names: VP9 superframes, VP9 SVC, VP9
  tiles, VP8 token partitions.

Avoid:

- C control names as exported Go identifiers unless already established and
  clearly better than a Go name.
- Compatibility aliases for unreleased public names.
- Public types that only exist for oracle tracing, scoreboards, or diagnostic
  tests.

## Root Facade Plan

Files that should stay root public API:

| File | Final role |
| --- | --- |
| `codec.go` | Codec identifiers and version constants |
| `errors.go` | Sentinel errors |
| `image.go` | Public image/buffer types |
| `rtp.go` | Shared RTP fragment/result types |
| `options.go` | Shared option groups after Wave 5 |
| `vp8.go` | VP8 encoder/decoder public facade |
| `vp9.go` | VP9 encoder/decoder public facade |
| `doc.go` | Short package guide in Go doc style |

Files that should become root adapters or be merged into adapter files:

| Current file | Adapter role |
| --- | --- |
| `encoder.go` | `VP8Encoder` public handle and methods forwarding to `internal/vp8/encoder` |
| `decoder.go` | `VP8Decoder` public handle and methods forwarding to `internal/vp8/decoder` |
| `encoder_config.go` | Public VP8 options only; normalized config moves internal |
| `encoder_firstpass.go` | Public VP8 first-pass stats/result surface only |
| `encoder_roi.go` | Public ROI map value only if both VP8 and VP9 keep using it |
| `vp9_encoder.go` | `VP9Encoder` public handle and methods after same-package split |
| `vp9_encoder_config.go` | Public VP9 options only; normalized config moves internal |
| `vp9_decoder.go` | `VP9Decoder` public handle and methods after same-package split |
| `vp9_firstpass.go` | Public VP9 first-pass stats/result surface only |
| `vp8_rtp.go` | Public VP8 RTP wrappers over `internal/vp8/rtp` |
| `vp9_rtp.go`, `vp9_superframe.go` | Public VP9 RTP/superframe wrappers over `internal/vp9/rtp` |

Files that should move internal:

| Current file family | Target |
| --- | --- |
| root `encoder_*.go` implementation files | `internal/vp8/encoder` |
| root `ratecontrol_*.go` VP8 implementation files | `internal/vp8/encoder` or `internal/vpx/ratecontrol` for mechanical value helpers |
| root `decoder*.go` private decode state | `internal/vp8/decoder` |
| root `vp9_*` encoder/AQ/TPL/rate-control/partition/search files | `internal/vp9/encoder` |
| root `vp9_decoder*.go` private decode state | `internal/vp9/decoder` |
| oracle trace/probe/debug files | tagged package-local oracle suites or `internal/vpx/testharness` |

## Public Name Map

These are intended rename or ownership decisions. Apply them only in the API
cleanup wave unless a package move requires a temporary internal staging name.

| Current name | Intended final shape | Notes |
| --- | --- | --- |
| `EncoderOptions` | `VP8EncoderOptions` | Current unqualified name is VP8-specific. |
| `DecoderOptions` | `VP8DecoderOptions` | Current unqualified name is VP8-specific. |
| `VP9EncoderOptions` | keep | Embed shared option groups after Wave 5. |
| `VP9DecoderOptions` | keep | Embed shared option groups after Wave 5. |
| `RateControlConfig` | `RateControlOptions` or internal normalized config | Keep public config small; move derived state internal. |
| `RealtimeTarget` | keep if runtime-control API stays | It is a runtime update, not constructor config. |
| `EncodeResult` | keep for VP8; consider shared result only if fields truly match | Do not hide VP9 metadata differences. |
| `VP9EncodeResult` | keep | VP9 carries codec-specific layer/frame metadata. |
| `FirstPassFrameStats` | keep VP8-specific or rename `VP8FirstPassFrameStats` | Choose explicitness in Wave 5. |
| `VP9FirstPassFrameStats` | keep | Already explicit. |
| `VP9ComputeARFBoost`, `VP9DefaultARFBoostParams` | move internal | Parity/model helpers, not normal API. |
| `VP9AdjustARNRFilter` and related structs | move internal unless documented as public analysis API | Currently implementation/parity surface. |
| `SpeedFeatures` and related VP9 speed-feature enums | move internal | Libvpx tuning internals should not be root public API. |
| `VP9TPLFrameDelta` | move internal unless a public consumer is documented | Current reason is row-MT/oracle plumbing. |
| `ProbeVP9SearchFilterRefFires`, `ResetVP9SearchFilterRefProbes` | move out of public docs; keep tagged test-only if needed | Oracle trace only. |
| `ErrVP9EncoderNotImplemented` | remove | Deprecated staging sentinel. |
| `DecoderOptions.ErrorResilient` | remove alias; use `ErrorConcealment` where that is the actual behavior | Current comments call it compatibility alias. |
| `DecoderOptions.PostProcess` | remove alias; use `PostProcessOptions`/flags | Avoid two ways to ask for the same postprocess path. |
| `VP9DecoderOptions.ErrorResilient` | remove alias; use `ErrorConcealment` | Same as VP8. |
| `VP9DecoderOptions.PostProcess` | remove alias; use `PostProcessOptions`/flags | Same as VP8. |

## Method Map

| Current shape | Intended shape | Notes |
| --- | --- | --- |
| `VP8Encoder.EncodeInto(dst, src, pts, duration, flags)` | keep meaning | Caller-owned output buffer. |
| `VP9Encoder.EncodeInto(img, dst)` | change argument order and metadata shape in Wave 5 | Align with caller-owned-buffer convention. |
| `VP9Encoder.Encode`, `EncodeWithFlags` | remove from preferred docs; delete or keep internal tests only during transition | Allocation helpers are not the core API. |
| `VP9Encoder.EncodeIntoWithResult` | fold into `EncodeInto` result | `EncodeInto` should return metadata consistently. |
| `VP8Encoder.FlushInto(dst)` | keep meaning | Caller-owned output buffer. |
| `VP9Encoder.FlushInto(dst)` and `FlushIntoWithResult(dst)` | unify to result-returning `FlushInto` | Avoid two flush spellings. |
| `Decode`, `DecodeWithPTS`, `NextFrame` | keep | Matches libvpx decode/get-frame lifetime shape and Go-style iterator state. |
| `DecodeInto`, `DecodeIntoWithPTS` | keep meaning | Caller-owned image buffers. |
| `LastFrameInfo` | keep where meaningful | Prefer one metadata method family. |

## Libvpx Control Map

Use this map when reviewing API fields or moving control code.

| govpx concept | libvpx field/control |
| --- | --- |
| dimensions | `vpx_codec_enc_cfg.g_w`, `g_h`; decoder config `w`, `h` |
| threads | `vpx_codec_enc_cfg.g_threads`; decoder config `threads` |
| timebase | `vpx_codec_enc_cfg.g_timebase` |
| error resilience | `vpx_codec_enc_cfg.g_error_resilient` |
| lookahead | `vpx_codec_enc_cfg.g_lag_in_frames` |
| frame dropping | `vpx_codec_enc_cfg.rc_dropframe_thresh` |
| rate-control mode | `vpx_codec_enc_cfg.rc_end_usage` |
| two-pass stats | `vpx_codec_enc_cfg.rc_twopass_stats_in` |
| target bitrate | `vpx_codec_enc_cfg.rc_target_bitrate` |
| quantizer range | `vpx_codec_enc_cfg.rc_min_quantizer`, `rc_max_quantizer` |
| undershoot/overshoot | `vpx_codec_enc_cfg.rc_undershoot_pct`, `rc_overshoot_pct` |
| buffer model | `vpx_codec_enc_cfg.rc_buf_sz`, `rc_buf_initial_sz`, `rc_buf_optimal_sz` |
| two-pass VBR controls | `rc_2pass_vbr_bias_pct`, `rc_2pass_vbr_minsection_pct`, `rc_2pass_vbr_maxsection_pct`, `rc_2pass_vbr_corpus_complexity` |
| keyframe interval | `kf_mode`, `kf_min_dist`, `kf_max_dist` |
| active/ROI maps | `VP8E_SET_ACTIVEMAP`, `VP8E_SET_ROI_MAP`, `VP9E_GET_ACTIVEMAP`, `VP9E_SET_ROI_MAP` |
| CPU-used | `VP8E_SET_CPUUSED` |
| auto alt-ref | `VP8E_SET_ENABLEAUTOALTREF`; VP9 implementation mirrors VP9 encoder behavior |
| VP8 token partitions | `VP8E_SET_TOKEN_PARTITIONS` |
| CQ level | `VP8E_SET_CQ_LEVEL`; VP9 public CQ maps through VP9 quantizer selection |
| VP8/VP9 max intra/inter bitrate | `VP8E_SET_MAX_INTRA_BITRATE_PCT`, `VP9E_SET_MAX_INTER_BITRATE_PCT` |
| temporal layer ID | `VP8E_SET_TEMPORAL_LAYER_ID`; VP9 SVC layer ID uses `VP9E_SET_SVC_LAYER_ID` |
| VP9 tile columns/rows | `VP9E_SET_TILE_COLUMNS`, `VP9E_SET_TILE_ROWS` |
| VP9 frame-parallel decode bit | `VP9E_SET_FRAME_PARALLEL_DECODING` |
| VP9 AQ/content tuning | `VP9E_SET_AQ_MODE`, `VP9E_SET_TUNE_CONTENT` |
| VP9 lossless | `VP9E_SET_LOSSLESS` |
| VP9 color/render | `VP9E_SET_COLOR_SPACE`, `VP9E_SET_COLOR_RANGE`, `VP9E_SET_RENDER_SIZE` |
| VP9 row-MT | `VP9E_SET_ROW_MT`, `VP9D_SET_ROW_MT` |
| VP9 TPL/keyframe filtering | `VP9E_SET_TPL`, `VP9E_SET_KEY_FRAME_FILTERING` |
| VP9 post-encode drop and CBR overshoot | `VP9E_SET_POSTENCODE_DROP`, `VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR` |
| VP9 loop filter | `VP9E_SET_DISABLE_LOOPFILTER`, `VP9_SET_SKIP_LOOP_FILTER`, `VP9D_SET_LOOP_FILTER_OPT` |
| decryptor | `VPXD_SET_DECRYPTOR` / `VP8D_SET_DECRYPTOR` |
| VP9 byte alignment | `VP9_SET_BYTE_ALIGNMENT` |
| VP9 SVC decode | `VP9_DECODE_SVC_SPATIAL_LAYER` |
| external frame buffers | `vpx_codec_set_frame_buffer_functions` |

## Documentation Migration

Keep user docs current and short. Move parity engineering detail out of public
paths instead of copying it into the README.

| Current doc | Action |
| --- | --- |
| `README.md` | Keep install, quick encode/decode, RTP/WebRTC summary, and links only. |
| `doc.go` | Rewrite as Go package docs: package purpose, scope, basic examples, errors, build tags. |
| `docs/api.md` | User API guide after final names are chosen. |
| `docs/architecture.md` | Package ownership and data flow. |
| `docs/codec-status.md` | VP8/VP9 feature support and unsupported features. |
| `docs/validation.md` | Local, CI, oracle, fuzz, and performance commands. |
| `docs/vp8_controls_audit.md` | Fold stable facts into `docs/codec-status.md` or `docs/validation.md`; archive parity notes if still useful. |
| `docs/vp8_libvpx_control_gap.md` | Fold verified control map into `docs/codec-status.md` and this migration map. |
| `docs/vp9_parity_guidelines.md` | Keep as parity engineering note or fold into `docs/validation.md`; do not duplicate in README. |
| realtime perf-gap docs | Keep as dated engineering notes or move under a documented diagnostics/perf area. |
| `UPSTREAM.md` | Keep authoritative for pinned libvpx version and scope. |

Before landing docs in later waves, run enough code/doc checks to prevent stale
examples:

- `go test ./... -count=1` for package examples and default tests.
- `go test ./examples/webrtc-vp8 ./examples/webrtc-vp9 -count=1` from their
  module roots when examples change.
- `go doc github.com/thesyncim/govpx` after package docs change.

## Review Rules For Moves

- Do file splits before package moves for files above 2,500 lines.
- Keep behavior changes out of move commits.
- Keep libvpx-derived behavior comments tied to the checked source file or
  control name.
- Do not update parity baselines for structural changes.
- If a doc mentions a public name, update or remove it in the same wave that
  renames the API.
