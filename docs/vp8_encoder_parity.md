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
    [`frameSizeRecodeQuantizerWithContext`](../ratecontrol.go).
  - libvpx:
    [`onyx_if.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/onyx_if.c)
    `encode_frame_to_data_rate`.
  - Status: partial. govpx has a bounded frame-size recode loop, now feeds
    initial Q selection through libvpx-style active best/worst bounds for
    one-pass warmup, CQ floor, and CBR full-buffer cases, and failed key/inter
    recode attempts no longer commit entropy or skip-prob state before the
    accepted attempt. Recode retries now carry local `q_low/q_high` bounds,
    libvpx-style over/undershoot history, damped local rate-correction estimates,
    and `zbin_over_quant` low/high state for max-Q retries. Initial and retry Q
    regulation compute the libvpx zbin over-quant value, including the GF/ARF
    cap, and the accepted attempt applies it to coefficient zbin and the RD
    multiplier. Oversized frames at `active_worst_quality` now relax the active
    worst bound toward worst-Q with libvpx's 4%-per-Qstep model and suppress
    rate-correction-factor updates for that loop.
  - Missing: forced/auto key-frame recodes, entropy projected-size decisions,
    full saved-coding-context restore coverage after failed attempts, and trace
    coverage for GF/ARF zbin-over-quant cases once automatic ARF state is in
    place.
  - Done when oracle traces match Q attempts, final Q, recode reasons, frame
    size bounds, and encoded bytes across CBR/VBR/CQ/key/golden/alt-ref frames.

- [ ] Align active best/worst quantizer selection.
  - govpx: [`selectQuantizerForFrameKindWithScreenContent`](../ratecontrol.go).
  - libvpx: `vp8_regulate_q`, active-best-quality, and active-worst-quality
    branches in `onyx_if.c`.
  - Status: partial. govpx now constrains Q through libvpx's one-pass
    active-min tables for key/golden/inter frames, CBR active-worst buffer
    logic after normal-inter warmup, CBR full-buffer active-best/worst clamps,
    and CQ floors. Remaining gaps are oracle trace coverage for ARF/GF variants
    and interactions with the full recode loop.
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
    aligned. Full RD now walks libvpx's `MAX_MODES` / `vp8_mode_order` table,
    interleaves intra modes in that same loop, applies speed-feature baseline
    `rd_threshes` per mode, propagates static encode-breakout `x->skip` as an
    RD-loop stop, mutates `rd_thresh_mult` / hit-count mode gating across
    tested modes, uses libvpx's static-breakout rate sentinel and inter-intra
    RD penalty, keeps the RD-only NSTEP `first_step` / final one-pixel
    refining search separate from the high-speed non-RD picker, and compacts
    enabled LAST/GOLDEN/ALT references through the same four-slot reference
    search map as `get_reference_search_order`. RD NEWMV no longer reuses the
    fast-path zero-vector rejection, RD NEWMV vector cost uses libvpx's weight
    96, and RD subpel acceptance now has a dedicated helper instead of sharing
    the fast picker decision path. Whole-MB full-pel NSTEP/full/refine searches
    now keep libvpx's SAD-based site walk but return variance plus `mv_err_cost`
    for completed searches; hex search remains on its libvpx SAD return path.
    Encoder near/best MV helpers, mode validation, mode-probability contexts,
    packet writing, and MV-probability adaptation now apply libvpx-style
    reference sign bias before predictor dedupe/counting. Inter residual
    scoring now uses libvpx-shaped transform-domain accounting: `rate2`,
    default no-skip `other_cost`, skip backout when `tteob == 0`,
    split Y/UV token rates and distortions, and Y-only `yrd` for intra4x4 /
    SplitMV pruning. Temporal-layer RD thresholds now mirror libvpx's
    `closest_reference_frame` tweak for LAST+GOLDEN temporal layers, including
    frame-number tracking through refresh/copy updates and `/8` vs `/2`
    reductions for `THR_ZERO2`, `THR_NEAREST2`, and `THR_NEAR2`.
  - Missing: high-level sign-bias policy/reference switching, full SplitMV
    label-level segmentation search with `THR_NEW1/2/3` gating, active-map skip
    short-circuiting, and recode-loop interactions. Active-map behavior is
    tracked in the dedicated active-map checklist item elsewhere.
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
    grid, libvpx realtime gate, and low-level sign-biased near/best MV
    predictor helpers are present. Remaining work is exact border-mode-info
    indexing, high-level sign-bias policy/reference switching, and oracle
    traces for `near_sadidx`, predictor MV, and `sr`.
    End-to-end quality smoke now covers best-quality panning, good-quality RD
    and fast-pick panning, and realtime `CpuUsed` 0, 3, 4, 5, 8, 9, and 15 on
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
  - Status: partial. UV intra mode selection uses transform/quantize,
    coefficient-token rate, and transform-domain distortion. Above-right
    predictor edges for the rightmost B-block column read directly from the
    above-row reference (`refs.YAbove[16:20]`), matching libvpx's
    `intra_prediction_down_copy` payload semantically. The 4x4 RD picker
    bails out per block when `total_rd >= bestRD`, mirroring libvpx's
    `rd_pick_intra4x4mby_modes` early-exit. Mode iteration order
    (`B_DC_PRED..B_HU_PRED`), keyframe `bmode_costs[A][L]` neighbor
    sensitivity, and the per-block bailout are pinned by parity tests in
    `encoder_intra4x4_picker_test.go`. The 16x16 intra RD picker
    (`predictBestWholeBlockIntraModeRD`) has parity coverage in
    `encoder_intra16x16_picker_test.go` that pins the Y mode iteration order,
    mbmode_cost addition, token context seeding, and Y/UV rate-distortion
    breakdowns against libvpx `rd_pick_intra16x16mby_mode`.
  - Missing: exact thresholds and activity/tuning hooks (gated on
    `VP8_TUNE_SSIM`, which govpx does not expose).
  - Done when key-frame per-MB traces match Y mode, UV mode, B modes,
    coefficient EOBs, rate, distortion, and reconstructed pixels.

- [ ] Audit quantization and coefficient optimization against libvpx.
  - govpx:
    [`quantizeOptimizedBlock`](../encoder_reconstruct.go),
    [`internal/vp8/encoder/quant.go`](../internal/vp8/encoder/quant.go).
  - libvpx: `encodemb.c` and `vp8_quantize.c`.
  - Status: partial. Coefficient optimization now ports the libvpx
    `vp8_optimize_b` two-state Viterbi trellis: forward DP from `eob-1` down
    to `i0`, the shift-toward-zero shortcut gated on overshoots inside one
    quant step (`|x|*dq` in `(|c|, |c|+dq)`), plane-specific `Y1/Y2/UV`
    rdmult, the intra `(rdmult * 9) >> 4` scaling, the `RDTRUNC` tie-break
    when two trellis paths share an RDCOST, and EOB rollback by backtrace.
    Fast-vs-regular quantizer selection follows the libvpx speed-feature
    gates, RD scoring uses the same unoptimized fast/regular quantizer
    family, and the post-optimization `check_reset_2nd_coeffs` behavior
    clears tiny Y2 residuals that would inverse-transform to zero. Regular
    quantization applies libvpx `zbin_extra` for mode boost plus
    `zbin_over_quant` (half on Y2), while fast quant intentionally bypasses
    it like libvpx. Frame-level quant deltas now match libvpx
    `vp8_set_quantizer`: low-Q frames write and use `y2dc_delta_q = 4 - Q`,
    and screen-content frames above Q40 write and use clamped negative UV
    DC/AC deltas. The whole-block coefficient rate
    (`coefficientBlockTokenRate`) is anchored against libvpx's
    `cost_coeffs`, including the `skip_eob_node` subtree elision when the
    prior token's `prev_token_class == 0` and the band exceeds the plane
    threshold, by `TestCoefCoeffsParityMatchesReferenceWalk` and
    `TestCoefCoeffsParityIncrementalMatchesWholeBlock` in
    `encoder_cost_coeffs_parity_test.go`.
  - Required/keep: libvpx Viterbi trellis coefficient optimization, including
    `RDTRUNC` tie-breaks; do not replace it with a cheaper greedy optimizer.
  - Missing: `act_zbin_adj` (gated on `VP8_TUNE_SSIM`, which govpx does not
    expose) and per-coefficient token-cost trace anchors for oracle parity.
  - Done when exhaustive small-block oracle tests match qcoeff, dqcoeff, EOB,
    token rate, and reconstruction across Q, block type, context, skipDC, zbin
    boosts, and coefficient patterns.

## Segmentation, Cyclic Refresh, Skin, And Active Maps

- [x] Port cyclic background refresh exactly.
  - govpx:
    [`cyclicRefreshSegmentationConfig`](../encoder_segmentation.go),
    [`aggressiveDenoiseSegmentationActive`](../encoder_segmentation.go),
    [`assignInterFrameStaticSegments`](../encoder_segmentation.go),
    [`updateCyclicRefreshMapFromInterFrame`](../encoder_segmentation.go),
    `forceMaxQuantizer` field on `VP8Encoder` ([encoder.go](../encoder.go)).
  - libvpx: cyclic refresh setup in `onyx_if.c` (`cyclic_background_refresh`,
    `force_maxqp` gate), drop-overshoot wiring in `ratectrl.c`, and
    segmentation packing in `segmentation.c`.
  - Status: complete. govpx has default CBR/error-resilient enablement,
    base-layer gating (via `staticSegmentationAllowed = !temporal.Enabled ||
    LayerID == 0`), screen-content mode-2 disable on golden refresh,
    cyclic map cooldown/dirty states, segment tree-prob updates, key-frame
    reset, and the libvpx segment-1 clear-on-non-LAST/ZEROMV transition.
    Force-max-Q is now plumbed end-to-end: an inter frame dropped due to
    overshoot sets `forceMaxQuantizer = true`, and the next frame's
    `cyclicRefreshModeEnabled` returns false (mirroring
    `cpi->force_maxqp == 0` in `onyx_if.c`); a key frame or successful
    non-dropped commit clears the flag. Aggressive-denoise now switches the
    cyclic-refresh feature data from a Q delta to an alt-LF delta of -40
    when `NoiseSensitivity` ≥ 3, the current Q is below
    `denoise_pars.qp_thresh` (80), and `frames_since_key >
    2 * consec_zerolast` (30), matching the libvpx
    `cyclic_background_refresh` denoiser branch. Per-MB segment transitions
    are oracle-tested across the libvpx state machine
    (`updateCyclicRefreshMapFromInterFrame`).
  - Done when per-frame segment map, feature data, tree probabilities, and
    segment IDs match for CBR, error-resilient, temporal, and screen-content
    modes.

- [x] Port skin map and dot-artifact bias.
  - govpx:
    [`computeSkinMap`](../encoder_segmentation.go),
    [`computeSkin8x8Block`](../encoder_segmentation.go),
    [`classifyStaticSegmentationBlocks`](../encoder_segmentation.go),
    [`checkDotArtifactCandidate`](../encoder_segmentation.go),
    [`updateConsecutiveZeroLastWithDotSuppress`](../encoder_segmentation.go).
  - libvpx: dot artifact logic in `pickinter.c` and skin detection in
    `common/vp8_skin_detection.c`.
  - Status: complete. govpx computes a skin map (SKIN_16X16 for frames above
    352x288, SKIN_8X8 four-sub-block detector with the libvpx
    `num_skin >= 2` threshold for smaller frames) and uses it to mask
    cyclic-refresh candidates and reset the ZEROMV-LAST RD multiplier to 100
    on skin macroblocks. The dot-artifact corner-gradient detector runs on Y,
    U, and V planes with a 1.5x ZEROMV-LAST penalty gated on base layer,
    non-screen-content, and the libvpx MBs/10 suppression cap. The second
    `consec_zero_last_mvbias` counter is tracked separately and reset on any
    MB that this frame's dot-artifact eligibility check inspected, so the
    threshold gate gives the same MB a fresh num_frames window.
  - Done when per-MB skin/dot flags and resulting RD adjustments match on face,
    noisy-flat, and screen-dot patterns.

- [x] Implement active-map behavior.
  - govpx: [`SetActiveMap`](../encoder.go),
    [`encodeInactiveInterMacroblock`](../encoder_reconstruct.go),
    [`TestSetActiveMapOracleVectorPreservesEveryInactiveMB`](../encoder_test.go).
  - libvpx: inactive MB early exit in `pickinter.c` and `vp8_set_active_map` in
    `onyx_if.c`.
  - Status: complete. Public `SetActiveMap` accepts the libvpx mb_rows*mb_cols
    map (and a nil map disables it). Inactive inter MBs skip mode decision and
    code as ZEROMV-LAST with skip=1 in segment 0; the per-MB oracle vector
    test covers a checkerboard pattern across a 64x64 frame, asserts every
    inactive MB's mode/MV/segment/skip flags, decodes the bitstream and
    verifies inactive pixels preserve the prior LAST reconstruction
    byte-for-byte while active neighbors update, and re-runs the encode to
    prove determinism (per-MB modes and decoded pixels match). govpx encodes
    single-threaded by design, so libvpx's row-threaded ethreading.c
    encodeframe loop is N/A.
  - Done when inactive macroblocks match active-map oracle vectors across
    single-threaded and threaded encodeframe paths.

## Denoising And Noise-Sensitive Decisions

- [x] Replace non-libvpx denoising behavior.
  - govpx:
    [`denoiserFilterY`](../encoder_denoiser.go),
    [`denoiserFilterUV`](../encoder_denoiser.go),
    [`denoiserSetParameters`](../encoder_denoiser.go),
    [`applyDenoiserToInterFrame`](../encoder_denoiser.go),
    [`copyDenoiserAvgForRefresh`](../encoder_denoiser.go),
    [`denoiserPickmodeMVBias`](../encoder_denoiser.go).
  - libvpx:
    [`denoising.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/denoising.c)
    plus denoiser re-evaluation in `pickinter.c` and update_reference_frames
    in `onyx_if.c`.
  - Status: complete. govpx ports the libvpx denoiser data path: per-pixel
    `vp8_denoiser_filter_c` / `vp8_denoiser_filter_uv_c` math (including the
    weak-fallback delta loop, `MOTION_MAGNITUDE_THRESHOLD`,
    `SUM_DIFF_FROM_AVG_THRESH_UV`, and per-row 16x16 / 8x8 strides), the
    NoiseSensitivity 1-6 → kDenoiserOnYOnly / kDenoiserOnYUV /
    kDenoiserOnYUVAggressive mapping, and the matching `denoise_params`
    table (`scale_sse_thresh`, `scale_motion_thresh`,
    `scale_increase_filter`, `denoise_mv_bias`, `pickmode_mv_bias`,
    `qp_thresh`, `consec_zerolast`). Per-MB FILTER_BLOCK / COPY_BLOCK / kNoFilter
    state is recorded after inter reconstruction. Running-average buffers
    are seeded from the key-frame source and propagated to LAST / GOLDEN /
    ALTREF following the encoder's refresh policy
    (update_reference_frames). The fast inter mode RD path applies
    `pickmode_mv_bias` to ZEROMV-LAST scores, biasing aggressive-denoise
    encodes toward zero motion as libvpx does. The motion-compensated
    running average uses the encoder's reconstructed analysis frame as the
    mc input; libvpx motion-comps from the parallel running_avg buffer
    directly via `vp8_build_inter_predictors_mb`, which is a deeper
    integration that would require shared inter-prediction plumbing.
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
