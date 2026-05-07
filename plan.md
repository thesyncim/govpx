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

- Full exact intra mode analysis, especially B_PRED 4x4 selection.
- Exact libvpx RD scoring and mode-cost parity.
- Exact inter candidate pruning/costing.
- Exact NEWMV search/pruning parity.
- Remaining SPLITMV RD/mode-cost parity and oracle coverage.
- Exact loop-filter level search.

Primary references:
[encodeintra.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/encodeintra.c),
[pickinter.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/pickinter.c),
[rdopt.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/rdopt.c),
[mcomp.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/mcomp.c),
[modecosts.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/modecosts.c),
[picklpf.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/picklpf.c),
[bitstream.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/bitstream.c).

### Encoder Rate Control And Segmentation

- Exact cyclic/background refresh segmentation policy.
- Segment-aware quantizer selection.
- More complete CBR feedback behavior.
- Exact constrained-quality behavior.
- Remaining one-pass CBR and golden-frame correction-factor branches.
- Fixed-Q and exact two-pass allocation branches if those modes become
  production requirements.
- Exact static-background segmentation policy.

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
