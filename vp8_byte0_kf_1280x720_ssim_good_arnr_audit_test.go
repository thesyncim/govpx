//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestVP8Byte0KF1280x720SSIMGoodARNRAudit pins task #227 / fuzz seed
// regression_option_grid_788d442c: a 1280x720 GoodQuality / cpu=0 / VBR /
// screen-content=1 / threads=4 / TuneSSIM / ARNR=1/1/2 clip whose
// frame-0 keyframe and frame-1 inter both byte-diverge from libvpx with
// the SAME first-partition signature as the BestQuality companion
// 19981bff cohort pinned in vp8_byte0_kf_1280x720_ssim_best_arnr_audit_test.go.
//
// Cohort decode (seed bytes "A1", 0x41 = 65, 0x31 = 49;
// oracleRuntimeControlFuzzBytes wraps len(data)==2 so buckets alternate
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
//   - vp8_byte0_kf_1280x720_ssim_best_arnr_audit_test.go (companion 19981bff
//     audit; same frame-0 byte signature, BestQuality deadline)
//   - vp8_byte0_kf_1280x720_ssim_audit_test.go (companion 94eb71d5 audit;
//     task #213's closed cohort baseline)
//   - vp8_task210_mb_activity_tracer_test.go (per-MB activity-quartet tracer
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
func TestVP8Byte0KF1280x720SSIMGoodARNRAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #227 audit replay")
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
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task227-byte0-kf-1280x720-ssim-good-arnr-audit", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future fix-commits surface their effect
	// through this audit. Task #213 closed the companion 22f3d67c CBR cohort
	// byte-exactly; task #236 then ported libvpx's stale BLOCK->zbin_extra
	// carry into the per-MB intra RD picker (see encoder_reconstruct.go
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
	// `quantizeBlockWithZbinAndActivity` (encoder_inter_quantize.go) is
	// algebraically identical to libvpx's `b->zbin_extra` precomputed by
	// `vp8_update_zbin_extra` (vp8_quantize.c:410-428): both use
	// `(Y1dequant[Q][1] * (zbin_over_quant + zbin_mode_boost +
	// act_zbin_adj)) >> 7` with the FINAL picked mode's zbin_mode_boost
	// (MV_ZBIN_BOOST=4 for NEWMV/NEARESTMV/NEARMV cohorts here).
	//
	// Task #282 re-diagnosis: a verbatim audit of govpx's
	// optimizeQuantizedBlockWithRDConstants (encoder_inter_quantize.go)
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
	//   (3) Residual gather — gatherMacroblockUVResiduals4x4
	//       (encoder_inter_residuals.go:38-58) vs libvpx vp8_subtract_mbuv
	//       (encodemb.c:78-92).
	//
	// Task #284 charter: extend the oracle tracer with a pre-trellis UV
	// hook on both sides (govpx: between quantizeBlockWithZbinAndActivity
	// and optimizeQuantizedBlockWithRDConstants; libvpx: between
	// vp8_regular_quantize_b and optimize_b at encodemb.c:413-415) to
	// dump qcoeff / dqcoeff / eob for blocks 16-23 on frame 1 of seed
	// 19981bff, then bisect upstream layer by layer. The tracer
	// extension requires rotating both the libvpx oracle SHA pin
	// (oracleSHAvpxencArm64Darwin in internal/coracle/oracle_sha_test.go)
	// and the build_vpxenc_oracle.sh want_config string. The -5/-6 byte
	// delta is the steady-state cohort budget until that probe lands.
	wantFrame0GovpxLen := 145534
	wantFrame0LibvpxLen := 145534
	wantFrame0GovpxFirstPart := 20463
	wantFrame0LibvpxFirstPart := 20463
	wantFrame1GovpxLen := 6128
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

	t.Logf("task #227 pinned: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("task #227 pinned: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}

// TestVP8Byte0KF1280x720SSIMGoodCBRArnrClosed pins task #213's confirmation
// for fuzz seed regression_option_grid_22f3d67c: the same 1280x720 / Good /
// cpu=0 / threads=4 / token=1 / SSIM / sc=1 frame layout as the VBR variant
// pinned above, but with RateControl=CBR + ARNR=1/2/1 instead of VBR +
// ARNR=1/1/2. Task #213's activityProbeStaleActZbinAdj + per-attempt rdmult
// carry CLOSED this seed byte-exactly. Pinning keeps the regression detect-
// able if a future change re-opens the CBR side of the cohort while leaving
// the VBR variant (788d442c) unchanged.
//
// Cohort decode (seed bytes "A120"):
//
//   - Resolution: 1280x720
//   - Deadline:   GoodQuality
//   - CpuUsed:    0
//   - RateControl CBR
//   - Feature:    screen-content-mode=1
//   - TokenPart:  1
//   - Threads:    4
//   - Tuning:     TuneSSIM
//   - ARNR:       maxframes=1, strength=2, type=1
//
// Companion live regression:
//
//	testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_22f3d67c
func TestVP8Byte0KF1280x720SSIMGoodCBRArnrClosed(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #227 closed-cohort confirmation")
	}
	vpxencOracle := findVpxencOracle(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlCBR,
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
		ARNRStrength:      2,
		ARNRType:          1,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=cbr",
		"--screen-content-mode=1",
		"--token-parts=1",
		"--threads=4",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=2",
		"--arnr-type=1",
	})

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(1280, 720, i)
	}

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task227-byte0-kf-1280x720-ssim-good-cbr-arnr-closed", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Task #213 originally confirmed byte parity on this seed; task #236
	// then ported libvpx's stale BLOCK->zbin_extra carry into the per-MB
	// intra RD picker, which flipped a handful of MB picker decisions
	// on this CBR/ARNR=2/1 cohort, shifting govpx away from libvpx by 53
	// bytes on frame 0 and 63 bytes on frame 1. Task #254 tightened the
	// threaded keyframe stale-carry across rows; on THIS seed it dropped
	// frame 0 from +53 to +49 bytes (govpx 145545 vs libvpx 145496).
	//
	// Task #262 closes the residual divergence: libvpx
	// vp8/encoder/encodeframe.c line 427-438 calls
	// vp8cx_mb_init_quantizer(cpi, x, ok_to_skip=1) BEFORE the picker
	// on every MB whenever xd->segmentation_enabled is set. For CBR
	// (which enables cyclic_refresh_mode at onyx_if.c line 1857) the
	// KF cyclic_background_refresh call sets segmentation_enabled=1, so
	// the picker's block[i].zbin_extra is refreshed from THIS MB's
	// activity-driven x->act_zbin_adj via the vp8_quantize.c line 387-
	// 407 `else if (last_act_zbin_adj != act_zbin_adj)` branch. The
	// picker therefore quantizes with the current MB's zbin_extra, not
	// the stale prev-MB value the task #236 picker uses. After threading
	// segmentation.Enabled into the keyframe picker in
	// encoder_reconstruct.go / encoder_row_threaded.go, this seed
	// matches libvpx byte-for-byte on both frames again.
	wantFrame0GovpxLen := 145496
	wantFrame0LibvpxLen := 145496
	wantFrame0GovpxFirstPart := 20441
	wantFrame0LibvpxFirstPart := 20441
	wantFrame1GovpxLen := 6324
	wantFrame1LibvpxLen := 6324
	wantFrame1GovpxFirstPart := 2363
	wantFrame1LibvpxFirstPart := 2363

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

	t.Logf("task #262 closed: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("task #262 closed: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}
