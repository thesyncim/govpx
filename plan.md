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
  - 128x128 panning realtime CBR CpuUsed 8 (round 2, post-recode-loop
    parity gate): per-MB predictor + post-residual dumps confirm that
    every MB of frame 1 reconstructs byte-identically to libvpx (Y/U/V,
    all 64 MBs). Frame 2+ predictor diffs cascade from a **loop-filter
    level fast-picker divergence** (govpx LF=11 vs libvpx LF=5 for
    frame 1, identical q=16 and identical clamped seed); the chroma
    sixtap math itself is correct.
  - 1280x720 realtime CBR CpuUsed 8, R11-M token-rate audit
    (2026-05-09, parity-close-r11-m-token-rate, see
    `diag_r11_m_token_rate_test.go`): at matched Q (frames 1-19 of the
    panning fixture, both encoders pin q_index=106 / public Q=56),
    per-frame size_bytes ratio is 0.998..1.005 and per-MB EOB sums match
    exactly (frame 1: gov_eob_sum=lib_eob_sum=136595). The four
    R11-M hypotheses (band-zero-bit-cost lookup, coef-prob context
    selection, excessive non-zero coeffs from quantize, ZeroToken vs
    token-tree divergence) would all manifest as byte-ratio drift even
    at matched Q -- they don't. The cmd/govpx-bench harness's
    avg_interframe_bytes ratio of ~1.30-1.40x against libvpx is driven
    by **mode-decision divergence under wall-clock autoSpeed
    adaptation** (vp8_auto_select_speed evolves cpi->Speed based on
    avgPickModeTime / avgEncodeTime, and govpx's converged value
    diverges from libvpx because per-frame timing varies between cold
    and warm cache, producing different mode picks), not by
    coefficient-token rate. Capturing the trace itself inflates
    wall-clock timing and pushes autoSpeed away from the bench-measured
    trajectory, so per-MB qcoeff in the trace is biased; bench
    measurements are the authoritative ground truth for byte-ratio.
    Side-effect: tightened Reset() to mirror libvpx's
    vp8_create_compressor calloc-zero of cpi->Speed /
    avg_pick_mode_time / avg_encode_time so a sequence re-init does not
    leak warmed adaptive Speed into a "fresh" pass (preserves all 16
    scoreboards green and verify-production).
- Performance: intentionally deferred until parity gates are strong enough to
  catch regressions.

## Missing VP8 Features

### Encoder Quality

- Inter-frame chroma sub-pel filter rounding: tracked by
  [`TestOracleChromaSubpelScoreboard`](oracle_chroma_subpel_scoreboard_test.go)
  with a per-fixture baseline at
  [`testdata/chroma_subpel_scoreboard_baseline.json`](testdata/chroma_subpel_scoreboard_baseline.json).
  **Closed 2026-05-08 (round 3)**: the dominant root cause turned out
  to be the libvpx full loop-filter picker's bias scaling. libvpx
  unconditionally scales `Bias = (best_err >> (15 - filt_mid/8)) *
  filter_step` by `section_intra_rating / 20` whenever
  `section_intra_rating < 20`, and because `cpi->twopass` is calloc'd
  and `section_intra_rating` is never written in one-pass / realtime /
  CBR, the scaling forces `Bias = 0` every iteration. govpx's
  `loopFilterFullPickerBias` previously omitted the scaling and used
  the unscaled bias, which caused the full picker to converge on a
  different `filt_best` than libvpx whenever multiple trials scored
  within the bias delta of `best_err` (e.g. govpx LF=2/1 vs libvpx
  LF=8/4 on frames 2/3 of the 128x128 panning fixture). Closing the
  bias scaling collapses the entire downstream cascade through the
  LAST reference. Mirrored libvpx's behaviour by piping
  `twoPassState.sectionIntraRating` (defaults to 0 like libvpx's
  calloc) into `loopFilterFullPickerBias`. Effect on the scoreboard:
  - 160x96 realtime CBR cpu8: 3/3/3 -> 0/0/0 (every inter frame
    byte-identical Y/U/V).
  - 96x96 realtime CBR cpu8: Y 3 -> 0; U/V remain 3 but
    max_inter_size_pct_abs falls 25.72% -> 0.115% and the inter Q
    drift collapses (govpx now matches libvpx q every frame).
  - 128x128 realtime CBR cpu8: Y/U/V counts unchanged, but
    max_inter_size_pct_abs falls 1.42% -> 1.11%; per-MB predictor diff
    confirms the residual is right-edge chroma (cols 6-7) on MB rows
    1..5, not the LF picker. The LF picker per-trial-level eval order
    now matches libvpx exactly on every frame
    (TestOracleLFTrialDiag).

  The remaining 96x96 / 128x128 chroma U/V residual lives in chroma
  subpel rounding on right-edge MBs at specific MVs; per-pixel
  `last_ref_window` bytes (including border extension) are
  byte-identical between encoders, so the divergence is downstream of
  the reference plane in the chroma predictor or in the per-MB mode
  decisions for cols 6-7 from MB row 1+. Diagnostic harness lives in
  [`oracle_chroma_subpel_predictor_diag_test.go`](oracle_chroma_subpel_predictor_diag_test.go)
  (gate `GOVPX_DEBUG=1`, optional `GOVPX_DEBUG_ALL_ROWS=1`).
- R11-J localizer (2026-05-08): a focused diag harness in
  [`diag_r11_j_pre_lf_recon_test.go`](diag_r11_j_pre_lf_recon_test.go)
  drives the chroma-subpel scoreboard's exact option set (no buffer
  overrides -> libvpx defaults) for the 128x128 panning fixture and
  walks per-MB mb / predictor / reconstructed rows on frame 1. Findings
  for that config (q=14 on both sides, govpx LF=5 vs libvpx LF=10):
  - **First divergent reconstructed MB: MB(2,7) Y pos 200** (16x16
    offset 12,8 = block 14 row 0); 4 divergent MBs total on frame 1,
    all col-7 B_PRED (MB(2,7), (3,7), (4,7), (5,7)) — Y/U/V all diverge
    by 32-55 of 256 / 38-64 of 64 bytes per plane.
  - mode matches (both INTRA_FRAME B_PRED) but residual MV state
    diverges (govpx (0,0) vs libvpx (6..8,16)) — for intra B_PRED the
    MV doesn't drive prediction but the per-4x4 sub-mode picks evidently
    do.
  - qcoeff diverges on every MB (mostly chroma AC -1/+1 magnitudes that
    govpx keeps but libvpx zeros); pre-LF reconstructed Y on the other
    60 inter MBs is byte-identical.
  - Frame 1 LF fast-picker diverges: govpx pre:0 SSE=29350 vs libvpx
    pre:0 SSE=29467 (delta=117 over the 4 col-7 B_PRED MBs); chosen
    LF=5 vs 10 cascades into the y_adler/u_adler/v_adler mismatch the
    scoreboard counts.
  - The buf-override config (q=16, LF=5 on both sides) shows the col-7
    B_PRED Y plane MATCH and only U/V DIFFER, so the Y-plane B_PRED
    divergence is q-state dependent and only surfaces under the
    scoreboard config.
  Next step is to surface b_modes for inter B_PRED on both encoder
  oracle and govpx trace so the per-4x4 sub-mode picks can be diffed
  directly. The reconstruction emit was widened to include intra B_PRED
  inter MBs (encoder_reconstruct.go) so the harness no longer silently
  skips them.
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
