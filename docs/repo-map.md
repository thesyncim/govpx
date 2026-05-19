# govpx Repo Map

Generated for Wave 0 of `docs/repo-tidy-plan.md` on 2026-05-19.
Last updated for the first Wave 3/4 boundary move on 2026-05-19.

This document is the coordination map for splitting the current flat root
package into a public facade plus codec-owned internal packages. It records the
current shape, protected validation gates, and no-overlap work packets for the
next waves. The RTP rows now reflect the code move landed on the
`codex/repo-tidy` branch; the rest of the inventory remains the Wave 0
baseline unless a row says otherwise.

## Snapshot

The module import path is `github.com/thesyncim/govpx`.

Tracked Go files, excluding ignored oracle build output:

| Area | Go files |
| --- | ---: |
| Root package | 536 |
| Root package tests | 371 |
| Root package implementation/non-test files | 165 |
| `internal/` | 493 |
| `cmd/` | 41 |
| `examples/` | 5 |
| `benchmarks/` | 1 |

Default `go list ./...` sees fewer root files because many parity and
diagnostic files are behind build tags: 151 root source files, 242 root
same-package tests, and 6 root external tests in the default build.

Ignored local/generated output includes editor metadata, local Go caches,
local worktree caches, `internal/coracle/build/`, and `.scoreboard.log`. The
oracle build directory is not tracked. Tracked generated/provenance assets include
`cmd/govpx-bench/default.pgo`,
`cmd/govpx-bench/default.pgo.sources.sha256`,
`internal/vp9/dsp/testdata/dsp_oracle.bin`, and
`internal/vp9/encoder/testdata/token_cost_oracle.bin`.

## File Clusters

| Cluster | Current files | Target owner |
| --- | --- | --- |
| Public shared surface | `codec.go`, `errors.go`, `image.go`, `rtp.go`, `streaminfo.go`, `temporal.go`, `doc.go` | Root facade; stream-info parsing now delegates to codec-owned decoder packages |
| VP8 public encode/decode facade | `encoder.go`, `decoder.go`, public parts of `encoder_config.go`, `ratecontrol.go` | Root facade forwarding to `internal/vp8/{encoder,decoder}` |
| VP8 encoder implementation | `encoder_*.go`, `ratecontrol_*.go`, VP8-specific parts of `encoder_config.go`, root encoder tests | `internal/vp8/encoder` |
| VP8 decoder implementation | `decoder.go` plus existing `internal/vp8/decoder` internals | `internal/vp8/decoder` |
| VP8 RTP helpers | root `vp8_rtp.go` facade, `internal/vp8/rtp/rtp.go`, `vp8_rtp_test.go`, `vp8_rtp_fuzz_test.go` | Root facade plus `internal/vp8/rtp` and shared `internal/vpx/rtp` mechanics |
| VP9 public encode/decode facade | public parts of `vp9_encoder.go`, `vp9_decoder.go`, `vp9_encoder_config.go`, VP9 first-pass/result/options types | Root facade forwarding to `internal/vp9/{encoder,decoder}` |
| VP9 encoder implementation | `vp9_encoder.go`, `vp9_*` encoder/rate-control/AQ/TPL/partition files, VP9 encoder tests | `internal/vp9/encoder` |
| VP9 decoder implementation | `vp9_decoder*.go`, `vp9_frame_parallel.go`, VP9 decoder tests | `internal/vp9/decoder` |
| VP9 RTP and superframe helpers | root `vp9_rtp.go` and `vp9_superframe.go` facades, `internal/vp9/rtp/rtp.go`, `internal/vp9/bitstream/superframe.go`, related tests/fuzz | Root facade plus `internal/vp9/{rtp,bitstream}` and shared `internal/vpx/rtp` mechanics |
| Oracle/parity harness | `oracle_*_test.go`, `vp9_oracle_*_test.go`, `internal/coracle`, `cmd/scoreboard-report` | `internal/vpx/testharness`, `internal/coracle`, package-local oracle suites |
| Diagnostics/audits | `vp8_task*_test.go`, `vp8_byte*_test.go`, `diag_*_test.go`, `*_audit_test.go`, `*_bisect_test.go` | Rename into regression suites or document as diagnostics |
| Performance and quality gates | `feature_quality_gates*_test.go`, `benchmarks`, `cmd/govpx-bench`, `*_bench_test.go` | Package-local benches plus `cmd/govpx-bench` |

Existing internal codec packages are already substantial:

| Package area | Current role |
| --- | --- |
| `internal/vp8/boolcoder` | VP8 boolean decoder and fuzz tests |
| `internal/vp8/common` | VP8 shared frame, quantizer, loop-filter, border helpers |
| `internal/vp8/decoder` | VP8 parser, reconstruction, loop filter, postprocess, threading |
| `internal/vp8/dsp` | VP8 scalar/SIMD kernels |
| `internal/vp8/encoder` | VP8 packet writing, transforms, quantization, tokenization, motion helpers |
| `internal/vp8/rtp` | VP8 RTP payload descriptor parse/pack and frame packetize/assemble logic; RFC 7741, not libvpx-derived |
| `internal/vp8/mem`, `internal/vp8/scale`, `internal/vp8/tables` | VP8 support packages |
| `internal/vp9/bitstream` | VP9 bit reader/writer and superframe index parser/writer |
| `internal/vp9/common` | VP9 constants, enums, quantization |
| `internal/vp9/decoder` | VP9 parser, stream-info peeking, reconstruction, loop filter, tile/thread plumbing |
| `internal/vp9/dsp` | VP9 scalar/SIMD kernels |
| `internal/vp9/encoder` | VP9 bitstream writer and transform/quant helpers |
| `internal/vp9/rtp` | VP9 RTP payload descriptor, scalability-structure, and frame packetize/assemble logic; RFC 9628, not libvpx-derived |
| `internal/vp9/mem`, `internal/vp9/tables` | VP9 support packages |
| `internal/vpx/errors` | Shared sentinel errors used by internal packages and re-exported by the root facade |
| `internal/vpx/rtp` | Codec-neutral RTP fragment type and overflow-safe packetization-size helpers |

## Largest Files

Largest hand-authored root implementation files:

| Lines | File | Wave 2 action |
| ---: | --- | --- |
| 16,828 | `vp9_encoder.go` | Must split by responsibility before any package move |
| 2,475 | `vp9_pick_inter_mode_nonrd.go` | Watch cap; split if edited materially |
| 2,123 | `vp9_speed_features.go` | Move as VP9 encoder config/speed feature ownership |
| 2,065 | `vp9_decoder.go` | Split public facade from decoder state/lifecycle before move |
| 1,907 | `vp9_decoder_modes.go` | Move with VP9 decoder internals |
| 1,726 | `encoder_config.go` | Split public VP8 options from private normalized config |
| 1,486 | `encoder_twopass_state.go` | Move with VP8 encoder two-pass internals |
| 1,360 | `encoder.go` | Split VP8 public type/methods from encoder state |
| 1,343 | `vp9_spatial_svc.go` | Keep public SVC facade small; move implementation helpers |
| 1,301 | `encoder_frame.go` | Move with VP8 encoder frame encode path |

Largest root test files:

| Lines | File | Wave 2/6 action |
| ---: | --- | --- |
| 10,111 | `vp9_encoder_test.go` | Split into constructor/options, encode entrypoints, reference, rate-control, SVC, superframe suites |
| 7,964 | `vp9_decoder_test.go` | Split into parser/header, decode, threading, postprocess, reference, conformance suites |
| 5,366 | `vp9_oracle_stream_parity_scoreboard_test.go` | Split scoreboard cases by feature and keep oracle tag |
| 5,323 | `oracle_encoder_stream_parity_runtime_controls_test.go` | Split runtime-control matrix from helpers |
| 1,956 | `feature_quality_gates_vp8_test.go` | Move quality gates near benchmark/validation ownership |
| 1,938 | `vp9_encoder_vpxenc_oracle_test.go` | Split VP9 vpxenc parity cases |
| 1,771 | `encoder_runtime_controls_test.go` | Move with VP8 encoder runtime-control tests |
| 1,706 | `vp9_spatial_svc_test.go` | Split SVC public API and implementation cases |

Largest internal codec files are already below the 2,500-line implementation
cap. The current internal high-water mark is
`internal/vp9/encoder/transform_quant.go` at 1,769 lines.

## Public API Inventory

The root package currently exposes both user-facing APIs and implementation or
parity concepts.

Keep as public facade concepts:

- Codec identifiers: `Codec`, `CodecVP8`, `CodecVP9`, `Version`,
  `UpstreamLibvpxVersion`.
- Shared errors: `ErrInvalidData`, `ErrNeedKeyFrame`, `ErrFrameNotReady`,
  `ErrBufferTooSmall`, `ErrFrameRejected`, `ErrInvalidConfig`,
  `ErrInvalidBitrate`, `ErrInvalidQuantizer`, `ErrClosed`,
  `ErrInvalidVP9Data`, `ErrVP9NotImplemented`.
- Shared image/buffer values: `Image`, `RTPPayloadFragment`.
- Constructors: `NewVP8Encoder`, `NewVP9Encoder`, `NewVP8Decoder`,
  `NewVP9Decoder`.
- Public codec handles: `VP8Encoder`, `VP9Encoder`, `VP8Decoder`,
  `VP9Decoder`.
- Shared result/metadata families: `EncodeResult`, `VP9EncodeResult`,
  `FrameInfo`, `VP9FrameInfo`, `StreamInfo`, `VP9StreamInfo`.
- Stream inspection: `PeekVP8StreamInfo`, `PeekVP9StreamInfo`.
- RTP public helpers and descriptors for VP8 and VP9.
- Explicit codec-specific public concepts: VP8 token partitions, VP8/VP9
  reference controls, VP9 superframes, VP9 spatial SVC, VP9 color/render
  metadata, VP9 tile/row-MT controls, VP9 first-pass stats.

Split or redesign as shared public options in Wave 5:

- `EncoderOptions` and `VP9EncoderOptions` currently duplicate video
  dimensions, timebase/FPS, threading, rate control, realtime controls,
  quantizer ranges, lookahead, ARNR, tuning, and two-pass controls.
- `DecoderOptions` and `VP9DecoderOptions` duplicate threading,
  postprocess, error concealment/resilience, decryptor, max dimensions, and
  resolution-change rejection.
- Proposed shared structs: `VideoOptions`, `TimebaseOptions`,
  `ThreadOptions`, `RateControlOptions`, `RealtimeOptions`,
  `PostProcessOptions`. Embed them into `VP8EncoderOptions`,
  `VP9EncoderOptions`, `VP8DecoderOptions`, and `VP9DecoderOptions` after the
  facade plan is accepted.

Move out of public surface unless Wave 1 explicitly keeps them:

- VP9 internal tuning/data-model types from `vp9_speed_features.go`, including
  `SpeedFeatures`, `MvSpeedFeatures`, `MeshPattern`,
  `PartitionSearchBreakoutThr`, and low-level search enum families.
- VP9 quality experiment helpers such as `VP9ComputeARFBoost`,
  `VP9DefaultARFBoostParams`, `VP9AdjustARNRFilter`, and
  `VP9TPLFrameDelta` unless a user-facing use case is documented.
- Oracle/debug probes: `ProbeVP9SearchFilterRefFires`,
  `ResetVP9SearchFilterRefProbes`, trace flags, leaf trace plumbing, and
  scoreboard-only helpers.
- Any root-level trace writer setters that are only used by tagged oracle
  tests.

Current method families:

| Handle | Current method shape |
| --- | --- |
| `VP8Encoder` | `EncodeInto`, `FlushInto`, `CollectFirstPassStats`, `SetTwoPassStats`, `SetRateControl`, `SetRealtimeTarget`, many `Set*` runtime controls, `ForceKeyFrame`, `LastQuantizer`, reference set/copy |
| `VP9Encoder` | `Encode`, `EncodeWithFlags`, `EncodeInto`, `EncodeIntoWithFlags`, `EncodeIntoWithResult`, `FlushInto`, `FlushIntoWithResult`, intra-only/show-existing helpers, first-pass/two-pass, many VP9-specific `Set*` controls |
| `VP8Decoder` | `Decode`, `DecodeWithPTS`, `DecodeInto`, `DecodeIntoWithPTS`, `NextFrame`, `LastFrameInfo`, corruption/reference metadata, reference set/copy |
| `VP9Decoder` | VP8-like decode methods plus tile filters, row-MT, loop-filter options, byte alignment, external frame buffers, SVC spatial layer filtering |

Wave 5 should make `EncodeInto`, `DecodeInto`, and `FlushInto` mean
caller-owned output consistently. VP9 currently has legacy allocation helpers
(`Encode`, `EncodeWithFlags`, `EncodeIntraOnlyFrame`,
`EncodeShowExistingFrame`) and an argument order mismatch
(`VP9Encoder.EncodeInto(img, dst)` vs `VP8Encoder.EncodeInto(dst, src, ...)`).

## Test Categories

The repository has 586 `*_test.go` files. A filename/package heuristic gives:

| Category | Approx. count | Current locations |
| --- | ---: | --- |
| Unit and pure Go codec tests | 389 | root, `internal/vp8/*`, `internal/vp9/*`, `cmd/*` |
| Oracle/parity tests | 138 | mostly root `oracle_*`, `vp9_oracle_*`, `*_vpxdec_oracle_*`, `*_vpxenc_oracle_*` |
| Diagnostic/audit/bisect tests | 27 | root `vp8_task*`, `vp8_byte*`, `diag_*`, `*_audit_*`, `*_bisect_*` |
| Performance/quality tests | 18 | `feature_quality_gates*`, `*_bench_test.go`, `cmd/govpx-bench/benchcmd` |
| Fuzz entrypoints/regressions | 12 | root fuzz files plus `internal/vp8` fuzz tests |
| Named repro/regression tests | 2 | root repro files |

Root-package tests should shrink to public facade and API coverage. Codec
implementation tests should live beside `internal/vp8/*` or `internal/vp9/*`
code. Oracle harness helpers should move to `internal/vpx/testharness` or
`internal/testutil` only when they are codec-neutral.

Tracked fuzz corpus seeds live under `testdata/fuzz` with 160 regression-named
seed files. Do not update parity baselines or fuzz seeds merely to make a
structural move pass.

## Build Tags And Instrumentation

Important build tags:

| Tag | Purpose |
| --- | --- |
| `govpx_oracle_trace` | Enables oracle trace, scoreboard, byte-parity, and parity fuzz code |
| `!govpx_oracle_trace` | Keeps production no-op trace/probe paths compiled without trace dependencies |
| `govpx_oracle_trace && diag` | Manual diagnostics only |
| `purego` | Excludes architecture-specific assembly and uses scalar Go fallbacks |
| `amd64 && !purego`, `arm64 && !purego` | Architecture-specific SIMD dispatch |
| `ignore` | Generators and local C/oracle support files |

All moved trace, oracle, debug counter, phase stat, and scoreboard hooks must
stay zero-cost when disabled: no allocations, clock reads, atomics, interface
dispatch, or hot-loop branches beyond existing behavior.

## Protected Gates

Use these gates to choose validation depth:

| Command | Covers | Required for |
| --- | --- | --- |
| `go test ./... -count=1` | Default pure-Go package tests across root, internal, commands, benchmarks | Minimum structural safe point |
| `go test -tags purego ./... -count=1` | Scalar fallback build and tests | SIMD/dispatch moves; included in CI |
| `make pre-commit` | `gofmt` check, fuzz seed naming check, PGO freshness/build check | Before commits that touch Go or benchmark hot-path sources |
| `make ci` | `fmtcheck`, `pgo-check`, default tests, purego tests, VP9 decoder conformance, VP9 quality smoke | Codec/parity-sensitive packets |
| `make verify-decoder-parity` | `ci` plus VP8/VP9 decoder oracle tests | Decoder moves |
| `make verify-bd-rate` | Slow VP9 BD-rate feature gates against libvpx | VP9 AQ/ARNR/TPL/loop-filter quality changes |
| `make verify-bd-rate-vp8` | Slow VP8 BD-rate feature gates against libvpx | VP8 quality/rate-control changes |
| `make verify-production` | `ci`, `oracle-test`, `byte-parity`, `scoreboard` | Final integration and behavior-sensitive waves |

Protected parity binaries live under `internal/coracle/build/` and are built
by the Makefile targets. Required oracle/test-data environment is set by the
targets; do not inline ad hoc paths in tests.

Never use `scoreboard-update` or `GOVPX_UPDATE_BASELINES=1` as part of a tidy
move unless a separate, explicitly approved parity-baseline packet requires it.

## Proposed Move Ledger

| Wave | Packet | Owned paths | Notes |
| --- | --- | --- | --- |
| 1 | Public facade draft | `docs/api.md`, `doc.go`, `README.md` only if documenting | Decide final user surface before API changes |
| 1 | Root facade file plan | `docs/repo-map.md`, future `options.go`, `vp8.go`, `vp9.go` plan only | No code moves yet |
| 2 | VP9 encoder split | `vp9_encoder.go`, new `vp9_encoder_*.go`, focused `vp9_encoder_*_test.go` | Same package, no behavior change |
| 2 | VP9 decoder split | `vp9_decoder.go`, `vp9_decoder_test.go`, new focused VP9 decoder files | Same package, no behavior change |
| 2 | VP9 oracle scoreboard split | `vp9_oracle_stream_parity_scoreboard_test.go`, VP9 oracle helper files | Keep `govpx_oracle_trace` tags |
| 2 | VP8 encoder oversized files | `encoder.go`, `encoder_config.go`, `encoder_runtime_controls_test.go`, `encoder_ratecontrol_paths_test.go` | Split public option shell from private config |
| 2 | Root diagnostic naming | `vp8_task*_test.go`, `vp8_byte*_test.go`, `diag_*_test.go` | Rename only; no expectation changes |
| 3 | VP8 decoder move | root `decoder*.go` private pieces, `internal/vp8/decoder/**` | Root keeps `VP8Decoder` facade |
| 3 | VP8 encoder move | root `encoder*.go`, `ratecontrol*.go`, VP8 encoder tests, `internal/vp8/encoder/**` | Root keeps `VP8Encoder` facade |
| 3 | VP9 decoder move | root `vp9_decoder*.go`, VP9 decoder tests, `internal/vp9/decoder/**` | Root keeps `VP9Decoder` facade |
| 3 | VP9 encoder move | root `vp9_*` encoder/ratecontrol/AQ/TPL files, VP9 encoder tests, `internal/vp9/encoder/**` | Move after same-package split |
| 3/4 | RTP ownership move | root `rtp.go`, `vp8_rtp.go`, `vp9_rtp.go`, `internal/vpx/{errors,rtp}/**`, `internal/vp{8,9}/rtp/**`, RTP tests/fuzz | Current branch: root files are public facade aliases/wrappers; descriptor logic lives in codec-owned internal packages; shared mechanics are fragment sizing and sentinel errors only |
| 3 | VP9 superframe move | root `vp9_superframe.go`, `vp9_decoder.go` parser wrapper, `internal/vp9/bitstream/superframe.go`, superframe tests/fuzz | Current branch: root keeps public pack helpers; VP9 bitstream package owns parse/write mechanics |
| 3 | Stream-info parser move | root `streaminfo.go`, `internal/vp8/decoder/streaminfo.go`, `internal/vp9/decoder/streaminfo.go`, stream-info tests | Current branch: root keeps public structs and peek functions; VP8/VP9 decoder packages own parser-visible metadata extraction |
| 4 | Shared validation/options helpers | new `internal/vpx/{buffers,ratecontrol}/**`, related tests | Mechanical helpers only; keep codec semantics separate |
| 4 | Shared test harness | `internal/testutil/**`, new `internal/vpx/testharness/**`, oracle helper tests | No hot-path imports from oracle/test packages |
| 5 | API cleanup | root public files, examples, docs | Remove unreleased compatibility aliases at wave end |
| 6 | Test suite hygiene | package-local `*_unit`, `*_oracle`, `*_fuzz`, `*_bench`, `*_regression` files | Move helpers first, then suites |
| 6.5 | Tracing/perf hygiene | trace/probe files, allocation tests, representative benches | Preserve disabled-path zero cost |
| 7 | Docs rewrite | `README.md`, `docs/api.md`, `docs/architecture.md`, `docs/codec-status.md`, `docs/validation.md`, `UPSTREAM.md`, `plan.md` links | Current branch: README links to focused architecture/status/validation docs; parity notes stay out of the README |
| 8 | Final sweep | stale shims, examples, `.gitignore`, `docs/repo-map.md` | Full production verification |

## No-Overlap Ownership Table

Only one packet should own a file family at a time. New files inherit the
packet owner if they are created from an owned file.

| Owner lane | Exclusive paths |
| --- | --- |
| Public facade lane | `codec.go`, `errors.go`, `image.go`, `rtp.go`, `streaminfo.go`, `temporal.go`, `doc.go`, future `options.go`, `vp8.go`, `vp9.go`, `example_test.go` |
| VP8 encoder lane | root `encoder*.go`, `ratecontrol*.go`, VP8 encoder/root rate-control tests, `internal/vp8/encoder/**` |
| VP8 decoder lane | root `decoder*.go`, VP8 decoder tests, `internal/vp8/decoder/**` |
| VP8 DSP/common lane | `internal/vp8/{boolcoder,common,dsp,mem,scale,tables}/**` |
| VP8 RTP lane | root `vp8_rtp.go`, `vp8_rtp_test.go`, `vp8_rtp_fuzz_test.go`, `internal/vp8/rtp/**` |
| VP9 encoder lane | `vp9_encoder*.go`, `vp9_*` encoder/ratecontrol/AQ/TPL/partition files, VP9 encoder tests, `internal/vp9/encoder/**` |
| VP9 decoder lane | `vp9_decoder*.go`, `vp9_frame_parallel.go`, VP9 decoder tests, `internal/vp9/decoder/**` |
| VP9 DSP/common lane | `internal/vp9/{bitstream,common,dsp,mem,tables}/**` |
| VP9 RTP/packet lane | root `vp9_rtp.go`, `vp9_superframe.go`, RTP/superframe tests, `internal/vp9/rtp/**`, `internal/vp9/bitstream/superframe.go` |
| Oracle/test harness lane | root `oracle_*`, `vp9_oracle_*`, `internal/coracle/**`, `cmd/scoreboard-report/**`, new `internal/vpx/testharness/**` |
| Benchmark/quality lane | `cmd/govpx-bench/**`, `benchmarks/**`, `feature_quality_gates*_test.go`, `cmd/govpx-bench/default.pgo*` |
| Docs lane | `README.md`, `UPSTREAM.md`, `plan.md`, `docs/**`, example READMEs |
| Examples lane | `examples/webrtc-vp8/**`, `examples/webrtc-vp9/**` |

If a packet must cross lanes, record the reason in the commit message and run
the stricter gate for both lanes.

## Immediate Safe Next Steps

1. Land this Wave 0 map and the repo tidy plan as documentation only.
2. Draft `docs/api.md` without code changes.
3. Split `vp9_encoder.go` in package `govpx` before moving any VP9 encoder
   code. This is the largest review risk and the only hand-authored
   implementation file currently above 2,500 lines.
4. Split `vp9_encoder_test.go`, `vp9_decoder_test.go`, and the two largest
   oracle scoreboard tests before moving the corresponding implementations.
5. Keep root-package code moves behind green `go test ./... -count=1` safe
   points. Escalate to `make ci` for codec behavior-sensitive boundaries and
   `make verify-production` for final integration.
