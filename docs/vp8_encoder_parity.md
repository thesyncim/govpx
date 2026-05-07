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

## Current Estimate

- Production validity: high. `make verify-production` passes against pinned
  libvpx v1.16.0 tools and corpus minima.
- Quality smoke parity: high on the current tiny corpus, but not complete. The
  current oracle cases are near-equal on SSIM, while motion still shows a max
  frame PSNR gap around 1.4 dB and a large bitrate delta. Current smoke numbers:
  motion govpx/libvpx PSNR 49.87/50.35, bitrate 357.9/268.7 kbps; static
  govpx/libvpx PSNR 49.84/49.71, bitrate 376.6/372.3 kbps; realtime panning
  govpx/libvpx PSNR 48.03/48.07, bitrate 308.0/304.6 kbps.
- Encoder decision parity: roughly 45-55% complete. This is an engineering
  estimate, not a measured percentage, because govpx still lacks the trace
  oracle needed to count matching frame/MB decisions.
- The largest remaining parity weight is in recode/rate control, full RD mode
  decision, quant/token optimization, first/two-pass planning, ARF/ARNR, and
  denoiser-driven mode re-evaluation.

## Acceptance Gates

- [ ] `make verify-production` must pass with pinned libvpx v1.16.0 tools and
  required decode/encode corpus minima.
- [ ] Quality/rate/checksum oracle tests remain smoke gates only; encoder
  parity requires trace comparison for headers, entropy state, segmentation
  state, reference updates, and per-MB decisions.
- [ ] Deterministic real-time/no-lag cases must match libvpx bitstream headers,
  partition sizes, frame flags, reference refresh/copy/sign-bias bits, decoded
  MD5s, and trace state.
- [ ] Non-bitexact cases must match decision traces within documented
  tolerances for rate-control attempts, recode reasons, Q choices, entropy
  save/restore, mode/ref/MV choices, segmentation IDs, loop filter, and token
  probabilities.

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
- [ ] Extend the C oracle beyond decode-MD5 JSON.
  - Status: missing. Current oracle validation checks decoder checksums,
    quality, bitrate, and feature smoke coverage; it does not compare encoder
    decisions.
  - Done when the comparator fails CI on the first divergent frame, MB, header,
    probability, reference, or segmentation field and prints enough state to
    identify govpx, libvpx instrumentation, or harness-config mismatches.

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
  - Missing: `recode_loop_test`, `q_low/q_high`, `zbin_over_quant`,
    `active_worst_qchanged`, forced/auto key-frame recodes, entropy
    projected-size decisions, and coding-context restore after failed attempts.
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
  - Missing: `vp8_pick_frame_size`, post-pack rate-control bookkeeping,
    `kf_overspend_bits`, `kf_bitrate_adjustment`, `inter_frame_target`,
    `min_frame_bandwidth`, temporal-layer propagation, and GF overspend
    recovery.
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
  - Status: partial. govpx now writes the internal CBR old-GF-to-ARF copy,
    applies copy-buffer state locally before refresh, tracks libvpx-style
    `gold_is_last` / `alt_is_last` / `gold_is_alt` flags, prunes aliased
    references from availability/mode search, and prices constrained
    single-reference alias states through the libvpx special cases. Encoder
    packet configs now reject invalid copy selectors and copy-to-reference
    state when that reference is refreshed. Remaining work is ARF/two-pass
    copy-buffer edge cases, sign-bias policy, and trace coverage.
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
  - Missing: raw zero-motion checks, last/golden first-pass motion searches,
    `simple_weight` SSIM weighting, MV accumulation/variance, first-pass
    reference swap/GF copy behavior, and terminal total-stats packet.
  - Done when fixed Y4M corpus stats match libvpx within defined tolerances for
    every field.

- [ ] Port second-pass KF/GF group allocation and VBR section limits.
  - govpx: [`twoPassState`](../encoder_firstpass.go).
  - libvpx: second-pass helpers in `firstpass.c` and `Pass2Encode` in
    `onyx_if.c`.
  - Status: partial. govpx distributes bits by per-frame modified error only.
  - Missing: `frames_to_key`, KF/GF group bits/error, `gf_bits`,
    `alt_extra_bits`, section max-Q factor, active worst-Q estimates, VBR
    min/max section limits, CBR buffer adjustments, and ARF pending decisions.
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
  - Missing: `source_alt_ref_pending`, `source_alt_ref_active`,
    `alt_ref_source`, `is_src_frame_alt_ref`, hidden-frame insertion from future
    lookahead, later source-frame show handling, and altref sign-bias/reference
    state updates.
  - Done when hidden/show cadence, timestamps, refresh flags, and decoded output
    match libvpx with alternate-reference enabled.

- [ ] Align lookahead queue semantics.
  - govpx: [`pushLookahead`](../encoder_preprocess.go),
    [`popLookahead`](../encoder_preprocess.go), and
    [`lookaheadFutureEntry`](../encoder_preprocess.go).
  - libvpx: `lookahead.c`.
  - Status: partial. govpx queues and drains frames for delayed encode, but
    libvpx also defines exact forward/backward peek semantics, active-map copy
    behavior, first-pass backward-source use, lag clamp behavior, and ARF future
    source selection.
  - Done when queue depth, pop/drain timing, timestamps, flags, peeks, EOS
    flushing, and active-map copies match libvpx.

- [ ] Replace simplified ARNR with libvpx motion-compensated temporal filter.
  - govpx: [`applyARNRFilter`](../encoder_preprocess.go).
  - libvpx:
    [`temporal_filter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/temporal_filter.c).
  - Status: partial. govpx blends colocated pixels; libvpx searches matching
    macroblocks and weights by frame distance and prediction error. ARNR
    control validation now matches libvpx bounds (`maxframes` 0-15, strength
    0-6, type 1-3), with zero-value options normalized to centered type 3.
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
  - Missing: libvpx `MAX_MODES` walk with `vp8_mode_order`, reference ordering,
    sign-bias switching, hit-count gating, threshold mutation, skip
    short-circuiting, and intra modes in the same RD loop.
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
    End-to-end quality smoke now covers realtime `CpuUsed` 4, 5, 9, and 15 on
    a panning corpus in addition to the token-partition motion case.
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
  - Missing: UV RD after Y split selection and exact per-block EOB/token
    context storage.
  - Done when partition, subblock modes/MVs, label rates, distortion, EOBs, and
    final MB RD match libvpx.

## Intra, Quantization, And Tokens

- [ ] Align key-frame and inter-intra pickers with libvpx RD code.
  - govpx:
    [`buildReconstructingKeyFrameCoefficients`](../encoder_reconstruct.go),
    [`predictBestInterIntraModeCost`](../encoder_reconstruct.go),
    [`predictBestBPredLumaModeRD`](../encoder_reconstruct.go).
  - libvpx: `vp8_pick_intra_mode`, RD intra pickers, and `encodeintra.c`.
  - Status: partial. UV intra mode selection now uses transform/quantize,
    coefficient-token rate, and transform-domain distortion. Remaining gaps
    include exact thresholds, activity/tuning hooks, predictor edge setup, and
    per-block bailout behavior.
  - Done when key-frame per-MB traces match Y mode, UV mode, B modes,
    coefficient EOBs, rate, distortion, and reconstructed pixels.

- [ ] Audit quantization and coefficient optimization against libvpx.
  - govpx:
    [`quantizeOptimizedBlock`](../encoder_reconstruct.go),
    [`internal/vp8/encoder/quant.go`](../internal/vp8/encoder/quant.go).
  - libvpx: `encodemb.c` and `vp8_quantize.c`.
  - Status: partial. Several optimized-block paths are tested; final coefficient
    optimization and fast-vs-regular quantizer selection now follow the libvpx
    speed-feature gates, RD scoring uses the same unoptimized fast/regular
    quantizer family, and post-optimization `check_reset_2nd_coeffs` behavior
    now clears tiny Y2 residuals that would inverse-transform to zero. Full
    parity still needs every zbin/round/quant/dequant path, trellis decision,
    EOB handling, and Y2/Y1/UV context behavior.
  - Missing: libvpx Viterbi trellis, `zbin_extra`, `RDTRUNC` tie-breaks, and
    token-cost trace anchors.
  - Done when exhaustive small-block oracle tests match qcoeff, dqcoeff, EOB,
    token rate, and reconstruction across Q, block type, context, skipDC, zbin
    boosts, and coefficient patterns.

## Segmentation, Cyclic Refresh, Skin, And Active Maps

- [ ] Port cyclic background refresh exactly.
  - govpx:
    [`cyclicRefreshSegmentationConfig`](../encoder_segmentation.go),
    [`assignInterFrameStaticSegments`](../encoder_segmentation.go).
  - libvpx: cyclic refresh setup in `onyx_if.c` and `segmentation.c`.
  - Status: partial. govpx already has default CBR/error-resilient enablement,
    base-layer gating, screen-content cadence/disable behavior, cyclic map,
    cooldown/dirty states, segment tree-prob updates, keyframe reset, and
    segment-1 clear-on-non-LAST/ZEROMV. Remaining work is force-max-Q gating,
    ALT_LF/denoiser segmentation feature behavior, exact per-MB final segment
    transition tracing, and temporal enhancement-layer disablement parity.
  - Done when per-frame segment map, feature data, tree probabilities, and
    segment IDs match for CBR, error-resilient, temporal, and screen-content
    modes.

- [ ] Port skin map and dot-artifact bias.
  - govpx:
    [`computeSkinMap`](../encoder_segmentation.go),
    [`classifyStaticSegmentationBlocks`](../encoder_segmentation.go).
  - libvpx: dot artifact logic in `pickinter.c` and skin detection in
    `common/vp8_skin_detection.c`.
  - Status: partial/missing. govpx computes a skin map and uses it to mask
    cyclic-refresh candidates only when static threshold is enabled and screen
    content mode is off. Dot-artifact RD biasing is missing, and libvpx's
    `SKIN_8X8` behavior for small frames is not matched.
  - Done when per-MB skin/dot flags and resulting RD adjustments match on face,
    noisy-flat, and screen-dot patterns.

- [ ] Implement active-map behavior.
  - govpx: [`SetActiveMap`](../encoder.go),
    [`encodeInactiveInterMacroblock`](../encoder_reconstruct.go).
  - libvpx: inactive MB early exit in `pickinter.c` and `vp8_set_active_map` in
    `onyx_if.c`.
  - Status: partial. Public `SetActiveMap` exists; inactive inter MBs skip
    mode decision, code as ZEROMV-LAST with skip=1/segment 0, and have unit
    coverage proving decoded inactive pixels preserve the previous LAST
    reconstruction while active neighbors update. Remaining work is oracle
    trace coverage and integration with multi-threaded encodeframe paths.
  - Done when inactive macroblocks match active-map oracle vectors across
    single-threaded and threaded encodeframe paths.

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
    re-evaluation, ZEROMV denoise decisions, and noise sampling. Control bounds
    now match libvpx for levels 0-6.
  - Done when denoised buffers, selected modes after denoiser re-evaluation, and
    final quality/rate match for `noise_sensitivity` 1-6.

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
  - Missing: independent coefficient-context handling for error-resilient
    partitions, key-frame forced coef-prob updates, RD ref-prob heuristics,
    per-reference entropy contexts, and exact zero-reference/alt-ref
    skip-probability edge cases.
  - Done when every frame matches coefficient probs, MV probs, ref probs,
    refresh entropy bit, projected entropy savings, and next-frame mode-cost
    inputs.
