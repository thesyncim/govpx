# VP8 controls + parity audit tracker

Source: VP8 gap audit, started 2026-05-17. Branch: `vp8-encoder-decoder-controls`.

## Items

| #  | Item                                                                   | Status |
|----|------------------------------------------------------------------------|--------|
| 1  | Decoder GETs: FrameCorrupted, LastReferenceUpdates, LastReferencesUsed | ✅      |
| 2  | Encoder runtime SetAutoAltRef                                          | ✅      |
| 3  | VP8E_SET_SCALEMODE / spatial resampler                                 | ⏳      |
| 4  | Burn down ~75 deferred VP8 fuzz seeds                                  | ⬜      |
| 5  | ALT_LF segmentation                                                    | ✅      |
| 6  | CBR golden-frame correction-factor branch                              | ✅      |
| 7  | Cyclic-refresh + static-background segmentation parity                 | ✅      |
| 8  | SPLITMV label-level RD oracle + improved-MV comparator                 | ⬜      |
| 9  | Right-edge chroma sub-pel residual (96x96 / 128x128)                   | ⬜      |
| 10 | VP8 version 1-3 decoder spec corners                                   | ✅      |
| 11 | VP8D_SET_DECRYPTOR (encrypted bitstream)                               | ✅      |

⬜ open  ⏳ in flight  ✅ landed

## Sync log

- 2026-05-17 worktree branched from origin/main `4219a4c`.
- 2026-05-17 rebased onto `0dfca64`; items 1+2 shipped (PR #19, merged to main).
- 2026-05-17 item 5 (ALT_LF) verified already implemented; plan.md text stale.
- 2026-05-17 item 3 phase 1: ported vpx_scale/generic kernels + Scale2D + ScaleFrame to internal/vp8/scale. Phase 2 is encoder integration (SetScalingMode + scale_and_extend_source + bitstream emission).
- 2026-05-17 item 11 (VP8D_SET_DECRYPTOR) shipped: applied at VP8Decoder packet entry (per-fill-window granularity skipped to keep zero-alloc hot path).
- 2026-05-17 items 6 and 10 verified already implemented: golden-frame correction-factor lives in ratecontrol_postencode.go (usesGoldenFrameCorrectionFactor), recode-loop dampVar dispatch in ratecontrol_recode.go; VP8 version 1-3 dispatch lives in internal/vp8/decoder/version.go with all 4 call sites wired in decoder.go (IsSupportedVersion, LoopFilterHeaderForVersion, InterPredictionConfigForVersion, VersionSkipsLoopFilter).
- 2026-05-17 item 7 verified already implemented: cyclicRefreshMap + cyclicRefreshIndex state, assignInterFrameStaticSegmentsWithMap MB walker, cyclicRefreshMaxMBsPerFrameForConfig (screen-content qp_thresh 80/100 + 250-frames-since-key skip rule), cyclicRefreshQuantizerDeltaForQuantizer (Q/2 delta), aggressiveDenoiseSegmentationActiveForQuantizer override (-40 LF delta when denoiser_mode == kDenoiserOnYUVAggressive). Gated by TestOracleEncoderStreamByteParitySegmentation.
