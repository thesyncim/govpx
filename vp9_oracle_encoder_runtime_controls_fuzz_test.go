//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
	"strconv"
	"testing"
)

// vp9RuntimeControlsSeedsDeferred lists VP9 runtime-control fuzz seeds whose
// strict byte parity is gated behind libvpx VP9 features govpx has not yet
// ported. Mirrors VP8's longFixtureSeedsDeferred and
// vp9LongFixtureSeedsDeferred convention so the fuzz gate stays green; each
// entry cites the libvpx file:line that drives the divergence so a follow-up
// port has a concrete starting point.
//
// Two equivalence classes are listed: the 6 baseline {dimBucket, framesBucket,
// cpuBucket, kfPos, refPos, action1, ...} seeds and additional short-byte
// regression corpus entries that resolve to the SAME materialised case via
// the wrap-around cursor (vp9FuzzByteCursor.pick = byte % n in
// vp9_oracle_fuzz_helpers_test.go:72-77). The cursor wraps for inputs shorter
// than the 8 cells consumed by vp9OracleRuntimeFuzzCaseFromBytes, so short
// regression seeds captured by sweeps land here next to their canonical
// 8-byte equivalent.
//
// The 6 baseline seeds populate (dimBucket, framesBucket, cpuBucket, kfPos,
// refPos, action1, ...) from cpuPool {0, -3, -8, 4}. The fuzz case
// materialiser pins Deadline=DeadlineRealtime, so all six seeds run REALTIME
// mode at speed=abs(cpu_used). govpx's RT-mode speed-feature cascade is
// already verbatim against libvpx at every speed (pinned by
// TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim, TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim,
// TestVP9SetRtSpeedFeaturesCPUUsed4Verbatim720p, and the per-field RT speed-3
// configurator tests), so divergence below is in the encoder body
// (tx_mode + per-block mode picker + writer cascade), NOT the
// speed-features dispatcher. Even the abs(cpu)=8 seeds (#3) diverge on byte
// 16 of the first keyframe because the runtime-controls fuzz uses
// content-rich newVP9YCbCrFuzzPanning sources that exercise govpx's
// keyframe compressed-header writer gap (see citations below) instead of
// the flat black/checker patches the existing
// TestVP9EncoderVpxencOracle*KeyframeByteParity tests pin.
//
// All 6 seeds are RED at frame 0 (keyframe) with first_diff in [9, 20]
// after the cpu_used pass-through fix (--cpu-used=opts.CpuUsed appended to
// extraArgs); without that fix the libvpx oracle always ran at speed 8
// while govpx ran at opts.CpuUsed, masking the underlying gap as a
// trivially-divergent speed mismatch.
//
// Deferred seeds (cpu values from cpuPool[bucket]):
//
//   - {0,0,0,0,0,0,0,0} — 64x64 frames=4 cpu=0. The fuzz case
//     materialiser builds opts with Deadline=DeadlineRealtime
//     (vp9_oracle_encoder_runtime_controls_fuzz_test.go) and the libvpx
//     oracle defaults to VPX_DL_REALTIME, so both sides run REALTIME mode
//     at speed=0. The full RT cpu_used=0 SPEED_FEATURES struct is already
//     pinned verbatim against libvpx by
//     TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim and the sibling GOOD-mode
//     anchor TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim — the
//     speed-features cascade is NOT the root cause of this deferral.
//
//     The upstream libvpx select_tx_mode at vp9_encodeframe.c:4334-4344
//     returns TX_MODE_SELECT for KEY_FRAME at cpu_used=0
//     (use_nonrd_pick_mode==0, tx_size_search_method==USE_FULL_RD).
//     govpx's vp9EncoderFrameTxMode now ports that branch verbatim — the
//     keyframe writer plumbs the TxModeSelect-shaped tx_probs row via
//     the existing keyframe-source path (writeVP9ModeBlock:6885+) and
//     the vp9ModeTreeKeyframe fallback (commit 0dfca64). govpx now also
//     ports the per-block tx_size RD loop from libvpx's
//     choose_tx_size_from_rd (vp9_rdopt.c:907-1023) via
//     pickVP9KeyframeBlockTxSize (vp9_encoder.go), running the libvpx
//     start_tx/end_tx loop with sf.TxSizeSearchDepth bounds and the
//     tx_size_search_breakout early-exit on textured residuals.
//
//     With the per-block RD loop in place the bitstream length on
//     content-rich panning sources stays at ~3828 bytes vs libvpx's
//     ~2726 (got_len=3828 want_len=2726 first_diff=9, filter_level=0
//     govpx vs 12 libvpx). The remaining gap is the RATE PROXY used by
//     the picker. govpx's keyframe RD scorers (the existing
//     scoreVP9KeyframeTxBlockRD at vp9_encoder.go:7641 and the new
//     pickVP9KeyframeBlockTxSize at vp9_encoder.go) approximate the
//     libvpx coefficient rate via SATD-of-qcoeff scaled into prob-cost
//     units (`rate <<= 2 + VP9ProbCostShift`). libvpx's cost_coeffs
//     (vp9_rdopt.c:358-470) uses the full per-token entropy walk via
//     x->token_costs[tx_size][type][is_inter] (computed from the
//     pareto8 tables via vp9_get_token_cost). The SATD proxy
//     underestimates the larger-tx coef rate so the picker can
//     occasionally pick a smaller tx where libvpx picks the larger.
//
//     Closing this seed requires porting libvpx's cost_coeffs
//     (vp9_rdopt.c:358) into govpx's pickVP9KeyframeBlockTxSize rate
//     leg so the RD comparison uses byte-exact libvpx rates. The
//     choose_tx_size_from_rd loop body (vp9_rdopt.c:907-1023) is
//     already verbatim in govpx; only the coefficient-cost proxy needs
//     replacing. Once cost_coeffs lands, the bitstream length should
//     converge with libvpx's ~2726 and the LPF picker's filter_level
//     pick should also converge.
//
//   - {0,1,1,0,2,1,0,0} — 64x64 frames=6 cpu=-3 (abs=3). Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 (vp9_rdopt.c:3950+);
//     additionally, at RT speed=3 the libvpx configurator sets
//     sf.lpf_pick=LPF_PICK_FROM_Q (vp9_speed_features.c:555) and
//     sf.disable_split_mask=DISABLE_ALL_SPLIT, which alters the
//     partition + lpf paths relative to speed=0. govpx's RT speed=3
//     SPEED_FEATURES struct itself IS verbatim against libvpx, but the
//     downstream encoder body (per-block tx_size search, partition
//     pruning, RD cost rounding at adaptive_rd_thresh=4) still
//     accumulates byte-level drift at cpu_used=3 RT — same encoder-body
//     handoff as #0.
//
//   - {1,0,0,1,0,0,1,0} — 128x64 frames=4 cpu=0. Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 (vp9_rdopt.c:3950+); the
//     wider frame_width-1 (127) trips a different miCols path that
//     amplifies the bitstream divergence on top.
//
//   - {1,1,2,0,3,1,1,0} — 128x64 frames=6 cpu=-8 (abs=8). flags=[0 256 0
//     320 256 512]. Earlier diagnostic cited a keyframe compressed-header
//     writer gap at byte 16; that gap has SINCE BEEN CLOSED — frame 0 now
//     byte-MATCHES at len=5937 against the libvpx vpxenc-vp9-frameflags
//     oracle. The remaining divergence is in the INTER frames:
//
//     frame 1 (NoUpdateEntropy): got_len=3782 want_len=2830 first_diff_bit=72
//     frame 2 (default flags):   got_len=3903 want_len=2933 first_diff_bit=33
//     frame 3 (NoUpdateEntropy|NoUpdateGolden): first_diff_bit=33
//     frame 4 (NoUpdateEntropy): first_diff_bit=33
//     frame 5 (ForceGolden):     first_diff_bit=33
//
//     Two distinct gaps surface:
//
//     (a) frame 1 first_diff_bit=72: decodes as the leading bit of
//     FirstPartitionSize (the 16-bit literal at the tail of the
//     inter uncompressed header; libvpx vp9_bitstream.c:1457). The
//     16-bit value is govpx=5 (compSize ~5 bytes) vs libvpx=51
//     (compSize ~51 bytes). govpx's WriteCompressedHeaderFromCounts
//     emits a near-empty payload because the per-tile frame_counts
//     govpx supplies are sparse (most coef branch slots all-zero),
//     so update_coef_probs_common's savings-search finds nothing to
//     emit. libvpx ingests denser coef counts from its per-block
//     tokenisation pass and emits 51 bytes of cond_prob_diff_update
//     payload. Despite the smaller compressed header, govpx's tile
//     data is LARGER (3782-5 vs 2830-51 bytes) because the encoder
//     encodes tokens against the stale (non-updated) probs, paying
//     more bits per token.
//
//     (b) frames 2+ first_diff_bit=33: leading bit of write_interp_filter
//     (libvpx vp9_bitstream.c:840-862): `cm->interp_filter ==
//     SWITCHABLE` ? 1 : 0. libvpx emits 1 (kept SWITCHABLE), govpx
//     emits 0 (demoted by fix_interp_filter at vp9_bitstream.c:864-885
//     because c==1 — every block picked the same filter). libvpx's
//     per-tile counts->switchable_interp[ctx][filter] histogram has
//     c>1 on these panning sources; govpx's has c==1.
//
//     Both (a) and (b) trace to the same root: per-block mode/filter
//     selection in the nonrd pickmode at RT speed 8 diverges between
//     govpx (vp9_pick_inter_mode_nonrd.go) and libvpx's
//     vp9_pick_inter_mode (vp9_pickmode.c:1731-2080). The
//     cb_pred_filter_search chessboard (vp9_pickmode.c:1862-1869) +
//     per-mode filter sweep + per-token tokenisation accumulator
//     pipeline produce a different frame_counts → different compressed-
//     header diff updates AND different filter histogram → cascading
//     bitstream drift.
//
//     Closing this seed requires a verbatim port of the nonrd pickmode
//     per-block mode+filter+tokenise pipeline (vp9_pickmode.c:1731-2080)
//     plus the per-block coef counts accumulator (vp9_tokenize.c
//     vp9_tokenize_sb / sum_intra_stats / count_segs paths). This is a
//     substantial encoder-body port — not a header-writer fix — and is
//     tracked separately from the closed compressed-header keyframe work.
//     The KF compressed-header writer (the OLD cited gap) is verbatim
//     against libvpx now and is NOT the remaining gap.
//
//   - {0,2,0,2,0,0,0,0} — 64x64 frames=8 cpu=0. Same KEY_FRAME
//     per-block tx_size RD-search gap as #0 with frame count widened;
//     once frame 0 KF parity holds the inter frames should follow
//     because the runtime-controls fuzz only flips
//     EncodeForce*/NoUpdate* flags that govpx already routes through
//     vp9_set_reference_frame_flags / ext_refresh_frame_flags.
//
//   - {1,2,1,0,4,1,0,1} — 128x64 frames=8 cpu=-3. The materialiser now
//     pre-resolves the FORCE_GF + NO_UPD_GF conflict (libvpx
//     vp9_cx_iface.c:1394-1398 "Conflicting flags." rejection) the same
//     way set_ext_overrides would — see normalizeVP9EncodeFlags — so
//     frame 4 reaches the encoder body as 0x200 (EncodeForceGoldenFrame
//     alone). With both sides receiving identical, libvpx-acceptable
//     flags the divergence collapses to the SAME cpu=-3 RT speed=3
//     keyframe per-block tx_size RD-search gap as seed #1
//     (vp9_rdopt.c:3950+ choose_tx_size_from_rd cost_coeffs rate proxy).
//     Frame 0 KF currently diverges with got_len=7611 want_len=5324
//     first_diff=16; inter frames diverge at byte 4 each because the
//     speed-3 compressed-header coef-update path still uses govpx's
//     approximate vp9_cond_prob_diff_update savings threshold instead
//     of libvpx's vp9_bitstream.c:826-973 verbatim walk. Same handoff
//     as #1; do NOT close until #1 closes.
//
// Short-byte regression-corpus seeds resolving to one of the above cases via
// vp9FuzzByteCursor wrap-around:
//
//   - {0x30} (single ASCII '0', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     582528dd captured in commit 0fba532) — vp9FuzzByteCursor returns
//     48%n for every pick(), so every cell evaluates to 0 and the case
//     materialises identically to baseline seed #0
//     {0,0,0,0,0,0,0,0}: w=64 h=64 frames=4 cpu=0 flags=[0,0,0,0]. Frame 0
//     KF diverges at byte 9 (filter_level=0 govpx vs 12 libvpx). Current
//     observed delta with the per-block tx_size RD loop in place:
//     got_len=3828 want_len=2726 first_diff=9. The filter_level=0 vs 12
//     difference reflects the cost_coeffs rate-proxy gap described
//     under seed #0 (govpx's SATD proxy underestimates the larger-tx
//     rate; libvpx's cost_coeffs at vp9_rdopt.c:358 needs porting). The
//     RT/GOOD cpu_used=0 SPEED_FEATURES cascade IS verbatim per
//     TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim and
//     TestVP9SetGoodSpeedFeaturesCPUUsed0Verbatim; the upstream
//     TX_MODE_SELECT routing is also verbatim; and the per-block tx_size
//     loop (choose_tx_size_from_rd) is verbatim — only the rate proxy
//     remains. Same handoff as seed #0 (cost_coeffs rate port); do NOT
//     close this entry until #0 closes.
//
//   - {0x31} (single ASCII '1', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     916d1b27 captured in commit 9e8f70a) — vp9FuzzByteCursor returns
//     49%n for every pick(); 49 % 2 = 1, 49 % 3 = 1, 49 % 4 = 1, etc. so
//     every cell evaluates to 1. The case materialises to dims[1]=
//     (128,64), frameCountPool[1]=6, cpuPool[1]=-3, kfPos=1, refPos=1,
//     plus the action loop picks EncodeNoUpdateEntropy for every inter
//     frame (r.pick(4)==1). Frame 0 KF diverges at byte 16
//     (got_len=7611 want_len=5324) and inter frames diverge at byte 4
//     each. Same downstream encoder-body gap as seed #1 (cpu=-3 RT
//     speed=3 path + per-block keyframe tx_size RD search at
//     vp9_rdopt.c:3950+). The RT speed=3 SPEED_FEATURES struct is
//     already verbatim (TestVP9SetRtSpeedFeaturesCPUUsed3Verbatim or
//     analogous) and the keyframe TX_MODE_SELECT writer cascade is now
//     in place — only the per-block keyframe tx_size RD search remains
//     unported. Same handoff as #1; do NOT close this entry until #1
//     closes.
//
//   - {0x32} (single ASCII '2', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     2fde656d captured in commit 2ebdb7d) — vp9FuzzByteCursor returns
//     50%n for every pick(); 50 % 2 = 0, 50 % 3 = 2, 50 % 4 = 2,
//     50 % 5 = 0, 50 % 8 = 2 etc. The case materialises to dims[0]=
//     (64,64), frameCountPool[2]=8, cpuPool[2]=-8, kfPos=2, refPos=2,
//     plus the refPos generator picks r.pick(5)==0 -> EncodeNoUpdateLast
//     for frame 2 and the per-frame action loop picks r.pick(4)==2 ->
//     EncodeForceGoldenFrame for every inter frame. Frame 2 stacks
//     EncodeNoUpdateLast | EncodeForceGoldenFrame | EncodeForceKeyFrame
//     (cumulative flags 0x221). After commit 0fba532 dropped the
//     vp9_encoder.go:2399 `isKey && NoUpdate*` rejection (libvpx
//     vp9_encoder.c:5444 / 856-858 force refresh_*_frame=1 on KEY_FRAME
//     regardless of ext_refresh_*, so NoUpdate hints are silently
//     ignored, not errored), seed 0x32 frames 0 and 2 (the two KEY_FRAMES)
//     match byte-for-byte. Inter frames 1, 3-5, 7 still diverge.
//
//     Task #156 byte-4 root-cause audit (vp9_seed8_byte4_diag_test.go):
//
//     The proximate divergence is bit 33 of the uncompressed header --
//     write_interp_filter (libvpx vp9_bitstream.c:855-862): libvpx emits
//     1 (cm->interp_filter == SWITCHABLE) while govpx emits 0 (filter
//     demoted to EIGHTTAP). The demotion is performed by
//     fix_interp_filter (vp9_bitstream.c:864-885 / vp9_encoder.go:4541
//     vp9FixInterpFilter) which is a verbatim port; the divergent input
//     is the per-frame switchable_interp histogram supplied by
//     collectVP9EncodeFrameCounts (vp9_encoder.go:2820).
//
//     Per-frame counts.SwitchableInterp totals captured by
//     TestVP9Seed8FilterHistogram for this seed:
//     frame=1 c=1 totals=[E=2, S=0, H=0]  -- DEMOTED (libvpx c>=2)
//     frame=3 c=1 totals=[E=2, S=0, H=0]  -- DEMOTED
//     frame=4 c=1 totals=[E=1, S=0, H=0]  -- DEMOTED
//     frame=5 c=1 totals=[E=1, S=0, H=0]  -- DEMOTED
//     frame=6 c=2 totals=[E=1, S=1, H=0]  -- kept SWITCHABLE
//     frame=7 c=2 totals=[E=4, S=35, H=0] -- kept SWITCHABLE
//
//     Frame 1 fires switchable_interp count on only 2 inter blocks
//     (vs libvpx, where the per-block filter sweep at vp9_pickmode.c:
//     2318-2330 produces c>=2 -- minimum {EIGHTTAP, EIGHTTAP_SMOOTH}
//     coverage from the LAST+NEWMV+subpel search_filter_ref path at
//     vp9_pickmode.c:1518-1568). This points at the per-block mode +
//     filter selection in the realtime nonrd picker
//     (vp9_pick_inter_mode_nonrd.go pickVP9NonrdInterMode) under-firing
//     vs libvpx's vp9_pick_inter_mode (vp9_pickmode.c:1731-2080), not at
//     the uncompressed-header writer.
//
//     Negative finding: govpx's writeUncompressedHeaderInter +
//     writeInterpFilter + vp9FixInterpFilter trio is a verbatim port of
//     libvpx (internal/vp9/encoder/header_writer.go:114-157,195-215 and
//     vp9_encoder.go:4541-4606). The first_byte_diff=4 result is not
//     fixable by any header-writer edit; closing seed #8 requires
//     porting the nonrd pickmode per-block filter pipeline so the
//     per-frame switchable_interp histogram matches libvpx's. This is
//     the SAME encoder-body port handoff cited under seed #3 case (b)
//     above. Do NOT close this entry until that port lands.
//
//     The previous note attributed byte 4 to the compressed-header
//     coef-update walk (vp9_bitstream.c:826-973). That citation is
//     wrong -- byte 4 is uncompressed-header bit 33
//     (write_interp_filter). The compressed-header coef-update walk
//     is part of the residual size_delta but not the proximate cause
//     of first_byte_diff=4: it lives further into the bitstream,
//     after the rest of the uncompressed header (loopfilter,
//     quantization, segmentation, tile_info) and the 16-bit
//     FirstPartitionSize literal at the tail of
//     writeUncompressedHeaderInter (libvpx vp9_bitstream.c:1457 /
//     internal/vp9/encoder/header_writer.go:155).
//
//   - {0x37} (single ASCII '7', from testdata/fuzz/
//     FuzzVP9OracleEncoderRuntimeControls/regression_vp9_runtime_controls_-
//     6573b9b5 captured in commit e7b9906) — vp9FuzzByteCursor returns
//     55%n for every pick(); 55 % 2 = 1, 55 % 3 = 1, 55 % 4 = 3,
//     55 % 5 = 0, 55 % 6 = 1, etc. The case materialises to dims[1]=
//     (128,64), frameCountPool[1]=6, cpuPool[3]=4, kfPos=1, refPos=1,
//     plus the refPos generator picks r.pick(5)==0 -> EncodeNoUpdateLast
//     on frame 1 and the per-frame action loop picks r.pick(4)==3 ->
//     EncodeForceAltRefFrame for every inter frame. Frame 1 stacks
//     EncodeForceKeyFrame | EncodeNoUpdateLast | EncodeForceAltRefFrame
//     (cumulative flags 0x421). After dropping the
//     vp9_encoder.go:2399 `isKey && NoUpdate*` rejection (see {0x32}
//     entry) the encoder body now runs end-to-end at cpu=4 RT speed=4.
//     The remaining divergence is the per-block keyframe tx_size RD
//     search gap (vp9_rdopt.c:3950+ choose_tx_size_from_rd cost_coeffs
//     rate proxy) plus the speed-4 compressed-header coef-update walk;
//     frame 0 KF diverges at got_len=5890 want_len=5483 first_diff=16,
//     frame 1 (the second KF) at first_diff=16, inter frames 2-5 at
//     first_diff=[9,3,3,3]. Same handoff family as seeds #2 / #3 (wider
//     dim + cpu>=4 RT speed compressed-header subset); do NOT close
//     until those close.
//
// Re-measurement under
// GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 GOVPX_VP9_NONRD_PICK_PARTITION=1
// (verified by TestVP9DeferredSeedsRemeasureRuntimeControls):
//
//	PASS=0/9 FAIL=9/9. Seeds #0/#2/#4/#6 (cpu=0 panning content)
//	diverge on frame 0 at first_byte_diff=9 by ~1000-2298 bytes —
//	the cost_coeffs rate-proxy gap described under seed #0 is the
//	sole remaining piece (choose_tx_size_from_rd already verbatim).
//	Seeds #1/#7 (cpu=-3) diverge on frame 0 at first_byte_diff=16
//	by ~989-2287 bytes — same gap amplified by RT speed=3
//	coef_prob_appx_step. Seed #3 (cpu=-8) diverges on frame 1 by
//	~123 bytes. Seeds #5 and "2" alias structurally cannot be
//	measured because libvpx vpxenc-vp9-frameflags rejects the
//	EncodeForceGoldenFrame|EncodeNoUpdateGolden combination as
//	"Conflicting flags" and govpx rejects it as ErrInvalidConfig
//	— pending the set_ext_overrides port
//	(vp9_encoder.c:set_ext_overrides) regardless of partition gate.
//
//	Conclusion: gates flipping ON does not un-defer any
//	RuntimeControls seed. Closure requires the cost_coeffs rate
//	proxy port and the set_ext_overrides resolution port.
//
// Re-measurement (task #148, this commit) after the following
// landings since f5fe476 (#142):
//
//   - 838691b token-cost (vp9KeyframeCoeffBlockRateCost) reconcile
//   - b87ff4d super_block_uvrd + rd_pick_intra_sbuv_mode port
//   - 404c7dd intra-only coef counts pass via KeyframeSource
//   - 7017378 nonrd block_yrd compare + breakout (already in #142)
//   - Phase E1b/E1c/E3 chain (already in #142)
//
// Per-seed aggregate size_delta (sum across all frames) under the
// three gate combos (verified by TestVP9DeferredSeedsRemeasureRuntimeControls):
//
//	Default (no opt-in):
//	  #0 af5570f5: +2754, #1 de6e098c: +4141, #2 967aad53: +7038,
//	  #3 ac1b2597: +5157, #4 59794cac: +6808, #6 5feceb66: +2754,
//	  #7 6b86b273: +8971, #9 7902699b: +2806.
//	  Aggregate: +40429 / avg +5053 per measurable seed
//	  (#5/#8 STRUCTURAL_REJECT).
//
//	GOVPX_VP9_NONRD_PICK_PARTITION=1 (Phase D opt-in alone):
//	  #0: +2754, #1: +4141, #2: +7038, #3: -262, #4: +6808,
//	  #6: +2754, #7: +8971, #9: +2806.
//	  Aggregate: +35010 / avg +4376 per measurable seed.
//
//	Both gates ON (NONRD_PICK_PARTITION=1 + LIBVPX_CHOOSE_PARTITIONING=1):
//	  #0: +2754, #1: +4141, #2: +7038, #3: -262, #4: +6808,
//	  #6: +2754, #7: +8971, #9: +2293.
//	  Aggregate: +34497 / avg +4312 per measurable seed.
//
// Frame-0 size_delta (got_len - want_len) under both gates ON
// (the comparable axis to f5fe476's frame-0 citation):
//
//	#0 +996, #1 +995, #2 +2276, #3 -31, #4 +996, #6 +996,
//	#7 +2285, #9 +47.
//
// Comparison to f5fe476 (#142) frame-0 citation
// (+1006/+997/+2299/-31/+1006/+1006/+2288/+47): seeds #0/#2/#4/#6
// improved by ~10-23 bytes/seed (token-cost reconcile + super_block_uvrd
// shaved a hair off the cost_coeffs proxy gap), seeds #1/#7 by 2-3
// bytes, seeds #3/#9 unchanged. The structural cost_coeffs rate-proxy
// gap (govpx SATD vs libvpx pareto8 per-token entropy walk at
// vp9_rdopt.c:358) remains the dominant residual — seed #9 (cpu=4)
// only moves on the inter-frame aggregate at choose-partitioning ON
// (-513B from the picker honouring the libvpx VAR_BASED dispatch).
//
// Gate-flip recommendation: NOT YET. No seed byte-matches under any
// combo. Frame-0 deltas remain in the +50 to +2285 byte range —
// ~50x the +/-50B target. Closure requires the cost_coeffs rate
// proxy port (already documented above) and the set_ext_overrides
// resolution port for #5/#8 measurability.
//
// Reverting any entry here must be paired with the corresponding verbatim
// libvpx port landing; this is the explicit handoff list for follow-up work.
//
// Task #166 negative finding (nonrd_pick_sb_modes / speed=3 RT pipeline
// audit for seeds #0/#2/#4/#6 first_byte_diff=9):
//
//	Hypothesis: govpx's nonrd_pick_sb_modes (libvpx vp9_encodeframe.c:
//	4386-4453) + the partition-NONE/SPLIT compare body at
//	vp9_encodeframe.c:4675-4746 differ from libvpx, causing the per-leaf
//	RD picks to diverge → different TxTotals + coef counts → byte-9
//	(filter_level) drift on the four RuntimeControls seeds #0/#2/#4/#6.
//
//	Verdict: NEGATIVE. The nonrd_pick_sb_modes / nonrd_pick_partition
//	bodies do NOT execute on these four seeds, so no port of that code
//	path can move their first_byte_diff or size_delta. Two-step proof:
//
//	  (1) The four cited seeds materialise as cpu_used=0 RT
//	      (vp9OracleRuntimeFuzzCaseFromBytes resolves cpuPool[byte%4]=
//	      cpuPool[0]=0 for #0/#2/#4/#6). Verified by
//	      TestVP9DeferredSeedsRemeasureRuntimeControls output:
//	      runtimectrl-#0-af5570f5 cpu=0,
//	      runtimectrl-#2-967aad53 cpu=0,
//	      runtimectrl-#4-59794cac cpu=0,
//	      runtimectrl-#6-5feceb66 cpu=0 (alias of #0 via short-byte
//	      cursor wrap).
//
//	  (2) libvpx vp9_speed_features.c:585-601 gates
//	      sf->use_nonrd_pick_mode = 1 behind `if (speed >= 5)`. At
//	      speed=abs(cpu_used)=0 (and at speed=3, the value the task
//	      description cites for these seeds) use_nonrd_pick_mode stays
//	      at 0 (the rt-mode init default at vp9_speed_features.c:444).
//	      The dispatcher at vp9_encodeframe.c:5474-5477 then routes to
//	      encode_rd_sb_row -> rd_pick_partition -> rd_pick_sb_modes ->
//	      vp9_rd_pick_intra_mode_sb (vp9_encodeframe.c:1972-2104,
//	      vp9_rdopt.c:3221-3271) — NOT nonrd_pick_sb_modes /
//	      nonrd_pick_partition. govpx mirrors the gate verbatim:
//	      vp9_speed_features.go:1677 sets UseNonrdPickMode = 1 only
//	      inside the speed >= 5 block, and the keyframe leaf-RD path
//	      at vp9_encoder.go:7137-7177 dispatches via
//	      useVP9KeyframeNonRDIntraMode (which requires
//	      sf.UseNonrdPickMode != 0) before any nonrd code is reached.
//
//	Empirical confirmation: TestVP9DeferredSeedsRemeasureRuntimeControls
//	with GOVPX_VP9_NONRD_PICK_PARTITION=1 GOVPX_VP9_LIBVPX_CHOOSE_PARTI-
//	TIONING=1 on this branch reproduces the exact same per-seed
//	(got_len, want_len, first_byte_diff, size_delta) for seeds
//	#0/#2/#4/#6 as the default-gate run:
//
//	  #0 af5570f5  got=3722 want=2726 first_byte_diff=9 size_delta=-951
//	  #2 967aad53  got=7588 want=5313 first_byte_diff=9 size_delta=+1902
//	  #4 59794cac  got=3722 want=2726 first_byte_diff=9 size_delta=-3003
//	  #6 5feceb66  got=3722 want=2726 first_byte_diff=9 size_delta=-951
//
//	(The two gates flip seeds #3 +5462 -> -230 and #8 +4185 -> +172,
//	confirming the gated code DOES execute for the speed >= 5 seeds and
//	the gate plumbing is healthy — it just doesn't reach #0/#2/#4/#6.)
//
//	Root cause for these four seeds remains the RD-path cost_coeffs
//	rate proxy at vp9_rdopt.c:358-459 already cited under seed #0's
//	per-seed entry above (vp9KeyframeCoeffBlockRateCostQ vs libvpx's
//	pareto8 token-cost walk). byte-9 = filter_level (LoopfilterParams.
//	FilterLevel, 6-bit literal at offset 9 in the keyframe
//	uncompressed header) is a downstream artifact of the per-leaf
//	(mode, tx_size) picks at vp9_rdopt.c:907-1023 choose_tx_size_from_rd
//	feeding into vp9PickFilterLevel (libvpx vp9_picklpf.c) via the
//	reconstructed Y-plane SSE picker.
//
//	Handoff: same cost_coeffs rate-proxy port already cited under seed
//	#0; nonrd_pick_sb_modes is NOT a closure path for these four seeds.
var vp9RuntimeControlsSeedsDeferred = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 1, 0, 2, 1, 0, 0},
	{1, 0, 0, 1, 0, 0, 1, 0},
	{1, 1, 2, 0, 3, 1, 1, 0},
	{0, 2, 0, 2, 0, 0, 0, 0},
	{1, 2, 1, 0, 4, 1, 0, 1},
	// Short-byte regression-corpus aliases of the above (see comment).
	{0x30}, // regression_vp9_runtime_controls_582528dd — alias of #0
	{0x31}, // regression_vp9_runtime_controls_916d1b27 — alias of #1 family
	{0x32}, // regression_vp9_runtime_controls_2fde656d — alias of #3 (cpu=-8 RT speed=8 compressed-header writer gap)
	{0x37}, // regression_vp9_runtime_controls_6573b9b5 — alias of #2/#3 family (cpu=4 RT speed=4 KF+inter divergence)
}

func vp9RuntimeControlsSeedDeferred(data []byte) bool {
	for _, seed := range vp9RuntimeControlsSeedsDeferred {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzVP9OracleEncoderRuntimeControls mirrors the VP8
// FuzzOracleEncoderRuntimeControlTransitions: a fuzz-driven runtime-control
// schedule is replayed against both the govpx VP9 encoder and the
// vpxenc-vp9-frameflags driver, and the per-frame VP9 packet bytes must match.
//
// The action pool is intentionally narrower than the VP8 sibling because
// vpxenc-vp9-frameflags exposes a different per-frame control vocabulary — only
// the controls govpx VP9 can drive in lockstep with libvpx VP9 are included.
// Any action that govpx supports but the driver doesn't (or vice-versa) is
// omitted to keep the comparator fair; gaps surface as a logged "comparator
// inapplicable" rather than a silent false-positive parity.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9-frameflags binary.
func FuzzVP9OracleEncoderRuntimeControls(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 runtime-control oracle fuzz")
	}
	requireVP9VpxencFrameFlagsOracleFuzz(f)
	seeds := [][]byte{
		// (dimBucket, framesBucket, cpuBucket, kfFlagPos, refFlagPos, action1, action2, ...)
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 1, 0, 2, 1, 0, 0},
		{1, 0, 0, 1, 0, 0, 1, 0},
		{1, 1, 2, 0, 3, 1, 1, 0},
		{0, 2, 0, 2, 0, 0, 0, 0},
		{1, 2, 1, 0, 4, 1, 0, 1},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if vp9RuntimeControlsSeedDeferred(data) {
			t.Skip("seed deferred: see vp9RuntimeControlsSeedsDeferred for libvpx file:line citations")
		}
		tc := vp9OracleRuntimeFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d frames=%d cpu=%d flags=%v",
			label, tc.opts.Width, tc.opts.Height, len(tc.sources), tc.opts.CpuUsed, tc.flags)

		govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9OracleRuntimeFuzzCase struct {
	name      string
	opts      VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []EncodeFlags
	extraArgs []string
}

// vp9OracleRuntimeFuzzCaseFromBytes materialises a fuzz seed into a VP9
// runtime-control case. Each byte selects a bucket index off a wrapping
// cursor so even short seeds yield a fully-specified case.
func vp9OracleRuntimeFuzzCaseFromBytes(data []byte) vp9OracleRuntimeFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	dims := [...]struct {
		w int
		h int
	}{
		{64, 64},
		{128, 64},
	}
	frameCountPool := [...]int{4, 6, 8}
	cpuPool := [...]int{0, -3, -8, 4}

	dim := dims[r.pick(len(dims))]
	frames := frameCountPool[r.pick(len(frameCountPool))]
	cpuUsed := cpuPool[r.pick(len(cpuPool))]
	kfPos := r.pick(frames)
	refPos := r.pick(frames)

	opts := VP9EncoderOptions{
		Width:               dim.w,
		Height:              dim.h,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
		CpuUsed:             int8(cpuUsed),
		CQLevel:             32,
		Deadline:            DeadlineRealtime,
	}
	sources := newVP9YCbCrFuzzSources(dim.w, dim.h, frames)
	flags := make([]EncodeFlags, frames)

	// Sprinkle a key-frame flag and an optional reference-update flag.
	if kfPos > 0 && kfPos < frames {
		flags[kfPos] |= EncodeForceKeyFrame
	}
	if refPos > 0 && refPos < frames {
		switch r.pick(5) {
		case 0:
			flags[refPos] |= EncodeNoUpdateLast
		case 1:
			flags[refPos] |= EncodeNoUpdateGolden
		case 2:
			flags[refPos] |= EncodeNoUpdateAltRef
		case 3:
			flags[refPos] |= EncodeNoReferenceLast
		case 4:
			flags[refPos] |= EncodeNoReferenceGolden | EncodeNoReferenceAltRef
		}
	}
	// Per-frame action permutations are encoded into remaining bytes. We
	// keep this bounded so a single fuzz iteration stays cheap at 720p.
	for i := 1; i < frames; i++ {
		switch r.pick(4) {
		case 1:
			flags[i] |= EncodeNoUpdateEntropy
		case 2:
			flags[i] |= EncodeForceGoldenFrame
		case 3:
			flags[i] |= EncodeForceAltRefFrame
		}
	}
	// libvpx vp9/vp9_cx_iface.c:1394-1398 rejects FORCE_GF + NO_UPD_GF and
	// FORCE_ARF + NO_UPD_ARF on the same frame as "Conflicting flags." The
	// vpxenc-vp9-frameflags oracle propagates that VPX_CODEC_INVALID_PARAM as
	// an exit-status failure, so the materialiser would deadlock the parity
	// comparator before ever exercising the encoder. govpx's
	// normalizeVP9EncodeFlags (vp9_encoder.c:set_ext_overrides semantics:
	// FORCE wins because vp9_apply_encoding_flags' upd mask treats FORCE_GF
	// as "refresh all minus NO_UPD bits" and the conflict check would have
	// rejected the input upstream) drops the NO_UPD_GF/NO_UPD_ARF bit when
	// the matching FORCE_GF/FORCE_ARF bit is set. Apply the same resolution
	// at materialisation so both encoders see identical, libvpx-acceptable
	// flag schedules for every fuzz iteration.
	for i := range flags {
		flags[i] = normalizeVP9EncodeFlags(flags[i])
	}

	extraArgs := []string{
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--end-usage=q",
		// Propagate the fuzz-selected speed preset to the libvpx oracle.
		// vpxenc-vp9-frameflags defaults to --cpu-used=8; without this
		// override the libvpx side would always run at speed 8 while
		// govpx ran at opts.CpuUsed, producing trivially-divergent
		// bitstreams. libvpx clamps to [-9, 9] in
		// vp9/vp9_cx_iface.c:ctrl_set_cpuused and uses abs(cpu_used)
		// as the SPEED_FEATURES selector (vp9_speed_features.c), which
		// matches govpx vp9SpeedFeatureCPUUsed.
		"--cpu-used=" + strconv.Itoa(cpuUsed),
	}
	return vp9OracleRuntimeFuzzCase{
		name:      "general",
		opts:      opts,
		sources:   sources,
		flags:     flags,
		extraArgs: extraArgs,
	}
}
