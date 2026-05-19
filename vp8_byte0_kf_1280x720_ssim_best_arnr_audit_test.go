//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestVP8Byte0KF1280x720SSIMBestARNRAudit pins task #207 / fuzz seed
// regression_option_grid_19981bff: a 1280x720 BestQuality / cpu=0 / VBR /
// screen-content=1 / threads=4 / TuneSSIM / ARNR=1/1/3 clip whose
// frame-0 keyframe and frame-1 inter both byte-diverge from libvpx at
// byte 0 / byte 1, mirroring (but in the OPPOSITE direction from) the
// task #198 GoodQuality cohort pinned in
// `vp8_byte0_kf_1280x720_ssim_audit_test.go`.
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
//	Serial` (encoder_reconstruct.go) shifts 94eb71d5 frame 0 from 20570 to
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
// Companion task-#198 audit (opposite-signed cohort):
//
//	vp8_byte0_kf_1280x720_ssim_audit_test.go (94eb71d5)
func TestVP8Byte0KF1280x720SSIMBestARNRAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #207 audit replay")
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
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task207-byte0-kf-1280x720-ssim-best-arnr-audit", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future regressions don't silently
	// re-interpret what this audit captured. Task #213 closed the
	// activity-probe recon divergence; task #236 then ported libvpx's
	// stale BLOCK->zbin_extra carry into the per-MB intra RD picker
	// (see encoder_reconstruct.go pickerActZbinAdj comment), which
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
	// (vp8_task210_mb_activity_tracer_test.go probe). All accepted-mode
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
	// `quantizeBlockWithZbinAndActivity` (encoder_inter_quantize.go) IS
	// equivalent to libvpx's `b->zbin_extra = (Y1dequant[Q][1] *
	// (zbin_over_quant + zbin_mode_boost + act_zbin_adj)) >> 7` at the
	// quantize-input boundary.
	//
	// Task #282 re-diagnosis: a verbatim audit of govpx's
	// optimizeQuantizedBlockWithRDConstants (encoder_inter_quantize.go)
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
	//       (encoder_inter_residuals.go:38-58) vs libvpx
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
	// divergence — divergence lives in scalar-side encoder logic
	// (candidate #2: picker-vs-accepted `act_zbin_adj` skew on the
	// inter-side, or #3: `interRDCacheReusable` UV-RD reuse).
	wantFrame0GovpxLen := 145534
	wantFrame0LibvpxLen := 145534
	wantFrame0GovpxFirstPart := 20463
	wantFrame0LibvpxFirstPart := 20463
	wantFrame1GovpxLen := 6116
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

	if got := decodeFirstPartitionSize(govpxFrames[0]); got != wantFrame0GovpxFirstPart {
		t.Fatalf("frame 0 govpx first_partition_size drift: got=%d want=%d", got, wantFrame0GovpxFirstPart)
	}
	if got := decodeFirstPartitionSize(libvpxFrames[0]); got != wantFrame0LibvpxFirstPart {
		t.Fatalf("frame 0 libvpx first_partition_size drift: got=%d want=%d", got, wantFrame0LibvpxFirstPart)
	}
	if got := decodeFirstPartitionSize(govpxFrames[1]); got != wantFrame1GovpxFirstPart {
		t.Fatalf("frame 1 govpx first_partition_size drift: got=%d want=%d", got, wantFrame1GovpxFirstPart)
	}
	if got := decodeFirstPartitionSize(libvpxFrames[1]); got != wantFrame1LibvpxFirstPart {
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
