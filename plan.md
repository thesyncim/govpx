# govpx VP8 parity tracker

Reference: libvpx v1.16.0. Scope: VP8 only, pure Go, no cgo, no VP9/AV1/WebM
muxing, and no libvpx C API clone.

## Gates

- Full parity/production gate: `make verify-production`
- Decoder-only proof gate: `make verify-decoder-parity`
- Focused work should add or extend oracle coverage before claiming support.
- Correctness and libvpx parity come before performance.
- Every safe point should end with `make verify-production` and
  `git status --short`.

Status details live in [UPSTREAM.md](UPSTREAM.md). Build/test wiring lives in
[Makefile](Makefile).

## Current Status

- Decoder: no known behavioral parity gap for the supported VP8 surface covered
  by `make verify-decoder-parity`.
- Encoder: functional and oracle-guarded for many paths, including opt-in
  lookahead, ARNR-style filtering, spatial/temporal denoising, first-pass stats,
  two-pass VBR targeting, and scene-cut keyframe placement. Exact libvpx
  quality/rate-control tuning parity is still open.
- Performance: intentionally deferred until parity gates are strong enough to
  catch regressions.

## Missing VP8 Features

### Encoder Quality

- Precomputed `vp8_init_mode_costs` `ModeCosts` table (refactor — current
  per-call tree walks are functionally equivalent, but the libvpx pattern
  precomputes once per frame).
- Faithful remaining motion-search branches: the `bestRefMV` centring,
  MV-cost ref, sub-pel `±MAX_FULL_PEL_VAL` reject, libvpx NSTEP
  `vp8_init3smotion_compensation` table, and realtime `CpuUsed > 4`
  `vp8_hex_search` path are in place; remaining gaps are exact improved MV
  predictor search-range adjustment, the alternate DIAMOND path, and SPLITMV
  integer-search pruning/details.
- Remaining SPLITMV RD/mode-cost parity and oracle coverage; libvpx
  compressor-speed partition ordering, 8x8-first pruning, and the
  `no_skip_block4x4_search` gate are in place, while per-subset
  LEFT/ABOVE/ZERO/NEW mode trials and predictor/step reuse remain open.
- Remaining loop-filter parity; previous filter-level carry, libvpx Q-based
  min/max clamps, fast/full trial-filter search, and partial-frame luma SSE
  scoring are in place, while mode/ref deltas, ALT_LF segmentation, and exact
  simple-filter/version behavior remain open.

Primary references:
[encodeintra.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/encodeintra.c),
[pickinter.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/pickinter.c),
[rdopt.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/rdopt.c),
[mcomp.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/mcomp.c),
[modecosts.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/modecosts.c),
[picklpf.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/picklpf.c),
[bitstream.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/bitstream.c).

### Encoder Rate Control And Segmentation

- Public 0..63 quantizers now map through libvpx `q_trans` into internal
  0..127 VP8 qindex before rate-control, segmentation, loop-filter, and packet
  writing; `EncodeResult.Quantizer` remains public-facing.
- Exact cyclic/background refresh segmentation policy.
- Segment-aware quantizer selection.
- More complete CBR feedback behavior.
- Exact constrained-quality behavior.
- Remaining one-pass CBR and golden-frame correction-factor branches.
- Fixed-Q and exact two-pass allocation branches if those modes become
  production requirements.
- Exact static-background segmentation policy.
- Cross-frame ref-frame probability tracking (`prob_intra_coded`,
  `prob_last_coded`, `prob_gf_coded`) is wired against
  `vp8_estimate_entropy_savings`'s default mode-count formula (63/128/128
  init); the keyframe-after-keyframe boost branch (`prob_intra_coded += 40`,
  `prob_last_coded = 200`, `prob_gf_coded = 1`) and `source_alt_ref_active`
  branch from `onyx_if.c` are still flat.

Primary references:
[ratectrl.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ratectrl.c),
[encodeframe.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/encodeframe.c),
[segmentation.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/segmentation.c),
[firstpass.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/firstpass.c),
[onyx_if.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/onyx_if.c).

### Encoder Preprocessing

- Tighten ARNR filter weights, alt-ref group placement, and denoiser
  mode-decision feedback against stricter libvpx oracle cases.
- Expand oracle coverage for lookahead/ARNR/denoise/two-pass configurations.

Primary references:
[lookahead.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/lookahead.c),
[temporal_filter.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/temporal_filter.c),
[denoising.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/denoising.c),
[vp8_skin_detection.c](internal/coracle/build/libvpx-v1.16.0/vp8/common/vp8_skin_detection.c).

### Realtime Temporal/SVC

- Expose remaining libvpx-style temporal controls where they map cleanly to Go.
- Tighten per-layer buffer behavior against external libvpx oracle streams.
- Keep temporal pattern flags aligned with the libvpx example encoder.

Primary references:
[vpx_temporal_svc_encoder.c](internal/coracle/build/libvpx-v1.16.0/examples/vpx_temporal_svc_encoder.c),
[onyx_if.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/onyx_if.c),
[ratectrl.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ratectrl.c).

### Performance After Parity

- DSP dispatch layer.
- amd64 SSE2 and arm64 NEON kernels.
- Decoder row threading.
- Encoder row threading.
- Motion-search speed-feature tuning.

Primary references:
[rtcd_defs.pl](internal/coracle/build/libvpx-v1.16.0/vp8/common/rtcd_defs.pl),
[vpx_dsp_rtcd_defs.pl](internal/coracle/build/libvpx-v1.16.0/vpx_dsp/vpx_dsp_rtcd_defs.pl),
[vp8/common/x86](internal/coracle/build/libvpx-v1.16.0/vp8/common/x86),
[vp8/common/arm](internal/coracle/build/libvpx-v1.16.0/vp8/common/arm),
[vp8/encoder/x86](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/x86),
[vpx_dsp/x86](internal/coracle/build/libvpx-v1.16.0/vpx_dsp/x86).

## Execution Order

1. Keep decoder parity green with `make verify-decoder-parity`.
2. Finish realtime/SVC controls and oracle-backed layer-buffer parity.
3. Port encoder RD/mode-decision and motion-search parity.
4. Port rate-control and segmentation behavior.
5. Tighten lookahead, ARNR, denoising, and two-pass behavior against stricter
   oracle cases.
6. Only then start dispatch/SIMD/threading/performance work.
