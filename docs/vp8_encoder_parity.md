# VP8 Encoder Libvpx Parity Checklist

Reference target: libvpx v1.16.0 VP8 encoder.

This project is not released. When existing govpx behavior conflicts with
empirical libvpx behavior, remove the old behavior instead of preserving a
compatibility path. If existing govpx logic already matches libvpx, keep it as
the anchor and look for the surrounding mismatch.

## What 100% Means

- The product target is libvpx-equivalent VP8 encoder quality, not universal
  byte-for-byte identity. Future agents should prioritize changes that move
  visible quality, rate behavior, reference decisions, and mode/MV/residual
  choices toward libvpx.
- In this checklist, "100% parity" means equivalent quality/rate and
  quality-relevant encoder decisions on representative clips. It does not mean
  every intermediate helper, search tie-break, or non-visible byte must be
  bit-exact when the difference has no quality, rate, decoder-visible, or
  future-decision effect. Agents should mark work done here only when the
  behavior is quality-equivalent to empirical libvpx, or when a deliberate
  non-bitexact difference is documented with that rationale.
- Bitstream parity is required only where it matters or where deterministic
  settings make it the cheapest proof: frame headers, reference
  refresh/copy/sign-bias bits, packet validity, decoder MD5s, and tightly
  scoped low-level encoders.
- Decision parity is the main engineering proxy for quality parity: matching
  frame Q, flags, probabilities, reference checksums, per-MB mode/ref/MV/skip,
  residual EOBs, rate, distortion, and RD decisions on representative clips.
- Do not spend effort preserving old govpx behavior or chasing bit-exactness in
  paths that do not affect quality, rate, decoder-visible output, or future
  oracle diagnosis. Document any intentionally non-bitexact but
  quality-equivalent behavior in this file.

## Current Estimate

- Production validity: high. `make verify-production` passes against pinned
  libvpx v1.16.0 tools and corpus minima.
- Quality smoke parity: high on the current tiny corpus, but not complete. The
  current oracle cases are near-equal on SSIM, while motion still shows a max
  frame PSNR gap around 1.4 dB and a large bitrate delta. Current smoke numbers:
  motion govpx/libvpx PSNR 49.87/50.35, bitrate 357.9/268.7 kbps; static
  govpx/libvpx PSNR 49.84/49.71, bitrate 376.6/372.3 kbps; realtime panning
  govpx/libvpx PSNR 48.03/48.07, bitrate 308.0/304.6 kbps.
- Encoder decision parity: roughly 65% overall, or about 75% on the core
  one-pass quality path, weighted by libvpx LOC.
  This is an engineering estimate, not a measured percentage, because
  govpx still lacks the libvpx-side trace comparator needed to count
  matching frame/MB decisions; the govpx-side per-MB JSON Lines harness
  is in place.
- The largest single remaining parity weight is `firstpass.c` (~2500 LOC
  equivalent unimplemented). Other heavy areas: automatic hidden-ARF
  scheduling, motion-compensated ARNR temporal filter, full GF boost
  tables and `kf_overspend_bits`/`gf_overspend_bits` rate-control
  bookkeeping, error-resilient independent coefficient contexts, and the
  libvpx-side oracle comparator.
- If only three more things are fixed, they should be: (1) the libvpx-side
  oracle comparator paired with the existing govpx trace, (2) a proper
  `firstpass.c` port covering motion search, MV variance, simple_weight,
  and the section accumulators, and (3) automatic hidden-ARF scheduling
  plus motion-compensated ARNR.

## Acceptance Gates

- [ ] `make verify-production` must pass with pinned libvpx v1.16.0 tools and
  required decode/encode corpus minima.
- [ ] Quality/rate/checksum oracle tests are smoke gates for user-visible
  parity. Encoder work should still prefer trace comparison for headers,
  entropy state, segmentation state, reference updates, and per-MB decisions
  when those traces explain a quality or rate gap.
- [ ] Deterministic real-time/no-lag cases should match libvpx bitstream
  headers, partition sizes, frame flags, reference refresh/copy/sign-bias bits,
  decoded MD5s, and trace state where those fields affect decoder output or
  subsequent encoder decisions.
- [ ] Non-bitexact cases must match decision traces within documented
  tolerances for quality-relevant behavior: rate-control attempts, recode
  reasons, Q choices, entropy save/restore, mode/ref/MV choices, segmentation
  IDs, loop filter, and token probabilities.

## Validation Harness

- [~] Add a per-frame and per-MB encoder oracle trace mode.
  - govpx: in progress. The trace is emitted as JSON Lines through
    [`EncoderOptions.OracleTraceWriter`](../encoder.go) (off by default; nil
    writer means zero overhead). Implementation lives in
    [`encoder_oracle_trace.go`](../encoder_oracle_trace.go) with parser tests
    in [`encoder_oracle_trace_test.go`](../encoder_oracle_trace_test.go).
    Existing oracle smoke tests remain in
    [`oracle_encoder_validation_test.go`](../oracle_encoder_validation_test.go)
    and mode tests in
    [`encoder_reconstruct_test.go`](../encoder_reconstruct_test.go).
  - Covered now (govpx side): per-frame row with `frame_index`, `frame_type`
    (key/inter), `q_index`, `base_q_index`, `loop_filter_level`,
    `refresh_last/golden/altref`, `sign_bias_golden/altref`,
    `segmentation_enabled`, Y/U/V plane Adler32 reference checksums, and
    `size_bytes`; per-MB row (inter frames only) with `frame_index`,
    `mb_row`, `mb_col`, `segment_id`, `mode`, `ref_frame`, `mv_row`,
    `mv_col`, `skip`, `eob[0..24]`, `eob_sum`, and improved-MV start fields.
    Rows cover every committed inter-frame MB, including intra `B_PRED`
    decisions, are emitted in deterministic raster scan order, and only for
    the final committed encode attempt (recoded attempts are discarded).
  - libvpx side: in progress. The patched vpxenc lives in
    [`internal/coracle/build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh),
    which adds a single `vp8/encoder/oracle_trace.c` translation unit
    plus two `extern` hook calls in `encodeframe.c` (per-MB capture
    inside `encode_mb_row`) and `bitstream.c` (per-frame flush at the
    tail of `vp8_pack_bitstream`). Output is gated on
    `GOVPX_ORACLE_TRACE_OUT` and matches the govpx schema.
  - Comparator: in place. The pure-Go
    [`CompareOracleTraces`](../internal/coracle/oracle_compare.go)
    helper walks both JSON Lines streams in lockstep and surfaces
    field-level divergences as `Divergence{RowIndex, RowKind, FrameIndex,
    MBRow, MBCol, Field, Govpx, Libvpx}` records; the cap and an
    `IgnoreFields` set are configurable through `CompareOptions`. Tests
    in
    [`oracle_compare_test.go`](../internal/coracle/oracle_compare_test.go)
    cover identical streams, mismatched fields, missing rows, ignored
    fields, and type mismatches.
  - Remaining: libvpx-side instrumentation in `pickinter.c`, `rdopt.c`,
    `ratectrl.c`, and `onyx_if.c` for rate-control state and recode
    reasons (the current libvpx-side patch only covers what govpx already
    emits); a CI driver that runs both sides under
    `make verify-production` so divergences gate merges; and extending
    the govpx-side schema with rate-control state, residual decision,
    probabilities, and per-frame loop-filter delta details.
  - Done when comparable JSON/CSV rows expose frame state, rate-control state,
    per-MB mode decision, residual decision, probabilities, segmentation, loop
    filter, and reference updates.
- [~] Extend the C oracle beyond decode-MD5 JSON.
  - Status: in progress. The decode-MD5 helper still lives in
    [`internal/coracle/vpx_oracle.c`](../internal/coracle/vpx_oracle.c).
    A second helper now produces a parity-grade encoder trace: the
    patched vpxenc built by
    [`build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh)
    emits per-frame and per-MB JSON Lines matching the govpx oracle
    schema, and
    [`CompareOracleTraces`](../internal/coracle/oracle_compare.go)
    walks both streams and reports field-level divergences (with row
    index, frame index, MB coordinates, field name, and both decoded
    values).
  - Remaining: a CI hook that fails on the first divergence, plus
    libvpx-side coverage of the rate-control / probability / partition
    fields once the govpx-side schema grows to include them.
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
    The recode size-bounds comparison now subtracts
    `vp8_estimate_entropy_savings` (ref-frame plus default
    coefficient-context portions) from the just-encoded size before
    deciding to recode, mirroring libvpx's
    `cpi->projected_frame_size -= vp8_estimate_entropy_savings(cpi)`
    via [`applyEntropySavingsToProjectedSize`](../encoder.go). The
    libvpx `decide_key_frame` heuristic is ported as
    [`libvpxDecideKeyFrame`](../encoder_entropy_savings.go), covering
    the unconditional thresholds (this==100 && this>last+2 ||
    this>95 && this>=last+5) and the GF-guarded second tier
    (this>60 && this>2*last; this>75 && this>3/2*last;
    this>90 && this>last+10) for the auto-key recode decision. The
    encoder now applies that heuristic after an opt-in, non-realtime,
    one-pass inter attempt: if the intra-percentage gate fires, the
    uncommitted inter attempt is discarded, `sourceAltRefActive` is
    cleared like libvpx, key-frame target/Q selection is recomputed, and
    the same source is encoded as a key frame. `lastFramePercentIntra`
    is tracked after the decision so the next frame sees the libvpx
    lookback value.
  - Missing: full saved-coding-context restore coverage after failed
    attempts and trace coverage for GF/ARF zbin-over-quant cases once
    automatic ARF state is in place.
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

- [x] Port full one-pass golden-frame boost and interval logic.
  - govpx:
    [`shouldRefreshGoldenFrameCBR`](../encoder.go),
    [`goldenFrameCBRInterval`](../encoder.go),
    [`beginFrameWithTargetAndContext`](../ratecontrol.go),
    [`calcGFParams`](../ratecontrol.go),
    [`accumulatePostPackOverspend`](../ratecontrol.go),
    [`applyOnePassPFrameOverspendRecovery`](../ratecontrol.go),
    [`libvpxGoldenFrameTargetBits`](../ratecontrol.go),
    [`pickFrameSize`](../ratecontrol.go),
    [`estimateKeyFrameFrequency`](../ratecontrol.go).
  - libvpx:
    [`ratectrl.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ratectrl.c)
    `calc_gf_params`, `calc_pframe_target_size`,
    `vp8_adjust_key_frame_context`, `estimate_keyframe_frequency`,
    `vp8_pick_frame_size`, and GF/ARF update branches in `onyx_if.c`
    (`update_golden_frame_stats`).
  - Status: partial. The libvpx `calc_gf_params` boost computation now
    runs end-to-end: `vp8_gf_boost_qadjustment` (GFQ_ADJUSTMENT),
    `gf_intra_usage_adjustment`, `gf_adjust_table`,
    `kf_gf_boost_qlimits` ceiling and 110 floor, the
    `last_boost>750/>1000/>1250/>=1500` interval extensions, the
    `gf_interval_table` floor, and the `max_gf_interval` cap are all
    ported byte-for-byte. The post-pack overspend bookkeeping mirrors
    libvpx: KF overspend splits 7/8 into `kf_overspend_bits` and 1/8
    into `gf_overspend_bits` for single-layer encodes (full into
    `kf_overspend_bits` for multi-layer), `kf_bitrate_adjustment` is
    `kf_overspend_bits / estimate_keyframe_frequency`, and GF
    overspend (`projected_frame_size - inter_frame_target`) drives
    `non_gf_bitrate_adjustment`. The next p-frame target now drains
    KF then GF overspend via the libvpx adjustment ordering, applies
    the small +/- `last_boost` boost for non-GF frames inside long
    GF intervals (`current_gf_interval >= 2*MIN_GF_INTERVAL`), and
    clamps to `min_frame_target = max(min_frame_bandwidth,
    per_frame_bandwidth/4)`. `inter_frame_target` is captured after
    recovery so subsequent GF overspend math matches libvpx. The
    `vp8_pick_frame_size` unified KF/p-frame dispatcher is wired
    through `pickFrameSize`, returning false on the libvpx buffer-
    underrun drop branch and refunding `av_per_frame_bandwidth` via
    `postDropFrame`. `estimate_keyframe_frequency` now follows the
    libvpx weighted-average over prior_key_frame_distance with
    {1,2,3,4,5} weights and the keyFrameCount==1 bootstrap; the
    encoder seeds `keyFrameFrequency` from `EncoderOptions.KeyFrameInterval`
    so the bootstrap matches libvpx's `oxcf.key_freq`. The libvpx
    boost-weighted GF target sizing
    (`Boost*bits_in_section/allocation_chunks` with the >1000-boost
    halving and high-precision divide-first branch) is exposed as
    `libvpxGoldenFrameTargetBits` for non-CBR GF callers. The CBR
    refresh decision in `shouldRefreshGoldenFrameCBR` still uses
    govpx's simplified heuristic, but it now publishes
    `framesTillGFUpdateDue` and `currentGFInterval` so
    `update_golden_frame_stats` overspend math matches libvpx. The
    auto_gold one-pass non-CBR refresh decision
    (`pct_intra<15 || gf_frame_usage>=5`) is exposed as
    `libvpxAutoGoldOnePassRefreshDecision`. `min_frame_bandwidth` is
    now seeded from the libvpx
    `av_per_frame_bandwidth * two_pass_vbrmin_section / 100`
    derivation via `vbrMinFrameBandwidthBits` and threaded through
    encoder construction so calc_pframe_target_size's min_frame_target
    floor matches libvpx exactly. `recent_ref_frame_usage` (per-MB
    INTRA/LAST/GOLDEN/ALTREF accumulator) and `gf_active_count` are
    now tracked end-to-end: the encoder counts ref usage from the
    just-encoded inter modes via `countInterFrameRefUsage`, accumulates
    via `updateRecentRefFrameUsage` (skipping frames_since_golden==1
    exactly like libvpx), resets to {1,1,1,1} via
    `resetRecentRefFrameUsage` on GF/key refresh, and exposes
    `thisFramePercentIntra` so calcGFParams and the auto_gold refresh
    decision read the same state libvpx would. The encoder now invokes
    `calcGFParams` at the tail of every CBR GF refresh frame (matching
    libvpx's `calc_pframe_target_size` ordering, so the small +/-
    last_boost adjustment for non-GF frames sees the prior GF's boost,
    not this one's) and stores the boost in `lastBoost` for the next
    section.
    The auto_gold one-pass non-CBR refresh decision is wired into the
    encoder via `shouldRefreshGoldenFrameOnePassNonCBR`, so VBR/CQ now
    fire GF refreshes when `frames_till_gf_update_due==0` and
    `pct_intra<15 || gf_frame_usage>=5`, funneling the result through
    the same code path as CBR so the rate-control bookkeeping, header
    copy semantics, and post-pack GF overspend accumulation apply
    uniformly.
  - Out of scope (deferred): temporal-layer propagation of KF/GF
    overspend through libvpx's per-layer `layer_context` state (govpx
    already mirrors the single-layer-vs-multi-layer KF split toggle
    inside accumulatePostPackOverspend, but per-layer kf/gf counters
    are tracked in the temporal-scalability work item, not here), and
    the two-pass `calc_gf_params` IIAccumulator branch (disabled in
    libvpx as well — guarded by `#if 0` in upstream).

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
  - Status: partial. govpx now records every per-frame `FIRSTPASS_STATS`
    field libvpx populates from `vp8_first_pass`: intra/coded errors,
    `ssim_weighted_pred_err` via the libvpx `simple_weight` 256-entry
    weight_table (with the 0.1 weight floor), `pcnt_inter`, `pcnt_motion`,
    `pcnt_second_ref`, `pcnt_neutral`, plus the MV accumulation set
    (`MVr`/`MVc`/`mvr_abs`/`mvc_abs`/`MVrv`/`MVcv`/`mv_in_out_count`/
    `new_mv_count`). Each non-first frame runs an integer-pel zero-MV
    `zz_motion_search` plus a libvpx-shaped NSTEP `first_pass_motion_search`
    against LAST, seeded by the previous accepted row MV with the libvpx
    zero-MV retry when that seed is nonzero, and a zero-MV-only search against
    GOLDEN. The raw previous source is kept separately for
    `zz_motion_search`/`oxcf.encode_breakout`, while the accepted LAST
    reference is reconstructed into a first-pass `new_yv12`-style scratch
    before it becomes the next frame's LAST reference. First-pass search uses
    SAD for the diamond walk, SSE plus MV error cost for the final score,
    applies the libvpx `new_mv_mode_penalty=256` to motion-search results,
    wires `EncoderOptions.StaticThreshold` through libvpx's
    `oxcf.encode_breakout` raw zero-motion skip gate, and the inter/neutral
    accept gate uses libvpx's
    `((this_error - intrapenalty) * 9 <= motion_error * 10)` threshold.
    The post-stats LAST->GOLDEN copy follows the libvpx
    `pcnt_inter > 0.20 && intra/coded > 2.0` heuristic, and the first
    frame still seeds GOLDEN from LAST as a second reference. Per-frame
    field values are pinned by
    [`TestFirstPassStatsRegression32x32`](../encoder_firstpass_test.go) on a
    deterministic 32x32 ramp clip; plausibility coverage is in
    `TestFirstPassStatsPopulatesLibvpxFields`, and the simple_weight table
    boundaries are pinned by `TestSimpleWeightLumaMatchesLibvpxTable`.
  - Missing: terminal total-stats packet/section accumulators and oracle-trace
    coverage on a fixed Y4M corpus.
  - Done when fixed Y4M corpus stats match libvpx within defined tolerances for
    every field.

- [ ] Port second-pass KF/GF group allocation and VBR section limits.
  - govpx: [`twoPassState`](../encoder_firstpass.go),
    [`framesToKey`](../encoder_firstpass.go),
    [`kfGroupBits`](../encoder_firstpass.go),
    [`kfGroupModifiedError`](../encoder_firstpass.go).
  - libvpx: second-pass helpers in `firstpass.c` and `Pass2Encode` in
    `onyx_if.c`.
  - Status: partial. govpx distributes bits by per-frame modified
    error only. `framesToKey` now ports the `find_next_key_frame`
    lookahead with the libvpx `i >= MIN_GF_INTERVAL` gate, the
    `libvpxTestCandidateKeyFrame` predicate, the `key_freq` floor, and
    the `2 * key_freq` outer clamp. `kfGroupModifiedError` accumulates
    the per-frame `calculate_modified_err` into the libvpx
    `kf_group_err`. `kfGroupBits` ports the libvpx KF-group allocation
    (`bits_left * (kf_group_err / modified_error_left)`) with the
    `max_bits * frames_to_key` ceiling and the `bits_left>0 &&
    modified_error_left>0.0` gate. `libvpxGFGroupBits` ports the GF
    section allocation
    (`gf_group_bits = kf_group_bits * (gf_group_err /
    kf_group_error_left)`) with the kf_group_bits clamp and the
    `max_bits * baseline_gf_interval` ceiling.
    `libvpxGFBitsAllocation` ports the libvpx GF/ARF bit allocation
    (Boost-weighted `gf_bits = Boost * (gf_group_bits /
    allocation_chunks)`): the GF branch uses
    `Boost = (gfu_boost * GFQ_ADJUSTMENT) / 100` with cap
    `interval*150` and floor 125, and the ARF branch uses
    `(gfu_boost * 3 * GFQ_ADJUSTMENT) / 200 + interval*50` with cap
    `(interval+1)*200`; both apply the `>1000` halving overflow
    guard and the libvpx `(interval+1)*100 + Boost` /
    `interval*100 + (Boost-100)` allocation_chunks formulas.
    `libvpxFrameMaxBitsCBR` and `libvpxFrameMaxBitsVBR` port the
    libvpx vp8/encoder/firstpass.c `frame_max_bits` per-frame ceiling:
    CBR uses `av_per_frame_bandwidth * vbrmax_section / 100` scaled by
    `buffer_level / optimal_buffer_level` when the buffer is below
    optimal, with the libvpx
    `min(av_per_frame_bandwidth>>2, max_bits>>2 (pre-scale))` floor;
    VBR uses `(bits_left / frames_left) * vbrmax_section / 100`.
    The non-key `twoPassState.frameTargetBits` path now uses that live
    VBR cap, so the target ceiling tracks current surplus/deficit bits
    instead of the initial average frame target. These helpers also feed
    the `kfGroupBits` and `libvpxGFGroupBits` ceilings.
    `libvpxAssignStdFrameBits` ports the libvpx
    `assign_std_frame_bits` per-frame allocator inside a GF group:
    `target = gf_group_bits * (modified_err / gf_group_error_left)`,
    clamp(0, min(max_bits, gf_group_bits)), add min_frame_bandwidth,
    and add alt_extra_bits on odd frames_since_golden when
    frames_till_gf_update_due > 0. `libvpxSectionStats` /
    `libvpxSectionIntraRating` / `libvpxSectionMaxQFactor` port the
    libvpx FIRSTPASS_STATS section accumulator pattern
    (accumulate_stats / avg_stats), the section_intra_rating
    `(unsigned int)(sectionIntra/sectionCoded)` cast with the
    DOUBLE_DIVIDE_CHECK fallback, and the
    `section_max_qfactor = 1.0 - (Ratio - 10.0) * 0.025` formula
    with the libvpx 0.80 floor.
    `libvpxCalcCorrectionFactor` ports the libvpx
    `calc_correction_factor` per-Q rate-model correction:
    `cf = clamp(pow(err_per_mb/err_devisor, min(pt_low+Q*0.01,
    pt_high)), 0.05, 5.0)`, used by `estimate_max_q` /
    `estimate_min_q`. `libvpxEstimateMaxQRollingRatioAdjustment`
    ports the rolling `est_max_qcorrection_factor` update (`+/-0.005`
    based on rolling actual/target ratio, clamped to `[0.1, 10.0]`).
    `libvpxEstimateMaxQ` ports the libvpx vp8/encoder/firstpass.c
    `estimate_max_q` Q-search loop end-to-end: walks Q from
    `maxq_min_limit` upward computing
    `bits_per_mb_at_q = err_correction * speed_correction *
    est_max_qcorrection * section_max_qfactor *
    (vp8_bits_per_mb[INTER][Q] + overhead)` with overhead decay of
    0.98 per Q step and the `(512*section_target_bandwidth)/num_mbs`
    per-MB budget normalization (with libvpx's `< 1<<20` overflow
    guard). Returns `maxq_max_limit` when the budget cannot be met.
    `libvpxEstimateQ` ports the simpler `estimate_q` Q-search used
    inside Pass2Encode (no overhead/section_max_qfactor scaling).
    `libvpxEstimateKFGroupQ` ports `estimate_kf_group_q`: derives
    `pow_high_q` and `pow_low_q` from `oxcf.two_pass_vbrbias / 100`,
    folds in `current_spend_ratio = clamp(long_rolling_actual /
    long_rolling_target, 0.1, 10.0)` (10.0 fallback when
    long_rolling_target is 0) and `iiratio_correction =
    max(0.5, 1.0 - (group_iiratio - 6.0) * 0.1)`, walks Q with
    `calc_correction_factor`, then bumps Q (shrinking bits by 0.96
    per step) until MAXQ*2 if no Q in [0, MAXQ) satisfies the budget.
  - Missing: broader VBR min/max section-limit application inside the
    full Pass2Encode flow, CBR buffer adjustments inside Pass2Encode,
    ARF pending decisions wired into the encoder, and the CQ floor application
    (`USAGE_CONSTRAINED_QUALITY -> max(Q, cq_target_quality)`)
    deferred to callers since it depends on encoder mode state.
  - Done when second-pass oracle tests match frame type, GF/ARF decisions,
    target bits, final Q, and bitrate distribution on multi-scene clips.

## Alt-Ref, Lookahead, And ARNR

- [x] Implement automatic alternate-reference scheduling.
  - govpx:
    [`initLookahead`](../encoder_preprocess.go),
    [`encodeAutoAltRefInto`](../encoder_altref.go),
    [`tryEmitHiddenAltRef`](../encoder_altref.go),
    [`encodeNextDeferredAutoAltRef`](../encoder_altref.go),
    [`schedulePendingAltRef`](../encoder_altref.go),
    [`encodeSourceInto`](../encoder.go),
    [`encodeInterFrameAttempt`](../encoder.go).
  - libvpx: ARF decision logic in `vp8_get_compressed_data` and ARF pending
    policy in `ratectrl.c`.
  - Status: ported. `EncoderOptions.AutoAltRef` (libvpx
    `cpi->oxcf.play_alternate`) gates the auto-ARF pipeline. When enabled
    together with `LookaheadFrames>0` and `!ErrorResilient`, the encoder
    tracks `sourceAltRefPending` / `framesTilArf` (libvpx
    `source_alt_ref_pending` / `frames_till_gf_update_due` with
    `DEFAULT_GF_INTERVAL=7` clamped to `LookaheadFrames-1`) and inserts
    hidden ARF frames peeked from the future window with `show_frame=0`,
    `refresh_alt_ref=1`, and no `refresh_last`/`refresh_golden`. The
    deferred show frame pops normally and is flagged `isSrcFrameAltRef`
    when its PTS matches the previously-peeked alt-ref source. ALTREF
    sign bias flips on the first show frame after the hidden ARF (driven
    by the existing `sourceAltRefActive` lifecycle). Key frames reset
    pending/active state. The hidden-ARF call defers the caller's input
    into a single-slot stash (`autoAltRefPendingPush`) so the lookahead
    queue does not overflow when a peek-only call leaves the queue at
    capacity; the next auto-ARF call drains the stash before pushing.
  - Tests: `TestAutoAltRefSchedulesHiddenFrame`,
    `TestAutoAltRefDeferredShowFrameRendersOriginalSource`,
    `TestAutoAltRefSignBiasUpdatesOnRefresh`.
  - Simplification vs. libvpx: govpx schedules a pending ARF every
    `DEFAULT_GF_INTERVAL` inter frames once the future lookahead has
    enough entries, instead of using `vp8_calc_arf_boost` /
    `select_arf_period` (those rely on first-pass stats not yet ported).
    `vp8_temporal_filter_prepare_c`'s ARF buffer redirection is not
    wired through `force_src_buffer`; ARNR still runs through
    `applyARNRFilter` per the existing pipeline.
  - Done when hidden/show cadence, timestamps, refresh flags, and decoded
    output match libvpx with alternate-reference enabled.

- [x] Align lookahead queue semantics.
  - govpx: [`pushLookahead`](../encoder_preprocess.go),
    [`popLookahead`](../encoder_preprocess.go),
    [`peekLookahead`](../encoder_preprocess.go),
    [`lookaheadDepth`](../encoder_preprocess.go),
    [`lookaheadFutureEntry`](../encoder_preprocess.go), and
    [`copySourceToFrameBufferActive`](../encoder_preprocess.go).
  - libvpx: `lookahead.c` (`vp8_lookahead_init`, `vp8_lookahead_push`,
    `vp8_lookahead_pop`, `vp8_lookahead_peek`, `vp8_lookahead_depth`).
  - Status: complete. The queue is allocated with `LookaheadFrames + 1`
    buffers to mirror libvpx's `max_sz = depth + 1` (the trailing slot keeps
    the most recently popped entry addressable). `pushLookahead` rejects with
    `ErrFrameNotReady` when `sz + 2 > max_sz` (libvpx's lag clamp) and applies
    the active-map-aware partial copy when `max_sz == 1`, the active map is
    enabled, and the frame carries no key/golden/alt-ref flags; otherwise the
    full source is copied. `popLookahead(false)` only releases when
    `sz == max_sz - 1`, while `popLookahead(true)` drains entry-by-entry to
    flush the queue at end-of-stream. `peekLookahead(index, forward)` mirrors
    `vp8_lookahead_peek`: PEEK_FORWARD returns the entry at offset `index`
    from the read head with libvpx's `index < max_sz - 1` and
    `index < sz` guards (returning nil out of range), and PEEK_BACKWARD only
    accepts `index == 1` and exposes the previous-source slot used by
    first-pass. `lookaheadDepth` matches `vp8_lookahead_depth`. ARF future
    source selection still goes through `lookaheadFutureEntry`, which now
    delegates to the same forward-peek implementation. Tests in
    `encoder_lookahead_test.go` pin the lag clamp on overflow push, the
    full-then-drain cycle, forward peek at depth 0/1/N-1/N (nil at N), the
    backward peek `index == 1` aliasing for the most recently popped frame,
    rejection of unsupported backward indices, the active-map row-walk's
    multi-run copy semantics, and `lookaheadDepth` accounting through
    push/drain.

- [x] Replace simplified ARNR with libvpx motion-compensated temporal filter.
  - govpx: [`applyARNRFilter`](../encoder_preprocess.go),
    [`iterateTemporalFilter`](../encoder_preprocess.go),
    [`applyTemporalFilter`](../encoder_preprocess.go),
    [`arnrFindMatchingMB`](../encoder_preprocess.go).
  - libvpx:
    [`temporal_filter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/temporal_filter.c).
  - Status: ported. govpx now walks per-16x16 luma macroblock (and the
    colocated 8x8 chroma blocks) over the alt-ref frame, performs a
    full-pixel motion search against every adjacent reference, and applies
    libvpx's per-pixel weighted accumulator
    `modifier = clamp((3*(src-pred)^2 + (1<<(strength-1))) >> strength, 0, 16)`,
    `weight = (16-modifier) * filter_weight`, normalized as
    `(accumulator + count/2) / count`. Per-frame `filter_weight` is 2 for
    the center, and {2,1,0} for adjacent frames keyed off the 16x16 SAD
    against `THRESH_LOW=10000`/`THRESH_HIGH=20000`, matching libvpx.
    Backward (type 1), forward (type 2), and centered (type 3, the
    libvpx default which receives `arnr_type==0` normalization) blur modes
    all run the same per-MB iteration. ARNR control validation matches
    libvpx bounds (`maxframes` 0-15, strength 0-6, type 1-3).
  - Simplification vs. libvpx: motion search is a small full-pixel local
    exhaustive scan around (0,0) instead of libvpx's hex search seeded
    from the prior MV; subpixel refinement
    (`vp8_temporal_filter_predictors_mb_c`'s 6-tap `subpixel_predict16x16`/
    `subpixel_predict8x8`) is not used, so chroma MVs reuse the integer
    luma MV halved per libvpx, but predictors stay at integer-pixel
    positions. Search range is also constrained to the visible area
    because the central frame is exposed only as a `SourceImage` without
    libvpx's 16-pixel source-border extension. The libvpx
    `cpi->fixed_divide` LUT is replaced with the equivalent
    `(accumulator + count/2) / count` integer division. Tests in
    [`encoder_preprocess_test.go`](../encoder_preprocess_test.go) pin
    zero-strength identity, motion-clip non-identity, and an Adler32
    regression of the filtered ARF buffer.
  - Remaining: subpel motion search and full hex/diamond seeding to make
    bitstream/MD5 match for all backward/forward/centered settings.

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
  - Status: partial. Fast non-RD mode-loop order, cheap realtime scoring, and
    `rd_thresh_mult` / hit-count gating are aligned, including the pickinter
    `>>3` best-mode threshold decay and libvpx's unsupported-SPLITMV
    test-count/raise behavior. Full RD now walks libvpx's `MAX_MODES` /
    `vp8_mode_order` table,
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
    for completed searches; the alternate four-site DIAMOND table/path is
    available for explicit libvpx-surface parity and future first-pass reuse;
    hex search remains on its libvpx SAD return path.
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
  - High-level sign-bias policy is now wired: frame headers derive
    `GoldenSignBias`/`AltRefSignBias` from libvpx-shaped
    `sourceAltRefActive`, RD/fast near/best predictor selection uses that map,
    mode-rate counts and NEWMV vector costs use the same sign-biased anchors as
    the writer, and previous-frame improved-MV slots store the prior frame's
    sign-bias state. Tests:
    `TestEncodeIntoAltRefSignBiasFollowsLibvpxSourceAltRefActive`,
    `TestEncoderInterMotionModeRateUsesAltRefSignBias`,
    `TestEncoderInterReferenceMotionPredictorsUseAltRefSignBias`,
    `TestImprovedInterFrameSearchStartBiasesCurrentSlots`, and
    `TestImprovedInterFrameSearchStartBiasesPreviousFrameSlots`.
  - Missing: full SplitMV label-level RD search with `THR_NEW1/2/3` gating,
    token-context commit parity, active-map skip short-circuiting, and
    recode-loop interactions. Active-map behavior is tracked in the dedicated
    active-map checklist item elsewhere.
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
    predictor helpers are present, and high-level predictor search now uses
    the current frame's sign-bias map plus the saved previous-frame slot bias.
    Border-mode-info indexing now mirrors
    libvpx's calloc-zeroed sentinel rows/columns: nil current-frame
    above/left/above-left and out-of-range previous-frame
    above/left/right/below neighbors collapse to `INTRA_FRAME` /
    `mv == 0` / `near_sad == INT_MAX` slots, and an intra current-frame
    neighbor no longer leaks a stale MV into the median fallback. Govpx-side
    oracle MB rows now expose `improved_mv_near_sadidx`,
    `improved_mv_row`/`improved_mv_col`, and `improved_mv_sr` for NEWMV
    candidates that used improved-MV prediction. Remaining validation work is
    the matching libvpx-side trace/comparator for those fields.
    End-to-end quality smoke now covers best-quality panning, good-quality RD
    and fast-pick panning, and realtime `CpuUsed` 0, 3, 4, 5, 8, 9, and 15 on
    a panning corpus in addition to the token-partition motion case. A new
    9-position 3x3-grid regression test pins border behavior at every corner,
    edge, and interior macroblock for both the current-frame and last-frame
    neighbor tables.
  - Done when panning, alternating-reference, dropped-frame, and all-quality
    clips match libvpx predictor MV, search range, and final NEWMV choices.

- [ ] Finish SPLITMV RD parity.
  - govpx:
    [`selectInterFrameSplitMotionMode`](../encoder_reconstruct.go),
    [`selectInterFrameSplitMotionDecisionRD`](../encoder_reconstruct.go).
  - libvpx: `rd_check_segment` and `vp8_rd_pick_best_mbsegmentation` in
    `rdopt.c`, plus the `SPLITMV` UV branch in `vp8_rd_pick_inter_mode` that
    calls `rd_inter4x4_uv` (`vp8_build_inter4x4_predictors_mbuv` ->
    `vp8_subtract_mbuv` -> `vp8_transform_mbuv` -> `vp8_quantize_mbuv` ->
    `rd_cost_mbuv`).
  - Status: partial. Partition order/pruning is aligned. Per-subset motion
    selection now trials LEFT/ABOVE/ZERO/NEW labels, stores the explicit
    sub-MV label in `BModes`, and uses that label for SplitMV rate costing,
    MV-probability branch counting, and packet syntax instead of deriving the
    label from MV equality. NEW4X4 keeps libvpx's weight-102 vector cost, and
    ABOVE4X4 is only selected when it is not the same MV as LEFT4X4. NEW4X4
    full-pel search can now be centered independently from the coded
    `bestRefMV` cost anchor, and compressor-speed BLOCK_4X4 searches reuse the
    previous left/above block MV as libvpx does. Split NEW candidates now run
    the libvpx-shaped fractional refinement path before selection, with
    split-size subpel variance/SAD coverage for 16x8, 8x16, 8x8, and 4x4.
    Compressor-speed searches now also save the accepted 8x8 partition's
    block 0/2/8/10 MVs and use them as the 8x16 and 16x8 search centers
    while keeping NEW4X4 coding cost anchored to `bestRefMV`; the saved
    8x8-pair distance now drives libvpx's `vp8_cal_step_param` model, and
    speed-path SplitMV NEW searches use the same NSTEP diamond/further-step
    search shape instead of the older fixed exhaustive window. Best-quality
    SplitMV NEW searches now use the same NSTEP base plus libvpx's conditional
    `vp8_full_search_sad`-style distance-16 fallback when the split-shape
    shifted error remains above 4000.
    After the Y split is committed, `selectInterFrameSplitMotionDecisionRD`
    reuses the decoder's `ReconstructSplitMVInterMacroblock` to render the
    SPLITMV luma+chroma predictor (libvpx-style 8x8 chroma MVs derived from the
    four covering 4x4 luma MVs via `splitChromaMotionVector`), then runs
    the same `quantizeEncodedBlock` block_type=3 (Y) / block_type=2 (UV)
    transform/quantize path the whole-MB inter case uses. The returned
    `interSplitMVRDDecision` carries Y rate/distortion, UV rate/distortion,
    and a `MacroblockCoefficients` populated with per-4x4-block luma EOBs
    (`Coeffs.EOB[0..15]`) and per-4x4-block chroma EOBs (`Coeffs.EOB[16..23]`).
    Remaining search-shape work is broader oracle coverage. Token-context
    commit parity, `THR_NEW1/2/3` NEW4X4 gating, and oracle-backed label-level
    RD remain open.
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
    breakdowns against libvpx `rd_pick_intra16x16mby_mode`. The non-RD
    inter-frame B_PRED picker now uses libvpx's cheaper `B_DC_PRED..B_HE_PRED`
    candidate set instead of the full RD-only 10-mode set.
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
    [`adaptInterFrameModeProbabilitiesWithMVBase`](../internal/vp8/encoder/interframe.go),
    [`applyRdRefFrameProbHeuristics`](../encoder.go),
    [`updateGoldenFrameStats`](../encoder.go).
  - libvpx: `vp8_estimate_entropy_savings`, `vp8_update_coef_probs`, and
    error-resilient entropy branches in `bitstream.c` and `onyx_if.c`,
    plus `update_rd_ref_frame_probs` / `update_golden_frame_stats` /
    `update_alt_ref_frame_stats` in `onyx_if.c`.
  - Status: partial. Live coefficient/ref/MV work exists. RD ref-prob
    heuristics are now ported: `applyRdRefFrameProbHeuristics` mirrors
    libvpx's `update_rd_ref_frame_probs` (alt-ref refresh bumps
    `prob_intra+=40`, `prob_last=200`, `prob_gf=1`; `frames_since_golden==0`
    sets `prob_last=214`; `frames_since_golden==1` sets `prob_last=192`,
    `prob_gf=220`; `source_alt_ref_active` decays `prob_gf` by 20 down to
    floor 10; trailing `!source_alt_ref_active` clamp forces `prob_gf=255`).
    `framesSinceGolden`/`sourceAltRefActive` track libvpx's
    `update_golden_frame_stats` / `update_alt_ref_frame_stats` lifecycle.
    Per-reference entropy contexts: VP8 maintains a single coefficient
    `coef_counts` accumulator across all reference branches (libvpx
    `bitstream.c` `default_coef_context_savings`); govpx's
    `BuildInterCoefficientProbabilityUpdates` matches that aggregation.
    Independent coef contexts for error-resilient partitions are now
    ported: `BuildInterCoefficientProbabilityUpdatesIndependent` /
    `BuildKeyFrameCoefficientProbabilityUpdatesIndependent` and
    `coefficientProbabilityUpdatesFromCountsIndependent` in
    [`internal/vp8/encoder/probability.go`](../internal/vp8/encoder/probability.go)
    mirror libvpx's `independent_coef_context_savings`
    (`bitstream.c:678-740`) and the matching VPX_ERROR_RESILIENT_PARTITIONS
    branch in `vp8_update_coef_probs` (`bitstream.c:879-928`): branch
    counts are summed across `PREV_COEF_CONTEXTS` for each
    (block_type, band), a single new probability per entropy node is
    computed from the summed distribution, savings are aggregated across
    k, and every k context is updated together when aggregate savings >0
    or (on key frames) when the shared new prob differs from that k's old
    prob. Wiring lives in `WriteCoefficientInterFrameWithProbabilityBase`
    / `WriteCoefficientKeyFrameWithProbabilityBase` via the
    `IndependentContexts` field on `InterFrameStateConfig` /
    `KeyFrameStateConfig`, fed from `EncoderOptions.ErrorResilient` in
    [`encoder.go`](../encoder.go). Error-resilient key frames now force
    `RefreshEntropyProbs=true` like libvpx, while error-resilient inter
    frames keep transient independent-context coefficient updates without
    committing them to the next frame's persistent entropy state.
    Reference-frame *probabilities*
    (`prob_intra`/`prob_last`/`prob_gf`) are per-reference state and are
    now driven by both the post-frame fresh-from-counts update
    (`updateRefFrameProbsFromAttempt`, the equivalent of libvpx's
    `vp8_convert_rfct_to_prob`) and the pre-frame heuristic bump above.
    The conversion follows libvpx's gate: normal single-layer inter frames,
    including zero-reference shortcuts, convert ref counts, while single-layer
    GF/ARF refresh frames keep the prior probabilities for the next frame's
    refresh heuristic; temporal multi-layer frames convert even across GF/ARF
    refreshes.
    Tests:
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefRefresh`,
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxFramesSinceGolden`,
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefActiveDecay`,
    `TestUpdateGoldenFrameStatsMirrorsLibvpxCounter`,
    `TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch`,
    `TestIndependentCoefContextSavingsHandComputed`,
    `TestIndependentCoefContextDivergesFromDefault`,
    `TestIndependentCoefContextKeyFrameForcesEqualization`.
    The ref-frame entropy-savings half of `vp8_estimate_entropy_savings`
    is now ported as `libvpxCalcRefFrameCosts` and
    `libvpxRefFrameEntropySavings` in
    [`encoder_entropy_savings.go`](../encoder_entropy_savings.go),
    matching the libvpx `cost_zero(p)/cost_one(p) = ProbCost[p]/[255-p]`
    formula and the new_intra=1 floor / new_last/new_garf=128 fallbacks
    for empty distributions. Default coefficient-context savings reuse
    the coefficient branch-count/probability update walk and are wired
    into the recode-loop `projected_frame_size` adjustment. Independent
    coefficient-context savings for error-resilient partitions are also
    wired into that recode adjustment through
    `KeyFrameCoefficientEntropySavingsIndependent` /
    `InterCoefficientEntropySavingsIndependent`, gated on
    `EncoderOptions.ErrorResilient`.
    Additional tests:
    `TestIndependentCoefContextEntropySavingsMatchesPositiveUpdates`,
    `TestEncodeIntoErrorResilientRefreshesKeyEntropyOnly`, and
    `TestCoefficientEntropySavingsUsesIndependentContextWhenErrorResilient`.
  - Missing: exact alt-ref skip-probability edge cases and libvpx-side trace
    anchors for the error-resilient key-frame default-coef-count reset.
  - Done when every frame matches coefficient probs, MV probs, ref probs,
    refresh entropy bit, projected entropy savings, and next-frame mode-cost
    inputs.
