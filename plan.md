# govpx VP8 parity tracker

Reference: libvpx v1.16.0. Scope: VP8 only, pure Go, no cgo, no VP9/AV1/WebM
muxing, and no libvpx C API clone.

## Gates

- Full parity/production gate: `make verify-production`
- Decoder-only proof gate: `make verify-decoder-parity`
- Focused work should add or extend oracle coverage before claiming support.
- Correctness and libvpx quality parity come before performance.
- Encoder parity means libvpx-equivalent visible quality, rate behavior,
  reference policy, mode/MV choices, and residual decisions. Do not chase
  byte-for-byte identity in paths that do not affect quality, decoder-visible
  output, future encoder decisions, or oracle diagnosis.
- Treat "100% parity" as quality/rate and quality-relevant decision
  equivalence, not universal bit-exactness.
- Future agent handoffs should state that percentages are quality-equivalence
  estimates. Bit exactness is a tool for proving important paths, not the
  product target by itself.
- Bit-exact output is still required where deterministic paths make it the
  right proof, especially packet validity, frame headers, reference
  refresh/copy/sign-bias bits, decoder MD5s, and low-level entropy writers.
- This is still pre-release encoder work: internal helper signatures should
  follow the current parity model directly. Do not carry legacy compatibility
  wrappers for older internal call shapes.
- Every safe point should end with `make verify-production` and
  `git status --short`.

Status details live in [UPSTREAM.md](UPSTREAM.md). The detailed checklist for
100% encoder decision parity lives in
[docs/vp8_encoder_parity.md](docs/vp8_encoder_parity.md). Build/test wiring
lives in [Makefile](Makefile).

## Current Status

- Decoder: no known behavioral parity gap for the supported VP8 surface covered
  by `make verify-decoder-parity`.
- Encoder: functional and oracle-guarded for many paths, including opt-in
  lookahead, ARNR-style filtering, spatial/temporal denoising, first-pass stats,
  two-pass VBR targeting, pre-analysis scene-cut keyframe placement, and
  libvpx post-inter auto-key recode for opt-in one-pass non-realtime encodes.
  Estimate ~74% overall, ~84% on the core one-pass quality path
  (quality/rate-equivalence estimates, not bit-exactness percentages).
  See [docs/vp8_encoder_parity.md](docs/vp8_encoder_parity.md) for the
  per-area checklist.
- Reconstruction byte-identity, 2026-05-08 (measured by capturing both
  oracle traces and diffing the projected-out
  `y_adler32`/`u_adler32`/`v_adler32`/`size_bytes` fields):
  - 64x64 panning fixture, realtime CBR CpuUsed 0/4/8 and good CpuUsed 5:
    y/u/v Adler32 byte-identical on every frame; per-frame size delta in
    -0.03..+0.77%; Q matches.
  - 128x128 panning realtime CBR CpuUsed 8: keyframe byte-identical
    (size delta -0.07%); inter frames 1+ diverge with **Q drift** (govpx
    picks Q=5..7 vs libvpx Q=13..14 at the same min-q/max-q bounds)
    producing size deltas of +23..+44%. The Q divergence — not chroma
    sixtap rounding alone — dominates the inter-frame gap. Closing this
    needs Q-regulation parity on inter frames at this resolution before
    per-pixel reconstruction diff is meaningful.
- Performance: intentionally deferred until parity gates are strong enough to
  catch regressions.

## Missing VP8 Features

### Encoder Quality

- Inter-frame chroma sub-pel filter rounding: at sizes >64x64 (96x96,
  128x128, 160x96 verified), the realtime-CBR inter Y/U/V Adler32 still
  differ from libvpx even though Q now matches and per-frame size delta
  is 0.5..1.5%. Decoded per-pixel deltas peak at 4 (Y) / 3 (U) / 1 (V)
  with mean magnitude < 0.04 - quality-equivalent (PSNR vs libvpx > 60
  dB) but breaks strict byte-identity. Subagent localized to
  `internal/vp8/dsp/subpixel.go sixTapPredict` outputs at MB(0,0) row 0
  cols 3 / 7 (govpx 118/137 vs libvpx 117/139); the H/V tap math
  matches libvpx C reference, so the residual disagreement most likely
  lives in a sub-pel rounding edge case the test corpus exercises.
  Closing this needs a per-pixel libvpx-side predictor dump
  (patch `build_vpxenc_oracle.sh` to capture `xd->predictor` after
  `vp8_build_inter_predictors_mb`).
- Precomputed `vp8_init_mode_costs` `ModeCosts` table (refactor; per-call
  tree walks are functionally equivalent).
- Intra/Quant/Tokens: SSIM-gated activity tuning and oracle token-cost
  anchors remain. Exhaustive small-block oracle for qcoeff/dqcoeff/EOB
  parity is open.
- Motion search: improved-MV libvpx-side comparator and candidate-level
  rate attribution remain — without them the recode-loop
  `projected_frame_size` keeps a 64-bit oracle tolerance.
- SPLITMV RD: token-context commit parity, transform/quant token segment
  RD inside label selection, and oracle-backed label-level RD are open.
- Loop filter: ALT_LF segmentation and VP8 version 1-3 behavior remain.

Primary references:
[encodeintra.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/encodeintra.c),
[pickinter.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/pickinter.c),
[rdopt.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/rdopt.c),
[mcomp.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/mcomp.c),
[modecosts.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/modecosts.c),
[picklpf.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/picklpf.c),
[bitstream.c](internal/coracle/build/libvpx-v1.16.0/vp8/encoder/bitstream.c).

### Encoder Rate Control And Segmentation

- One-pass CBR and golden-frame correction-factor branches.
- Exact constrained-quality behavior.
- Exact cyclic/background refresh segmentation policy and segment-aware
  quantizer selection.
- Exact static-background segmentation policy.
- Fixed-Q and exact two-pass allocation branches if those modes become
  production requirements.
- First-pass section stats and external/two-pass `.fpf` oracle coverage
  beyond the deterministic ramp + Y4M-shaped corpus.
- Broader mode-cost caching, exact per-frame mode-table setup, and
  current-prob oracle coverage.

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
