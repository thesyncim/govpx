//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestVP8KF1280x720SSIMBestARNRParity pins fuzz seed
// regression_option_grid_19981bff: a 1280x720 BestQuality / cpu=0 / VBR /
// screen-content=1 / threads=4 / TuneSSIM / ARNR=1/1/3 clip whose
// frame-0 keyframe and frame-1 inter both byte-diverge from libvpx at
// byte 0 / byte 1, mirroring (but in the OPPOSITE direction from) the
// GoodQuality cohort pinned by TestVP8KF1280x720SSIMGoodARNRParity.
//
// Cohort decode (seed bytes "A", 0x41 = 65; oracleRuntimeControlFuzzBytes
// wraps len(data)==1 so every bucket reads byte 0):
//   - Resolution: bucket 65%11=10  ⇒  1280x720
//   - Deadline:   bucket 65%3=2    ⇒  BestQuality
//   - CpuUsed:    bucket 65%5=0    ⇒  cpu=0
//   - RateControl bucket 65%4=1    ⇒  VBR
//   - Feature:    bucket 65%8=1    ⇒  screen-content-mode=1
//   - TokenPart:  bucket 65%4=1
//   - Threads:    bucket 65%3=2    ⇒  threads=4
//   - Sharpness:  bucket 65%4=1    ⇒  sharpness=0 (idx 0, libvpx omits flag)
//   - Tuning:     bucket 65%3=2    ⇒  TuneSSIM
//   - ARNR:       bucket 65%4=1    ⇒  maxframes=1, strength=1, type=1+1=2
//     (the fuzz harness logs arnr=1/1/3
//     because it prints `arnrType` bucket;
//     the libvpx CLI value forwarded via
//     `c.arnrType` is 2, matching the type=2
//     `--arnr-type=2` arg below.)
//
// Current divergence (origin/main, commit 15babf6d):
//
//	Frame 0 (KF):
//	  govpx:  first_partition_size = 20474, total len = 145485
//	  libvpx: first_partition_size = 20463, total len = 145534
//	  first_byte_diff = 0 (govpx OVERSHOOTS libvpx by +11 on first_part,
//	                       UNDERSHOOTS libvpx by -49 bytes on total)
//
//	Frame 1 (inter):
//	  govpx:  first_partition_size = 2240, total len = 5940
//	  libvpx: first_partition_size = 2264, total len = 6121
//	  first_byte_diff = 1 (govpx UNDERSHOOTS libvpx by -24 on first_part,
//	                       UNDERSHOOTS libvpx by -181 bytes on total)
//
// The 11-byte first-partition OVERSHOOT on the keyframe is OPPOSITE-SIGNED
// from task #198's 94eb71d5 cohort (where govpx UNDERSHOT libvpx by -5 on
// the keyframe), confirming the residual SSIM-cohort drift at 1280x720 is
// not a single-sided over- or under-quantization but per-MB activity SSE
// drift propagating into mode-flip ties on both sides of libvpx's RDCOST
// boundaries.
//
// Experimental finding (task #207 picker stale-prev-MB actZbinAdj fix):
//
//	The companion 94eb71d5 audit recorded "Mirroring libvpx by carrying
//	lastActZbinAdj across MBs (resets to 0 at frame start) MOVED frame 0
//	first_part to 20583 (overshoots libvpx's 20575 by +8 bytes)". Task #207
//	confirmed that experiment with a fresh probe: threading the stale-
//	previous-MB actZbinAdj through `predictBestKeyFrameIntraModeWith
//	RDConstants` in `buildReconstructingKeyFrameCoefficientsWithSegmentation
//	Serial` (vp8_encoder_reconstruct.go) shifts 94eb71d5 frame 0 from 20570 to
//	20583 (opposite-direction overshoot of libvpx 20575 by +8 bytes) and
//	shifts frame 1 from 1146 to 1132 (worsening from -5 to -19). The port
//	is libvpx-faithful to encodeframe.c:1099-1108 (picker runs BEFORE
//	adjust_act_zbin + vp8_update_zbin_extra) yet lands net-negative in
//	isolation, consistent with the audit's hypothesis that an upstream
//	per-MB activity_map drift is feeding the picker mode-flip residual.
//
// Experimental finding (task #207 vp8_optimize_mby above_context==NULL
// gate):
//
//	Task #207 also confirmed that gating the activity probe's optimize
//	trellis off `activityProbeAboveContextSeeded` (matching libvpx
//	vp8/encoder/encodemb.c:436-438 vp8_optimize_mby short-circuit when
//	xd->above_context == NULL on the first probe of the encoder's
//	lifetime) has ZERO effect on these two seeds' bitstreams, mirroring
//	the audit's earlier prediction. The reason: both seeds recode their
//	frame 0 keyframe, and by the recoded attempt the analog flag has
//	flipped to true (the first attempt's encode_mb_row equivalent already
//	ran), so the committed-attempt probe always sees optimize=true in
//	both libvpx and govpx. The gate is libvpx-faithful and landed on
//	origin/main as part of task #207 even though it does not move these
//	specific seeds, because it correctly aligns the first attempt's
//	activity_map with libvpx's on cohorts whose first attempt IS the
//	committed attempt (no-recode paths).
//
// Path forward: extending vpxenc-oracle to emit per-MB
// `mb_activity_measure()` return values (vp8/encoder/encodeframe.c:95-111
// ALT_ACT_MEASURE=1 branch) plus per-MB `x->act_zbin_adj` and `x->rdmult`
// post-vp8_activity_masking would let the diag harness localize the first
// diverging MB's activity SSE, identify the upstream recon delta feeding
// that MB, and surface the compounding bug that makes the libvpx-correct
// picker port land on the wrong side of libvpx's RDCOST boundary. Until
// that instrumentation lands, the picker-stale-actZbinAdj port remains
// landing-blocked.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:95-111 mb_activity_measure
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:225-289 build_activity_map
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:293-313 vp8_activity_masking
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1094-1108
//     vp8cx_encode_intra_macroblock (picker BEFORE vp8_update_zbin_extra)
//   - libvpx v1.16.0 vp8/encoder/encodemb.c:436-438 vp8_optimize_mby
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:278-289 ZBIN_EXTRA macros
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:410-428 vp8_update_zbin_extra
//
// Companion live regression:
//
//	testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_19981bff
//
// Companion opposite-signed cohort:
//
//	TestVP8KF1280x720SSIMGoodARNRParity (94eb71d5)
func TestVP8KF1280x720SSIMBestARNRParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the BestQuality ARNR parity replay")
	}
	vpxencOracle := findVpxencOracle(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
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
	// Task #349: this audit's cohort is decoded with --threads=4 (see
	// the comment block above), which sits inside the libvpx threading
	// non-determinism quarantine. Use the re-run wrapper so that any
	// oracle-side flake is caught as a test failure with a SHA log
	// rather than silently contaminating the byte comparison below
	// (the original misattribution chain at #297/#298/#304/#324).
	libvpxFrames := encodeFramesWithLibvpxOracleReproducible(t, vpxencOracle, "task207-byte0-kf-1280x720-ssim-best-arnr-audit", opts, 700, sources, extraArgs, EncodeFramesWithLibvpxOracleReproducibleRuns)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future regressions don't silently
	// re-interpret what this audit captured. Task #213 closed the
	// activity-probe recon divergence; task #236 then ported libvpx's
	// stale BLOCK->zbin_extra carry into the per-MB intra RD picker
	// (see vp8_encoder_reconstruct.go pickerActZbinAdj comment), which
	// fixed the residual MB(0,69) B_PRED block-9 picker flip on the
	// task #227 seed 19981bff (this exact config). Task #236 left the
	// frame-0 KF within +5 bytes and frame-1 within +3.
	//
	// Task #254 then closed the THREADED keyframe stale-carry: in
	// libvpx's threaded path, worker 0 maps to the main thread whose
	// b->zbin_extra was set by vp8cx_frame_init_quantizer using the
	// previous attempt's last-MB act_zbin_adj, while helper workers
	// (workerIndex>0) start with zero-init b->zbin_extra (their
	// MB_ROW_COMP[i].mb is memset-zero at vp8cx_create_encoder_threads).
	// Within a single thread the stale carry survives across all rows
	// that thread handles (no per-row reset in either encode_mb_row at
	// encodeframe.c:316-575 or the helper loop at ethreading.c:76-310).
	// govpx previously RESET pickerActZbinAdj=activityProbeStaleActZbinAdj
	// at EVERY row (including all helper rows), drifting by exactly the
	// +5/+3-byte gap seen here. After task #254, frame 0 keyframe is
	// byte-identical to libvpx (145534/20463, sha matches). Frame 1
	// (inter) shifts to -5 bytes because the post-KF reconstruction is
	// now byte-aligned with libvpx, so the inter picker sees libvpx's
	// reference pixels but govpx's inter-side picker still uses
	// non-stale act_zbin_adj (rdopt.c:1930 vp8_update_zbin_extra runs
	// inside the inter candidate loop only when zbin_mode_boost_enabled
	// is true) — future inter-side ports will tighten that residual.
	//
	// Task #274 re-measurement: PIN HOLDS at govpx=6116 / libvpx=6121
	// (-5 bytes). First_partition_size is identical (2264 vs 2264) and
	// the per-MB activity quartet (mb_activity, act_zbin_adj=2, rdmult,
	// activity_avg) matches libvpx for every MB on frame 1 at threads=1
	// (vp8_mb_activity_parity_test.go probe). All accepted-mode
	// fields (mode, ref_frame, mv_row, mv_col, skip) for frame 1's MB(0,0)
	// also match (NEWMV/LAST_FRAME, MV=(8,16), mb_rate=20474). The
	// residual lives in the token (second) partition, not headers.
	//
	// Task #277 diagnosis: extended the per-MB oracle tracer to compare
	// qcoeff[][] payloads between govpx and libvpx on frame 1, filtering
	// out the benign post-encode mutations that libvpx's
	// vp8_dequant_idct_add_*_block / vp8_inverse_transform_mby eob_adjust
	// inject into the capture state (per-block memset(q, 0, 4) for
	// eob<=1, plus the inverse-Walsh DC-bump that flips Y luma eobs
	// from 0 to 1 when the Y2 inverse-Walsh DC slot is non-zero). After
	// that filter, frame 1 still shows ~519 MBs with bitstream-relevant
	// qcoeff divergences clustered on UV V0/V3 (blocks 20 / 23) at scan
	// positions whose zigzag raster index is in {4, 8}; the most common
	// pattern is `gov.qcoeff[blk][4]=1, lvp.qcoeff[blk][4]=0` with both
	// eobs[blk]=3. Both govpx and libvpx report identical
	// (zbin_over_quant, zbin_mode_boost=MV_ZBIN_BOOST=4, act_zbin_adj=2)
	// tuples for these MBs and identical Y2 qcoeff[24] (eob=16, exact
	// values), which implies the per-block luma DC pre-Walsh inputs
	// match — so the inline ZBIN_EXTRA_Y formula at
	// `quantizeBlockWithZbinAndActivity` (vp8_encoder_inter_quantize.go) IS
	// equivalent to libvpx's `b->zbin_extra = (Y1dequant[Q][1] *
	// (zbin_over_quant + zbin_mode_boost + act_zbin_adj)) >> 7` at the
	// quantize-input boundary.
	//
	// Task #282 re-diagnosis: a verbatim audit of govpx's
	// optimizeQuantizedBlockWithRDConstants (vp8_encoder_inter_quantize.go)
	// against libvpx's optimize_b (vp8/encoder/encodemb.c:200-356) found
	// the trellis port byte-faithful — RDCOST + RDTRUNC tie-break,
	// token_cost subtree elision, DefaultZigZag/DefaultInvZigZag,
	// next-state propagation all line up. The implication: the cohort
	// divergence at UV blocks 20/23 scan-pos 2 is NOT in the trellis
	// itself; the trellis is faithfully preserving an UPSTREAM
	// difference in its input. The candidates upstream are:
	//
	//   (1) MC predictor — vp8_build_inter16x16_predictors_mb
	//       (reconinter.c:297-356) vs reconstructWholeMVInterMacroblockFast
	//       (internal/vp8/decoder/reconstruct_inter_fast.go:127-291).
	//       The chroma-MV derivation `(mvRow + 1 + sign(mvRow)) / 2`
	//       and the `&= x->fullpixel_mask` gating must produce
	//       byte-identical chroma planes; the chroma sub-pel filter
	//       (sixtap_predict8x8 / bilinear_predict8x8) likewise.
	//   (2) FDCT — vp8_short_fdct4x4_c (dct.c:15-53) vs
	//       forwardDCT4x4Scalar (internal/vp8/encoder/dct.go:15-43) and
	//       the NEON/SSE2 batch ports. Algebraically equivalent on
	//       inspection, but a stride / batch-buffer drift would surface
	//       as identical aggregate eobs with a single coeff flipped.
	//   (3) Residual gather — gatherMacroblockUVResiduals4x4
	//       (vp8_encoder_inter_residuals.go:38-58) vs libvpx
	//       vp8_subtract_mbuv (encodemb.c:78-92): also algebraically
	//       equivalent, but the `pred.U[uOff:]` slice ordering against
	//       the analysis-image base must match libvpx's
	//       `xd->dst.{u,v}_buffer + recon_uvoffset`.
	//
	// Task #284 charter: extend the oracle tracer with a pre-trellis UV
	// hook (between vp8_regular_quantize_b and optimize_b on libvpx;
	// between quantizeBlockWithZbinAndActivity and
	// optimizeQuantizedBlockWithRDConstants on govpx) to dump qcoeff /
	// dqcoeff / eob for blocks 16-23 on frame 1 of seed 19981bff, then
	// walk upstream (qcoeff → dqcoeff → coeff → residual → predictor)
	// to pin the actual divergence layer. Tracer extension blocks on
	// libvpx oracle SHA pin (oracleSHAvpxencArm64Darwin in
	// internal/coracle/oracle_sha_test.go) being rotated alongside the
	// build_vpxenc_oracle.sh want_config bump. The -5/-6 byte delta
	// is the steady state until that probe lands.
	//
	// Task #286 NEON-disable audit: re-ran this pin with `-tags
	// "govpx_oracle_trace purego"` to route every encoder path through
	// the scalar reference (every SIMD dispatch file in
	// internal/vp8/{dsp,encoder} carries `!purego` so the purego tag
	// elides the NEON kernels — ForwardDCT4x4Batch, ForwardWalsh4x4,
	// vp8_regular_quantize_b_simd, the fast_quant batch, the FDCT/IDCT
	// per-block kernels, SAD/variance/subpixel, etc.). Result: the
	// govpx frame-1 SHA was IDENTICAL between the NEON-on and
	// NEON-off runs (Best=6b18859b0ed02b5c, Good=51aa383bd1489162),
	// and frame-1 govpx lengths held at 6116 / 6128. The NEON ports
	// are byte-faithful and are NOT the cause of the -5/-6 byte
	// divergence — divergence lives in scalar-side encoder logic.
	//
	// Task #288 interRDCacheReusable disable audit (NEGATIVE result):
	// applied Option A from the task brief — forced interRDCacheReusable
	// to return false unconditionally so the accepted path re-runs
	// predictor + residual gather + FDCT + quant from scratch (matching
	// the libvpx vp8_encode_inter16x16 contract — libvpx has no
	// cross-picker-accepted UV-DCT cache). Result: frame-1 govpx SHA
	// was BYTE-IDENTICAL between cache-on and cache-off runs (Best=
	// 6b18859b0ed02b5c, Good=51aa383bd1489162), and frame-1 govpx
	// lengths held at 6116 / 6128. The picker-stamped post-FDCT DCTs
	// already match the accepted-path re-FDCT byte-exactly for the
	// cohort that satisfies the cache parity check; the UV-RD cache is
	// NOT the source of the -5/-6 byte divergence. Cache stays enabled
	// to preserve the perf short-circuit.
	//
	// Picker-vs-accepted act_zbin_adj skew is ruled out:
	// vp8_activity_zbin_adjustment_parity_test.go verifies every RD picker subroutine consults
	// tunedZbinAdjustment(mbRow, mbCol) at entry —
	// estimateInterResidualRDAccountingWithModeContext (vp8_encoder_inter_rd.go:85-90),
	// estimateInterIntraModeRDScore (vp8_encoder_inter_modes_rd_intra.go:23-28),
	// selectInterFrameSplitModeRDScore (vp8_encoder_inter_modes_rd_split.go:81-85),
	// selectInterFrameSplitModeRDScore's accounting tail
	// (vp8_encoder_inter_modes_rd_split.go:203-208) — and the accepted-path
	// at vp8_encoder_reconstruct.go:652/713 uses the same expression. The
	// activity map is built once per frame in prepareTuningActivityMap
	// before encode_mb_row starts and is never mutated in-row, so
	// tunedZbinAdjustment is deterministic per (mbRow, mbCol) across
	// picker and accepted-path calls. interRDCacheReusable's
	// `actZbinAdj` equality check (vp8_encoder_inter_coefficients.go:178)
	// would refuse to fire if the two diverged — task #288's cache-off
	// SHA-equality experiment additionally confirms picker/accepted
	// actZbinAdj parity on this exact cohort. The -5/-6 byte residual
	// is NOT explained by an actZbinAdj skew.
	//
	// Task #292 chroma sub-pel predictor audit (NEGATIVE result):
	// per static inspection plus an exhaustive sub-pixel filter
	// sweep (vp8_chroma_subpel_predictor_parity_test.go), all four
	// sub-components of govpx's chroma sub-pel predictor are
	// byte-faithful to libvpx v1.16.0:
	//   (a) chroma MV derivation `(mvRow + 1 + sign)/2 &
	//       fullpixel_mask` — exhaustive sweep over mvRow ∈ [-256,
	//       256] × {fullPixel=false,true} matches libvpx
	//       vp8/common/reconinter.c:327-334 verbatim.
	//   (b) UV plane base offset `(uvMVRow >> 3)*uvStride +
	//       (uvMVCol >> 3)` — libvpx packs uv_stride = y_stride >>
	//       1 (vpx_scale/generic/yv12config.c:62); govpx packs
	//       identically (internal/vp8/common/frame.go:167-169) ⇒
	//       byte-equivalent strides at 1280x720 border=32 yield
	//       yStride=1344, uStride=672.
	//   (c) Sixtap/bilinear/copy dispatch decision `(uvRow|uvCol)&7
	//       == 0` matches libvpx `_16x16mv.as_int & 0x00070007 ==
	//       0` exhaustively across all 8×8 sub-pixel positions.
	//   (d) Filter kernel — SubPelFilters table (8×6) and
	//       BilinearFilters table (8×2) match libvpx byte-exactly
	//       (filter.c:15-31); the scalar kernels produce
	//       byte-identical output to a fresh libvpx-from-source
	//       reimplementation across every (xoffset, yoffset) ∈
	//       [0..7]×[0..7] \ {(0,0)} for both sixtap and bilinear.
	// The -5 byte ARNR pin-hold is NOT explained by a chroma
	// sub-pel predictor divergence.
	//
	// Remaining sharpest candidate (in walk order, per task #284):
	//   #3 residual gather slice ordering —
	//      gatherMacroblockUVResiduals4x4 vs vp8_subtract_mbuv.
	//
	// Task #297 pre-trellis UV bisect (RELOCATES the root cause): the new
	// per-UV-block oracle tracer (task #296) on both sides now lets us
	// surface the first divergent (mb_row, mb_col, block, scan_pos) on
	// frame 1 of this seed. The bisect (TestVP8PretrellisUVParity)
	// surfaces MB(0,0) block 16 scan_pos 0 in the COEFF (post-FDCT) layer
	// — i.e. the divergence is BEFORE the regular quantizer, with both
	// sides reporting identical UV.Dequant[1]/zbin_over_quant/act_zbin_adj
	// (=2) tuples but DIFFERENT zbin_extra (gov=6 vs lib=2). Walking back
	// upstream via the same trace's accepted-mode rows reveals the actual
	// upstream cause: libvpx codes MB(0,0) frame 1 as `SPLITMV` (split-MV,
	// zbin_mode_boost=0) while govpx codes it as `NEWMV` (whole-block MV,
	// zbin_mode_boost=MV_ZBIN_BOOST=4). The (4 vs 0) zbin_mode_boost gap
	// explains the (6 vs 2) zbin_extra gap byte-for-byte. The accepted-MV
	// for both sides matches (8,16) per task #277, so this is not a motion-
	// search divergence — it is a SPLITMV-vs-NEWMV mode-pick divergence in
	// the inter-frame RD picker (selectInterFrameSplitModeRDScore vs
	// libvpx rd_pick_inter_mode SPLITMV gating).
	//
	// Per-frame mode histogram on this cohort's frame 1 (filtered to the
	// 960 main-thread MBs whose mb_row/mb_col label survives libvpx's
	// pthread_setspecific limitation):
	//   govpx: 3482 NEARESTMV + 116 SPLITMV + 2 NEWMV (out of 3600)
	//   libvpx: 295 NEARESTMV + 664 SPLITMV + 1 NEWMV (out of 960 labeled)
	// libvpx picks SPLITMV ~6x more often than govpx. Each SPLITMV pick
	// flip costs/saves several bytes through the per-subblock MV coding
	// rate budget AND the cleaner UV residual (the SPLITMV per-subblock
	// predictor is strictly closer to the source than the NEWMV whole-
	// block predictor), which is what surfaces as the residual -5/-6 byte
	// ARNR pin-hold on this and the task #227 cohort.
	//
	// Path forward (HISTORICAL — see task #314 below for the corrected
	// root cause and retracted hypotheses): extend the per-MB
	// inter-candidate trace with the per-mode RD breakdown for SPLITMV
	// vs the simple-MV modes, and bisect which sub-component (split-
	// partition cost, sub-block MV search range, sub-block MV reference,
	// RDCOST tie-break) drives the picker flip for MB(0,0).
	//
	// Task #316 chroma optimize_b post-trellis bisect (LOCALIZES rdmult
	// divergence at MB(0,0) frame 1): the new
	// TestVP8ChromaOptimizeBlockParity captures the per-UV-block
	// POST-trellis qcoeff / dqcoeff / dequant / coeff / rdmult / rddiv on
	// both sides for frame 1 (oracle hook splices into
	// vp8_encode_inter16x16 right after optimize_mb on libvpx; mirrored on
	// the govpx side in reconstructMacroblockUVCoefficients after each
	// quantizeEncodedBlockWithRDZbinAndActivity call). On the BestARNR
	// cohort the bisect surfaces 4720 divergent UV blocks on frame 1 with
	// the FIRST divergence at MB(0,0) block 16 in the rdmult layer:
	//
	//   govpx rdmult=326 vs libvpx rdmult=551 (ratio 0.591) — both sides
	//   pass rddiv=1; both dequant arrays match byte-exactly
	//   ([85,140,...,140]); coeff inputs differ (which is the downstream
	//   residual reflecting the lower-rdmult preferred-distortion path).
	//
	// Task #319 chroma rdMult/rdDiv audit (RETRACTS the 326/551 rdmult-
	// divergence claim as a TRACE-EMIT ASYMMETRY, not a real bug):
	//
	//   libvpx's govpx_oracle_emit_chroma_optimize_b reads `rdmult_in =
	//   x->rdmult` (build_vpxenc_oracle.sh line 1891), which is the
	//   POST-vp8_activity_masking value — encodeframe.c:295-310 mutates
	//   x->rdmult per-MB to `(rdMult * (2*act + avg) + (a>>1)) /
	//   (act + 2*avg)` whenever TuneSSIM is active. govpx's
	//   emitOracleChromaOptimizeBTrace was emitting
	//   `libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)` directly —
	//   the PRE-activity-masking value — for the same scalar slot, so the
	//   comparator saw 326 (pre-activity) vs 551 (post-activity) and
	//   misread that as an rdmult divergence.
	//
	//   The 326/551 ratio ≈ 0.591 is consistent with MB(0,0) being a
	//   textured block whose per-MB activity is ~7.7x activity_avg:
	//   solving `(2A + V) / (A + 2V) = 1.69` for `A` (with V=avg) yields
	//   `A/V ≈ 7.7`. The actual VALUE consumed by the trellis on both
	//   sides IS the same activity-lifted rdmult — task #210's per-MB
	//   activity quartet (mb_activity, act_zbin_adj, rdmult, activity_avg)
	//   already pinned this match for every MB on frame 1.
	//
	//   Fix landed in task #319: vp8_encoder_inter_coefficients.go
	//   traceChromaOptimizeB branch now emits the threaded `rdMult` /
	//   `rdDiv` values (which the caller already wraps with
	//   tunedRDMultiplier(libvpxRDConstantsWithZbin, row, col)) so the
	//   emitted scalar matches libvpx's x->rdmult byte-for-byte. After the
	//   fix, the post-trellis bisect at MB(0,0) will report identical
	//   rdmult/rddiv on both sides, redirecting the chroma ±1 DC keep/drop
	//   investigation to the per-coefficient (rate, distortion) layers
	//   (token_costs[2][band][pt] / dctValueBaseCost / dx*dx distortion /
	//   shortcut boundary).
	//
	// The directional bin counts line up with task #314's encoder-side
	// qcoeff scoreboard (govpx KEEPS +1 DC where libvpx DROPS in 1934
	// blocks; govpx KEEPS -1 DC where libvpx DROPS in 1078 blocks). The
	// ±1 DC keep/drop direction split is NOT explained by an rdmult
	// divergence (per #319 retraction); the residual now requires a
	// per-coefficient (rate, distortion) audit at the same MB(0,0) block
	// 16/23 sites.
	//
	// Task #298 SPLITMV RD bisect (LOCALIZES the picker divergence,
	// LATER RETRACTED — see task #314):
	// the new TestVP8SPLITMVRDParity captures the per-mode
	// inter_candidate trace for MB(0,0) frame 1 on both sides and
	// surfaces the picker scoreboard. Key findings:
	//
	//   govpx NEWMV: rate=20474 rate_y=7519 rate_uv=9298 dist=58282 yrd=73707 score=102349
	//   libvpx NEWMV: rate=48796 rate_y=34799 rate_uv=10340 dist=55660 yrd=129509 score=160686
	//   libvpx SPLITMV: rate=31708 rate_y=15344 rate_uv=10340 dist=55660 yrd=92730 score=123907
	//
	// govpx never reaches a "tested" SPLITMV/LAST row for MB(0,0):
	// selectInterFrameSplitModeRDScore returns ok=false (the new
	// trace probe surfaces this as outcome="splitmv_rd_dropout").
	// libvpx's vp8_rd_pick_best_mbsegmentation succeeds because its
	// bsi.segment_rd cap = best_mode.yrd = 129509 is loose enough for
	// SPLITMV's per-label commit at segment_rd=92730 (<129509). govpx's
	// bestYRD cap going into SPLITMV is the NEWMV picker yrd=73707 —
	// LOWER than libvpx's 129509 by 55802, so SPLITMV's per-label
	// shape.SegmentYRD (which is anchored by the same residual on the
	// libvpx side at 92730) exceeds the cap and triggers the cutoff
	// without any partition committing.
	//
	// The divergent component is rate_y (govpx 7519 vs libvpx 34799,
	// delta=-27280; distortion delta is +2622 only). govpx's picker
	// quantize for NEWMV at MB(0,0) is producing all-zero Y qcoeff
	// (eob_sum=1 in the accepted-mode mb trace — the lone non-zero is
	// at qcoeff[23][0], one UV slot), while libvpx's picker quantize
	// for NEWMV is producing enough non-zero Y coefficients to yield
	// rate_y=34799. Same MV=(8,16), same ref (frame 0 reconstruction is
	// byte-identical per the SHA pin), same source — yet different
	// qcoeff. Static inspection of the quantize formula
	// (vp8_encoder_inter_quantize.go quantizeBlockWithZbinAndActivity line 64
	// vs libvpx vp8_quantize.c:75 vp8_regular_quantize_b) is byte-faithful,
	// so the upstream cause must surface in the actual residual /
	// zbin_extra at picker time and requires a per-block picker-side
	// Y qcoeff oracle trace similar to the task #296 pre-trellis UV hook.
	// Task #299+ charter: extend the oracle tracer with a per-Y-block
	// picker-side qcoeff dump (between buildPredictedMacroblockCoefficients
	// FDCT and the per-block quantize on govpx; between macro_block_yrd's
	// FDCT and quantize on libvpx) for MB(0,0) frame 1's NEWMV candidate,
	// then localize the first divergent (block, scan_pos) to either coeff
	// (residual layer) or qcoeff (quantize layer).
	//
	// Task #314 corrected the chain (see feedback_vp8_arnr_investigation_chain
	// for the full retraction set):
	//
	//   - The #297 "SPLITMV 664 vs 116" histogram was measured against
	//     libvpx --threads=4 MT-LF state vs threads=1 govpx; libvpx's
	//     own MT-LF produces a different frame-0 recon than ST-LF, so
	//     the histogram is apples-to-oranges and DOES NOT prove a
	//     SPLITMV picker divergence.
	//   - The #298 NEWMV rate_y=34799 scoreboard was actually libvpx's
	//     ZEROMV rate_y under the same threads=4 confusion. Retracted.
	//   - The #304 "all-zero qcoeff means govpx is incorrect" framing is
	//     wrong: libvpx threads=1 also produces all-zero qcoeff for that
	//     MB. The picker quantize (#310) is byte-faithful.
	//   - The #300 SPLITMV bounds port is libvpx-verbatim and correct on
	//     its own merits but produces zero observed impact on this cohort.
	//
	// ACTUAL ROOT CAUSE (task #314): post-encode CHROMA trellis
	// optimize_b for blockType=2 / PLANE_TYPE_UV makes different
	// keep/drop decisions for ±1 chroma DC coefficients. Frame 1 of
	// 19981bff: 2241/3600 MBs diverge, 2115 chroma-only, 85% DC-only,
	// 1934 blocks govpx=+1 (keeps where libvpx drops), 1078 govpx=-1.
	// Code layers: govpx
	// quantizeEncodedBlockWithRDZbinAndActivity → optimizeQuantizedBlock
	// vs libvpx vp8/encoder/encodemb.c:vp8_encode_inter16x16 →
	// optimize_mb → optimize_b. The trellis core itself (#282) is
	// byte-faithful — divergence is in the chroma input to the trellis
	// or the chroma-specific dispatch gating. Task #316 bisects this.
	// Cleared-candidate list (#282 / #284 / #286 / #288 / #290 / #292 /
	// #294 / #299 / #303 / #307 / #309 / #310 / #312 / #313 / #319) —
	// do NOT re-investigate. See feedback_vp8_arnr_investigation_chain
	// for the full breakdown.
	//
	// Task #319 chroma rdMult/rdDiv audit (NEGATIVE result): the
	// orthogonal angle to #316's bisect re-verified that the
	// Viterbi-trellis RDCOST(rdmult, rddiv, rate, dist) inputs are
	// byte-identical between govpx and libvpx at the chroma optimize_b
	// boundary. vp8_chroma_rd_cost_parity_test.go pins:
	//   (a) libvpxRDConstantsWithZbin re-derives vp8_initialize_rd_consts
	//       byte-for-byte for every qIndex ∈ [4,56] (the cohort's
	//       MinQuantizer..MaxQuantizer band), including the `cpi->RDMULT
	//       > 1000 → RDMULT/=100, RDDIV=1` split.
	//   (b) blockPlaneRDMultiplier mirrors libvpx's plane_rd_mult[4] =
	//       {Y_NO_DC=4, Y2=16, UV=2, Y_WITH_DC=4} indexed by PLANE_TYPE,
	//       so the chroma trellis input rdmult = mb->rdmult * UV_RD_MULT
	//       = mb->rdmult * 2 matches libvpx encodemb.c:174.
	//   (c) tunedRDMultiplier mirrors vp8_activity_masking's per-MB lift
	//       `(rdMult * (2*act + avg) + (a>>1)) / (act + 2*avg)` exactly,
	//       including the saturated-identity case (act == avg) and the
	//       BestARNR cohort's activity_avg = vp8ActivityAvgAltFixed.
	// Combined with task #210's per-MB mb_activity / act_zbin_adj /
	// rdmult / activity_avg quartet match for every MB on frame 1, the
	// chroma trellis's rdMult/rdDiv VALUES at MB(0,0) and every other
	// chroma block are byte-identical to libvpx. The ±1 DC keep/drop
	// divergence is NOT explained by an rdMult/rdDiv input drift; the
	// remaining candidates are the per-coefficient (rate, distortion)
	// pair fed into RDCOST — i.e. either the token_costs[2][band][pt]
	// table that govpx's `coefficientTokenCost` reads (#316's bisect
	// covers this), the per-block dx = qcoeff*dequant - coeff distortion
	// computation, or the shortcut |x|*dq vs |coeff| boundary check
	// (encodemb.c:246-256 vs vp8_encoder_inter_quantize.go:259).
	//
	// Task #324 chroma optimize_b per-coefficient KEEP/DROP cost audit
	// (NEGATIVE — RETRACTS the cost-computation-bug framing):
	//
	//   Re-run of the #316 chroma_optimize_b bisect on the BestARNR
	//   19981bff cohort frame 1 captures 4720 shared
	//   (mb_row, mb_col, block) triples between govpx and libvpx. Of
	//   those:
	//     * 0 triples have IDENTICAL `coeff` (FDCT residual input) AND
	//       diverging post-trellis qcoeff.
	//     * 4720 triples have DIVERGING `coeff` input.
	//
	//   This rules out optimize_b's per-coefficient KEEP_COST /
	//   DROP_COST math (and by extension token_costs, dctValueBaseCost,
	//   shortcut boundary, RDTRUNC tie-break) as the source of the ±1
	//   DC keep/drop split. The cost computation is byte-faithful;
	//   what is not byte-faithful is the `coeff` (transform residual)
	//   FED INTO it. The actual ARNR pin-hold residual lives in the
	//   per-MB MODE PICKER: of 960 shared frame-1 MBs in the trace,
	//   588 (61.25%) have a diverging `mode` selection — e.g. govpx
	//   NEWMV vs libvpx SPLITMV at MB(0,0) — both with effective
	//   MV=(8,16)/LAST_FRAME but the NEWMV path uses
	//   vp8_build_inter16x16_predictors_mbuv while SPLITMV uses
	//   vp8_build_inter4x4_predictors_mbuv. The two chroma predictor
	//   paths use different MV-derivation rounding and different
	//   subpixel filter granularity, so chroma residuals drift even
	//   when the source/reference frames are byte-identical.
	//
	//   Pinned by TestVP8OptimizeQuantizedBlockRDCostBoundaries and
	//   TestVP8OptimizeQuantizedBlockStructuralInvariants
	//   (vp8_optimize_quantized_block_rd_test.go). Cleared-
	//   candidate list for the chroma optimize_b cost computation
	//   extended: #282 / #299 / #319 / #322 / #324.
	//
	// Task #332 (BestARNR PIN CLOSED — 6121 == 6121):
	//
	//   The residual -5 byte ARNR pin-hold has been closed by porting
	//   libvpx's vp8cx_encode_inter_macroblock dispatch gate verbatim.
	//   The govpx-side breakoutSkip gate at vp8_encoder_reconstruct.go:693
	//   was conflating libvpx's two real x->skip=1 sources with the
	//   picker's downstream mbmi.mb_skip_coeff signal:
	//
	//     Pre-fix:  breakoutSkip = !intra && (picker.MBSkipCoeff ||
	//                                          staticBreakout)
	//     Post-fix: breakoutSkip = !intra && (interMacroblockInactive ||
	//                                          staticBreakout)
	//
	//   libvpx vp8/encoder/rdopt.c evaluate_inter_mode_rd sets x->skip = 1
	//   in exactly two places: rdopt.c:1607-1608 (active_map_enabled &&
	//   active_ptr[0]==0) and rdopt.c:1620-1628 (static encode_breakout
	//   sse/var/uvsse triple-threshold). The picker's later
	//   mbmi.mb_skip_coeff signal (set from tteob==0 rate accounting in
	//   calculate_final_rd_costs at rdopt.c:1700) DOES NOT set x->skip
	//   and does NOT gate the encode-side rebuild. The encode-side
	//   vp8_encode_inter16x16 (encodeframe.c:1275-1281) always runs when
	//   x->skip == 0, regenerating coefficients via vp8_subtract_mb +
	//   transform_mb + vp8_quantize_mb (regular_quantize_b post-pick
	//   switch at encodeframe.c:1176-1178) + optimize_mb (trellis).
	//
	//   The picker may report tteob==0 — e.g. SPLITMV at MB(17,79) frame
	//   1 of the 19981bff cohort, where the picker's lastTTEOB tracker
	//   reflected the LAST-tested subblock mode rather than the chosen
	//   shape — but the encode-side regular-quantizer + trellis rebuild
	//   produces non-zero coefficients (block 7 eob=13 in the 19981bff
	//   cohort). govpx's pre-fix gate short-circuited that entire encode
	//   step via clearMacroblockCoefficients(coeffs[index]) → skip-coded
	//   bitstream — a divergence visible at threads=1 (-6 bytes), threads
	//   =4 (-5 bytes), threads=2 (no impact only because the specific MB
	//   distribution dodged the divergent SPLITMV).
	//
	//   Closes the BestARNR -5 byte ARNR pin-hold and the GoodARNR -6
	//   byte ARNR pin-hold byte-exactly across threads ∈ {1, 2, 4}. The
	//   correct cleared-candidate list (#282 / #299 / #319 / #322 /
	//   #324) WAS RIGHT — all those layers ARE byte-faithful, and the
	//   missing piece was the picker→encode dispatch gate, not any cost
	//   computation or trellis math. Validated by
	//   TestVP8ThreadsValidation across threads ∈ {1, 2, 4}.
	wantFrame0GovpxLen := 145534
	wantFrame0LibvpxLen := 145534
	wantFrame0GovpxFirstPart := 20463
	wantFrame0LibvpxFirstPart := 20463
	wantFrame1GovpxLen := 6121
	wantFrame1LibvpxLen := 6121
	wantFrame1GovpxFirstPart := 2264
	wantFrame1LibvpxFirstPart := 2264

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

	t.Logf("task #207 pinned: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("task #207 pinned: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}
