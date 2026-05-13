# Upstream Baseline

govpx is pinned to libvpx v1.16.0.

- Upstream project: `chromium/webm/libvpx`
- Tag: `v1.16.0`
- Annotated tag object: `04def0a07f8bfa95785e30e6db95036cda17f9b2`
- Commit: `1024874c5919305883187e2953de8fcb4c3d7fa6`
- Release date: 2026-01-21
- Source URL: `https://chromium.googlesource.com/webm/libvpx/+/refs/tags/v1.16.0`

## Included Scope

The intended porting scope is both VP8 and VP9 from libvpx:

- `vp8/common/`
- `vp8/decoder/`
- `vp8/encoder/`
- `vp9/common/`
- `vp9/decoder/`
- `vp9/encoder/`
- `vpx/`
- `vpx_dsp/`
- `vpx_mem/`
- `vpx_ports/`
- `vpx_scale/`
- `vpx_util/`

The current repository contains a complete VP8 port and an in-progress VP9
port; VP9 status is tracked separately below.

The parity bar for both codecs is 100% byte parity with libvpx on the
supported configurations: bit-identical encoded packets out of the
encoder, bit-identical decoded pixels out of the decoder, validated by
the oracle harness against the pinned libvpx v1.16.0 build.

## Excluded Scope

- `av1/` is explicitly excluded.
- `examples/` and `tools/` are excluded except as optional references.
- WebM mux/demux code is excluded from the codec package.
- cgo is excluded from the Go package.

## License Obligations

libvpx is distributed under a BSD 3-Clause license with an additional patent
grant. This repository keeps libvpx license and patent notices in
`LICENSE.libvpx` and `PATENTS.libvpx`.

## VP8 Subsystem Status

| Subsystem | Status |
| --- | --- |
| Public API | scaffolded. Decoder options expose libvpx-style error concealment and granular postprocess. Runtime encoder controls cover bitrate, rate-control config, CQLevel, GF/AltRef flags, token partitions, sharpness, screen-content, PSNR/SSIM tuning, VP8 RTC external-rate-control mode, static threshold, noise sensitivity, ARNR, two-pass stats, realtime targets, temporal scalability/layer ID, deadline/CpuUsed, keyframe interval, adaptive keyframes, force-keyframe, flush, reset. Encoder results report temporal IDs, sync, TL0PICIDX, droppable metadata, lookahead depth, preprocessing flags, scene-cut keyframe promotion, first-pass stats, two-pass targets, per-layer rate/buffer/bit counters. Zero-allocation regression coverage on hot paths after initialization. |
| Upstream manifest | scaffolded |
| Oracle harness | `make verify-production` builds pinned vpxenc/vpxdec and the VP8 temporal SVC example encoder, fetches corpora, runs the full oracle gate. `make verify-decoder-parity` runs the decoder-only proof. Optional vpxdec smoke and libvpx checksum oracle support postprocess, ADDNOISE, and error-concealment decode modes. |
| Benchmark harness | synthetic govpx encoder JSON bench with optional libvpx-vpxenc reference (output size, latency, bitrate, PSNR/SSIM). Decoder CLI and `benchmarks/` package cover Decode/DecodeInto on the libvpx-authored smoke stream with optional checksum-oracle reference timing. |
| Hot-path allocation guards | Decode, DecodeInto, NextFrame, EncodeInto, temporal EncodeInto, runtime controls, ForceKeyFrame, Reset, Close, and core parser/DSP helpers — zero-allocation after init. |
| IVF/test vectors | IVF parser and oracle checksum parser scaffolded. Decoder parity gate covers the full pinned libvpx v1.16.0 valid VP8 subset (58 VP80 IVF vectors + 2 invalid rejection vectors; four `vp80-03-segmentation-0*.ivf` fixtures are I420), checksumming both `Decode` and `DecodeInto`. Generated libvpx feature streams cover profile 0–3, 1/2/4/8 token partitions, error-resilient cyclic-refresh segmentation, sharpness, static-threshold, postprocess, error-concealment, and keyframe resolution change. Opt-in encoder oracle covers B_PRED keyframes, intra B_PRED interframes, temporal SVC, CQLevel, govpx- and libvpx-encoded corpus decode acceptance, token-partition/segmentation assertions, aggregate and per-frame PSNR/SSIM floors, bitrate windows, and libvpx-vpxenc quality-gap guards. Production external Y4M/YUV source corpus is wired through `GOVPX_ENCODER_TEST_DATA_PATH`. |
| Frame memory | macroblock-padded, border-addressable frame buffers scaffolded |
| Bool decoder/writer | bool decoder scaffolded |
| Header parsing | frame tag and uncompressed keyframe header scaffolded |
| Decoder state and reconstruction | state headers, segment dequant setup, residual transform/add, intra predictor reference setup with row-edge extension, whole-block + B_PRED 4x4 prediction/reconstruction, keyframe/inter reference refresh, keyframe resolution-change reinitialization, version-specific inter prediction flags, extended-border whole-MB and SplitMV inter prediction/reconstruction, active error-concealment prediction for corrupt residuals, default and granular deblock/demacroblock postprocess, MFQE, optional luma ADDNOISE, narrow-frame output. |
| Token and mode parsing | tree reader, partition layout with 2/4/8 token-reader row cycling, coef/mode probability state, MB coefficient/mode grids including error-concealment corruption tracking, near-MV selection, split-MV parsing, MV decoding, active error-concealment missing-MV estimation. |
| Scalar DSP | clip/copy/reconstruction, SAD (16x16/16x8/8x16/8x8/4x4) with bounded 16x16 early-out, variance/SSE (all common sizes), bilinear subpel variance, bilinear/six-tap subpel prediction, dequant, IDCT4x4, IWHT4x4, intra predictors. |
| Loop filter | scalar edge primitives, limit table setup, decoder frame traversal. |
| Encoder rate-control API | one-pass keyframe target sizing/boosts, scene-cut keyframe promotion (one-pass + two-pass), first-pass stats collection, two-pass VBR targeting, droppable encoded-frame metadata, max-intra-bitrate KF cap, CBR GF refresh/boost with cyclic-refresh cadence (incl. screen-content cadence), low-buffer debt clamp, screen-content inter-Q drop limiting, temporal layer ID override, per-layer bit counters and cumulative buffer updates, bits-per-MB quantizer regulation with buffer-fullness scaling, rolling bit monitors and correction factors, non-show-frame overhead accounting, negative CBR buffer-debt drop, runtime bitrate buffer preservation, bounded correction-factor feedback (CBR/CQ/VBR/Q), boosted-golden branching, frame-size bounds for pre/post-commit Q feedback, bounded CBR drop, CQLevel floor. |
| VP8 constants and static tables | scaffolded; quant/dequant tables scaffolded |
| Encoder bitstream writer | bool writer, packet, tree-token, keyframe state, token-partition payloads, interframe intra/inter mode primitives. |
| Encoder frame algorithms | keyframe packets, keyframe mode, zero/nonzero coefficient token grid writers, lookahead, ARNR-style temporal filtering, spatial/temporal denoising. Whole-block luma/chroma intra mode selection with RD rate costs; keyframe B_PRED 4x4 luma with context-aware mode costs; residual analysis with skip/ref/inter-mode/MV/token bit costs and segment-aware quant/dequant reconstruction. StaticThreshold encode-breakout, rotating cyclic-refresh segmentation (default CBR/error-resilient enablement, screen-content cadence/disable, Q/2-Q ALT_Q boost, refresh-map cooldown, count-derived segment tree probs, base temporal-layer gating, consecutive-LAST/ZEROMV counters, skin-block classification). LAST/ZEROMV residual interframes; LAST/GOLDEN/ALTREF selection with LAST-only default refresh and force-GF/ARF flags; invisible-frame handling. Explicit ZEROMV/NEARESTMV/NEARMV/NEWMV inter RD candidates with near-MV reuse, full/half/quarter-pel motion search with bilinear subpel variance and RD reconstruction; SPLITMV candidate selection/bitstream emission across VP8 split shapes. Default reconstructed-frame loop filtering with base-Q initial level and keyframe sharpness reset; forward transforms; segment-aware coefficient-builder quant setup; fast block quantization. |
| SIMD/assembly | not started |

## Known Deviations

- `Decode` and `DecodeInto` have no known behavioral parity gap for the
  supported VP8 surface covered by `make verify-decoder-parity`: VP8 versions
  0-7, token partitions, keyframes, interframes, SplitMV, error-resilient and
  active error concealment, keyframe resolution changes, default and granular
  deblock/demacroblock post-processing, libvpx-style MFQE, and optional luma
  ADDNOISE using libvpx-compatible libc rand streams for the supported oracle
  platforms. Caller-configured size/resolution rejections return
  `ErrFrameRejected`; `ErrUnsupportedFeature` is retained only for API
  compatibility. This is an oracle-backed empirical parity claim, not a formal
  proof over every possible malformed byte stream.
- `EncodeInto` emits keyframes, LAST/ZEROMV interframes, intra macroblocks
  inside interframes, NEWMV interframes (LAST/GOLDEN/ALTREF selection with
  ZEROMV/NEARESTMV/NEARMV/NEWMV RD candidate scoring, near-MV reuse,
  libvpx-style full/subpel motion search), SPLITMV across VP8 split shapes,
  token partitions, LAST-only default refresh with GF/ARF preservation,
  invisible-frame handling, default reconstructed-frame loop filtering, and
  opt-in lookahead / ARNR / spatial-denoise preprocessing. Two-pass VBR with
  libvpx scene-cut placement, one-pass scene-cut keyframe promotion including
  post-inter auto-key recode, and CQLevel constrained-quality are wired.
  One-pass CBR/CQ/VBR start frames from libvpx's bits-per-MB Q regulator and
  update KF/inter/GF correction factors from encoded frame size.
- Open encoder gaps: exact cyclic/background segment selection, exact
  constrained-quality / GF bitrate heuristics, full libvpx rate-control
  heuristic parity, and the remaining oracle-scoreboard deltas tracked by the
  root oracle tests and `testdata/*_baseline.json`. Encoder corpus validation
  acts as a regression guard, not a parity proof.
- The package exposes a small Go API, not the libvpx C API.

## VP9 Subsystem Status

The VP9 port is in progress on the `vp9-port` branch. Tracking layout:

| Subsystem | Status |
| --- | --- |
| Public API (VP9Decoder / VP9Encoder) | scaffolded. VP9 decoder construction / option validation, `Decode`, `DecodeWithPTS`, `DecodeInto`, `LastFrameInfo`, `Reset`, and `NextFrame` are wired for 8-bit 4:2:0 intra frames: mode-info, detokenize, intra prediction, inverse transform/add, reference refresh, show-existing-frame display, PTS metadata, caller-owned output planes, configured resolution-change rejection, VP9's four frame-context slots / `refresh_frame_context` gate, single-reference zero-MV inter frames across skipped and non-skipped residual blocks, and direct non-scaled single-reference inter motion for NEWMV plus NEARESTMV/NEARMV candidate reuse, including interior subpel interpolation and libvpx-style edge extension for integer/subpel reference windows that cross the visible frame boundary. Fixed eighttap, bilinear, and switchable per-block eighttap-smooth / eighttap-sharp filters are covered. Edge-clipped zero-MV and tiled layouts are covered. Other valid VP9 frame classes still return `ErrVP9NotImplemented` at the reconstruction boundary. VP9 encoder construction / fixed-size `EncodeInto` are wired; the encoder currently emits a minimal keyframe path plus an intra-only inter fallback while the full prediction / residual pipeline is ported. |
| Module scaffolding | complete: `internal/vp9/{bitstream,common,decoder,dsp,encoder,mem,tables}` |
| Bitstream (range coder reader + writer) | complete with round-trip + zero-alloc tests; carry-fixup walk and 64-bit big-endian fill path mirror libvpx vpx_dsp/bitreader.c + bitwriter.c byte-for-byte |
| Common tables (entropy / scan / quant / MV / pred) | scan + iscan + neighbors (30 tables, auto-generated from vp9_scan.c + libvpx-source oracle); quant DC/AC × 8/10/12-bit (libvpx-source oracle); vpx_norm + common-data geometry/partition/chroma projection; entropy-mode and entropymv probability tables not yet ported |
| DSP (idct, intra pred, inter convolve, loop filter, SAD, variance) | inverse transforms (IDCT 4/8/16/32, IWHT4, IADST 4/8/16, IHT dispatches), intra prediction (DC/V/H/TM × 4 sizes + 6 directional × 3 sizes + 10 hand-coded 4x4), convolve (8-tap subpel horiz/vert + avg + copy/avg pass-through), loop filter (4/8/16-pixel + dual), SAD (13 block sizes), variance (13 block sizes + MSE + get4x4sse_cs) — all with byte-parity oracle (1415 records vs libvpx) |
| Decoder (uncompressed + compressed header, tile, detok, recon) | scaffolded: the public decoder parses uncompressed + compressed headers, initializes frame-context slots / loop-filter / dequant state, consumes keyframe + intra-only tile mode-info plus residual tokens, publishes reconstructed I420 output for 8-bit 4:2:0 intra frames, refreshes VP9 reference slots, can display stored show-existing frames without disturbing preserved header state, carries bitdepth / chroma sampling across inter headers, computes libvpx inter-mode contexts from nearest MV-ref neighbors, stores decoded inter MVs in the MI grid, performs the first MV-ref candidate scan for same-ref plus sign-bias-adjusted different-ref neighbors, and reconstructs same-size single-reference inter blocks by copying zero-MV references or running direct inter prediction for NEWMV/NEARESTMV/NEARMV before inverse-transform-adding residual coefficients. Interior and edge-extended integer/subpel windows dispatch through the ported VP9 convolve kernels for fixed eighttap, bilinear, and switchable per-block smooth/sharp filters. Segmentation, partition / mode drivers, MV helpers, coefficient detokenize, inverse-transform dispatch, and intra/inter prediction helpers have unit coverage. Loop-filter traversal, scaled/compound refs, full frame-class coverage, and external VP90 corpus parity are still open. |
| Encoder | scaffolded: uncompressed / compressed header writers, tile and partition walkers, mode / ref / MV / coefficient writers, probability update helpers, and a public `EncodeInto` stub that emits skip=1 DC-pred keyframes across multi-SB and edge-clipped grids. The stub now maintains decoder-visible MI context so keyframe block contexts match libvpx across 1D, 2D, and partial-SB layouts; full mode decision, residual generation, inter prediction, rate control, and frame-context updates remain open. |
| Oracle harness (vp90 IVF corpus + vpxenc/vpxdec gates) | DSP oracle complete: `internal/coracle/build_libvpx_vp9.sh` builds a VP9-decoder+encoder libvpx variant + the DSP oracle binary, which emits a deterministic 1415-record byte-parity corpus checked in under `internal/vp9/dsp/testdata/dsp_oracle.bin`. `internal/coracle/build_vpxdec_vp9.sh` builds a VP9-enabled vpxdec/vpxenc pair; root VP9 encoder tests wrap emitted payloads in VP90 IVF and gate single-SB, horizontal multi-SB, vertical multi-SB, 4x3-SB, edge-clipped/sub-SB keyframes, matching intra-only inter fallback frames, and skipped inter frames through vpxdec. The production oracle gate now runs those VP9 vpxdec structural tests plus VP9 decoder I420 byte-parity checks against libvpx vpxdec for supported nonzero-residue intra, show-existing, skipped zero-MV inter, zero-MV inter-residual, direct integer/subpel NEWMV, NEARESTMV reuse, bilinear, switchable smooth/sharp, and top/right border-extended NEWMV streams, including sub-SB / right-edge / bottom-edge / corner-edge residual fixtures. |
| Byte parity gate | DSP kernels: 1415/1415 records byte-identical to libvpx v1.16.0 (inverse transforms + intra + convolve + loop filter + SAD + variance). VP9 decoder output is byte-identical to libvpx vpxdec for the currently supported 8-bit 4:2:0 intra residual/show-existing/skipped zero-MV inter/inter-residual/direct integer-subpel NEWMV/NEARESTMV/filter-mode/border-extension streams covered by `TestVP9DecoderVpxdecOracleMatches*`. Encoder bitstreams are currently structural-parity gated by vpxdec acceptance for the minimal skip=1 DC path; full encoded-packet byte identity, full decoded-pixel parity, and VP90 corpus gates are still open. |
| Hot-path allocation guards | bitstream Reader.Read / Writer.Write, idct4x4 / idct8x8 / idct16x16 hot paths, VP9Encoder `EncodeInto` steady state after setup, and VP9Decoder `Decode` / `DecodeInto` header + tile/residual parse plus intra, zero-MV inter-residual, direct integer/subpel NEWMV, border-extended subpel NEWMV, switchable subpel NEWMV, and NEARESTMV reconstruct output steady state after setup. |
| SIMD/assembly | not started |

The VP9 port follows the same philosophy as the VP8 port:

- 100% byte parity with libvpx on supported configurations is the gate.
- Hot paths must be zero-allocation after init; allocation tests guard
  steady-state `Decode`/`Encode` paths.
- Inner kernels must remain leaf-callable and inlinable; the public API
  takes caller-owned buffers and aliases into them where libvpx aliases
  into caller-provided storage.
- Stack-only data on hot paths. Heap allocations live in setup / Reset
  only, not in steady-state per-frame work.
- Doc comments name libvpx call sites for any non-obvious code so the
  upstream behavior is traceable.
