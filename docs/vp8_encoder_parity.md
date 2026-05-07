# VP8 Encoder Libvpx Parity Checklist

Reference target: libvpx v1.16.0 VP8 encoder.

This project is not released. When existing govpx behavior conflicts with
empirical libvpx behavior, remove the old behavior instead of preserving a
compatibility path. If existing govpx logic already matches libvpx, keep it as
the anchor and look for the surrounding mismatch.

## What 100% Means

- Bitstream parity where deterministic settings allow it: matching frame
  headers, mode grids, tokens, partitions, reference updates, and decoded MD5s.
- Decision parity where bitstream identity is not practical: matching frame Q,
  flags, probabilities, reference checksums, per-MB mode/ref/MV/skip/segment,
  residual EOBs, rate, distortion, and RD decisions.
- Quality parity is the final smoke test, not the definition of done. PSNR/SSIM
  gaps should tighten only after the decision traces match.

## Validation Harness

- [ ] Add a per-frame and per-MB encoder oracle trace mode.
  - govpx: current oracle tests in
    [`oracle_encoder_validation_test.go`](../oracle_encoder_validation_test.go)
    and mode tests in
    [`encoder_reconstruct_test.go`](../encoder_reconstruct_test.go).
  - libvpx: instrument `encodeframe.c`, `pickinter.c`, `rdopt.c`,
    `ratectrl.c`, `onyx_if.c`, and `bitstream.c`.
  - Done when comparable JSON/CSV rows expose frame state, rate-control state,
    per-MB mode decision, residual decision, probabilities, segmentation, loop
    filter, and reference updates.

## Encode Driver, Recode, And Q Bounds

- [ ] Port libvpx `encode_frame_to_data_rate` recode-loop semantics.
  - govpx:
    [`encodeKeyFrameWithQuantizerFeedback`](../encoder.go),
    [`encodeInterFrameWithQuantizerFeedback`](../encoder.go),
    [`frameSizeFeedbackQuantizerWithContext`](../ratecontrol.go).
  - libvpx:
    [`onyx_if.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/onyx_if.c)
    `encode_frame_to_data_rate`.
  - Status: partial. govpx has a two-attempt size feedback loop; libvpx carries
    active best/worst Q, frame-size bounds, coding-context save/restore,
    projected entropy savings, and richer recode decisions.
  - Done when oracle traces match Q attempts, final Q, recode reasons, frame
    size bounds, and encoded bytes across CBR/VBR/CQ/key/golden/alt-ref frames.

- [ ] Align active best/worst quantizer selection.
  - govpx: [`selectQuantizerForFrameKindWithScreenContent`](../ratecontrol.go).
  - libvpx: `vp8_regulate_q`, active-best-quality, and active-worst-quality
    branches in `onyx_if.c`.
  - Status: partial. govpx estimates Q directly from target bits and correction
    factor; libvpx constrains Q through active quality bands, buffer fullness,
    frame class, CQ floor, and early-frame dampening.
  - Done when table-driven oracle tests match active best/worst Q and chosen Q
    for first frames, low/full buffer, key, GF, ARF, CQ, CBR, and screen
    content cases.

## Rate Control And Reference Policy

- [ ] Port full one-pass golden-frame boost and interval logic.
  - govpx:
    [`shouldRefreshGoldenFrameCBR`](../encoder.go),
    [`goldenFrameCBRInterval`](../encoder.go),
    [`beginFrameWithTargetAndContext`](../ratecontrol.go).
  - libvpx:
    [`ratectrl.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ratectrl.c)
    `calc_gf_params`, `calc_pframe_target_size`, and GF/ARF update branches in
    `onyx_if.c`.
  - Status: partial. govpx uses a simplified CBR refresh heuristic; libvpx
    tracks GF active count, recent ref usage, boost tables, overspend recovery,
    and interval updates.
  - Done when sequence tests match `refresh_golden_frame`, GF interval,
    `last_boost`, `gf_overspend_bits`, `non_gf_bitrate_adjustment`, and frame
    targets on motion/static clips.

- [ ] Implement reference alias and copy-buffer policy.
  - govpx:
    [`InterFrameStateConfig`](../internal/vp8/encoder/interframe.go) copy
    fields and reference refresh helpers in [`encoder.go`](../encoder.go).
  - libvpx: copy old GF to ARF, `gold_is_last`, `alt_is_last`,
    `gold_is_alt`, and refresh/copy header bits in `onyx_if.c` and
    `bitstream.c`.
  - Status: partial. govpx can write copy fields, but normal policy does not
    mirror libvpx alias state and availability.
  - Done when forced and natural GF/ARF sequences match header copy bits,
    reference checksums, reference availability, and subsequent mode choices.

## First Pass And Two Pass

- [ ] Replace simplified first-pass stats with libvpx first-pass analysis.
  - govpx:
    [`CollectFirstPassStats`](../encoder_firstpass.go),
    [`computeFirstPassStats`](../encoder_firstpass.go).
  - libvpx:
    [`firstpass.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/firstpass.c).
  - Status: partial. govpx computes coarse zero-MV/intra errors; libvpx records
    motion vectors, motion percentages, second-ref use, neutral counts,
    intra/coded/SSIM-weighted error, and section accumulators.
  - Done when fixed Y4M corpus stats match libvpx within defined tolerances for
    every field.

- [ ] Port second-pass KF/GF group allocation and VBR section limits.
  - govpx: [`twoPassState`](../encoder_firstpass.go).
  - libvpx: second-pass helpers in `firstpass.c` and `Pass2Encode` in
    `onyx_if.c`.
  - Status: partial. govpx distributes bits by per-frame modified error only.
  - Done when second-pass oracle tests match frame type, GF/ARF decisions,
    target bits, final Q, and bitrate distribution on multi-scene clips.

## Alt-Ref, Lookahead, And ARNR

- [ ] Implement automatic alternate-reference scheduling.
  - govpx:
    [`initLookahead`](../encoder_preprocess.go),
    [`encodeSourceInto`](../encoder.go),
    [`encodeInterFrameAttempt`](../encoder.go).
  - libvpx: ARF decision logic in `vp8_get_compressed_data` and ARF pending
    policy in `ratectrl.c`.
  - Status: missing/partial. govpx supports explicit invisible/force ARF flags
    but not libvpx hidden-ARF insertion and later show-frame handling.
  - Done when hidden/show cadence, timestamps, refresh flags, and decoded output
    match libvpx with alternate-reference enabled.

- [ ] Replace simplified ARNR with libvpx motion-compensated temporal filter.
  - govpx: [`applyARNRFilter`](../encoder_preprocess.go).
  - libvpx:
    [`temporal_filter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/temporal_filter.c).
  - Status: partial. govpx blends colocated pixels; libvpx searches matching
    macroblocks and weights by frame distance and prediction error.
  - Done when ARF buffer MD5s and final ARF frame bitstreams match for
    backward/forward/centered ARNR settings.

## Inter Mode Decision And Motion Search

- [ ] Complete full RD inter-mode loop parity.
  - govpx:
    [`selectInterFrameModeDecision`](../encoder_reconstruct.go),
    [`selectBestInterFrameMode`](../encoder_reconstruct.go).
  - libvpx:
    [`rdopt.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/rdopt.c)
    `vp8_rd_pick_inter_mode` and
    [`pickinter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/pickinter.c)
    `vp8_pick_inter_mode`.
  - Status: partial. Fast non-RD mode-loop order and cheap realtime scoring are
    aligned, but full RD still needs dynamic mode thresholds, hit counts,
    threshold adaptation, exact per-mode accounting, active-map handling, and
    recode-loop interactions.
  - Done when per-MB traces match tested mode order, skipped modes, selected
    mode/ref/MV, rate, distortion, RD, skip flag, and threshold updates across
    best/good/realtime speeds.

- [ ] Finish improved-MV predictor parity.
  - govpx:
    [`improvedInterFrameSearchStart`](../encoder_reconstruct.go),
    [`interAnalysisSearchConfig`](../encoder_reconstruct.go).
  - libvpx:
    `vp8_mv_pred` and `vp8_cal_sad` in `rdopt.c`.
  - Status: partial. Current-frame SAD ordering, previous inter-frame mode/MV
    grid, and libvpx realtime gate are present. Remaining work is sign-bias
    parity, exact border-mode-info indexing, and oracle traces for `near_sadidx`,
    predictor MV, and `sr`.
  - Done when panning, alternating-reference, dropped-frame, and all-quality
    clips match libvpx predictor MV, search range, and final NEWMV choices.

- [ ] Finish SPLITMV RD parity.
  - govpx:
    [`selectInterFrameSplitMotionMode`](../encoder_reconstruct.go).
  - libvpx: `rd_check_segment` and `vp8_rd_pick_best_mbsegmentation` in
    `rdopt.c`.
  - Status: partial. Partition order/pruning is aligned; per-subset
    LEFT/ABOVE/ZERO/NEW trials, predictor reuse, label entropy contexts, and
    selected 4x4 EOB storage remain open.
  - Done when partition, subblock modes/MVs, label rates, distortion, EOBs, and
    final MB RD match libvpx.

## Intra, Quantization, And Tokens

- [ ] Align key-frame and inter-intra pickers with libvpx RD code.
  - govpx:
    [`buildReconstructingKeyFrameCoefficients`](../encoder_reconstruct.go),
    [`predictBestInterIntraModeCost`](../encoder_reconstruct.go),
    [`predictBestBPredLumaModeRD`](../encoder_reconstruct.go).
  - libvpx: `vp8_pick_intra_mode`, RD intra pickers, and `encodeintra.c`.
  - Status: partial. Remaining gaps include exact thresholds, activity/tuning
    hooks, predictor edge setup, and per-block bailout behavior.
  - Done when key-frame per-MB traces match Y mode, UV mode, B modes,
    coefficient EOBs, rate, distortion, and reconstructed pixels.

- [ ] Audit quantization and coefficient optimization against libvpx.
  - govpx:
    [`quantizeOptimizedBlock`](../encoder_reconstruct.go),
    [`internal/vp8/encoder/quant.go`](../internal/vp8/encoder/quant.go).
  - libvpx: `encodemb.c` and `vp8_quantize.c`.
  - Status: partial. Several optimized-block paths are tested, but full parity
    needs every zbin/round/quant/dequant path, trellis decision, EOB handling,
    and Y2/Y1/UV context behavior.
  - Done when exhaustive small-block oracle tests match qcoeff, dqcoeff, EOB,
    token rate, and reconstruction across Q, block type, context, skipDC, zbin
    boosts, and coefficient patterns.

## Segmentation, Cyclic Refresh, Skin, And Active Maps

- [ ] Port cyclic background refresh exactly.
  - govpx:
    [`cyclicRefreshSegmentationConfig`](../encoder_segmentation.go),
    [`assignInterFrameStaticSegments`](../encoder_segmentation.go).
  - libvpx: cyclic refresh setup in `onyx_if.c` and `segmentation.c`.
  - Status: partial. govpx has alt-Q segment progression; libvpx has richer
    state, force-max-Q gating, screen-content branches, feature lifetimes, and
    exact map updates.
  - Done when per-frame segment map, feature data, tree probabilities, and
    segment IDs match for CBR, error-resilient, temporal, and screen-content
    modes.

- [ ] Port skin map and dot-artifact bias.
  - govpx:
    [`computeSkinMap`](../encoder_segmentation.go),
    [`classifyStaticSegmentationBlocks`](../encoder_segmentation.go).
  - libvpx: dot artifact logic in `pickinter.c` and skin detection in
    `common/vp8_skin_detection.c`.
  - Status: partial/missing.
  - Done when per-MB skin/dot flags and resulting RD adjustments match on face,
    noisy-flat, and screen-dot patterns.

- [ ] Implement active-map behavior.
  - govpx: missing public/internal active-map path.
  - libvpx: inactive MB early exit in `pickinter.c` and `vp8_set_active_map` in
    `onyx_if.c`.
  - Status: missing.
  - Done when inactive macroblocks skip mode decision, code as skipped, preserve
    pixels/references, and match active-map oracle vectors.

## Denoising And Noise-Sensitive Decisions

- [ ] Replace non-libvpx denoising behavior.
  - govpx:
    [`applySpatialDenoiser`](../encoder_preprocess.go),
    [`temporalDenoisePlane`](../encoder_preprocess.go).
  - libvpx:
    [`denoising.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/denoising.c)
    plus denoiser re-evaluation in `pickinter.c`.
  - Status: partial/non-libvpx. govpx smooths spatially/temporally; libvpx uses
    motion-compensated running averages, adaptive/aggressive modes, per-MB
    re-evaluation, ZEROMV denoise decisions, and noise sampling.
  - Done when denoised buffers, selected modes after denoiser re-evaluation, and
    final quality/rate match for `noise_sensitivity` 1-4.

## Probability, Entropy, And Header State

- [ ] Audit probability updates under all entropy-refresh cases.
  - govpx:
    [`commitInterFrameEntropy`](../encoder.go),
    [`BuildInterCoefficientProbabilityUpdates`](../internal/vp8/encoder/probability.go),
    [`adaptInterFrameModeProbabilitiesWithMVBase`](../internal/vp8/encoder/interframe.go).
  - libvpx: `vp8_estimate_entropy_savings`, `vp8_update_coef_probs`, and
    error-resilient entropy branches in `bitstream.c` and `onyx_if.c`.
  - Status: partial. Live coefficient/ref/MV work exists, but full parity needs
    refresh/no-refresh save-restore behavior, projected entropy savings in
    recode decisions, zero-reference edge cases, and temporal-layer
    interactions.
  - Done when every frame matches coefficient probs, MV probs, ref probs,
    refresh entropy bit, projected entropy savings, and next-frame mode-cost
    inputs.

