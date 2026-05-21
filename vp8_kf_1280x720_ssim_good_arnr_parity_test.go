//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8KF1280x720SSIMGoodARNRParity pins task #227 / fuzz seed
// regression_option_grid_788d442c: a 1280x720 GoodQuality / cpu=0 / VBR /
// screen-content=1 / threads=4 / TuneSSIM / ARNR=1/1/2 clip whose
// frame-0 keyframe and frame-1 inter both byte-diverge from libvpx with
// the SAME first-partition signature as the BestQuality companion
// 19981bff cohort pinned in vp8_kf_1280x720_ssim_best_arnr_parity_test.go.
//
// Cohort decode (seed bytes "A1", 0x41 = 65, 0x31 = 49;
// vp8OracleRuntimeControlFuzzBytes wraps len(data)==2 so buckets alternate
// between byte 0 and byte 1):
//
//   - Resolution: bucket 65%11=10  ⇒  1280x720
//   - Deadline:   bucket 49%3=1    ⇒  GoodQuality
//   - CpuUsed:    bucket 65%5=0    ⇒  cpu=0
//   - RateControl bucket 49%4=1    ⇒  VBR
//   - Feature:    bucket 65%8=1    ⇒  screen-content-mode=1
//   - TokenPart:  bucket 49%4=1
//   - Threads:    bucket 65%3=2    ⇒  threads=4
//   - Sharpness:  bucket 49%4=1    ⇒  sharpness=0 (sharpness pool[0]=0; flag omitted)
//   - Tuning:     bucket 65%3=2    ⇒  TuneSSIM
//   - ARNR:       bucket 49%4=1    ⇒  maxframes=1
//   - ARNR Str:   bucket 65%4=1    ⇒  strength=1
//   - ARNR Type:  bucket 49%3=1    ⇒  type=1+1=2
//
// Current divergence (origin/main, commit 49cd912b, task #224 baseline):
//
//	Frame 0 (KF):
//	  govpx:  first_partition_size = 20432, total len = 145487
//	  libvpx: first_partition_size = 20463, total len = 145534
//	  first_byte_diff = 0 (govpx UNDERSHOOTS libvpx by -31 on first_part,
//	                       UNDERSHOOTS libvpx by -47 bytes on total)
//
//	Frame 1 (inter):
//	  govpx:  first_partition_size = 2177, total len = 6068
//	  libvpx: first_partition_size = 2169, total len = 6134
//	  first_byte_diff = 1 (govpx OVERSHOOTS libvpx by +8 on first_part,
//	                       UNDERSHOOTS libvpx by -66 bytes on total)
//
// Frame 0's keyframe first_partition_size signature (20432 vs 20463) and
// total length (145487 vs 145534) are IDENTICAL to the BestQuality companion
// 19981bff (Deadline=BestQuality, ARNR=1/1/2) — even though the Deadline,
// the ARNR type, and the frame 1 inter signature all differ. That cross-
// deadline collision on the keyframe byte counts is consistent with the
// task #213 / #210 / #220 finding that the residual divergence on this
// cohort is upstream of the deadline-specific RD speed-feature gates — the
// activity-probe reconstruction state that drives the frame-0 KF picker
// is shared between Good/cpu=0 and Best/cpu=0 (both use the full rdopt.c
// picker), so both deadlines hit the same MB-level mode-pick boundary.
//
// Task #213 verification status (per task #227 charter):
//
//   - Companion seed 22f3d67c (Good/cpu=0/CBR/sc=1/threads=4/token=1/SSIM/
//     ARNR=1/2/1): task #213's activityProbeStaleActZbinAdj + per-attempt
//     rdmult carry CLOSES this seed byte-exactly (matches libvpx at
//     145496/20441 frame 0, 6324/2363 frame 1). The 22f3d67c regression
//     seed is now a passing pin.
//   - This seed (788d442c, Good/VBR/sc=1/threads=4/token=1/SSIM/ARNR=1/1/2):
//     task #213's fix does NOT close the residual. The byte signature
//     matches 19981bff's frame-0 (145487/20432 vs 145534/20463) — the
//     residual mode-picker divergence at MB (0, 2) (FIRST_CANON_DIV idx=2
//     per task #210's tracer) is unchanged. The activity-quartet (mb_activity,
//     act_zbin_adj, rdmult, activity_avg) ALREADY matches across all 3600 MBs
//     on this seed (ACTIVITY_MATCH per the task #210 tracer extension); the
//     remaining divergence is downstream of the activity map and upstream of
//     the bitstream tokenizer — likely in a per-MB RD-pick decision whose
//     inputs (stale Y2 qcoeff from a rejected 16x16 candidate, or UV plane
//     state that survives between picker attempts) differ between govpx and
//     libvpx on this VBR + ScreenContent + threads=4 combination.
//
// Path forward (inherited from task #210 / #213 / #220):
//
//	Extending vpxenc-oracle to emit per-MB rd_pick_intra_mode attempt
//	candidates (with per-candidate Y2 qcoeff snapshots before B_PRED
//	commits) plus per-MB UV plane state at the picker-entry point would
//	let the diag harness localize the divergent picker attempt. Until
//	that instrumentation lands, the byte-level VBR+SC=1 cohort residual
//	remains tracked here as a pinned regression so subsequent fix attempts
//	surface their effect through this audit.
//
// References:
//   - vp8_kf_1280x720_ssim_best_arnr_parity_test.go (companion 19981bff
//     regression; same frame-0 byte signature, BestQuality deadline)
//   - vp8_mb_activity_parity_test.go (per-MB activity-quartet parity coverage
//     showing ACTIVITY_MATCH but FIRST_CANON_DIV at MB (0, 2))
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:91-111 mb_activity_measure
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:225-289 build_activity_map
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1094-1128
//     vp8cx_encode_intra_macroblock (B_PRED commit path)
//   - libvpx v1.16.0 vp8/encoder/rdopt.c rd_pick_intra_mode (Y2 candidate
//     state survives between mode evaluations)
//
// Companion live regression:
//
//	testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_788d442c
func TestVP8KF1280x720SSIMGoodARNRParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the VP8 GoodQuality ARNR parity replay")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
		Tuning:            TuneSSIM,
		ScreenContentMode: 1,
		TokenPartitions:   1,
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=vbr",
		"--screen-content-mode=1",
		"--token-parts=1",
		"--threads=4",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=1",
		"--arnr-type=2",
	})

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(1280, 720, i)
	}

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	// The --threads=4 cohort is routed through the libvpx threading
	// quarantine wrapper so oracle-side non-determinism is caught as a SHA
	// mismatch before the byte comparison below.
	libvpxFrames := encodeVP8FramesWithLibvpxOracleReproducible(t, vpxencOracle, "kf-1280x720-ssim-good-arnr", opts, 700, sources, extraArgs, VP8OracleReproducibleRuns)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future fix-commits surface their effect
	// through this audit. Task #213 closed the companion 22f3d67c CBR cohort
	// byte-exactly; task #236 then ported libvpx's stale BLOCK->zbin_extra
	// carry into the per-MB intra RD picker (see vp8_encoder_reconstruct.go
	// pickerActZbinAdj comment), which fixed the residual MB(0,69) B_PRED
	// block-9 picker flip on the task #227 cohort (seeds 19981bff +
	// 788d442c — this exact GoodQuality/VBR config is the 788d442c seed
	// variant). Task #236 left frame-0 within +5 bytes and frame-1 +0.
	//
	// Task #254 then closed the THREADED keyframe stale-carry: each row
	// worker had been resetting pickerActZbinAdj=activityProbeStaleActZbinAdj
	// per-row, but libvpx's threaded path only seeds the carry ONCE per
	// worker dispatch and lets it survive both within and across the rows
	// that worker handles (the encoded-MB stride is workerCount, so worker
	// i handles rows {i, i+W, i+2W, ...}; b->zbin_extra and x->act_zbin_adj
	// flow through that subsequence without per-row reset). Worker 0 now
	// seeds from activityProbeStaleActZbinAdj (mirroring libvpx's main
	// thread, whose b->zbin_extra was set by vp8cx_frame_init_quantizer
	// using the prev-attempt's last-MB act_zbin_adj); helper workers
	// (workerIndex>0) seed from 0 (mirroring MB_ROW_COMP zero-init at
	// vp8cx_create_encoder_threads:521-523). After task #254 frame 0 is
	// byte-identical to libvpx (145534/20463, sha matches). Frame 1
	// (inter) shifts to -6 bytes vs +0 previously because the post-KF
	// reconstruction is now byte-aligned with libvpx, so the inter
	// picker sees libvpx's reference pixels but the inter side's picker
	// still uses non-stale act_zbin_adj (the inter rdopt path updates
	// zbin_extra inside the candidate loop only when
	// zbin_mode_boost_enabled is true at rdopt.c:1913-1930) — future
	// inter-side ports will tighten that residual.
	//
	// Task #274 re-measurement: PIN HOLDS at govpx=6128 / libvpx=6134
	// (-6 bytes). First_partition_size identical (2169 vs 2169), KF
	// byte-identical. Frame-1 divergence remains confined to the token
	// (second) partition, mirroring the Best/19981bff sibling cohort.
	//
	// Task #277 diagnosis: extended the per-MB oracle tracer to compare
	// qcoeff[][] payloads between govpx and libvpx on frame 1 after
	// filtering libvpx's post-encode mutations (per-block memset(q, 0, 4)
	// when eob<=1 in `vp8_dequant_idct_add_*_block_c`, plus the Y luma
	// eob_adjust bump in `vp8_inverse_transform_mby`). The audit confirms
	// that the inline ZBIN_EXTRA_Y formula at
	// `vp8enc.QuantizeBlockWithZbinAndActivity` (internal/vp8/encoder/inter_quantize.go) is
	// algebraically identical to libvpx's `b->zbin_extra` precomputed by
	// `vp8_update_zbin_extra` (vp8_quantize.c:410-428): both use
	// `(Y1dequant[Q][1] * (zbin_over_quant + zbin_mode_boost +
	// act_zbin_adj)) >> 7` with the FINAL picked mode's zbin_mode_boost
	// (MV_ZBIN_BOOST=4 for NEWMV/NEARESTMV/NEARMV cohorts here).
	//
	// Task #282 re-diagnosis: a verbatim audit of govpx's
	// vp8enc.OptimizeQuantizedBlockWithRDConstants (internal/vp8/encoder/inter_quantize.go)
	// against libvpx's optimize_b (vp8/encoder/encodemb.c:200-356) found
	// the trellis port byte-faithful. The cohort-specific UV blocks
	// 20/23 scan-pos 2 (raster zigzag rc=4) divergence — pattern
	// `gov.qcoeff[blk][4]=1, lvp.qcoeff[blk][4]=0` with eobs[blk]=3 —
	// is therefore UPSTREAM of the trellis. Trellis is faithfully
	// preserving a difference in its INPUT (post-regular_quantize qcoeff
	// or coeff/dqcoeff). Upstream candidates (in walk order):
	//
	//   (1) MC predictor — vp8_build_inter16x16_predictors_mb
	//       (reconinter.c:297-356) vs reconstructWholeMVInterMacroblockFast
	//       (internal/vp8/decoder/reconstruct_inter_fast.go:127-291),
	//       including the chroma sub-pel filter and the chroma-MV
	//       derivation `(mvRow + 1 + sign(mvRow)) / 2 & fullpixel_mask`.
	//   (2) FDCT — vp8_short_fdct4x4_c (dct.c:15-53) vs
	//       forwardDCT4x4Scalar (internal/vp8/encoder/dct.go:15-43) and
	//       the NEON / SSE2 batch ports.
	//   (3) Residual gather — GatherMacroblockUVResiduals4x4
	//       (internal/vp8/encoder/residual_gather.go) vs libvpx vp8_subtract_mbuv
	//       (encodemb.c:78-92).
	//
	// Task #284 charter: extend the oracle tracer with a pre-trellis UV
	// hook on both sides (govpx: between vp8enc.QuantizeBlockWithZbinAndActivity
	// and vp8enc.OptimizeQuantizedBlockWithRDConstants; libvpx: between
	// vp8_regular_quantize_b and optimize_b at encodemb.c:413-415) to
	// dump qcoeff / dqcoeff / eob for blocks 16-23 on frame 1 of seed
	// 19981bff, then bisect upstream layer by layer. The tracer
	// extension requires rotating both the libvpx oracle SHA pin
	// (oracleSHAvpxencArm64Darwin in internal/coracle/oracle_sha_test.go)
	// and the build_vpxenc_oracle.sh want_config string. The -5/-6 byte
	// delta is the steady-state cohort budget until that probe lands.
	//
	// Task #286 NEON-disable audit: re-ran this pin with `-tags
	// "govpx_oracle_trace purego"` to elide every NEON kernel in
	// internal/vp8/{dsp,encoder} (each per-arch dispatch file is gated
	// on `!purego`; the purego variant in *_other.go routes through
	// the scalar reference). Result: frame-1 govpx SHA was IDENTICAL
	// between the NEON-on and NEON-off runs (51aa383bd1489162), and
	// frame-1 govpx len held at 6128. The NEON ports are byte-faithful
	// and are NOT the cause of the -6 byte divergence — divergence
	// lives in scalar-side encoder logic.
	//
	// Task #288 interRDCacheReusable disable audit (NEGATIVE result):
	// applied Option A — forced interRDCacheReusable to return false
	// unconditionally so the accepted path re-runs predictor + residual
	// gather + FDCT + quant from scratch (libvpx contract — no
	// cross-picker-accepted UV-DCT cache). Result: frame-1 govpx SHA
	// BYTE-IDENTICAL between cache-on and cache-off runs
	// (51aa383bd1489162), len held at 6128. The picker-stamped
	// post-FDCT DCTs already match the accepted-path re-FDCT
	// byte-exactly for the cohort that satisfies the cache parity
	// check; the UV-RD cache is NOT the source of the -6 byte
	// divergence. Cache stays enabled to preserve the perf
	// short-circuit.
	//
	// Picker-vs-accepted act_zbin_adj skew was ruled out. The suspected
	// failure mode was that tunedZbinAdjustment() only ran in the accepted
	// path, leaving the picker to score actZbinAdj=0 while accepted encode
	// used the activity-tuned value. Code inspection and
	// vp8_activity_zbin_adjustment_parity_test.go disprove that:
	// every RD picker subroutine reads tunedZbinAdjustment(mbRow, mbCol)
	// at entry — estimateInterResidualRDAccountingWithModeContext
	// (vp8_encoder_inter_rd.go:85-90), estimateInterIntraModeRDScore
	// (vp8_encoder_inter_modes_rd_intra.go:23-28),
	// selectInterFrameSplitModeRDScore +
	// estimateSplitInterResidualRD (vp8_encoder_inter_modes_rd_split.go:81-85
	// and :203-208) — and the accepted-path at
	// vp8_encoder_reconstruct.go:652/713 uses the same expression. The
	// activity map is built once per frame in prepareTuningActivityMap
	// before encode_mb_row starts (no in-row mutation), so
	// tunedZbinAdjustment(row, col) returns the SAME value for any
	// (row, col) regardless of when in the encode loop it is invoked.
	// interRDCacheReusable's `actZbinAdj` equality check
	// (vp8_encoder_inter_coefficients.go:178) would refuse to fire if the
	// picker stored a different actZbinAdj than the accepted-path
	// requested — task #288's cache-off SHA-equality experiment
	// additionally confirms picker/accepted actZbinAdj parity on this
	// cohort. The -6 byte residual is NOT explained by an actZbinAdj
	// skew.
	//
	// Chroma sub-pel predictor check: internal/vp8/decoder covers chroma
	// MV derivation, copy-vs-subpel dispatch, and sixtap/bilinear filter
	// arithmetic against libvpx v1.16.0. The -6 byte ARNR pin-hold is not
	// explained by a chroma sub-pel predictor divergence.
	//
	// Remaining sharpest candidate (in walk order, per task #284):
	//   #3 residual gather slice ordering —
	//      GatherMacroblockUVResiduals4x4 vs vp8_subtract_mbuv.
	//
	// Task #297 pre-trellis UV bisect (RELOCATES the root cause): same
	// finding as the Best/19981bff sibling pin. The pre-trellis UV trace
	// (task #296) on this Good/788d442c cohort surfaces the first
	// divergent (mb_row, mb_col, block, scan_pos) on frame 1 at
	// MB(0,0) block 16 scan_pos 0 in the COEFF layer (post-FDCT), with
	// gov_coeff=-48 vs lib_coeff=-46 AND gov_zbin_extra=6 vs
	// lib_zbin_extra=2. Walking back via the same trace's accepted-mode
	// rows shows libvpx codes MB(0,0) frame 1 as `SPLITMV`
	// (zbin_mode_boost=0) while govpx codes it as `NEWMV`
	// (zbin_mode_boost=MV_ZBIN_BOOST=4). The frame-wide mode histogram
	// (filtered to the 960 main-thread MBs whose mb_row/mb_col label
	// survives libvpx's pthread_setspecific limitation) confirms:
	//   govpx: 116 SPLITMV (out of 3600)
	//   libvpx: 664 SPLITMV (out of 960 labeled)
	// libvpx picks SPLITMV ~6x more often. The root cause is in govpx's
	// inter-frame SPLITMV mode picker (selectInterFrameSplitModeRDScore)
	// — it under-picks SPLITMV relative to libvpx rd_pick_inter_mode for
	// this cohort. The -6 byte ARNR pin-hold is the cumulative effect of
	// these picker flips on the inter token partition, NOT a UV qcoeff
	// / FDCT / residual / quantize pipeline bug.
	//
	// Path forward (HISTORICAL — see task #314 below for the corrected
	// root cause): bisect SPLITMV vs the simple-MV modes' per-mode RD
	// breakdown to identify which sub-component drives the picker flip.
	//
	// Task #314 corrected the chain (see feedback_vp8_arnr_investigation_chain
	// for the full retraction set):
	//
	//   - The #297 "SPLITMV 664 vs 116" histogram was measured against
	//     libvpx --threads=4 MT-LF vs threads=1 govpx; libvpx's MT-LF
	//     produces a different frame-0 recon than ST-LF, so the picker
	//     histogram is apples-to-oranges. Retracted.
	//   - The #298 picker-scoreboard rate_y comparison was misaligned
	//     under the same threads=4 confusion. Retracted.
	//   - The #304 "all-zero qcoeff means govpx is incorrect" framing
	//     is wrong: libvpx threads=1 also produces all-zero qcoeff for
	//     that MB. The picker quantize (#310) is byte-faithful.
	//   - The #300 SPLITMV bounds port is libvpx-verbatim and correct
	//     but produces zero observed impact on this cohort.
	//
	// ACTUAL ROOT CAUSE (task #314): post-encode CHROMA trellis
	// optimize_b for blockType=2 / PLANE_TYPE_UV makes different
	// keep/drop decisions for ±1 chroma DC coefficients. Frame 1 of
	// 19981bff (sibling cohort): 2241/3600 MBs diverge, 2115
	// chroma-only, 85% DC-only, 1934 blocks govpx=+1 (keeps where
	// libvpx drops), 1078 govpx=-1. Code layers: govpx
	// vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity → vp8enc.OptimizeQuantizedBlock
	// vs libvpx vp8/encoder/encodemb.c:vp8_encode_inter16x16 →
	// optimize_mb → optimize_b (PLANE_TYPE_UV branch). The trellis core
	// itself (#282) is byte-faithful — divergence is in the chroma
	// input to the trellis or the chroma-specific dispatch gating.
	// Task #316 bisects this. Cleared-candidate list (#282 / #284 /
	// #286 / #288 / #290 / #292 / #294 / #299 / #303 / #307 / #309 /
	// #310 / #312 / #313) — do NOT re-investigate. See
	// feedback_vp8_arnr_investigation_chain for the full breakdown.
	wantFrame0GovpxLen := 145534
	wantFrame0LibvpxLen := 145534
	wantFrame0GovpxFirstPart := 20463
	wantFrame0LibvpxFirstPart := 20463
	wantFrame1GovpxLen := 6134
	wantFrame1LibvpxLen := 6134
	wantFrame1GovpxFirstPart := 2169
	wantFrame1LibvpxFirstPart := 2169

	if got := len(govpxFrames[0]); got != wantFrame0GovpxLen {
		t.Fatalf("frame 0 govpx len drift: got=%d want=%d", got, wantFrame0GovpxLen)
	}
	if got := len(libvpxFrames[0]); got != wantFrame0LibvpxLen {
		t.Fatalf("frame 0 libvpx len drift: got=%d want=%d", got, wantFrame0LibvpxLen)
	}
	if got := len(govpxFrames[1]); got != wantFrame1GovpxLen {
		t.Fatalf("frame 1 govpx len drift: got=%d want=%d", got, wantFrame1GovpxLen)
	}
	if got := len(libvpxFrames[1]); got != wantFrame1LibvpxLen {
		t.Fatalf("frame 1 libvpx len drift: got=%d want=%d", got, wantFrame1LibvpxLen)
	}

	if got, _ := parseVP8FramePartitionSizes(govpxFrames[0]); got != wantFrame0GovpxFirstPart {
		t.Fatalf("frame 0 govpx first_partition_size drift: got=%d want=%d", got, wantFrame0GovpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(libvpxFrames[0]); got != wantFrame0LibvpxFirstPart {
		t.Fatalf("frame 0 libvpx first_partition_size drift: got=%d want=%d", got, wantFrame0LibvpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(govpxFrames[1]); got != wantFrame1GovpxFirstPart {
		t.Fatalf("frame 1 govpx first_partition_size drift: got=%d want=%d", got, wantFrame1GovpxFirstPart)
	}
	if got, _ := parseVP8FramePartitionSizes(libvpxFrames[1]); got != wantFrame1LibvpxFirstPart {
		t.Fatalf("frame 1 libvpx first_partition_size drift: got=%d want=%d", got, wantFrame1LibvpxFirstPart)
	}

	govpxSHA0 := sha256.Sum256(govpxFrames[0])
	libvpxSHA0 := sha256.Sum256(libvpxFrames[0])
	govpxSHA1 := sha256.Sum256(govpxFrames[1])
	libvpxSHA1 := sha256.Sum256(libvpxFrames[1])

	t.Logf("GoodQuality ARNR parity pin: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("GoodQuality ARNR parity pin: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}
