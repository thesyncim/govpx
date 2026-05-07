# govpx parity plan

This file is intentionally small. The original project spec was useful for
bootstrapping, but the tree now contains the API, package layout, oracle tools,
and most baseline codec scaffolding. Treat this as the current roadmap for
closing the remaining gap to libvpx VP8 parity.

Frozen reference: libvpx v1.16.0.

## Rules

1. Correctness and libvpx parity come before performance.
2. No cgo in govpx.
3. No heap allocations after initialization in hot paths:
   `Decode`, `DecodeInto`, `NextFrame`, `EncodeInto`, runtime controls,
   `ForceKeyFrame`, `Reset`, and internal per-frame loops. Normal and temporal
   encode paths have explicit zero-allocation regression tests.
4. Port algorithms from libvpx source, but write idiomatic Go.
5. Every meaningful parity change lands with tests.
6. Public APIs return stable Go errors instead of panics.
7. SIMD, threading, and speed work wait until parity gates prove behavior.
8. Core package remains VP8-only: no VP9, AV1, WebM, or libvpx C API clone.

## Verification Gates

Run the automated gate before committing parity work:

```sh
make verify-production
```

That target builds optimized pinned libvpx tools, fetches the required libvpx
VP8 corpus/source data under ignored build directories, runs normal Go tests,
and runs oracle-backed `TestOracle*` checks.

For focused work, use the closest package tests first, then the full gate.
New bitstream features should add or extend oracle coverage.

## Current Baseline

Already established:

- Module/package/commands use `github.com/thesyncim/govpx`.
- Public decoder and encoder APIs exist.
- Pure-Go VP8 decoder and encoder paths exist.
- IVF/test helpers and libvpx oracle tooling exist.
- Makefile owns normal tests, oracle tests, optimized libvpx builds, and test
  corpus fetching.
- Encoder has basic realtime CBR, temporal layering controls, entropy refresh
  state, segmentation plumbing, token partitions, reconstruction, loop filter,
  SPLITMV emission across VP8 split partition shapes, rotating cyclic-refresh-style segmentation,
  and libvpx decode acceptance tests.
- Decoder has broad smoke/oracle coverage but is not production parity yet.

Use `UPSTREAM.md` for detailed subsystem status and known deviations.

## Remaining Parity Work

### 1. Conformance First

Goal: prove current behavior against libvpx before widening features.

- Expand required libvpx VP8 decoder corpus coverage.
- Require the full current libvpx v1.16.0 VP8 decoder IVF subset in the
  production oracle gate: 58 VP80 vectors plus 2 invalid rejection vectors.
- Keep invalid-stream rejection parity required in CI/oracle runs.
- Add generated streams for every feature edge before claiming support.
- For encoder validation, compare govpx and libvpx outputs through the same
  libvpx decode/checksum/PSNR/SSIM/bitrate gates; the production gate requires
  at least two external VP8 encoder source clips.

Useful references:

- `test/test-data.mk`
- `test/test_vector_test.cc`
- `test/vp8_fragments_test.cc`
- `test/vp8_ratectrl_rtc_test.cc`

### 2. Decoder Parity

Goal: remove unsupported-feature exits and match libvpx behavior on valid VP8.

High-priority gaps:

- Full error concealment for corrupt/truncated partitions; decoder API now has
  a libvpx-style `ErrorConcealment` option alias for the existing concealment path.
- Remaining postprocess tuning/corpus edges after granular deblock,
  demacroblock, add-noise, and MFQE controls.
- Multi-token-partition decode edge cases.
- Remaining profile/segmentation/loop-filter/header feature edges that still
  return `ErrUnsupportedFeature`.
- Resolution-change/reference-rescale behavior after scaler support exists.

Useful references:

- `vp8/decoder/error_concealment.c`
- `vp8/decoder/dboolhuff.c`
- `vp8/decoder/decodeframe.c`
- `vp8/decoder/onyxd_if.c`
- `vp8/common/postproc.c`
- `vpx_scale/yv12scaler.c`

### 3. Realtime Temporal SVC

Goal: make the realtime use case a first-class, tested configuration surface.

Remaining work:

- Expose missing libvpx-style temporal controls where they map cleanly to Go.
- Keep temporal pattern flags aligned with `vpx_temporal_svc_encoder.c`.
- Test layer sync, TL0PICIDX, reference refreshes, and entropy
  refresh/no-refresh per layer pattern; packet refresh/entropy flags now cover
  all libvpx example temporal patterns, and libvpx-style droppable encoded-frame
  metadata plus per-frame incremental/cumulative layer bitrate targets and
  per-layer buffer state are now reported; libvpx-style per-layer
  input/encoded/cumulative bit counters are reported with cumulative buffer
  updates for encoded and dropped frames.
- Add oracle-backed realtime/SVC encode validation clips; generated temporal
  streams now cover base-layer and full-sequence decode parity, and external
  libvpx temporal SVC example layer streams are decoded through the oracle gate.
- Verify per-layer buffer behavior against external libvpx oracle streams.

Useful references:

- `examples/vpx_temporal_svc_encoder.c`
- `vp8/encoder/onyx_if.c`
- `vp8/encoder/ratectrl.c`

### 4. Encoder Quality Parity

Goal: make govpx output look like libvpx VP8 before optimizing speed.

High-priority gaps:

- Full intra mode analysis, especially B_PRED 4x4 mode selection.
- Finish exact libvpx RD scoring; Q-derived lambda plus skip/reference/
  inter-mode/MV/token bit costs exist for current scalar analysis paths.
- Finish exact libvpx inter candidate pruning/costing; explicit ZEROMV,
  NEARESTMV, NEARMV, NEWMV, and SPLITMV RD candidates exist.
- Finish exact libvpx NEWMV search/pruning strategy; exhaustive full-pixel
  search plus libvpx-style iterative half/quarter-pel subpixel variance
  refinement already exists.
- Remaining SPLITMV libvpx RD/mode-cost parity and oracle coverage.
- Exact libvpx loop-filter level search; default-on filtering now uses the
  libvpx base-q initial level and keyframe sharpness reset.
- Token-partition writer coverage now spans neutral/zero/coefficient keyframes
  and zero/coefficient interframes across two/four/eight token partitions.

Useful references:

- `vp8/encoder/pickinter.c`
- `vp8/encoder/picklpf.c`
- `vp8/encoder/rdopt.c`
- `vp8/encoder/mcomp.c`
- `vp8/encoder/modecosts.c`
- `vp8/encoder/bitstream.c`

### 5. Encoder Rate Control And Segmentation

Goal: close the loop between measured frame size, quantizer choice, and
segment-aware decisions.

Remaining work:

- Complete exact libvpx cyclic/background refresh segmentation policy; rotating
  cyclic-refresh-style segment maps now use libvpx's default CBR/error-resilient
  enablement, temporal-layer MB cadence, screen-content cadence/disable rules,
  Q/2-Q ALT_Q boost, eligibility map, one-frame clean-block cooldown,
  count-derived segment tree probabilities, base temporal-layer gating, and
  screen-content inter-Q drop limiting.
- Make quantizer selection segment-aware.
- Implement libvpx CBR feedback more completely.
- Complete exact libvpx constrained-quality bitrate behavior for
  `RateControlCQ`; initial CQLevel quantizer floor/control and bounded
  overshoot feedback exist.
- Finish remaining one-pass CBR/golden-frame correction-factor branching; initial
  bits-per-macroblock quantizer regulation, libvpx frame-size bounds, and
  libvpx-style buffer-fullness target scaling, initialized/reset rolling bit
  monitors and correction factors, non-show-frame overhead accounting including
  temporal-layer buffers, negative CBR buffer-debt/drop threshold handling,
  temporal-layer frame-size bounds, runtime bitrate buffer preservation, and
  bounded feedback exist.
- Complete fixed-Q/two-pass keyframe target branches if those modes become
  production requirements; one-pass first and later keyframe target sizing now
  mirrors libvpx's buffer, framerate, Q-adjustment, and separation rules.
- Complete exact libvpx golden-frame CBR boost heuristics; GF-CBR
  target/refresh control exists and now uses libvpx's cyclic refresh cadence,
  default unboosted refresh, and prior LAST/ZEROMV majority gate.
- Implement VBR/two-pass planning if production parity requires VBR.
- Add adaptive keyframe/scene-cut behavior.
- Complete static-background segmentation policy; screen-content and
  static-threshold runtime controls exist.

Useful references:

- `vp8/encoder/ratectrl.c`
- `vp8/encoder/vp8_cyclic_refresh.c`
- `vp8/encoder/segmentation.c`
- `vp8/encoder/firstpass.c`
- `vp8/encoder/onyx_if.c`

### 6. Encoder Preprocessing

Goal: support the libvpx tools that quality/rate-control depends on.

Remaining work:

- Lookahead buffer.
- Alt-ref temporal filtering / ARNR.
- Spatial denoiser.
- Skin/static-region classification if needed for segmentation parity.

Useful references:

- `vp8/encoder/lookahead.c`
- `vp8/encoder/temporal_filter.c`
- `vp8/encoder/denoising.c`
- `vp8/encoder/vp8_skin_detection.c`

### 7. Performance Later

Do this only after parity gates are strong enough to catch regressions.

- DSP dispatch layer.
- amd64 SSE2 and arm64 NEON kernels.
- Decoder row threading.
- Encoder row threading.
- Motion-search speed-feature tuning.

Useful references:

- `vp8/common/rtcd_defs.pl`
- `vpx_dsp/vpx_dsp_rtcd_defs.pl`
- `vp8/common/x86/`
- `vp8/common/arm/`
- `vp8/encoder/x86/`
- `vpx_dsp/x86/`

## Execution Order

1. Strengthen conformance gates for the feature being touched.
2. Fix decoder unsupported-feature/error-concealment gaps.
3. Finish temporal SVC controls and oracle-backed realtime tests.
4. Port encoder RD/mode-decision and motion-search parity.
5. Port rate-control/segmentation behavior.
6. Add lookahead, ARNR, denoising, and related controls.
7. Only then start dispatch/SIMD/threading/performance work.

Every subgoal should end with:

```sh
make verify-production
git status --short
```

Update `UPSTREAM.md` when a subsystem changes status.
