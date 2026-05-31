# Repository Map

This map is a working inventory for the repo-tidy effort. Refresh it from the
tree before assigning large move packets; do not treat it as historical truth.

Last refreshed: 2026-05-31 from `main`.

## Current Counts

- Root Go files: 748.
- Root test files: 583.
- Internal Go files: 760.
- Root VP8 files: 82 implementation files and 320 test files.
- Root VP9 files: 74 implementation/public-adapter files and 253 test files.
- Internal package files:
  - `internal/vp8`: 326 Go files.
  - `internal/vp9`: 327 Go files.
  - `internal/vpx`: 17 Go files.
  - `internal/coracle`: 26 Go files.
  - `internal/testutil`: 60 Go files.
- Test-name clusters:
  - Non-internal tests: 607.
  - Internal tests: 326.
  - Test files with `oracle` in the name: 192.
  - Test files with `parity` in the name: 97.
  - Files with `fuzz` in the name: 48.
  - Test files with `bench` in the path: 28.

No tracked Go file is currently over 2500 lines. Keep it that way unless an
exception is written here with a concrete reason.

## Largest Files

Largest root files:

| Lines | File |
| ---: | --- |
| 1975 | `vp9_encoder_key_modes.go` |
| 1804 | `vp9_encoder_inter_modes.go` |
| 1789 | `vp9_decoder.go` |
| 1784 | `vp9_pick_inter_mode_nonrd.go` |
| 1723 | `vp8_encoder_config.go` |
| 1509 | `vp9_encoder_inter_partition.go` |
| 1487 | `vp8_encoder_twopass_state.go` |
| 1467 | `vp9_decoder_modes.go` |
| 1343 | `vp8_oracle_encoder_stream_parity_test.go` |
| 1317 | `vp8_encoder.go` |
| 1304 | `vp8_encoder_frame.go` |
| 1239 | `vp9_encoder_config.go` |
| 1224 | `vp9_encoder_tile_workers.go` |
| 1206 | `vp8_encoder_oracle_trace.go` |
| 1190 | `vp9_spatial_svc.go` |

Largest internal files:

| Lines | File |
| ---: | --- |
| 1754 | `internal/vp9/encoder/transform_quant.go` |
| 1483 | `internal/vp9/encoder/transform_quant_test.go` |
| 1141 | `internal/vp9/encoder/block_yrd.go` |
| 1105 | `internal/vp9/encoder/gf_group.go` |
| 1073 | `internal/vp9/encoder/cyclic_refresh.go` |
| 935 | `internal/vp8/decoder/reconstruct.go` |
| 921 | `internal/vp8/decoder/postprocess.go` |
| 896 | `internal/coracle/oracle_compare_test.go` |
| 843 | `internal/vp9/tables/coef_probs.go` |
| 833 | `internal/vp8/encoder/optimize_block_dp_state_test.go` |
| 793 | `internal/vp9/encoder/temporal_filter.go` |

## Public Surface Inventory

The public import path remains `github.com/thesyncim/govpx`.

Root public families currently exposed by `go doc -short .`:

- Constructors and handles: `NewVP8Encoder`, `NewVP8Decoder`,
  `NewVP9Encoder`, `NewVP9Decoder`, `NewVP9SpatialSVCEncoder`,
  `NewVP9MultiResolutionEncoder`.
- Caller-owned hot paths: `EncodeInto`, `DecodeInto`, `FlushInto`, RTP
  `*Into` packetization/assembly helpers, and VP9 superframe `*Into` helpers.
- Allocating convenience APIs: `Encode`, `Decode`, RTP helpers returning
  slices, and VP9 superframe helpers returning slices.
- Public data types: `Image`, `EncoderOptions`, `DecoderOptions`,
  `VP9EncoderOptions`, `VP9DecoderOptions`, `EncodeResult`,
  `VP9EncodeResult`, `FrameInfo`, `VP9FrameInfo`, RTP descriptors, temporal
  and spatial scalability options, rate-control options, and reference flags.
- Public probe helpers: `PeekVP8StreamInfo`, `PeekVP9StreamInfo`.
- Public errors: root sentinels from `errors.go`, including invalid data,
  unsupported feature, and buffer/option failures.

API cleanup should make this list smaller and easier to scan. Because the
module is unreleased, do not add compatibility aliases or deprecated wrappers
for old internal names.

## Package Clusters

Root `govpx` should converge on:

- public codec identifiers, errors, options, image/buffer types, stream info,
  constructors, and thin codec handles;
- public RTP descriptor and payload helpers;
- examples and public facade tests only.

`internal/vp8` owns VP8-only implementation:

- `decoder`: VP8 frame syntax, reconstruction, loop filter, postprocess,
  error concealment, references, and decode helpers;
- `encoder`: VP8 bool writer, packet writer, rate control, temporal
  scalability, denoiser, loop filter scoring, ARNR, motion/mode decisions,
  tokenization, transforms, and quantization;
- `rtp`, `dsp`, `common`, `scale`, `tables`: VP8-specific mechanics.

`internal/vp9` owns VP9-only implementation:

- `decoder`: VP9 syntax, tiles, frame contexts, reconstruction, loop filter,
  postprocess, threading, and frame-buffer handling;
- `encoder`: VP9 bitstream writing, rate control, SVC, TPL, AQ, ARNR, cyclic
  refresh, partitioning, transform/quantization, and mode decisions;
- `bitstream`, `rtp`, `dsp`, `common`, `tables`: VP9-specific mechanics.

`internal/vpx` is for codec-neutral mechanics only: RTP fragment loops, buffer
helpers, geometry/validation, arithmetic helpers, rate-control arithmetic, and
small shared value objects. Do not move codec syntax, mode decisions,
probability models, or reference semantics into `internal/vpx`.

`internal/coracle` owns libvpx integration: pinned build scripts, tool-path
resolution, subprocess wrappers, trace projection/comparison, and parity report
helpers. VP9 root oracle tests use `internal/testutil/vp9test` for
codec-specific packet/tool mechanics and `internal/testutil/vp9oracle` for
VP9 encoder-vpxenc option projection and stream-parity orchestration. VP8 root
oracle tests use `internal/testutil/vp8test` for VP8-specific oracle tool resolution, trace
projection/comparison, vpxenc/vpxdec calls, first-pass and two-pass captures,
temporal-SVC sample runs, checksum helpers, and JSON baselines. Root tests,
default-build tests, and production packages must not import coracle directly.

## Test Categories

Use objective file names. Avoid task numbers, audit labels, or historical
tracker wording in test names, subtest names, skip messages, temp file names,
and failure logs.

- Unit tests: local deterministic logic, preferably beside the internal package
  they validate.
- Pure Go codec tests: no external binaries; should be the default `go test`
  experience.
- Oracle parity tests: libvpx-backed tests, usually tagged or gated by
  `GOVPX_WITH_ORACLE=1`, using `internal/coracle`.
- Fuzz and regression tests: fuzzer entrypoints and named corpus regressions.
- Performance and allocation tests: `AllocsPerRun`, benchmarks, PGO-sensitive
  checks, and parity-report gates.
- Diagnostic tests: temporary probes only when documented. Prefer converting
  them to objective regression or parity tests once the finding is understood.

Current root split pass: VP8 and VP9 oracle parity, vpxdec/vpxenc parity,
decoder conformance/allocation, rate-control, motion-search, loop-filter,
speed-feature, AQ, runtime-segmentation, SVC runtime-control, and benchmark CLI
tests have been split into behavior-named files. These are package-local
mechanical splits only; the next cleanup pass should move implementation and
oracle-owned suites out of root instead of adding more root test surface.
The current VP9 naming pass removes root public-adapter suffixes and
historical stream-centric oracle filenames in favor of public-behavior and
encoder-oracle names, while preserving package boundaries and test bodies.

## Protected Gates

| Gate | Covers |
| --- | --- |
| `make pre-commit` | `gofmt`, fuzz seed naming, VP8 PGO fingerprint and PGO build check. |
| `go test ./... -count=1` | All default packages, unit tests, pure Go codec tests, facade tests, and internal tests. |
| `make test-purego` | All packages under `purego`; protects scalar fallbacks and SIMD dispatch assumptions. |
| `make test-trace` | Compile-only `govpx_oracle_trace` build for all packages; protects tagged trace/oracle shapes without running external tools. |
| `make ci` | `pre-commit`, default tests, purego tests, trace compile, VP9 decoder conformance, VP9 quality fixtures, and small VP9 BD-rate subset. |
| `make test-oracle` | Libvpx-backed VP8 and VP9 oracle suite with pinned binaries and fixture data. |
| `make test-vp9-internal-oracle` | Tagged VP9 internal source/blob oracle checks for generated tables, DSP kernels, token costs, and source-derived constants. |
| `make test-byte-parity` | Strict byte-parity gates under oracle trace builds. |
| `make test-parity-report` | Parity report gates without baseline updates. |
| `make test-bdrate-vp8` | VP8 BD-rate quality gates against pinned libvpx `vpxenc`. |
| `make test-bdrate-vp9` | VP9 BD-rate quality sweep. |
| `make verify-decoder-parity` | `make ci` plus decoder oracle checks. |
| `make verify-production` | Final integration gate: `make ci`, oracle tests, byte parity, and parity reports. |

Minimum structural gate: `go test ./... -count=1`.
Minimum behavior-sensitive gate: `make ci`.
Final integration gate: `make verify-production`.

## No-Overlap Ownership Table

| Lane | Owned Paths | Notes |
| --- | --- | --- |
| Public facade | `codec.go`, `errors.go`, `image.go`, `options.go`, `rtp.go`, public examples, package docs | Keep stable user API here; no codec internals. |
| VP8 facade and remaining root VP8 move work | root `vp8*.go` that is not public facade | Move private implementation into `internal/vp8/*`; keep root wrappers thin. |
| VP9 facade and remaining root VP9 move work | root `vp9*.go` that is not public facade | Move private implementation into `internal/vp9/*`; keep root wrappers thin. |
| VP8 internals | `internal/vp8/**` | Do not edit VP9 internals in the same packet unless shared code forces it. |
| VP9 internals | `internal/vp9/**` | Do not edit VP8 internals in the same packet unless shared code forces it. |
| Shared mechanics | `internal/vpx/**` | Only real cross-codec mechanics. No forced VP8/VP9 symmetry. |
| Oracle harness | `internal/coracle/**`, root `*_oracle_test.go` harness call sites | New subprocess plumbing belongs in coracle. |
| Test utilities | `internal/testutil/**`, `testdata/**` | No parity baseline update without an explicit baseline packet. |
| Docs | `README.md`, `docs/**`, `UPSTREAM.md`, `plan.md` | User docs and parity engineering notes stay separate. |

## Proposed Move Ledger

This ledger tracks intent, not completed work.

| Area | Current State | Target |
| --- | --- | --- |
| Root VP8 implementation | 82 root VP8 implementation files remain; VP8 encoder implementation comments no longer carry tracker task numbers and now describe the libvpx behavior, trace evidence, or pinned fixture directly. | Public VP8 handle/config in root; private encoder/decoder mechanics under `internal/vp8/encoder` and `internal/vp8/decoder`. |
| Root VP9 implementation | 73 root VP9 implementation/facade files remain; the VP9 speed-feature dispatcher is split by responsibility, and VP9 SVC layer-context state, spatial-SVC RTP access-unit packetization, Equator360 AQ helpers, variance/complexity AQ segmentation math, frame-context reset/commit/adaptation helpers, first-pass frame analysis, coefficient RD cost/metric helpers, public-quantizer/q-delta helpers, full-pel motion-search helpers, per-SB content/ARF buffer mechanics, partition geometry and partition/filter rate-cost helpers, CBR variance thresholds, ARNR temporal-filter windowing/search/prediction/filtering, and superframe index mechanics now live under `internal/vp9`; stale VP9 stderr debug hooks, unused non-RD filter and ARF-usage staging wrappers, the always-on non-RD staging predicate, and the root-private superframe adapter are removed. | Public VP9 handle/config in root; private encoder/decoder mechanics under `internal/vp9/encoder` and `internal/vp9/decoder`. |
| Root oracle process plumbing | VP8 direct `os/exec` test callers and the VP9 spatial-SVC sample runner have been moved behind coracle helpers. `internal/coracle` production code no longer imports `internal/testutil`; neutral IVF parsing/building lives in `internal/vpx/ivf`, and checksum-oracle JSON parsing lives in `internal/vpx/conformance`. VP9 root oracle tests no longer import `internal/coracle` or `internal/coracle/coracletest` directly; they use `internal/testutil/vp9test` for oracle gating, strict-mode env flags, and tool resolution, and `internal/testutil/vp9oracle` for reusable VP9 stream-parity helpers plus vpxenc option projection. VP9 corpus-required and minimum-file policy lives in `internal/testutil/vp9corpus`, not in root tests. VP8 root oracle tests no longer import `internal/coracle` or `internal/coracle/coracletest` directly; they use `internal/testutil/vp8test` for VP8 oracle gating across normal tests, strict threaded-oracle quarantine mode, tool resolution, trace projection/comparison, vpxenc/vpxdec calls, first-pass and two-pass captures, temporal-SVC sample runs, checksum subprocess runs, decoder checksum handles, and JSON baselines. Legacy string-path checksum wrappers and task-number bitrate env hooks are gone; `internal/coracle/coracletest` now keeps shared oracle enablement, tool resolution, and baseline helpers only under `govpx_oracle_trace`. | Keep subprocess and fixture mechanics in `internal/coracle`; root tests express behavior/parity only. |
| Root tests | 580 top-level root tests remain; many still build inside `package govpx` and are codec implementation or parity tests. Naming passes have converted VP8 behavior suites, VP8 oracle parity/match-rate suites, VP9 oracle/vpxenc/vpxdec suites, and the VP8/VP9 BD-rate quality gates from tracker-era labels into objective behavior names; root VP8/VP9 oracle test files no longer use `trace` filenames, and a hygiene test rejects that naming pattern. The shared BD-rate harness now exposes case observations instead of feature-trace APIs. Shared strided-plane append/equality helpers and first-byte-diff diagnostics now live in `internal/testutil`; VP8 external IVF corpus roots, benchmark corpus fallback, limits, source-clip parsing, and minimum rules now live in `internal/testutil/vp8corpus`; VP8 synthetic packet builders and BD-rate synthetic sources now live in `internal/testutil/vp8test`; shared VP9 YCbCr/I420/header helpers, compressed-header readers, rate-row packet enrichment, IVF payload extraction/building, packet byte-parity diagnostics, synthetic image fixtures, source generators, byte-level bit packing, show-existing/superframe packet builders, synthetic keyframe packet builders, skip/residue keyframe packet builders, column-residue keyframe packet builders, tile-start parsing, byte-parity counters, active-map fixtures, clamped I420 whole-pixel reference fixtures, vpxenc packet capture, vpxenc IVF capture, two-pass packet capture, frame-flags packet capture, frame-flags trace packet unpacking, copy-reference log capture, spatial-SVC sample packet capture, first-pass stats parsing/capture, vpxdec I420 decode wrappers, vpxdec WebM/invalid-IVF wrappers, vpxdec acceptance wrappers, panning sources, stream-parity row formatting, rate-trace row parsing/formatting, transition comparison stats, drop-aware stream parity summaries, single-stream parity report formatting, drop summaries, hidden-frame and alt-ref refresh counters, auto-alt-ref visibility formatting, and Q histograms now live in `internal/testutil/vp9test`; VP9 external corpus selection and minimum rules now live in `internal/testutil/vp9corpus`; VP9 oracle decode, DecodeInto, last-visible-frame, image-match, show-existing, SVC-style superframe, encoder-vpxdec I420 match, stream/copy-reference/lookahead mechanics, and vpxenc option projection live in `internal/testutil/vp9oracle`; public codec string tests, no-cgo/upstream/hygiene scans, split public stream-info coverage, VP8 and VP9 RTP fuzzers, public-only VP8 and VP9 decoder facade tests, VP8 facade tests for DecodeInto metadata, DecodeInto, valid-input, malformed-packet, error-concealment, and threaded decode fuzzers, last-frame controls, decryptor callbacks, reference controls, postprocess/noise behavior, size-limit errors, control-surface mapping, RTP roundtrips, encoder constructor/options validation, runtime-control fuzz seeds, scaling-mode emitted-header bits, auto-altref closed-state errors, temporal scalability option rejection, encoder quantizer metadata, public encoder allocation/benchmark guards, source-pixel/matching-reference inter-frame encode behavior, keyframe encode/decode behavior, public rate-control/CQ validation, reachable-target rate-control behavior, and runtime-control validation and packet effects, and VP9 facade tests for encoder constructor/options validation, encoder fuzz/runtime-control fuzzing, control-surface mapping, active-map exposure, byte-alignment behavior, header rejection, malformed multi-tile-prefix rejection, postprocess flags/noise/allocation behavior, postprocess DecodeInto behavior, deterministic postprocess noise, error concealment, Row-MT public decode/runtime/allocation/lifecycle behavior, loop-filter-opt control validation, decode-tile filter behavior/runtime clearing, public superframe packing/decoding, decoder fuzz dispatch, decryptor callbacks, external frame buffers, reference-frame controls, DecodeInto metadata, DecodeInto steady-state allocation, frame-info controls, show-existing decoder behavior, stream-info, public encoder keyframe controls, show-existing encoder controls, frame-parallel constructor/runtime/drain/lifecycle behavior, perceptual-AQ header behavior, rate-control-bound and GF-interval validation, public encoder tile-row allocation, IVF payload roundtrip, bench-parity allocation, public encoder-to-decoder roundtrips, synthetic-ramp realtime encode/decode, synthetic multi-tile keyframe decode, source-shape validation, closed-control errors, bitrate-limit validation, tuning validation, target-level validation, screen-content validation, worker cleanup, last-quantizer metadata, spatial-SVC controls, encode behavior, RTP packetization, reference controls, and allocation guards, and RTP roundtrips now run from `package govpx_test`; the largest VP9 spatial-SVC and VP9 rate-control suites are split into encode/control/runtime/RTP/allocation and rate-mode/state/realtime/CBR/runtime files; VP9 refs/scalability coverage is split into keyframe, temporal, spatial, closed-control, reference-flag, show-existing, and intra-only suites; VP9 keyframe decoder coverage is split into header validation, encoder roundtrip, show-existing, DecodeInto, frame-info/control, and keyframe reconstruction suites; VP9 inter-mode, inter-skip decoder, and tx/quant suites are split into objective residue, compound, motion-search, partition-scoring, reconstruction, DecodeInto, tx-mode, inter-tx, cyclic-refresh quantizer, keyframe quantizer, and lossless files; VP9 vpxdec integer-motion and lossless oracle coverage now runs from `package govpx_test` through public constructors instead of root private reference frames; VP9 vpxdec decoder-control oracle coverage now runs from `package govpx_test` through shared oracle helpers. VP9 vpxenc oracle coverage is split into keyframe, lookahead, inter, frame-flag, and helper files; VP9 stream parity coverage is split into selected cases, runtime-pinned cases, tile/thread behavior, realtime new-mode behavior, runtime controls, runtime matrices, resize, invisible-frame, and drop behavior; VP9 control-transition coverage is split into frame-flag, runtime-control, temporal-control, and helper files; VP8 stream oracle parity keeps byte-parity behavior in the root file while shared lifecycle helpers, reset/flush, two-pass, resize-basic, resize-control, resize-runtime-flag, and resize-helper coverage live in objective files. VP8 runtime-control, temporal, reference-flag, adaptive-keyframe, lookahead/preprocess, rate-control path, denoiser/active-map, reference-probability, golden/alt-ref, cyclic-refresh, split-RD, and BD-rate quality suites are split into objective behavior files. VP9 source-variance filter-search gating, reset-frame-context tests, cyclic-refresh state-machine tests, pure public-quantizer mapping tests, full-pel motion-search tests, coefficient RD-cost tests, partition geometry/rate-cost tests, and pure per-SB content/ARF buffer tests now live in `internal/vp9/encoder` instead of root same-package tests; VP9 superframe parser split/rejection tests and parser fuzzing now live in `internal/vp9/bitstream`; empty VP9 encoder/decoder test shells, duplicate root-only column-residue keyframe wrappers, and the root-only active-map oracle helper are gone. | Public facade tests remain in root; implementation tests move beside internal packages; reusable helpers move to `internal/testutil` or `internal/coracle`. |
| Shared helpers | `internal/vpx/rtp` owns shared RTP fragment packing, marker, assembly loops, descriptor allocation, and the common VPx PictureID wire value; `internal/vpx/ratecontrol` owns codec-neutral packet-size and percentage arithmetic; `internal/vpx/arith` owns integer saturation, checked multiplication, and coordinate clamps; `internal/vpx/buffers` owns alignment, typed slice length/zeroing helpers, capacity growth, strided plane copy/average, plane fill, I420 chroma dimensions, plane length, raw frame sizing, I420 plane serialization, and I420 encode-buffer sizing; `internal/vpx/ivf` owns IVF stream parsing/building; `internal/vpx/conformance` owns codec-neutral frame checksum JSON parsing; `internal/testutil` owns shared env integer parsing, corpus-minimum assertions, byte-diff diagnostics, and shared synthetic YCbCr fixtures used by parity and BD-rate tests; `internal/testutil/vp8test` owns VP8-specific BD-rate source fixtures; `internal/testutil/vp9test` owns VP9 active-map fixtures; `internal/testutil/vp9oracle` owns VP9 oracle stream/copy-reference/lookahead mechanics and vpxenc option projection; `internal/testutil/rtptest` owns codec-neutral RTP test mechanics while descriptor syntax stays codec-specific. Codec packages keep descriptor syntax, policy constants, and validation. | Add only mechanical shared helpers: RTP fragments, buffers, geometry, validation, arithmetic, and test harness utilities. |
| VP9 Profile 0 completion | VP9 Profile 0 support remains the highest-priority codec-completion lane. Existing proof includes default tests, synthetic pure-Go decoder/encoder coverage, tagged vpxdec/vpxenc oracle suites, strict official Profile 0 WebM/IVF parity gates, and byte-parity gates against pinned libvpx `v1.16.0`. A bounded strict Profile 0 WebM slice was re-run in this pass against the local pinned corpus. | Close remaining encoder/decoder feature and byte-parity gaps against libvpx before treating the tidy goal as done. Do not rely on status prose alone: each claimed gap closure needs a focused pure-Go test when possible, an oracle/parity test when behavior comes from libvpx, and the relevant validation command recorded. |
| Tracing/test hooks | Disabled trace state is build-tagged and has zero-size tests; unused VP9 ARNR and unsupported-decoder stderr env hooks are gone; VP9 internal libvpx source/blob oracle tests are tagged with `govpx_oracle_trace` and exercised by `make test-vp9-internal-oracle`, keeping default `go test ./...` free of libvpx checkout probes; the BD-rate command harness no longer imports `testing` in non-test files; root hygiene now rejects default-build production imports of test harness packages; VP8 coefficient-trace hot args keep zero-size disabled fields away from the final struct slot, and coefficient-trace constructors are only called under `govpx_oracle_trace`; VP8 PhaseStats option storage, helper methods, benchmark wiring, encode-attempt calls, motion-search stats setup, loop-filter trial routing, and reconstruction counters are guarded by `govpx_phase_stats` compile-time constants so default hot paths skip no-op helpers. Public VP8 encoder encode/control allocation guards now run from `package govpx_test`, keeping zero-allocation claims attached to the facade rather than private fields. | Keep disabled paths allocation-free and absent from production structs; expand allocation/escape checks when touching hot paths. |
| Documentation | `docs/architecture.md`, `docs/api.md`, `docs/codec-status.md`, `docs/validation.md`, and this map exist. | Keep README short; detailed docs under `docs/`; no migration promise before first release. |

### VP9 Root-Test Move Queue

- Keep true root facade/API tests in root: RTP facade, active maps, worker
  leaks, and small synthetic public encode/decode coverage. VP9 decoder
  byte-alignment, header-rejection, postprocess/error-concealment, Row-MT
  public decode/runtime/allocation/lifecycle behavior, loop-filter-opt validation,
  malformed multi-tile-prefix rejection,
  decode-tile filter behavior, public superframe packing/decoding, encoder and
  decoder fuzz dispatch, decryptor, external-frame-buffer, show-existing,
  frame-info, DecodeInto, corpus, stream-info, encoder keyframe controls,
  perceptual-AQ header behavior, rate-control-bound validation,
  spatial-SVC encode/RTP behavior, synthetic-ramp realtime encode/decode,
  synthetic multi-tile keyframe decode, and public encoder-to-decoder roundtrip
  facade coverage now runs from `package govpx_test`.
- Move oracle plumbing first: `vp9_*oracle*_test.go`,
  `vp9_oracle_*_test.go`, and VP9 parity-report/fuzz parity files should use
  `internal/testutil/vp9test` for harness mechanics and eventually live beside
  codec-owned internals once the private implementation moves.
- Extract shared VP9 fixtures next: decoder helper files, I420 fixtures, packet
  builders, and encoder test helpers belong in `internal/testutil/vp9test` or
  codec-specific internal test packages. VP9 synthetic keyframe packet builders
  used by tile/filter/header tests now live in `internal/testutil/vp9test`
  and are built through internal VP9 bitstream/encoder helpers instead of the
  root encoder's private state. Public VP9 option/control coverage for
  delta-Q-UV, color metadata, render size, and loop-filter disabling now runs
  from `package govpx_test` and checks emitted headers instead of private
  encoder fields. VP9 variance-partition NN table and inference tests now live
  in `internal/vp9/encoder`; the root ML partition file keeps only root-local
  private helper coverage pending the implementation move. VP9
  multi-resolution tests now run from `package govpx_test`; duplicate raw
  polyphase filter checks were removed from root because `internal/vp9/dsp`
  owns fuller resize coverage.
- Move pure implementation suites after the package boundary moves: coefficient
  costing, speed features, row workers, partition decisions, rate control,
  TPL/ARNR/AQ, loopfilter, decoder motion/context, and allocation contracts are
  implementation tests, not public root tests.
- Split mixed public/private files by behavior. Keep only user-visible public
  behavior in root and move assertions against private fields such as `e.rc`,
  `e.sf`, `d.miGrid`, parsed headers, or private `vp9*` methods under
  `internal/vp9`.

## Refresh Commands

```sh
find . -maxdepth 1 -type f -name '*.go' | wc -l
find . -maxdepth 1 -type f -name '*_test.go' | wc -l
find internal -type f -name '*.go' | wc -l
go list ./...
go doc -short .
wc -l $(git ls-files '*.go') | sort -nr | head -40
git ls-files '*_test.go' | awk '{ if ($0 ~ /^internal\//) internal++; else root++ } END { print root, internal }'
```
