//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestVP8Byte0KF1280x720SSIMAudit pins task #198 / fuzz seed
// regression_option_grid_94eb71d5: a 1280x720 keyframe + 1 inter clip under
// good / cpu=0 / CBR / tune=SSIM / arnr=1/2/1 that still diverges from libvpx
// at byte 0 of frame 0 even after the task #183/#201 fix (rebuild
// SSIM activity_map per recode attempt) landed on origin/main.
//
// Cohort:
//   - Resolution 1280x720 (80 mb-cols × 45 mb-rows = 3600 MBs)
//   - Deadline = good, cpu_used = 0  ⇒  cpi->sf.RD != 0 + compressor_speed != 2
//     ⇒  KF + inter use the full rdopt.c picker
//   - RC = CBR, target = 700kbps
//   - Tuning = SSIM       ⇒ build_activity_map runs every vp8_encode_frame
//   - ARNR maxframes/strength/type = 1/2/1
//   - Threads = 0
//
// Bisection from origin/main (commit e348abbe / task #202):
//
//	Frame 0 (KF):
//	  govpx:  first_partition_size = 20570, total len = 125345
//	  libvpx: first_partition_size = 20575, total len = 125346
//	  first_byte_diff = 0 (byte 0 differs in bits 5-7 = low 3 bits of FPS)
//
//	Frame 1 (inter):
//	  govpx:  first_partition_size = 1146, total len = 4331
//	  libvpx: first_partition_size = 1151, total len = 4327
//	  first_byte_diff = 0 (byte 0 differs in bits 5-7)
//
// The 5-byte first-partition gap on the keyframe and the 5-byte gap on the
// inter frame both come down to ~handfuls of MB-level mode flips in the
// rdopt.c picker. Those flips trace back to per-MB act_zbin_adj /
// activity-tuned rdmult values that depend on the activity_map SSE produced
// by mb_activity_measure (encodeframe.c lines 95-111). At 1280x720, ~3600
// MBs accumulate enough activity-SSE drift that the picker quantize tips a
// handful of mode picks across libvpx's RDCOST boundaries even when the
// activity_map is rebuilt per recode (task #183 fix).
//
// What's been examined and ruled out as the trigger:
//
//	(1) The SSIM activity probe's `optimize` (trellis) flag for the FIRST
//	    activity probe attempt.
//	    libvpx encodemb.c:436-438 `vp8_optimize_mby` short-circuits when
//	    `xd->above_context == NULL`. The VP8_COMP struct is zero-init at
//	    allocation (onyx_if.c:1774 memset). The first encode_mb_row of the
//	    first encoded frame assigns above_context (encodeframe.c:357). So
//	    the first build_activity_map sees above_context == NULL and trellis
//	    is skipped. Subsequent activity probes (same-frame recodes, next
//	    frame's first attempt) see the non-NULL pointer carried over.
//
//	    govpx ports this via activityProbeMBContextSeeded (encoder.go) ⇒
//	    prepareTuningActivityMap (encoder_tuning.go) gates `optimize` on the
//	    flag; first probe runs with optimize=false. However, frame 0 of this
//	    seed RECODES, and the COMMITTED attempt's activity probe sees the
//	    flag already flipped to true (just like libvpx's recode attempts).
//	    So although the first attempt's recon byte-shifts on the 124 DC16
//	    edge MBs, the recoded attempt that actually emits to the bitstream
//	    picks up the same optimize=true state govpx already had pre-fix. Net
//	    byte parity gap from the optimize gate alone: zero for this seed.
//	    (Confirmed by forcing optimize=false unconditionally: govpx
//	    diverges harder, e.g. frame 0 first_part=20556 vs libvpx 20575,
//	    proving the trellis IS active in the committed-attempt path.)
//
//	(2) The KF intra picker's per-MB act_zbin_adj passing.
//	    Hypothesis: libvpx's vp8cx_encode_intra_macroblock runs
//	    vp8_rd_pick_intra_mode BEFORE adjust_act_zbin + vp8_update_zbin_extra
//	    (encodeframe.c:1099-1108). Without segmentation, block.zbin_extra
//	    retains the PREVIOUS MB's act_zbin_adj across the picker. govpx
//	    currently passes the CURRENT MB's tunedZbinAdjustment to the picker.
//	    Mirroring libvpx by carrying lastActZbinAdj across MBs (resets to 0
//	    at frame start) MOVED frame 0 first_part to 20583 (overshoots
//	    libvpx's 20575 by +8 bytes), demonstrating the picker is sensitive
//	    but the libvpx-faithful port lands on the OPPOSITE side of libvpx's
//	    target. Likely an inter-MB recon delta (from a different upstream
//	    differential) is producing govpx's activity_map values that, run
//	    through the libvpx-correct picker, picks different modes than
//	    libvpx does on its own activity_map.
//
// The remaining ≤5-byte first-partition gap therefore looks like residual
// per-MB activity SSE drift: govpx's activity_map at this resolution diverges
// from libvpx's by a handful of SSE units on enough MBs to tip ≤5 picker
// decisions per frame. Closing this needs a libvpx-instrumented oracle that
// emits per-MB mb_activity_measure return values so the bisection can
// localise the divergent MB(s) and the upstream recon delta feeding that MB.
// vpxenc-oracle does not emit those values today — the same blocker called
// out in the (now-closed) task #183 audit.
//
// Companion live regression:
//
//	testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_94eb71d5
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:95-111 mb_activity_measure
//     (ALT_ACT_MEASURE=1 path)
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:225-289 build_activity_map +
//     calc_av_activity (activity_avg = 100000 fixed under ALT_ACT_MEASURE)
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:293-313
//     vp8_activity_masking (per-MB x->rdmult scale + adjust_act_zbin)
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1094-1108
//     vp8cx_encode_intra_macroblock (picker BEFORE update_zbin_extra)
//   - libvpx v1.16.0 vp8/encoder/encodemb.c:436-438 vp8_optimize_mby
//     above_context==NULL short-circuit
//   - govpx encoder_tuning.go prepareTuningActivityMap + ssimActivityMeasure
//   - govpx encoder_reconstruct.go buildReconstructingKeyFrameCoefficientsWithSegmentationSerial
//     (keyframe intra picker call site)
func TestVP8Byte0KF1280x720SSIMAudit(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #198 audit replay")
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
		ARNRMaxFrames:     1,
		ARNRStrength:      2,
		ARNRType:          1,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=cbr",
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
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task198-byte0-kf-1280x720-ssim-audit", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future regressions don't silently
	// re-interpret what this audit captured. Task #213 originally
	// closed the activity-probe recon divergence on this seed; task
	// #236 then ported libvpx's stale BLOCK->zbin_extra carry into the
	// per-MB intra RD picker, which flipped a handful of MB picker
	// decisions on this CBR/ARNR=2/1 seed and shifted the recode
	// trajectory by a few bytes (govpx 125358 vs libvpx 125346).
	//
	// Task #262 closes the residual divergence: libvpx
	// vp8/encoder/encodeframe.c line 427-438 calls
	// vp8cx_mb_init_quantizer(cpi, x, ok_to_skip=1) BEFORE the picker
	// whenever xd->segmentation_enabled is set. For CBR (which enables
	// cyclic_refresh_mode at onyx_if.c line 1857 via the
	// (end_usage==USAGE_STREAM_FROM_SERVER && Mode<=2) clause) the
	// KF cyclic_background_refresh call (onyx_if.c line 3779) sets
	// xd->segmentation_enabled=1, so on every KF MB the
	// vp8cx_mb_init_quantizer call refreshes block[i].zbin_extra from
	// THIS MB's just-set x->act_zbin_adj (vp8_quantize.c line 387-407
	// `else if (last_act_zbin_adj != act_zbin_adj)` branch). The
	// picker then quantizes with the current MB's zbin_extra, NOT
	// the stale prev-MB value the task #236 picker uses.
	// encoder_reconstruct.go now honors that: when segmentation.Enabled
	// the picker actZbinAdj is this MB's tunedZbinAdjustment value.
	// Net result: byte-identical (125346/4327, sha matches).
	wantFrame0GovpxLen := 125346
	wantFrame0LibvpxLen := 125346
	wantFrame0GovpxFirstPart := 20575
	wantFrame0LibvpxFirstPart := 20575
	wantFrame1GovpxLen := 4327
	wantFrame1LibvpxLen := 4327
	wantFrame1GovpxFirstPart := 1151
	wantFrame1LibvpxFirstPart := 1151

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

	t.Logf("task #198 pinned: frame 0 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame0GovpxLen, wantFrame0LibvpxLen,
		wantFrame0GovpxFirstPart, wantFrame0LibvpxFirstPart,
		hex.EncodeToString(govpxSHA0[:8]), hex.EncodeToString(libvpxSHA0[:8]))
	t.Logf("task #198 pinned: frame 1 govpx_len=%d libvpx_len=%d "+
		"govpx_first_part=%d libvpx_first_part=%d "+
		"govpx_sha=%s libvpx_sha=%s",
		wantFrame1GovpxLen, wantFrame1LibvpxLen,
		wantFrame1GovpxFirstPart, wantFrame1LibvpxFirstPart,
		hex.EncodeToString(govpxSHA1[:8]), hex.EncodeToString(libvpxSHA1[:8]))
}

// decodeFirstPartitionSize extracts the 19-bit first_partition_size field
// from a VP8 frame tag (first 3 bytes, little-endian, bits 5-23).
func decodeFirstPartitionSize(frame []byte) int {
	if len(frame) < 3 {
		return -1
	}
	v := uint32(frame[0]) | uint32(frame[1])<<8 | uint32(frame[2])<<16
	return int((v >> 5) & 0x7FFFF)
}
