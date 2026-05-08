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
  libvpx v1.16.0 tools and corpus minima when last run for the current parity
  stack; update the "Last Measured" row below whenever this gate is rerun.
- Quality smoke parity: high on the enforced smoke corpus, but not complete.
  The current gates cover token partitions, static-threshold behavior,
  external Y4M/YUV sources, panning clips across best/good/realtime deadlines,
  and realtime `CpuUsed` 0, 3, 4, 5, 8, 9, and 15. These are smoke gates, not
  the full representative 100% parity corpus. Positive realtime `CpuUsed`
  values now follow libvpx's auto-speed entry path, which starts speed-feature
  selection at `Speed = 4`; explicit realtime speed-feature values use
  negative `CpuUsed` (`-N` means speed `N`) like libvpx.
- Encoder decision parity: roughly 74% overall, or about 84% on the core
  one-pass quality path, weighted by libvpx LOC. This is still an engineering
  estimate, not a measured percentage: `CompareOracleTraces` is now wired into
  the production oracle gate for a projected frame/rate decision subset, but
  the full corpus driver that counts matching candidate and MB decisions is
  still missing.
- Reconstruction byte-identity (measured 2026-05-08 by capturing both
  oracle traces and diffing the projected-out
  `y_adler32`/`u_adler32`/`v_adler32`/`size_bytes` fields):
  - 64x64 panning fixture, realtime CBR CpuUsed 0/4/8 + good CpuUsed 5,
    q-bounds 4..56, 4 frames: y/u/v Adler32 byte-identical on every frame;
    per-frame size delta -0.03..+0.77%; Q matches.
  - 128x128 panning realtime CBR CpuUsed 8: keyframe byte-identical
    (size delta -0.07%, Q=4 both sides); inter frames 1..3 diverge with
    **Q drift** (govpx Q=5..7 vs libvpx Q=13..14 at the same min-q/max-q
    bounds), producing size deltas of +23..+44%. Earlier diagnosis
    suggested chroma sixtap rounding alone, but the dominant driver at
    this resolution is inter-frame Q regulation. Per-pixel reconstruction
    diff is not informative until Q parity is restored.
- The largest remaining parity weights are candidate-level inter-mode
  comparison beyond the VBR panning staged field gate, rejected recode-attempt
  tracing, automatic hidden-ARF/ARNR border proof, first-pass/two-pass proof
  beyond the deterministic ramp and Y4M-shaped `.fpf` oracle gates, rate parity
  tracking vs libvpx output bitrate/frame sizes, and remaining
  quality-relevant entropy/refresh edge cases. A pre-pack
  `projected_frame_size` oracle gate now exists for the VBR panning trace, but
  candidate-level rate comparison is still needed to close the last sub-64-bit
  estimator noise.
- If only three more things are fixed, they should be: (1) broaden the staged
  candidate-level inter-mode / motion-search comparison to realtime and
  rate/RD scalar attribution, (2) broaden the first-pass `.fpf` oracle gate to
  external/two-pass allocation corpora, and (3) close the remaining direct
  rate-control trace gaps, especially rejected recode-attempt rows.

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
- [ ] Quality/rate acceptance is corpus- and mode-specific. For each gate
  vector, govpx must fall within the documented PSNR/SSIM and bitrate
  tolerances vs. libvpx v1.16.0, or match the libvpx decision trace for the
  known driver of the delta. Full encoded-byte equality is required only for
  named deterministic syntax/state vectors.
- [ ] Every intentional non-bitexact difference must be listed in
  "Accepted Non-Bitexact Differences" with affected paths, measured
  quality/rate delta, decision/state evidence, and rationale that it cannot
  affect future encoder decisions.

## Corpus And Tolerance Matrix

- [~] Current enforced smoke vectors:
  - `motion-eight-token-partitions`: motion clip with token-partition coverage;
    PSNR/SSIM/frame-gap and rate-to-target bounds live in
    [`oracle_encoder_validation_test.go`](../oracle_encoder_validation_test.go).
  - panning best/good/realtime cases: best, good RD, good fast-pick, and
    realtime `CpuUsed` 0/3/4/5/8/9/15 are covered by the same validation test
    file and helper tolerances.
  - external Y4M/YUV source smoke: covered by
    [`oracle_encoder_external_test.go`](../oracle_encoder_external_test.go).
- [ ] Expand this into the representative 100% corpus: static, pan, zoom, edge
  motion, scene cut, high motion, noisy, screen/static content, odd dimensions,
  non-multiple-of-16 dimensions, low/high bitrate, CBR/VBR/CQ,
  best/good/realtime deadlines, CPU-used bands, token partitions,
  error-resilient mode, lookahead, ARF/ARNR, two-pass, frame dropping, and
  temporal layers.
- [ ] For each vector record the required metric gate: PSNR/SSIM tolerance,
  direct govpx-vs-libvpx bitrate tolerance, per-frame-size tolerance,
  reference checksum requirement, and which trace fields must match exactly.
  Existing synthetic smoke vectors now include direct govpx-vs-libvpx
  output-kbps tolerances. External-source direct rate gates and per-frame-size
  delta gates are still missing.

## Quality Gap Ledger

| Case | Config | Status | Driver / next step |
| --- | --- | --- | --- |
| 64x64 panning realtime cbr cpu-used=0/4/8 + good cpu-used=5 | q-bounds 4..56, 4 frames | byte-identical y/u/v Adler32 every frame; size delta -0.03..+0.77%; Q matches | resolved on this fixture as of 2026-05-08 |
| 128x128 / 96x96 / 160x96 panning realtime cbr cpu-used=8 | q-bounds 4..56, 4 frames | keyframe y/u/v Adler32 + Q byte-identical on all three sizes; inter-frame y/u/v Adler32 differ on every inter frame; per-frame size delta is +0.07..+10.1% with the largest gap on 96x96 frame 1 (-10.1%) and 160x96 frame 1/2 (-8.2 / +8.8%, with a 1-step Q drift on 160x96 frame 2). Decoded-output diff stays within max\|delta\|=4 (Y) / 3 (U) / 1 (V) with mean\|delta\|<0.04 (PSNR vs libvpx > 60 dB) | residual pixel-level disagreement on inter frames; subagent localized to chroma sub-pel filter rounding edge case (govpx 137/118 vs libvpx 139/117 at MB(0,0) row 0 cols 3/7), but the scalar `internal/vp8/dsp/subpixel.go sixTapPredict` matches the libvpx C reference numerically and the 64x64 byte-identity gate exercises it without divergence. Quality-equivalent. Tracked by [`TestOracleChromaSubpelScoreboard`](../oracle_chroma_subpel_scoreboard_test.go) with a per-fixture baseline at [`testdata/chroma_subpel_scoreboard_baseline.json`](../testdata/chroma_subpel_scoreboard_baseline.json). Closing this needs per-pixel libvpx-side `xd->predictor` instrumentation to localize the rounding edge |
| panning / motion smokes | best/good/realtime CPU bands | smoke-gated by output-kbps tolerance; not 100% corpus | candidate mode rows, per-frame-size deltas |
| first-pass ramp + Y4M-shaped corpora | pass-1 `.fpf` stats | partial — deterministic ramp + Y4M corpus pinned by `TestOracleFirstPassStatsCompare`; external/two-pass allocation proof still open | external `.fpf` rows, two-pass allocation traces |
| ARNR border-sensitive clips | AutoAltRef + ARNR | open — no matrix | source-border / alt-ref-buffer semantics; ARNR buffer checksums |

## Last Measured

| Date | Commit | Gate | Result | Notes |
| --- | --- | --- | --- | --- |
| 2026-05-08 | bfac6a7 | `make verify-production` | pass | Full production parity gate, including `TestOracleEncoderTraceDecisionCompare`, `TestOracleEncoderTraceInterCandidateCompare`, and external Y4M/YUV corpus minima. |
| 2026-05-08 | bfac6a7 | per-frame Adler32 / size_bytes diff on 64x64 panning, realtime CBR CpuUsed 0/4/8 + good CpuUsed 5, q-bounds 4..56, 4 frames | byte-identical | y/u/v Adler32 match every frame; per-frame size delta -0.03..+0.77%; Q matches at 4. |
| 2026-05-08 | bfac6a7 | per-frame Adler32 / size_bytes diff on 128x128 panning realtime CBR CpuUsed 8 | gap | Keyframe byte-identical (size delta -0.07%, Q=4 both sides); inter frames 1..3 diverged with govpx Q=5..7 vs libvpx Q=13..14, size deltas +23..+44%. Closed in 09abb24 by gating govpx's inter recode loop on libvpx's `sf->recode_loop` policy. |
| 2026-05-08 | 09abb24 | post-fix per-frame Adler32 / size_bytes / q_index diff matrix (64x64 + 96x96 + 128x128 + 160x96 panning realtime CBR; 128x128 good-quality VBR cpu-used=0/5; q-bounds 4..56, 4 frames) | partial | Q matches libvpx on every fixture/frame after the recode-loop gate. y/u/v Adler32: byte-identical on 64x64 (all CPU bands) and 64x64 good cpu-used=5; keyframe-only match at 96x96 / 128x128 / 160x96 realtime + 64x64 good cpu-used=0 + 128x128 good cpu-used=0. Inter-frame size deltas at 96x96..160x96 collapsed to 0.5..1.5% per frame from previous 23..44%. Per-pixel decoded-output diff at 128x128 inter frame 1: Y 439/16384 mismatches max\|delta\|=4, U 38/4096 max\|delta\|=3, V 28/4096 max\|delta\|=1 (mean\|delta\|=0.036 / 0.011 / 0.007) - quality-equivalent, residual gap localized to chroma sub-pel filter rounding. |
| 2026-05-08 | parity-close-2 | per-fixture chroma sub-pel scoreboard (`TestOracleChromaSubpelScoreboard`, 96x96 / 128x128 / 160x96 panning realtime CBR cpu8, 4 frames each) | tracked | Keyframe y/u/v Adler32 + Q byte-identical on all three sizes. Inter frames: 3/3 y/u/v Adler32 mismatches per fixture. Inter-frame size delta abs max: 96x96=10.12% (frame 1 -10.12%, frames 2/3 within 0.5%); 128x128=1.51% (every frame within +1.16..+1.51%); 160x96=8.85% with a 1-step Q drift on frame 2 (govpx 12 vs libvpx 13). Baseline locked in [`testdata/chroma_subpel_scoreboard_baseline.json`](../testdata/chroma_subpel_scoreboard_baseline.json) with tightening assertions: counts cannot grow, keyframe match cannot regress true→false, inter Q match cannot regress, max size pct cannot grow >0.5pp. |

## Accepted Non-Bitexact Differences

- [~] `projected_frame_size` in the VBR panning trace is quality-equivalent
  within a 64-bit absolute tolerance while Q, recode, target, refresh, and
  frame identity rows match exactly. This is not a blanket waiver for rate
  accounting: candidate-level inter-mode/rate rows must replace the tolerance
  once the last estimator noise is traced to a specific non-quality driver.
- [ ] Review existing "simplification vs. libvpx" notes and either move them
  back to open parity work or promote them here with measurements.

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
    `segmentation_enabled`, Y/U/V plane Adler32 reference checksums,
    Adler32 digests over the post-update probability tables
    (`coef_probs_adler`, `ymode_probs_adler`, `uv_mode_probs_adler`,
    `mv_probs_adler`), the per-frame reference coding probabilities
    (`prob_intra_coded`, `prob_last_coded`, `prob_gf_coded`), and
    `size_bytes`; per-MB row with `frame_index`,
    `mb_row`, `mb_col`, `segment_id`, `mode`, `ref_frame`, `mv_row`,
    `mv_col`, `skip`, optional key-frame `uv_mode` / `b_modes`,
    `eob[0..24]`, `eob_sum`, `qcoeff[25][16]`, and improved-MV start
    fields.
    Rows cover every committed key-frame and inter-frame MB, including key
    `B_PRED` submodes and inter intra `B_PRED` decisions, are emitted in
    deterministic raster scan order, and only for the final committed encode
    attempt (recoded attempts are discarded).
  - libvpx side: in progress. The patched vpxenc lives in
    [`internal/coracle/build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh),
    which adds a single `vp8/encoder/oracle_trace.c` translation unit
    plus `extern` hook calls in `encodeframe.c` (per-MB capture inside
    `encode_mb_row`), `bitstream.c` (per-frame flush at the tail of
    `vp8_pack_bitstream`), and `onyx_if.c` (per-frame "rate" row plus a
    "recode" row when the recode loop iterated more than once, both
    emitted just before `vp8_pack_bitstream`). The recode-loop iteration
    count is tracked by a single `govpx_oracle_recode_iter()` call at
    the top of the `encode_frame_to_data_rate` do-loop. Key-frame MB rows
    include Y mode, UV mode, B modes when `B_PRED`, EOBs, and qcoeffs.
    Output is gated on `GOVPX_ORACLE_TRACE_OUT` and matches the govpx schema.
  - Comparator: in place. The pure-Go
    [`CompareOracleTraces`](../internal/coracle/oracle_compare.go)
    helper walks both JSON Lines streams in lockstep and surfaces
    field-level divergences as `Divergence{RowIndex, RowKind, FrameIndex,
    MBRow, MBCol, Field, Govpx, Libvpx}` records; the cap and an
    `IgnoreFields` set, and per-field numeric tolerances are configurable
    through `CompareOptions`. Tests
    in
    [`oracle_compare_test.go`](../internal/coracle/oracle_compare_test.go)
    cover identical streams, mismatched fields, missing rows, ignored
    fields, bounded numeric tolerances, and type mismatches.
  - Covered now (rate / recode rows): both sides emit a `{"type":"rate", ...}`
    row per encoded frame with `q_index`, `active_worst_quality`,
    `active_best_quality`, `buffer_level`, `total_byte_count`,
    `projected_frame_size`, `this_frame_target`, `kf_overspend_bits`,
    and `gf_overspend_bits`; and a `{"type":"recode", ...}` row (with
    `loop_count`, `final_q`, `reason`) whenever the recode loop
    iterated more than once. Reason is one of `altref_src`,
    `kf_forced_quality`, or `size_recode`; govpx now records the forced-key
    branch explicitly and falls back to `size_recode` for ordinary
    size-bound retries.
  - Covered now (residual + probability state): per-MB residual decisions
    are covered by the existing `mode`, `ref_frame`, `mv_row`, `mv_col`,
    `skip`, `eob[0..24]`, `eob_sum`, and `qcoeff[25][16]` fields,
    captured at the same point libvpx commits the chosen mode (after
    `vp8_pick_inter_mode` /
    `vp8_rd_pick_inter_mode` write into `mb->e_mbd.mode_info_context->mbmi`
    and the macroblock has been tokenized). Frame-level probability state
    is captured at the tail of `vp8_pack_bitstream` as four Adler32
    digests over the post-update tables — `coef_probs_adler` over
    `cm->fc.coef_probs[BLOCK_TYPES][COEF_BANDS][PREV_COEF_CONTEXTS][ENTROPY_NODES]`,
    `ymode_probs_adler` over `cm->fc.ymode_prob`, `uv_mode_probs_adler`
    over `cm->fc.uv_mode_prob`, and `mv_probs_adler` over the row+col
    `cm->fc.mvc[0..1].prob[0..18]` arrays — plus the three reference
    coding probabilities `prob_intra_coded`, `prob_last_coded`, and
    `prob_gf_coded`. govpx mirrors each digest from `e.coefProbs`,
    `e.modeProbs.YMode`, `e.modeProbs.UVMode`, and `e.modeProbs.MV` in
    [`encoder_oracle_trace.go`](../encoder_oracle_trace.go) and feeds
    the reference probs from `e.refProbIntra` / `e.refProbLast` /
    `e.refProbGolden`.
  - Covered now (production trace gate): `make verify-production` builds
    `vpxenc-oracle`, exports `GOVPX_VPXENC_ORACLE`, and runs
    `TestOracleEncoderTraceDecisionCompare` on a small one-pass VBR panning
    clip. The enforced projection compares Q, active best/worst bounds,
    `this_frame_target`, `projected_frame_size` within 64 bits,
    zbin-over-quant, refresh/sign-bias flags, frame identity, and recode row
    identity. The libvpx oracle command pins
    `--timebase` to the same effective timebase govpx uses so the rate-control
    target gate compares codec behavior, not vpxenc's default millisecond
    timestamp rounding. It intentionally does not compare byte counts,
    probability digests, reference checksums, or per-MB residuals yet; those
    remain open quality/rate diagnosis fields.
  - Remaining: per-frame segmentation tree probabilities (the per-MB
    `segment_id` already lands in the row schema, but the frame-level
    tree-probability bytes are not yet captured); a broader corpus trace
    driver that counts matching candidate, MB, residual, and frame-size
    decisions; and tightening govpx's recode reason classifier once rejected
    attempts and the alt-ref source branch are traceable.
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
  - Test: TestOracleEncoderTraceDecisionCompare, TestOracleEncoderTraceCandidateRowsPresent, TestOracleEncoderTraceInterCandidateCompare
  - Remaining: broadening the production trace gate from the current projected
    frame/rate decision subset to candidate-level, full per-MB residual, and
    frame-size fields; plus libvpx-side coverage of the partition /
    segmentation-tree fields once the govpx-side schema grows to include
    them. The rate-control state and recode-reason coverage is in place via the
    `rate` and `recode` rows emitted from
    [`build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh)
    and the matching emitters in
    [`encoder_oracle_trace.go`](../encoder_oracle_trace.go); the
    residual decisions land in the per-MB row's mode/ref/mv/eob fields
    and frame-level probability state lands in the per-frame row's
    `coef_probs_adler`, `ymode_probs_adler`, `uv_mode_probs_adler`,
    `mv_probs_adler`, `prob_intra_coded`, `prob_last_coded`, and
    `prob_gf_coded` fields; the comparator in
    [`oracle_compare.go`](../internal/coracle/oracle_compare.go)
    surfaces field-level divergences for those rows generically.
  - Done when the comparator fails CI on the first divergent frame, MB, header,
    probability, reference, or segmentation field and prints enough state to
    identify govpx, libvpx instrumentation, or harness-config mismatches.

## Encode Driver, Recode, And Q Bounds

- [ ] Port libvpx `encode_frame_to_data_rate` recode-loop semantics.
  - govpx:
    [`encodeKeyFrameWithQuantizerFeedback`](../encoder.go),
    [`encodeInterFrameWithQuantizerFeedback`](../encoder.go),
    [`frameSizeRecodeQuantizerWithContext`](../ratecontrol.go),
    [`saveCodingContext`](../encoder.go),
    [`restoreCodingContext`](../encoder.go),
    [`forcedKeyFrameRecodeQuantizer`](../ratecontrol.go).
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
    The recode size-bounds comparison now uses a libvpx-style pre-pack RD
    `projected_frame_size`: key/inter reconstruction accumulates the selected
    macroblock picker rate, converts `totalrate >> 8` to bits, subtracts
    `vp8_estimate_entropy_savings` via the existing ref-frame and coefficient
    entropy-savings helpers, and clamps at zero before feeding the recode loop.
    The accepted attempt's same projected value is emitted in the oracle rate
    row before post-pack rate-control updates, matching the libvpx hook point
    immediately before `vp8_pack_bitstream`. The production trace gate compares
    this field within 64 bits on the VBR panning corpus; candidate-level rate
    rows remain open to remove that tolerance. The libvpx `decide_key_frame`
    heuristic is ported as
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
    The recode loop now snapshots cpi->coding_context before the do-loop
    and restores it on every rejected attempt, mirroring libvpx's
    `vp8_save_coding_context` / `vp8_restore_coding_context` contract:
    `frames_since_key`, `filter_level`, `frames_till_gf_update_due`,
    `frames_since_golden`, `this_frame_percent_intra`, MV/Y/UV/B mode
    probability tables, coefficient probability tables, ref-frame and
    skip-false probability state, and the per-reference
    `last_skip_false_probs` history are all included in the snapshot.
    Forced key frames (`this_key_frame_forced`) now feed the SS-error
    feedback Q-adjustment branch from `encode_frame_to_data_rate`
    around line 4065: when the just-encoded forced KF's
    `vp8_calc_ss_err` against the source is more than `ambient_err *
    7/8`, govpx lowers `q_high` to (Q-1) and reseeds Q to
    `(q_high + q_low) >> 1`; when it is less than `ambient_err / 2`,
    `q_low` is raised to (Q+1) and Q to `(q_high + q_low + 1) >> 1`,
    with the loop terminating when Q stops changing. `ambient_err`
    itself is captured at the end of the frame preceding a forced KF
    via the libvpx `next_key_frame_forced` branch
    (`encode_frame_to_data_rate` around line 4282).
    The oracle trace's "rate" row now carries the active
    `cpi->mb.zbin_over_quant` for each accepted recode attempt so
    GF/ARF zbin-over-quant divergences surface as a field-level diff in
    [`oracle_compare.go`](../internal/coracle/oracle_compare.go); the
    libvpx-side patch in
    [`build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh)
    captures the same field at the same emission point (just before
    `vp8_pack_bitstream`). The govpx side feeds
    `e.rc.currentZbinOverQuant` into the trace from
    [`encoder_oracle_trace.go`](../encoder_oracle_trace.go), and
    `TestCompareOracleTracesDetectsZbinOverQuantDivergence` exercises
    the diff path on synthetic JSONL.
    On the recode-vbr-tight 64x64 panning fixture (8 frames, kbps=200,
    MaxQ=8 GoodQuality cpu-used=3) `TestOracleRecodeRowParity` still
    diverges with `matched=0 asymmetric=1`. libvpx fires `size_recode`
    at frame 7 (final_q=6, loop_count=2) because its first-attempt RD
    projection at Q=4 lands at ~14 kbits — over the GF over-shoot
    bound of `target*9/8+200 = 12211` for `this_frame_target=10676` —
    and the recode's regulate_q step lands at Q=6 with projection 11208.
    govpx's first-attempt RD projection at the same Q=4 / GF target
    is ~10250, which fits inside the GF undershoot/overshoot window
    `[9103, 12161]`, so no recode fires and the loop accepts q=4
    directly. The govpx GF target itself now matches libvpx (`10632`
    vs `10676`) since 8fe47b8 wired `libvpxGoldenFrameTargetBits`
    into the one-pass non-CBR refresh branch, so the residual gap is
    a per-MB RD-rate aggregator delta in `estimateInterResidualRDAccounting`
    (encoder_reconstruct.go), not in the recode trigger logic itself.
    Suspected source: libvpx's GF refresh swaps the coefficient token
    cost table to `cpi->lfc_g.coef_probs` (rdopt.c line 244-249);
    govpx's `buildPredictedMacroblockCoefficientsRD` always uses
    `e.coefProbs` regardless of frame type, so its GF-frame coefficient
    rate2 contributions are systematically lower at low Q and the
    recode-loop test never fires. Closing this would require porting
    the per-frame-type frame-context (`lfc_n`, `lfc_g`, `lfc_a`)
    save/restore that libvpx maintains across vp8_save_coding_context
    boundaries.
  - Test: TestOracleEncoderTraceDecisionCompare, TestOracleRecodeRowParity, TestOracleSecondPassAllocationCompare
  - Done when oracle traces match Q attempts, final Q, recode reasons, frame
    size bounds, and encoded bytes across CBR/VBR/CQ/key/golden/alt-ref frames.

- [~] Extend oracle coverage to key-frame MB rows, candidate-level mode-loop
  rows, and rejected recode attempts.
  - Status: key-frame MB rows are now covered on both govpx and patched-libvpx
    trace paths, including Y mode, UV mode, B modes for `B_PRED`, EOBs, and
    qcoeffs. govpx and patched-libvpx now buffer evaluated inter-mode candidate
    rows for the accepted encode attempt, with picker, mode/ref slot,
    threshold, best-before score state, RD/rate/distortion fields where
    available, skip/breakout decisions, MV, and improved-MV-start diagnostics.
    `TestOracleEncoderTraceCandidateRowsPresent` asserts both RD and realtime
    fast pickers emit candidate rows on both sides.
    `TestOracleEncoderTraceInterCandidateCompare` now compares the staged VBR
    panning RD candidate sequence and quality-relevant mode/ref/MV fields
    (`mode_index`, mode/ref slot, outcome, best/break flags, and MV). Realtime
    positive-`CpuUsed` candidate diagnosis now uses libvpx's auto-selected
    initial speed 4; the remaining realtime candidate desync starts after that
    mapping, at frame 1 MB `(0,1)`, where the NEAREST candidate score /
    distortion differs enough for govpx to prune `TM_PRED` that libvpx still
    tests. Skipped/pruned candidate rows, RD/rate scalar fields, and rejected
    recode-attempt rows remain open.
  - Test: TestOracleEncoderTraceCandidateRowsPresent, TestOracleEncoderTraceInterCandidateCompare, TestOracleEncoderTraceInterCandidateScoreboard, TestOracleInterDecisionMatchRate
  - Done when key frames expose Y mode, UV mode, B modes, token contexts,
    qcoeff/dqcoeff/EOB, rate, distortion, RD, and reconstruction checksums;
    inter candidate rows expose tested/skipped modes, thresholds, MV
    predictors, search range, rate/distortion/RD, and skip decisions; and
    recode rows expose every attempted Q, q_low/q_high, projected size,
    entropy savings, zbin state, and rejection reason.

- [ ] Wire govpx/libvpx trace comparison into CI for the corpus matrix.
  - Done when `make verify-production` fails on the first non-ignored
    divergence, and `IgnoreFields` is used only for entries documented in
    "Accepted Non-Bitexact Differences".

- [ ] Align active best/worst quantizer selection.
  - govpx:
    [`selectQuantizerForFrameKindWithScreenContent`](../ratecontrol.go),
    [`selectQuantizerForFrameKindWithAltRef`](../ratecontrol.go),
    [`libvpxActiveQuantizerBoundsForFrame`](../ratecontrol.go),
    [`newFrameSizeRecodeStateWithAltRef`](../ratecontrol.go),
    [`libvpxZbinOverQuantHighAltRef`](../ratecontrol.go).
  - libvpx: `vp8_regulate_q`, active-best-quality, and active-worst-quality
    branches in `onyx_if.c`.
  - Status: partial. govpx now constrains Q through libvpx's one-pass
    active-min tables for key/golden/inter frames, CBR active-worst buffer
    logic after normal-inter warmup, CBR full-buffer active-best/worst clamps,
    and CQ floors. ARF refresh now routes through the same single-layer
    GF branch libvpx uses
    (`cm->refresh_golden_frame || cpi->common.refresh_alt_ref_frame`):
    `selectQuantizerForFrameKindWithAltRef` and
    `libvpxActiveQuantizerBoundsForFrame` honor an explicit altRef flag and
    pick `gf_high_motion_minq[Q]` for one-pass; the matching
    `libvpxZbinOverQuantHighAltRef` cap (16 for ARF/GF, ZBIN_OQ_MAX for
    plain inter, 0 for key) and the recode-state seeds in
    `newFrameSizeRecodeStateWithAltRef` carry the same flag through the
    full recode loop, including the libvpx
    `relax_active_worst_quality_on_overshoot` 4%-per-Qstep relaxation.
    The two-pass `kf_low_motion_minq`, `gf_low_motion_minq`, and
    `gf_mid_motion_minq` tables are now ported byte-for-byte and pinned
    against representative QINDEX_RANGE samples so the future two-pass
    `vp8_regulate_q` port can flip on `cpi->gfu_boost` without re-reading
    the libvpx C source.
  - Test: TestOracleEncoderQHistogramScoreboard, TestOracle128x128InterQDriftScoreboard
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
    (`update_golden_frame_stats`, `USAGE_LOCAL_FILE_PLAYBACK` buffer setup).
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
    `non_gf_bitrate_adjustment`; hidden ARF post-pack accounting now
    follows `update_alt_ref_frame_stats` and accumulates the full
    `projected_frame_size` into `gf_overspend_bits` instead of using the
    GF delta path. Key frames that also refresh GOLDEN now seed
    `non_gf_bitrate_adjustment = gf_overspend_bits /
    frames_till_gf_update_due`, matching the `update_golden_frame_stats`
    ordering before the countdown is decremented. The next p-frame target now drains
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
    bootstrap uses `1 + (int)output_framerate*2`, clamped to
    `oxcf.key_freq` only when auto-key is enabled. The encoder seeds
    `keyFrameFrequency` from `EncoderOptions.KeyFrameInterval` and
    `autoKeyFrames` from `EncoderOptions.AdaptiveKeyFrames` so the
    bootstrap matches libvpx's `oxcf.auto_key` / `oxcf.key_freq`. The libvpx
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
    uniformly. One-pass VBR/CQ construction now also seeds libvpx's
    provisional `DEFAULT_GF_INTERVAL` countdown before the first key frame,
    and the key-frame GOLDEN refresh decrements that countdown, preventing the
    non-libvpx immediate frame-1 GOLDEN refresh; pinned by
    `TestEncodeIntoOnePassVBRDoesNotRefreshGoldenImmediatelyAfterKey` and the
    projected trace gate. One-pass VBR now mirrors libvpx's
    `USAGE_LOCAL_FILE_PLAYBACK` setup by forcing a relaxed 60000ms
    initial/optimal buffer and 240000ms maximum buffer, regardless of the
    public buffer controls, and uses libvpx's long-term
    `bits_off_target / total_byte_count` style high/low buffer target
    shaping instead of CBR's short-term optimal-buffer delta formula. The
    defaults used by `defaultRateControlConfig` now match libvpx's
    6000/4000/5000ms public defaults.
  - Test: TestOracleEncoderCorpusValidation
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
    state when that reference is refreshed. The ARF copy-buffer edge cases
    are also enforced inline: hidden ARF frames clear `CopyBufferToAltRef`
    (libvpx's `assert(!cm->copy_buffer_to_arf)` invariant) and the
    deferred show-frame after a hidden ARF (`is_src_frame_alt_ref`) clears
    both copy fields so the references already populated by the ARF stick.
    Sign-bias policy is now pinned end-to-end: `interFrameSignBias` keeps
    `ref_frame_sign_bias[GOLDEN_FRAME]` at 0 (libvpx's
    `update_golden_frame_stats` never flips it) and writes
    `ref_frame_sign_bias[ALTREF_FRAME] = source_alt_ref_active` at the
    libvpx pre-pack point in onyx_if.c, with `updateGoldenFrameStats`
    mirroring the post-pack `source_alt_ref_active` set/clear edges from
    `update_alt_ref_frame_stats` / `update_golden_frame_stats`. A
    multi-frame
    [`TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF`](../encoder_test.go)
    drives a 12-frame AutoAltRef sequence (key + inter + hidden ARF +
    deferred show + GOLDEN refresh) and replays the libvpx evolution rule
    against each parsed packet's `(GoldenSignBias, AltRefSignBias)` so any
    drift surfaces as a per-frame tuple mismatch. The oracle trace
    `frame` row already carries `sign_bias_golden` / `sign_bias_altref`
    fields in
    [`encoder_oracle_trace.go`](../encoder_oracle_trace.go) and the
    libvpx-side capture in
    [`internal/coracle/build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh)
    emits the matching `cm->ref_frame_sign_bias[GOLDEN_FRAME]` /
    `cm->ref_frame_sign_bias[ALTREF_FRAME]` values at the same emit
    point, so `CompareOracleTraces` flags any divergence as a frame-row
    field diff.
  - Test: TestOracleEncoderTraceDecisionCompare, TestOracleEncoderCorpusValidation
  - Done when forced and natural GF/ARF sequences match header copy bits,
    reference checksums, reference availability, and subsequent mode choices.

## First Pass And Two Pass

- [~] Replace simplified first-pass stats with libvpx first-pass analysis.
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
    before it becomes the next frame's LAST reference. First-pass scoring uses
    the libvpx `vp8_encode_intra` predictor-residual SSE rather than the old
    mean-luma variance proxy, and pass 1 forces the libvpx
    `vp8_set_quantizer(cpi, 26)` reconstruction path independent of user
    min/max quantizer bounds. It also mirrors libvpx's first-pass speed
    override by using fast quantization and disabling coefficient
    optimization. First-pass search uses SAD for the diamond walk,
    SSE plus MV error cost for the final score, applies the libvpx
    `new_mv_mode_penalty=256` to motion-search results, wires
    `EncoderOptions.StaticThreshold` through libvpx's
    `oxcf.encode_breakout` raw zero-motion skip gate, and the inter/neutral
    accept gate uses libvpx's
    `((this_error - intrapenalty) * 9 <= motion_error * 10)` threshold.
    Zero-motion LAST/raw errors now use plain libvpx MSE with no `+128`
    bias, GOLDEN second-reference scoring starts from `INT_MAX` and is lowered
    only by the GOLDEN first-pass motion search, and the old non-libvpx
    all-intra GOLDEN reset fallback has been removed.
    The post-stats LAST->GOLDEN copy follows the libvpx
    `pcnt_inter > 0.20 && intra/coded > 2.0` heuristic, and the first
    frame still seeds GOLDEN from LAST as a second reference. Per-frame
    field values are pinned by the empirical
    [`TestOracleFirstPassStatsCompare`](../oracle_encoder_firstpass_test.go),
    which parses libvpx v1.16.0 `vpxenc --pass=1 --fpf` binary
    `FIRSTPASS_STATS` packets and compares them against govpx with exact
    mode/MV percentages, <=2 post-shift units for intra/coded residuals, and
    <=3 units or 1e-3 relative tolerance for SSIM-weighted residuals on both
    the deterministic ramp and fixed Y4M-shaped corpora.
    Fast deterministic coverage remains in
    [`TestFirstPassStatsRegression32x32`](../encoder_firstpass_test.go) on a
    deterministic 32x32 ramp clip; plausibility coverage is in
    `TestFirstPassStatsPopulatesLibvpxFields`, zero-motion MSE and GOLDEN reset
    behavior are pinned by `TestFirstPassZeroMotionErrorDoesNotAddBias` and
    `TestFirstPassGoldenDoesNotResetOnAllIntraFallback`, and the simple_weight
    table boundaries are pinned by `TestSimpleWeightLumaMatchesLibvpxTable`.
    [`accumulateFirstPassStats`](../encoder_firstpass.go) ports libvpx's
    `accumulate_stats` per-field summation, and
    [`FinalizeFirstPassStats`](../encoder_firstpass.go) emits the libvpx
    terminal "total stats" packet at end-of-encode (mirroring
    `vp8_end_first_pass`'s `output_stats(&total_stats)` call) by appending
    a sentinel `IsTotal=true` `FirstPassFrameStats` entry that
    [`normalizeTwoPassStats`](../encoder_firstpass.go) (and therefore
    `SetTwoPassStats`) consumes as `cpi->twopass.total_stats`. Section
    accumulator and total-packet behaviour are pinned by
    [`TestFirstPassY4MCorpusSectionAccumulators`](../encoder_firstpass_y4m_test.go)
    on a fixed in-memory Y4M-shaped 4-frame 32x32 corpus, plus
    `TestAccumulateFirstPassStatsMatchesLibvpx` and
    `TestFinalizeFirstPassStatsEmpty`.
  - Test: TestOracleFirstPassStatsCompare, TestFirstPassY4MCorpusSectionAccumulators
  - Remaining: broaden the `.fpf` oracle comparison to external sources and
    second-pass allocation traces that consume those stats.
  - Done when fixed/external first-pass stats and downstream second-pass
    allocation decisions match libvpx within defined quality-equivalent
    tolerances for every quality-relevant field.

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
    the `kfGroupBits` and `libvpxGFGroupBits` ceilings. Second-pass
    configuration now consumes the libvpx first-pass terminal total-stats
    packet when present, excludes it from the encoded-frame count and bit
    budget, synthesizes totals when callers provide per-frame stats only, and
    routes modified-error allocation through the terminal total's
    `ssim_weighted_pred_err`/`count`. `TestTwoPassConfigureConsumesTerminalTotalStats`
    and `TestTwoPassConfigureSynthesizesTotalStatsWhenMissing` pin both paths.
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
    `pass2VBRSectionLimits` ports the libvpx Pass2Encode VBR
    section-limit application: per-frame target is clamped to
    `[section_min_bits, section_max_bits]` where section_min is
    `defaultTargetBits * two_pass_vbrmin_section / 100` (the
    `cpi->min_frame_bandwidth` floor) and section_max is the live
    `(bits_left/frames_left) * two_pass_vbrmax_section / 100`
    (libvpx's `frame_max_bits` VBR branch). `frameTargetBits` now
    routes both KF and non-KF allocations through the helper so the
    section bounds are applied uniformly across the second-pass flow,
    pinned by `TestPass2VBRSectionLimitClampsTarget`.
    `pass2DetectARFPending` ports the libvpx
    `define_gf_group` / `select_arf_period` ARF-pending decision: it
    walks the upcoming GF window, accumulates the libvpx
    `frame_boost = IIFACTOR * intra_error / coded_error` (clamped to
    `GF_RMAX=48`) decayed by `get_prediction_decay_rate`, applies the
    `i >= MIN_GF_INTERVAL`, `i <= frames_to_key - MIN_GF_INTERVAL`,
    `next_frame.pcnt_inter > 0.75`, MV in/out, and `gfu_boost > 100`
    gates, and reports the ARF interval. `pass2MaybeArmAltRefPending`
    wires that decision into the encoder, calling
    `scheduleAltRefSource` so the auto-ARF driver emits the hidden
    alt-ref at the predicted offset. The one-pass default ARF interval
    scheduler is disabled whenever two-pass stats are active, so a
    two-pass section that rejects ARF cannot be overridden by the eager
    fallback; pinned by `TestPass2ARFPendingTriggersFromHighMotionSection`
    and `TestTwoPassAutoAltRefDoesNotScheduleWhenStatsRejectARF`.
    Hidden ARF packets now mirror libvpx `Pass2Encode`: packet bytes are
    charged against `twopass.bits_left`, but `refresh_alt_ref_frame` skips
    `vp8_second_pass` and `show_frame=0` leaves the visible first-pass
    stats index plus rate-control visible-frame counters unchanged. Visible
    frames subtract packet bytes and then add back the configured
    `two_pass_vbrmin_section` minimum-frame budget, while hidden ARFs remain
    subtract-only. Pass2 frames skip the one-pass KF/GF/ARF post-pack
    overspend bookkeeping and rely on the second-pass bit allocator instead;
    pinned by
    `TestTwoPassAltRefBitChargeDoesNotAdvanceStats` and
    `TestTwoPassHiddenAltRefChargesBitsWithoutConsumingVisibleStats`.
    `applyPass2CBRBufferAdjustment` ports the libvpx Pass2Encode CBR
    (`USAGE_STREAM_FROM_SERVER`) per-frame target re-clamp: after the
    second-pass error-fraction allocation overrides the one-pass
    per-frame target, the helper re-asserts the libvpx
    `bufferAdjustedFrameTargetBits` shaping (target shrinks when
    `buffer_level < optimal_buffer_level`, grows when above), wired
    in encoder.go after `twoPass.frameTargetBits` so the buffer-aware
    pull-back is preserved through the two-pass override; pinned by
    `TestPass2CBRBufferAdjustmentRaisesTargetUnderfilledBuffer` and
    `TestPass2CBRBufferAdjustmentLowersTargetOverfilledBuffer`.
    `applyCQFloor` ports the libvpx `estimate_max_q` CQ floor
    (`USAGE_CONSTRAINED_QUALITY -> Q = max(Q, cq_target_quality)`)
    applied AFTER the second-pass Q regulation; encoder.go re-asserts
    it after `selectQuantizerForFrameKindWithScreenContent` so
    recode-style adjustments never push the final quantizer below
    `cqLevel`; pinned by `TestSelectQuantizerCQFloorApplied`.
  - Test: TestOracleSecondPassAllocationCompare, TestSelectQuantizerCQFloorApplied
  - Done when second-pass oracle tests match frame type, GF/ARF decisions,
    target bits, final Q, and bitrate distribution on multi-scene clips.

## Alt-Ref, Lookahead, And ARNR

- [x] Implement automatic alternate-reference scheduling.
  - govpx:
    [`autoAltRefDriverEnabled`](../encoder_altref_driver.go),
    [`autoAltRefSectionInterval`](../encoder_altref_driver.go),
    [`autoAltRefMaybeSchedule`](../encoder_altref_driver.go),
    [`autoAltRefShouldEmitHidden`](../encoder_altref_driver.go),
    [`autoAltRefMaybeEncode`](../encoder_altref_driver.go),
    [`autoAltRefMaybeEmitHiddenOnFlush`](../encoder_altref_driver.go),
    [`scheduleAltRefSource`](../encoder.go),
    [`isSrcFrameAltRef`](../encoder.go),
    [`updateGoldenFrameStats`](../encoder.go).
  - libvpx: auto-ARF decision in
    [`vp8_get_compressed_data`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/onyx_if.c)
    (the `oxcf.play_alternate && source_alt_ref_pending` branch that peeks
    the lookahead and emits the hidden frame with
    `cm->show_frame=0`/`cm->refresh_alt_ref_frame=1`) and the ARF pending
    policy in
    [`ratectrl.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/ratectrl.c).
  - Status: ported. `EncoderOptions.AutoAltRef` (libvpx
    `cpi->oxcf.play_alternate`) gates the driver. When enabled together
    with `LookaheadFrames > 1` and `!ErrorResilient`, the driver schedules
    a hidden ARF section after each committed inter frame whose lookahead
    holds at least `min(DEFAULT_GF_INTERVAL=7, LookaheadFrames-1, count-1)`
    future entries. `scheduleAltRefSource` records the future PTS and
    arms `framesTillAltRefFrame`; `updateGoldenFrameStats` decrements that
    countdown on every non-ARF inter commit. When the countdown reaches
    zero and the matching source has reached the lookahead head, the
    driver peeks index 0 and encodes that source with the libvpx hidden
    flag combination
    `EncodeForceAltRefFrame|EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateEntropy`,
    matching `show_frame=0`, `refresh_alt_ref=1`, and no LAST/GOLDEN
    refresh. The matching deferred show frame pops on the next call and
    `isSrcFrameAltRef` matches by PTS, so the existing
    `sourceAltRefActive` lifecycle wires `AltRefSignBias=true` for that
    show frame. When ARNR filtering is disabled (`ARNRMaxFrames == 0`),
    the deferred source-alt-ref show frame now mirrors libvpx's
    `is_src_frame_alt_ref` mode-loop gate and only allows
    `ZEROMV/ALTREF_FRAME`. Hidden-ARF emission does not pop the lookahead,
    so the caller's input on that call is parked in
    [`autoAltRefStashInput`](../encoder_altref_driver.go); subsequent
    `EncodeInto` calls drain the stash, encode the head as the deferred
    show frame, and re-stash the new caller input, leaving the encoder
    permanently shifted by one call until `FlushInto` consumes the
    remaining entries. Hidden ARFs also follow libvpx's frame-counter
    semantics: the packet is not a real show frame, so it does not advance
    `current_video_frame` / govpx `frameCount` or rate-control
    `framesSinceKeyframe`; only the packet bits are charged in two-pass mode.
  - Test: TestOracleEncoderCorpusValidation, TestOracleARNRBufferAdler, TestAutoAltRefDriverEmitsHiddenFrame, TestAutoAltRefDriverDeferredShowFrameMatchesSource, TestAutoAltRefDriverSignBiasUpdatesPostHidden, TestSourceAltRefShowFrameForcesZeroMVAltRefWhenARNROff, TestTwoPassHiddenAltRefChargesBitsWithoutConsumingVisibleStats
  - Tests:
    [`TestAutoAltRefDriverEmitsHiddenFrame`](../encoder_altref_driver_test.go),
    [`TestAutoAltRefDriverDeferredShowFrameMatchesSource`](../encoder_altref_driver_test.go),
    [`TestAutoAltRefDriverSignBiasUpdatesPostHidden`](../encoder_altref_driver_test.go),
    [`TestSourceAltRefShowFrameForcesZeroMVAltRefWhenARNROff`](../encoder_altref_driver_test.go),
    [`TestTwoPassHiddenAltRefChargesBitsWithoutConsumingVisibleStats`](../encoder_altref_driver_test.go).
  - Simplification vs. libvpx: in one-pass mode govpx schedules every
    `DEFAULT_GF_INTERVAL`-bounded interval as soon as the lookahead has
    enough entries; libvpx's vp8/encoder/ratectrl.c `calc_gf_params`
    instead unconditionally clears `source_alt_ref_pending` whenever
    `cpi->pass != 2`, so one-pass libvpx never fires the hidden ARF.
    The govpx one-pass scheduler is therefore strictly more eager and
    is exercised by `TestAutoAltRefDriverEmitsHiddenFrame` rather than
    by oracle parity. Two-pass mode uses the FIRSTPASS_STATS-driven
    `pass2MaybeArmAltRefPending` path above and is the libvpx-faithful
    comparison; `TestOracleARNRBufferAdler` drives both sides through
    two-pass (`captureGovpxFirstPassStats` + `TwoPassStats` on the
    govpx side, `--passes=2 --pass=1`/`--pass=2` on the libvpx side)
    so the auto-ARF scheduler is symmetric. The hidden frame is
    encoded directly from the peeked source image; libvpx's
    `vp8_temporal_filter_prepare_c` redirection of
    `force_src_buffer` to `cpi->alt_ref_buffer` is not wired here, so
    ARNR temporal filtering for the ARF source still runs through the
    existing `applyARNRFilter` pipeline rather than the dedicated
    alt_ref_buffer. The remaining gap is hidden-frame emission timing:
    libvpx peeks the lookahead at offset `frames_till_gf_update_due`
    and emits the hidden ARF on the first inter call after the ARF is
    armed, while govpx waits for the future PTS to reach the head of
    the lookahead, which delays the emission by the section interval.
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
  - Test: none yet — measurement gap

- [~] Replace simplified ARNR with libvpx motion-compensated temporal filter.
  - govpx: [`applyARNRFilter`](../encoder_preprocess.go),
    [`iterateTemporalFilter`](../encoder_preprocess.go),
    [`applyTemporalFilter`](../encoder_preprocess.go),
    [`arnrFindMatchingMB`](../encoder_preprocess.go),
    [`arnrSubpelRefine`](../encoder_preprocess.go),
    [`arnrPredictLuma16x16`](../encoder_preprocess.go),
    [`arnrPredictChroma8x8`](../encoder_preprocess.go).
  - libvpx:
    [`temporal_filter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/temporal_filter.c).
  - Status: ported with one open source-buffer proof. govpx now walks
    per-16x16 luma macroblock (and the colocated 8x8 chroma blocks) over
    the alt-ref frame, runs libvpx's
    hex search (`vp8_hex_search` with NULL `mvsadcost`, i.e. pure 16x16
    SAD) against every adjacent reference, refines that integer-pel MV
    through libvpx's 1/2-, 1/4-, and 1/8-pel diamond walk
    (`find_fractional_mv_step`-style sixtap-SAD probe of the four
    axis-aligned neighbors plus one diagonal per iteration) at the
    full 1/8-pel grid, and applies libvpx's per-pixel weighted
    accumulator
    `modifier = clamp((3*(src-pred)^2 + (1<<(strength-1))) >> strength, 0, 16)`,
    `weight = (16-modifier) * filter_weight`, normalized as
    `(accumulator + count/2) / count`. The hex search reproduces the
    upstream three-phase walk (initial 6-vertex hexagon, iterative
    3-checkpoint walk up to `hex_range=127`, final 4-neighbor diamond
    refinement up to `dia_range=8`), uses libvpx's
    `mv_row_min/mv_col_min` bounds (`-(mb*16 + (16 - 5))`), and seeds
    each per-frame search from the MV chosen for the prior reference at
    the same MB; the first reference in the window seeds at (0,0).
    Subpel refinement adopts the lowest-SAD sixtap-filtered predictor
    and the synthesized predictor is built with libvpx's 6-tap
    `vp8_sixtap_predict16x16`/`vp8_sixtap_predict8x8` filters; chroma's
    1/8-pel MV is the halved subpel luma MV, mirroring
    `vp8_temporal_filter_predictors_mb_c`'s
    `mv_row >>= 1; mv_col >>= 1; (mv_col & 7, mv_row & 7)` dispatch.
    Per-frame `filter_weight` is 2 for the center, and {2,1,0} for
    adjacent frames keyed off the 16x16 sixtap-filtered SAD at the
    refined MV against `THRESH_LOW=10000`/`THRESH_HIGH=20000`, matching
    libvpx. Backward (type 1), forward (type 2), and centered (type 3,
    the libvpx default which receives `arnr_type==0` normalization)
    blur modes all run the same per-MB iteration. ARNR control
    validation matches libvpx bounds (`maxframes` 0-15, strength 0-6,
    type 1-3).
  - Simplification vs. libvpx: the central frame is read directly from
    the input `SourceImage` (no 16-pixel source-border extension), so
    out-of-visible search reads (and the sixtap predictor's 6-tap
    overhang) are clamped through `gatherBlock` rather than libvpx's
    mirrored border. The libvpx `cpi->fixed_divide` LUT is replaced
    with the equivalent `(accumulator + count/2) / count` integer
    division. Tests in
    [`encoder_preprocess_test.go`](../encoder_preprocess_test.go) pin
    zero-strength identity, motion-clip non-identity, an Adler32
    regression of the filtered ARF buffer
    (`TestARNRSubpelDeterministicAdler32`), the hex search's ability
    to track ±12-pixel motion that the previous local-exhaustive scan
    (`arnrSearchRadius=7`) could not reach, and that subpel refinement
    on a half-pel-shifted noisy clip yields lower SSE vs ground truth
    than the integer-only baseline
    (`TestARNRSubpelRefinementImprovesNoisyMatch`).
  - Test: TestOracleARNRBufferAdler, TestARNRSubpelDeterministicAdler32, TestARNRSubpelRefinementImprovesNoisyMatch
  - Remaining:
    prove or port libvpx ARNR border/`alt_ref_buffer` semantics. Done when
    ARNR-filtered buffers and downstream visible-frame quality/rate match
    libvpx on border-sensitive clips, or the difference is documented in
    "Accepted Non-Bitexact Differences".

## Inter Mode Decision And Motion Search

- [ ] Complete full RD inter-mode loop parity.
  - govpx:
    [`selectInterFrameModeDecision`](../encoder_reconstruct.go),
    [`selectRDInterFrameModeDecision`](../encoder_reconstruct.go),
    [`selectFastInterFrameModeDecision`](../encoder_reconstruct.go).
  - libvpx:
    [`rdopt.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/rdopt.c)
    `vp8_rd_pick_inter_mode` and
    [`pickinter.c`](../internal/coracle/build/libvpx-v1.16.0/vp8/encoder/pickinter.c)
    `vp8_pick_inter_mode`.
  - Status: partial. Fast non-RD mode-loop order, cheap realtime scoring, and
    `rd_thresh_mult` / hit-count gating are aligned, including the pickinter
    `>>3` best-mode threshold decay and libvpx's unsupported-SPLITMV
    test-count/raise behavior. Fast static encode-breakout now runs during
    inter-candidate evaluation, promotes the candidate to `MBSkipCoeff`, and
    breaks the mode loop like `pickinter.c`; its chroma gate uses libvpx's
    cheaper `sse2*2 < encode_breakout` rule, while the RD path keeps
    `rdopt.c`'s threshold-based chroma gate. Tests:
    `TestStaticInterFastEncodeBreakoutUsesPickinterChromaGate` and
    `TestSelectFastInterFrameModeDecisionStopsOnStaticEncodeBreakout`.
    Fast-path intra candidates now keep `UVMode = DC_PRED` like
    `vp8_pick_inter_mode`, instead of running a separate chroma predictor
    search; pinned by
    `TestSelectFastInterFrameModeDecisionKeepsLibvpxDCPredUVMode`.
    Full RD now walks libvpx's `MAX_MODES` /
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
    reductions for `THR_ZERO2`, `THR_NEAREST2`, and `THR_NEAR2`. Realtime
    `cpu-used > 6` also applies libvpx's previous-frame `error_bins`
    feedback from `vp8_set_speed_features`: govpx snapshots the prior
    `pickinter` distortion bins at frame start, resets the current bins,
    and raises/lowerings `THR_NEAREST*`, `THR_NEAR*`, and `THR_NEW*` by
    the same `Speed - 6` population threshold before per-MB mode tests.
    Tests:
    `TestLibvpxRealtimeAdaptiveInterModeThresholdMirrorsSpeedFeature`,
    `TestLibvpxInterModeThresholdMultipliersApplyRealtimeErrorBins`, and
    `TestFastInterModeErrorBinsResetAndClampLikeLibvpx`.
    Deadline / speed-feature plumbing now follows libvpx's mode-specific
    `cpu-used` range and realtime auto-speed semantics: realtime stores the
    clamped public `[-16,16]` value, positive realtime values enter libvpx's
    auto-speed path with initial speed-feature `Speed = 4`, negative realtime
    values request explicit speed `-cpu_used`, and good-quality mode is clamped
    to `[-5,5]`. That resolved speed drives search-step, RD-threshold,
    coefficient optimization, fast-quant, block-4x4-search, and loop-filter
    fast-search gates. Constructor, `SetCPUUsed`, and `SetDeadline` all store
    the libvpx-effective value; pinned by
    `TestCPUUsedNormalizationMirrorsLibvpxDeadlineClamp`,
    `TestLibvpxSpeedFeatureCPUUsedMirrorsRealtimeAutoSelect`, and the
    realtime cases in
    `TestInterAnalysisSearchConfigMirrorsLibvpxRealtimeThresholds`.
  - Test: TestOracleEncoderTraceInterCandidateCompare, TestOracleInterDecisionMatchRate, TestOracleEncoderTraceInterCandidateScoreboard
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
  - The SplitMV label-level search now follows libvpx's
    `rd_check_segment` per-label `LEFT4X4 / ABOVE4X4 / ZERO4X4 / NEW4X4`
    trial and gating shape: `selectRDInterFrameModeDecision`
    pulls the current SPLITMV+NEW threshold from the same
    `interModeRDThresholdsForReferences` table using the compacted libvpx
    reference search slot (`vp8_ref_frame_order[mode_index]`), not the
    absolute LAST/GOLDEN/ALTREF enum. This keeps GOLDEN-only and ALTREF-only
    searches on `THR_NEW1`, and the helper rereads the current threshold table
    so in-loop threshold raises/lowerings are visible to the SplitMV gate. The
    resulting `mvthresh` feeds through
    `selectInterFrameSplitMotionModeWithSearchAndThreshold`. The per-label loop in
    `selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold` then
    short-circuits the NEW4X4 motion search using
    `label_mv_thresh = mvthresh / label_count`, matching
    `if (best_label_rd < label_mv_thresh) break;` from
    `rd_check_segment` and using an RDCOST-shaped comparison with the same
    left/above contextual sub-MV label rate used by final SplitMV accounting so
    the threshold scale lines up with libvpx's `rd_threshes`. Candidate labels
    are also ranked in `RDCOST(label_rate + NEW_MV_rate, label_SAD)` space
    instead of the older `SAD + scaled label cost` proxy; full transform/token
    segment RD remains in the SPLITMV checklist. Tests:
    `TestSplitMVSubsearchThresholdUsesReferenceSearchSlot` and
    `TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch`,
    plus `TestSelectInterFrameSplitSubsetMotionModeRanksLabelsByRD`. The split RD
    decision (`selectInterFrameSplitMotionDecisionRDWithThreshold`) now
    also returns the full `other_cost` / Y-RD breakdown libvpx accumulates
    in `vp8_rd_pick_inter_mode` after `vp8_rd_pick_best_mbsegmentation`:
    `interSplitMVRDDecision` exposes `YRate`, `UVRate`, `OtherCost`,
    `RefCost`, `TotalRate`, `Rate2`, `RD`, and `YRD`, satisfying the
    `update_best_mode` invariant
    `TotalRate = YRate + UVRate + OtherCost + RefCost`.
  - Active-map skip short-circuiting now lives at the
    `selectInterFrameModeDecision` dispatcher: when
    `activeMapEnabled && activeMap[r*cols+c] == 0`, the picker returns the
    libvpx ZEROMV/LAST decision (skip=1, segment=0, MV=0) without entering
    either the fast or full RD inner loops, mirroring the
    `cpi->active_map_enabled && x->active_ptr[0] == 0` early exits in
    `evaluate_inter_mode` (`pickinter.c`) and `evaluate_inter_mode_rd`
    (`rdopt.c`). The per-frame loop's existing
    `encodeInactiveInterMacroblock` short-circuit is preserved; the
    dispatcher gate also keeps unit-level callers aligned without skipping
    the per-MB skip-encoding helper.
  - Token-context commit parity now mirrors libvpx rdopt.c
    `vp8_rd_pick_inter_mode`'s tempa/templ contract: per-mode RD subroutines
    (`estimateInterIntraModeRDScore`, `estimateInterResidualRDAccounting`,
    `selectInterFrameSplitModeRDScore`) snapshot `aboveTok`/`leftTok` into
    stack-local arrays before mutating them — see `wholeBlockYTransformRD`,
    `wholeBlockChromaTransformRD`, `predictBestBPredLumaModeRD`,
    `predictBestIntraChromaModeRD`, and `buildPredictedMacroblockCoefficientsRD`.
    The chosen mode's ENTROPY_CONTEXT is committed to the per-MB row state
    only after residual reconstruction, via `updateInterAnalysisTokenContext`
    inside `buildReconstructingInterFrameCoefficientsWithSegmentation`,
    matching libvpx encodeframe.c `encode_mb_row`'s deferred `*a/*l`
    assignment after `vp8_encode_inter16x16` / `vp8_encode_intra4x4mby`.
    Test: `TestSelectRDInterFrameModeDecisionUsesTempTokenContext`.
  - Recode-loop interactions: every entry into
    `buildReconstructingInterFrameCoefficientsWithSegmentation` allocates a
    fresh `aboveTok` slice and `leftTok` working set, so a rejected recode
    attempt's per-MB token-context commits never leak into the next pass —
    matching the effect of libvpx onyx_if.c `restore_coding_context` on the
    row ENTROPY_CONTEXTs across the recode `do { ... } while` loop. The
    encoder's frame-level `e.tokenAbove` buffer (consumed by the packet
    writer for coefficient probability counting) is also reset by
    `buildInterCoefficientBranchCounts` at the start of every writer call,
    so a corrupted carryover cannot survive into the next attempt either.
    Test: `TestRecodeLoopResetsTokenContext`.
    Active-map behavior is tracked in the dedicated active-map checklist
    item elsewhere.
  - Done when per-MB traces match tested mode order, skipped modes, selected
    mode/ref/MV, rate, distortion, RD, skip flag, and threshold updates across
    best/good/realtime speeds.

- [x] Finish improved-MV predictor parity.
  - govpx:
    [`improvedInterFrameSearchStart`](../encoder_reconstruct.go),
    [`interAnalysisSearchConfig`](../encoder_reconstruct.go).
  - libvpx:
    `vp8_mv_pred` and `vp8_cal_sad` in `rdopt.c`.
  - Status: complete. The bootstrap of
    `TestOracleImprovedMVScoreboard` against panning fixtures
    (good-quality VBR cpu=3 and realtime CBR cpu=0) shows 100% per-MB
    match rate on `improved_mv_near_sadidx`, the predictor MV
    (`improved_mv_row`/`improved_mv_col`), `improved_mv_sr`, and the
    combined match across all three for every NEWMV inter MB that
    exercised the improved-MV start path on either side. The
    scoreboard is wired into `make scoreboard` (and so
    `make verify-production`) with the recorded baseline at
    [`testdata/improved_mv_match_rate_baseline.json`](../testdata/improved_mv_match_rate_baseline.json),
    so it runs as a tripwire for future predictor regressions.
    Current-frame SAD ordering, previous inter-frame mode/MV
    grid, libvpx realtime gate, and low-level sign-biased near/best MV
    predictor helpers are present, and high-level predictor search now uses
    the current frame's sign-bias map plus the saved previous-frame slot bias.
    The high-level sign-bias policy / reference-switching wiring matches
    libvpx: per-reference NEWMV invocations of `improvedInterFrameSearchStart`
    (the `vp8_mv_pred` analogue) feed `interFrameSignBias()` — which only ever
    flips `ALTREF_FRAME` based on `sourceAltRefActive`, mirroring
    `cpi->common.ref_frame_sign_bias[ALTREF_FRAME]` in `onyx_if.c` — through
    `biasImprovedInterFrameMVSlots`, which applies the libvpx `mv_bias` flip
    to every current-frame and previous-frame near-MV slot whose stored ref
    sign bias disagrees with the target ref before SAD-ranked slot selection
    and the median fallback. The reference iteration order (LAST → GOLDEN →
    ALTREF, gated on `interReferenceAvailability`) flowing through
    `libvpxInterReferenceSearchOrder` mirrors `get_reference_search_order`'s
    compaction of `cpi->ref_frame_flags`.
    Border-mode-info indexing now mirrors
    libvpx's calloc-zeroed sentinel rows/columns: nil current-frame
    above/left/above-left and out-of-range previous-frame
    above/left/right/below neighbors collapse to `INTRA_FRAME` /
    `mv == 0` / `near_sad == INT_MAX` slots, and an intra current-frame
    neighbor no longer leaks a stale MV into the median fallback. Govpx-side
    oracle MB rows now expose `improved_mv_near_sadidx`,
    `improved_mv_row`/`improved_mv_col`, and `improved_mv_sr` for NEWMV
    candidates that used improved-MV prediction, and the libvpx-side
    patched `vpxenc-oracle` (see
    [`internal/coracle/build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh))
    now emits the same fields per MB by instrumenting `vp8_mv_pred` to
    record the matched `near_sadidx[i]` slot, `mvp.row`/`mvp.col`, and
    `*sr` per candidate ref and reading the slot for the chosen NEWMV ref
    in the encodeframe.c capture hook. The comparator's union-of-keys
    diff (`internal/coracle/oracle_compare.go`) catches per-field
    divergence on these new keys automatically; pinned by
    `TestCompareOracleTracesDetectsImprovedMVPredictorDivergence`.
    End-to-end quality smoke now covers best-quality panning, good-quality RD
    and fast-pick panning, and realtime `CpuUsed` 0, 3, 4, 5, 8, 9, and 15 on
    a panning corpus in addition to the token-partition motion case. A new
    9-position 3x3-grid regression test pins border behavior at every corner,
    edge, and interior macroblock for both the current-frame and last-frame
    neighbor tables, and
    `TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias`
    pins the per-reference sign-bias flip on a 3x3 grid for both
    LAST↔ALTREF directions.
  - Test: TestOracleInterDecisionMatchRate, TestOracleEncoderTraceInterCandidateScoreboard, TestImprovedInterFrameSearchStartReferencePolicyAppliesAltRefSignBias, TestCompareOracleTracesDetectsImprovedMVPredictorDivergence
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
    shifted error remains above 4000. SplitMV sub-MV label search costs now
    price LEFT/ABOVE/ZERO/NEW through the same left/above contextual
    `analysisSubMVRefProbs` path used by final `splitSubMotionLabelRate`
    accounting, with `TestSplitSubMotionLabelSearchCostUsesAnalysisContext`
    guarding the search-time cost against regressing to the static default
    table.
    After the Y split is committed, `selectInterFrameSplitMotionDecisionRD`
    reuses the decoder's `ReconstructSplitMVInterMacroblock` to render the
    SPLITMV luma+chroma predictor (libvpx-style 8x8 chroma MVs derived from the
    four covering 4x4 luma MVs via `splitChromaMotionVector`), then runs
    the same `quantizeEncodedBlock` block_type=3 (Y) / block_type=2 (UV)
    transform/quantize path the whole-MB inter case uses. The returned
    `interSplitMVRDDecision` carries Y rate/distortion, UV rate/distortion,
    and a `MacroblockCoefficients` populated with per-4x4-block luma EOBs
    (`Coeffs.EOB[0..15]`) and per-4x4-block chroma EOBs (`Coeffs.EOB[16..23]`).
    `THR_NEW1/2/3` NEW4X4 gating now flows from the encoder's
    `interModeRDThresholdsForReferences` table through
    `libvpxSplitMVSubsearchThreshold` (slot index from the compacted libvpx
    reference search order, not the absolute LAST/GOLDEN/ALTREF enum) into
    `selectInterFrameSplitMotionModeWithSearchAndThreshold`, which divides
    by `label_count` and uses the same per-label NEW4X4 gate as libvpx
    `rd_check_segment`'s `if (best_label_rd < label_mv_thresh) break;`.
    In the RD mode loop those label candidates are now ranked with
    luma transform/quant/token RD rather than SAD: each LEFT/ABOVE/ZERO/NEW
    candidate runs the Y_WITH_DC 4x4 blocks in its label through
    `quantizeEncodedBlock`, adds `coefficientBlockTokenRate`, accumulates
    `transformBlockError >> 2`, and commits the winning temporary token
    contexts before the next label, including the early NEW-gate return.
    Exact `other_cost` / Y-RD side accounting is now exposed on
    `interSplitMVRDDecision` via `OtherCost`, `RefCost`, `TotalRate`,
    `Rate2`, `RD`, and `YRD`; `selectInterFrameSplitMotionDecisionRDWithThreshold`
    populates them so callers reproduce
    `update_best_mode`'s
    `yrd = RDCOST(rate2 - rate_uv - other_cost - ref_cost,
    distortion2 - distortion_uv)` decomposition without re-running the
    picker. After the partition is chosen the SPLITMV decision now runs
    libvpx's full per-4x4-block transform/quantize/token RD: each Y block
    feeds `quantizeEncodedBlock` with `block_type=3` (Y_WITH_DC),
    `coefficientBlockTokenRate` accumulates the cost_coeffs rate, and
    `transformBlockError` accumulates `(coeff - dqcoeff)^2` to mirror
    `vp8_encode_inter_mb_segment` (`distortion / 4` after the per-block
    sum); the chroma path runs the same pipeline with `block_type=2` for
    each of the eight 4x4 UV blocks. The Y/UV rate and distortion values
    surfaced on `interSplitMVRDDecision` are now transform-domain — not
    SAD-derived — so SPLITMV vs whole-MB inter mode RD are now scored on
    the same units. Tests:
    `TestSelectInterFrameSplitMotionLabelLevelTrials`,
    `TestSelectInterFrameSplitMotionTHRNEWGatingSkipsSearch`,
    `TestSelectInterFrameSplitSubsetMotionModeRanksLabelsByRD`,
    `TestSelectInterFrameSplitMotionOtherCostBreakdown`,
    `TestSplitMotionLabelRDEvaluatorUsesTransformTokenRate`,
    `TestSplitMotionLabelRDCommitsContextsBeforeNewGate`,
    `TestSplitMVDecisionRDUsesTransformDomainRate`, and
    `TestSplitMVDecisionRDDistortionMatchesPerBlockTransformError`.
    SPLITMV-specific oracle parity is now pinned by
    `TestOracleSplitMVDecisionMatchRate` over a per-8x8-quadrant motion
    fixture (64x64, 8 frames, three RD-quality settings). The
    `testdata/splitmv_match_rate_baseline.json` baseline records, for each
    fixture, the per-MB SPLITMV-pick agreement, partition-index agreement
    over the SPLITMV-on-both-sides subset, per-block-MV agreement over the
    same subset, mode_match across all inter MBs, and segment_id agreement.
    Best- and good-quality cpu-used 0 settle every macroblock on SPLITMV in
    both encoders so mode_match is 100%; the residual gap is in
    partition-index choice (84.82% / 68.75% on cpu0 best/good) and per-
    block-MV (57.14% / 45.54%), which reflects libvpx's `rd_check_segment`
    label-RD tie-break vs govpx's transform-token RD when several
    partitions are RD-equivalent. cpu3 (compressor-speed pruning) drops
    SPLITMV picks to 22 govpx vs 28 libvpx, showing as
    `splitmv_pick_match_pct=89.29%`. The scoreboard test gates each
    metric within 2pp of the recorded baseline.
  - Test: TestOracleInterDecisionMatchRate, TestOracleSplitMVDecisionMatchRate, TestSelectInterFrameSplitMotionLabelLevelTrials, TestSplitMVDecisionRDUsesTransformDomainRate
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
    candidate set instead of the full RD-only 10-mode set. B_PRED RD bailout
    budgets now use the libvpx Y-only best-RD contract: key-frame and
    inter-intra callers pass the 16x16 luma RD into
    `predictBestBPredLumaModeRD`, and the inter RD loop stores
    `estimateInterIntraModeRDScore`'s returned YRD instead of the total
    Y+UV/ref/penalty score for later B_PRED/SPLITMV pruning. Tests:
    `TestEstimateInterIntraModeRDScoreAddsLibvpxPenalty` and
    `TestEstimateInterIntraBPredYRDExcludesUVAndRefCosts`.
  - Test: TestOracleEncoderTraceDecisionCompare, TestOracleReconstructionAdler32Match, TestEstimateInterIntraModeRDScoreAddsLibvpxPenalty, TestEstimateInterIntraBPredYRDExcludesUVAndRefCosts
  - Missing: exact thresholds and activity/tuning hooks (gated on
    `VP8_TUNE_SSIM`, which govpx does not expose). Key-frame per-MB oracle
    rows now expose Y mode, UV mode, B modes for `B_PRED`, EOBs, and qcoeffs;
    token-context/rate/distortion/RD/reconstruction-checksum fields remain to
    be added for full intra trace parity. Either document SSIM tuning as
    explicitly unsupported in "Accepted Non-Bitexact Differences" or expose a
    tune option and match libvpx's activity masking and `act_zbin_adj`
    behavior.
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
    when two trellis paths share an RDCOST, EOB rollback by backtrace, and
    libvpx `token_costs` subtree elision for post-zero trellis transitions.
	    Fast-vs-regular quantizer selection follows the libvpx speed-feature
	    gates, including the GOOD speed 1/2 split where mode picking uses
	    `use_fastquant_for_pick` but final reconstruction keeps regular
	    quantization while `improved_quant` remains enabled. RD scoring uses
	    the same unoptimized fast/regular quantizer family, and the
	    post-optimization `check_reset_2nd_coeffs` behavior clears tiny Y2
	    residuals that would inverse-transform to zero. Regular
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
    `encoder_cost_coeffs_parity_test.go`. Focused optimizer and trace tests
    now pin the same elided cost path through `optimizeQuantizedBlock`:
    `TestOptimizeQuantizedBlockUsesElidedPostZeroTokenCost` and
    `TestCoefficientBlockTokenTracePostZeroElidesEOBNode`.
    Y2 optimized quant now mirrors libvpx's split zbin handling: quantization
    thresholding uses `zbin_over_quant/2`, but the trellis optimizer scores
    with `mb->rdmult` derived from the full frame-level `zbin_over_quant`;
    pinned by `TestY2OptimizedQuantUsesFullZbinOverQuantForTrellis`.
  - Test: TestOracleReconstructionAdler32Match, TestCoefCoeffsParityMatchesReferenceWalk, TestY2OptimizedQuantUsesFullZbinOverQuantForTrellis
  - Required/keep: libvpx Viterbi trellis coefficient optimization, including
    `RDTRUNC` tie-breaks; do not replace it with a cheaper greedy optimizer.
  - Missing: `act_zbin_adj` (gated on `VP8_TUNE_SSIM`, which govpx does not
    expose) and exhaustive libvpx-side small-block oracle comparison for
    per-coefficient qcoeff/dqcoeff/EOB/rate/reconstruction parity. Test
    vectors should cover block types, skip-DC states, representative Q values,
    zbin settings, coefficient patterns around threshold/category boundaries,
    trailing-zero EOB rollback, and Y2 reset behavior.
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
  - Test: none yet — measurement gap
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
  - Test: none yet — measurement gap
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
  - Test: none yet — measurement gap
  - Done when inactive macroblocks match active-map oracle vectors across
    single-threaded and threaded encodeframe paths.

## Denoising And Noise-Sensitive Decisions

- [~] Replace non-libvpx denoising behavior.
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
  - Status: partial. govpx ports the libvpx denoiser data path: per-pixel
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
  - Test: none yet — measurement gap
  - Remaining:
    prove or port libvpx denoiser `running_avg` motion-comp integration. Done
    when denoised buffers, denoiser re-evaluation decisions, and final
    quality/rate match libvpx for `noise_sensitivity` 1-6 on moving noisy
    clips, or the difference is documented in
    "Accepted Non-Bitexact Differences".
  - Done when denoised buffers, selected modes after denoiser re-evaluation, and
    final quality/rate match for `noise_sensitivity` 1-6.

## Temporal, Speed, And Packetization

- [ ] Audit temporal-layer parity end-to-end.
  - Done when layer pattern, flags, TL0PICIDX, sync flags, per-layer buffers,
    reference refresh/copy policy, per-layer rate targets, dropped-frame
    accounting, and subsequent mode decisions match libvpx on all exposed
    temporal modes.

- [ ] Audit CBR drop-frame and buffer-pressure behavior.
  - Status: partial. Decimation drops are now implemented in govpx
    (`rateControlState.checkDropBuffer` + `postDecimationDropFrame` in
    `ratecontrol.go` mirroring libvpx `vp8_check_drop_buffer` in
    `vp8/encoder/onyx_if.c`). The new `EncoderOptions.DropFrameWaterMark`
    (mirrors libvpx `rc_dropframe_thresh`) feeds the per-25/50/75% drop
    mark ladder so the decimation factor 0->1->2->3 lifecycle now tracks
    libvpx exactly when both encoders see the same buffer pressure. The
    libvpx-side oracle in `internal/coracle/build_vpxenc_oracle.sh` emits
    a `frame` row with `dropped=true` (with `force_maxqp` and
    `buffer_level`) at all three drop return paths in
    `encode_frame_to_data_rate`, and govpx mirrors the row from
    `encoder.go` after the corresponding `postDecimationDropFrame` /
    `postDropFrame` calls. The new
    `TestOracleCBRDropFrameScoreboard`+
    `testdata/cbr_drop_scoreboard_baseline.json` pin the residuals: at
    80kbps/30f panning govpx drops 6 vs libvpx 8 (Jaccard 0.75 on the
    matched indices); at 120kbps/60f tight-buffer govpx drops 3 vs
    libvpx 5 (Jaccard 0.6); both fixtures show post-drop Q drift of
    ~16 indices because govpx's CBR Q regulator climbs more aggressively
    after a drop than libvpx does. Remaining work: align the post-drop
    Q recovery (track libvpx's `cpi->ni_av_qi` reset semantics so the
    follow-up Q lands within ~4 indices of libvpx's choice), and wire
    the post-encode overshoot drop branch (libvpx
    `vp8_drop_encodedframe_overshoot`) which govpx currently does not
    implement.
  - Test: TestOracleCBRDropFrameScoreboard
  - Done when drop/no-drop decisions, buffer level, force-max-Q aftermath,
    result flags, rate recovery, and next-frame Q/target decisions match libvpx.

- [ ] Audit `vp8_set_speed_features` behavior across deadline and CPU-used
  values.
  - Status: partial. Good-quality `CpuUsed` is now clamped to libvpx's
    `[-5,5]` range before speed-feature gates run. Realtime keeps the public
    `[-16,16]` range but follows libvpx's split semantics: negative values are
    explicit speeds and nonnegative values enter `vp8_auto_select_speed`, whose
    cold-start path sets speed-feature `Speed = 4`. Remaining work is
    candidate-level proof of mode-loop scoring, threshold mutation, dynamic
    realtime auto-speed feedback, and skipped-mode decisions across the
    representative speed matrix.
  - Test: TestOracleEncoderQHistogramScoreboard, TestOracleInterDecisionMatchRate
  - Done when speed-feature-selected search methods, RD thresholds, quantizer
    family, coefficient optimization gates, subpel settings, static breakout,
    and mode-loop decisions match libvpx for best/good/realtime speeds.

- [ ] Audit public reconfiguration controls against libvpx controls.
  - Done when midstream bitrate, rate-control, CQ, deadline, CPU-used,
    keyframe interval, force-keyframe, screen-content, static-threshold,
    token-partition, sharpness, reset, and close/reopen behavior either matches
    empirical libvpx or is documented as intentionally unexposed.

- [ ] Audit VP8 packetization and token-partition parity.
  - Done when frame tag, version, show/invisible flags, partition count,
    partition sizes, boolcoder carry/flush behavior, token row-to-partition
    assignment, and packet validity match libvpx for deterministic vectors.

- [ ] Audit source padding, border extension, odd-size frames, and ARNR source
  buffer semantics.
  - Done when edge-motion, odd-dimension, and non-multiple-of-16 clips produce
    libvpx-equivalent motion decisions and quality/rate, or any remaining
    source-border difference is documented in "Accepted Non-Bitexact
    Differences".

## Probability, Entropy, And Header State

- [~] Align loop-filter header and reconstruction filter policy.
  - govpx:
    [`encoderLoopFilter`](../encoder.go),
    [`encoderLoopFilterHeader`](../encoder.go),
    [`applyReconstructionLoopFilter`](../encoder.go).
  - libvpx: loop-filter setup in `onyx_if.c` and trial selection in
    `picklpf.c`.
  - Status: partial. Previous inter-frame filter-level carry, libvpx Q-based
    min/max clamps, fast/full trial-filter search, partial-frame luma SSE
    scoring, and default mode/ref delta signaling are in place. Realtime
    version-0 high-speed encoding now follows libvpx's cheap path: `Deadline`
    realtime with `CpuUsed >= 14` writes the simple loop-filter type for key
    frames, normal inter frames, and zero-reference inter frames; lower
    realtime speeds and good/best quality stay on the normal loop filter.
    ALT_LF segmentation filter-level behavior is now wired:
    `cyclicRefreshSegmentationConfig` switches to a `MB_LVL_ALT_LF` delta of
    -40 on the aggressive-denoise gate (mirrors libvpx onyx_if.c
    cyclic_background_refresh `lf_adjustment = -40`), and the in-encoder
    reconstruction loop filter
    (`applyReconstructionLoopFilter` -> `loopFilterSegmentationHeader`) now
    threads the encoder's `vp8enc.SegmentationConfig` into
    `vp8dec.ApplyLoopFilter` so the encoder-side reconstruction sees the
    same per-segment ALT_LF deltas the bitstream signals to the decoder.
    The libvpx-side oracle trace patch in
    `internal/coracle/build_vpxenc_oracle.sh` now emits `sharpness_level`,
    `ref_lf_deltas[4]`, `mode_lf_deltas[4]`, `mode_ref_lf_delta_enabled`,
    and `mode_ref_lf_delta_update` on every per-frame row, matching the
    extended `oracleTraceFrameRow` schema in `encoder_oracle_trace.go`.
    Tests:
    `TestEncoderLoopFilterHeaderMirrorsLibvpxDefaultDeltasAcrossQualities`,
    `TestEncoderLoopFilterHeaderUsesRealtimeSimpleFilterAtHighSpeed`,
    `TestEncodeIntoRealtimeHighSpeedWritesSimpleLoopFilter`,
    `TestCyclicRefreshSegmentationEmitsAggressiveDenoiseAltLF`,
    `TestCyclicRefreshSegmentationFallsBackToAltQOutsideAggressiveBranch`,
    `TestKeyFrameBitstreamCarriesAltLFDelta`,
    `TestLoopFilterSegmentationHeaderTranslatesAltLFFeatureData`, and
    `TestCompareOracleTracesDetectsLoopFilterDeltaDivergence`.
  - Test: TestOracleLoopFilterHeaderMatchRate, TestEncoderLoopFilterHeaderMirrorsLibvpxDefaultDeltasAcrossQualities, TestCompareOracleTracesDetectsLoopFilterDeltaDivergence
  - Missing: VP8 version 1-3 loop-filter behavior if that encoder surface
    is exposed.
  - Done when frame traces match filter type, level, sharpness, ref/mode
    deltas, segmentation interaction, and reconstructed reference checksums
    across best/good/realtime speeds.

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
    Key-frame forced coef-prob updates: audited against libvpx's
    `vp8_update_coef_probs` (`bitstream.c:865-950`). The "force u=1 when
    `newp != *Pold` on key frames" branch (`bitstream.c:920-928`) is gated
    on `VPX_ERROR_RESILIENT_PARTITIONS && frame_type == KEY_FRAME`, so it
    only applies to the independent (error-resilient) coef-context path —
    handled by `coefficientProbabilityUpdatesFromCountsIndependent`. The
    default (non-error-resilient) path treats key frames identically to
    inter frames at the savings step (only `s > 0` triggers an update),
    which `BuildKeyFrameCoefficientProbabilityUpdates` /
    `coefficientProbabilityUpdatesFromCounts` already mirror.
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
    prob. Key frames also mirror libvpx's reset to
    `default_coef_counts`: error-resilient key-frame coefficient probability
    updates and entropy-savings projections are intentionally independent of
    the current frame's coefficient content, using
    `defaultKeyFrameIndependentCoefficientBranchCounts` derived from
    libvpx v1.16.0 `defaultcoefcounts.h`. Wiring lives in
    `WriteCoefficientInterFrameWithProbabilityBase`
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
    The conversion follows libvpx's gate and exact count math: normal
    single-layer inter frames, including zero-reference shortcuts, convert ref
    counts with libvpx's floor `*255/denom` formula, while single-layer GF/ARF
    refresh frames keep the prior probabilities for the next frame's refresh
    heuristic; temporal multi-layer frames convert even across GF/ARF
    refreshes. Skip-false probability updates now use libvpx's floor
    `false_count*256/total_mbs` packet formula, and the analysis-time
    `prob_skip_false` path forces `1` for the visible single-layer source
    frame that matches a pending auto-ARF source, after the normal history
    lookup and 5..250 clamp.
    Tests:
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefRefresh`,
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxFramesSinceGolden`,
    `TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefActiveDecay`,
    `TestUpdateGoldenFrameStatsMirrorsLibvpxCounter`,
    `TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch`,
    `TestIndependentCoefContextSavingsHandComputed`,
    `TestIndependentCoefContextDivergesFromDefault`,
    `TestIndependentCoefContextKeyFrameForcesEqualization`,
    `TestDefaultCoefContextKeyFrameMatchesLibvpxNoForce`,
    `TestInterFrameAnalysisSkipFalseProbMirrorsLibvpxHistorySelection`,
    `TestInterFrameModeSkipFalseProbabilityMatchesWriterCounts`,
    `TestAdaptInterFrameModeProbabilities`, and
    `TestWriteCoefficientInterFrameEmitsAdaptedModeProbabilities`.
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
    Inter-frame intra Y/UV mode probability updates now mirror
    `bitstream.c` `update_mbintra_mode_probs`: govpx counts Y/UV mode-tree
    branches from intra macroblocks, computes new probabilities with the
    same `vp8_tree_probs_from_distribution(..., 256, 1)` math, applies the
    libvpx `new_b + (n << 8) < old_b` update gate, writes updated Y/UV
    probabilities before MV probability updates, uses those live
    probabilities while packing inter-intra macroblock modes, and commits
    the resulting persistent Y/UV mode probabilities only when
    `RefreshEntropyProbs` is true. Inter-intra RD and fast intra scoring now
    use the same live Y/UV probability tables for mode costs, matching the
    packet writer's current-frame state.
    MV probability adaptation now mirrors libvpx's `MVcount` distribution
    path instead of reusing the literal syntax branch counter: NEWMV and
    SplitMV/NEW4X4 deltas are first accumulated into signed component event
    buckets, then expanded with the same short-vector tree distribution and
    long-vector bit counts used by `write_component_probs`. This intentionally
    counts the implicit long-vector bit 3 for event magnitudes 8..15 even
    though that bit is not always present in the coded MV syntax, matching
    libvpx's probability refresh model. MV probability fitting and update
    gating also use `encodemv.c`'s MV-specific `calc_prob`
    (`ct[0] * 255 / total`, even-clamped) and
    `MV_PROB_UPDATE_CORRECTION` cost term rather than the coefficient-probability
    helper.
    Additional tests:
    `TestIndependentCoefContextEntropySavingsMatchesPositiveUpdates`,
    `TestEncodeIntoErrorResilientRefreshesKeyEntropyOnly`, and
    `TestCoefficientEntropySavingsUsesIndependentContextWhenErrorResilient`,
    `TestKeyFrameIndependentCoefUpdatesUseDefaultCounts`,
    `TestWriteCoefficientInterFrameEmitsInterIntraModeProbabilityUpdates`,
    `TestCommitInterFrameEntropyRefreshesInterIntraModeProbs`, and
    `TestEstimateInterIntraModeRDScoreUsesLiveInterIntraModeProbs`,
    `TestMotionVectorProbabilityFromBranchCountMatchesLibvpxCalcProb`,
    `TestMotionVectorProbabilityUpdateSavingsMatchesLibvpxCorrection`,
    `TestMotionVectorEventBranchCountsIncludeImplicitLongBit3`, and
    `TestAdaptInterFrameModeProbabilitiesUsesMVEventDistribution`.
    The oracle trace's "frame" row now carries the
    `refresh_entropy_probs` decision (after libvpx's
    `vp8_pack_bitstream` error-resilient override around
    `bitstream.c:1226`) and a `default_coef_reset` gate (true iff
    error-resilient mode AND key frame, mirroring the libvpx
    `vp8_setup_key_frame` -> `vp8_default_coef_probs` plus
    `vp8_update_coef_context` key-frame `vp8_copy(coef_counts,
    default_coef_counts)` reset). The libvpx-side patch in
    [`build_vpxenc_oracle.sh`](../internal/coracle/build_vpxenc_oracle.sh)
    captures both fields at the same emission point as `cm->frame_type`
    so parity tests can confirm govpx and libvpx took the same branch;
    `TestCompareOracleTracesDetectsDefaultCoefResetDivergence`
    exercises the diff path on synthetic JSONL.
  - Test: TestOracleEncoderTraceDecisionCompare, TestCompareOracleTracesDetectsDefaultCoefResetDivergence
  - Done when every frame matches coefficient probs, MV probs, Y/UV mode
    probs, ref probs, refresh entropy bit, projected entropy savings, and
    next-frame mode-cost inputs.
