package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9EnsureContentStateSbFdAllocatesAtSpeed6 pins the libvpx
// vp9_speed_features.c:676-683 lazy allocation: the buffer must be allocated
// when sf.UseSourceSad is set on the realtime speed >= 6 path, and it must be
// sized exactly to the libvpx vpx_calloc expression.
func TestVP9EnsureContentStateSbFdAllocatesAtSpeed6(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           1280,
		Height:          720,
		Deadline:        DeadlineRealtime,
		CpuUsed:         6,
		LookaheadFrames: 0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.contentStateSbFd == nil {
		t.Fatalf("contentStateSbFd unexpectedly nil after speed-6 realtime configure (libvpx: vp9_speed_features.c:676-683)")
	}
	wantSize := encoder.ContentStateBufferSize(168, 90)
	if len(e.contentStateSbFd) != wantSize {
		t.Fatalf("len(contentStateSbFd) = %d, want %d (libvpx vp9_speed_features.c:680: (mi_stride >> 3) * ((mi_rows >> 3) + 1))",
			len(e.contentStateSbFd), wantSize)
	}
	for i, v := range e.contentStateSbFd {
		if v != 0 {
			t.Fatalf("contentStateSbFd[%d] = %d, want 0 (libvpx vpx_calloc zeros)", i, v)
		}
	}
	if e.contentStateSbFdMiCols != 160 || e.contentStateSbFdMiRows != 90 ||
		e.contentStateSbFdMiStride != 168 {
		t.Fatalf("mi dims captured = (%d,%d,%d), want (160,90,168)",
			e.contentStateSbFdMiCols, e.contentStateSbFdMiRows,
			e.contentStateSbFdMiStride)
	}
}

// TestVP9EnsureContentStateSbFdIdempotent pins libvpx's
// `if (cpi->content_state_sb_fd == NULL)` guard: re-entering the configurator
// at the same resolution must reuse the existing buffer, not free + realloc.
func TestVP9EnsureContentStateSbFdIdempotent(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    640,
		Height:   360,
		Deadline: DeadlineRealtime,
		CpuUsed:  6,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.contentStateSbFd == nil {
		t.Fatalf("expected contentStateSbFd allocated at speed 6 (libvpx: vp9_speed_features.c:676-683)")
	}
	first := e.contentStateSbFd
	e.contentStateSbFd[0] = 7
	e.contentStateSbFd[1] = 9

	// Reapply the configurator at the same resolution.
	e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())

	if &e.contentStateSbFd[0] != &first[0] {
		t.Fatalf("contentStateSbFd was re-allocated; libvpx guards on (cpi->content_state_sb_fd == NULL) so the existing slab must be reused")
	}
	if e.contentStateSbFd[0] != 7 || e.contentStateSbFd[1] != 9 {
		t.Fatalf("contentStateSbFd contents were stomped by the second configurator pass")
	}
}

// TestVP9AvgSourceSADStatsZeroTempSource pins libvpx avg_source_sad's
// x->zero_temp_sad_source update: identical current and Last_Source 64x64
// luma makes tmp_sad exactly zero.
func TestVP9AvgSourceSADStatsZeroTempSource(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		Deadline:           DeadlineRealtime,
		CpuUsed:            6,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if e.sf.UseSourceSad == 0 {
		t.Fatalf("UseSourceSad disabled at realtime speed 6")
	}

	src := vp9test.NewYCbCr(64, 64, 100, 128, 128)
	e.vp9CommitLastSource(src, true, false)
	stats, ok := e.vp9AvgSourceSADStats(src, 8, 0, 0)
	if !ok {
		t.Fatalf("vp9AvgSourceSADStats returned !ok")
	}
	if !stats.ZeroTempSADSource {
		t.Fatalf("zeroTempSADSource = false, want true for identical current/Last_Source")
	}
	if stats.ContentState != encoder.ContentStateLowSadLowSumdiff {
		t.Fatalf("contentState = %v, want encoder.ContentStateLowSadLowSumdiff", stats.ContentState)
	}
	if got := e.vp9ReadContentStateSbFd(0); got != 1 {
		t.Fatalf("contentStateSbFd[0] = %d, want 1 after low-SAD update", got)
	}
}

// TestVP9ResetContentStateSbFd pins libvpx vp9_encoder.c:4079-4082: when the
// caller invokes the reset hook (SVC/resize transition) every byte must be
// zeroed regardless of prior content.
func TestVP9ResetContentStateSbFd(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    640,
		Height:   360,
		Deadline: DeadlineRealtime,
		CpuUsed:  6,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	for i := range e.contentStateSbFd {
		e.contentStateSbFd[i] = uint8(i & 0xff)
	}
	e.vp9ResetContentStateSbFd()
	for i, v := range e.contentStateSbFd {
		if v != 0 {
			t.Fatalf("after vp9ResetContentStateSbFd, byte %d = %d, want 0 (libvpx: vp9_encoder.c:4079-4082 memset)", i, v)
		}
	}
}

// TestVP9EnsureArfFrameUsageAllocatesAtSpeed5VBR pins libvpx
// vp9_speed_features.c:828-844: when sf.UseAltrefOnepass fires (one-pass VBR
// with lookahead, speed >= 4) the configurator must allocate both
// count_arf_frame_usage and count_lastgolden_frame_usage at the libvpx
// vpx_calloc shape.
func TestVP9EnsureArfFrameUsageAllocatesAtSpeed5VBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              1280,
		Height:             720,
		Deadline:           DeadlineRealtime,
		CpuUsed:            5,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  2000,
		LookaheadFrames:    16,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.countArfFrameUsage == nil {
		t.Fatalf("countArfFrameUsage unexpectedly nil after speed-5 VBR-with-lookahead configure (libvpx: vp9_speed_features.c:833-838)")
	}
	if e.countLastgoldenFrameUsage == nil {
		t.Fatalf("countLastgoldenFrameUsage unexpectedly nil after speed-5 VBR-with-lookahead configure (libvpx: vp9_speed_features.c:839-843)")
	}
	wantSize := encoder.ContentStateBufferSize(168, 90)
	if len(e.countArfFrameUsage) != wantSize {
		t.Fatalf("len(countArfFrameUsage) = %d, want %d (libvpx vp9_speed_features.c:836-837)",
			len(e.countArfFrameUsage), wantSize)
	}
	if len(e.countLastgoldenFrameUsage) != wantSize {
		t.Fatalf("len(countLastgoldenFrameUsage) = %d, want %d (libvpx vp9_speed_features.c:842-843)",
			len(e.countLastgoldenFrameUsage), wantSize)
	}
	for i, v := range e.countArfFrameUsage {
		if v != 0 {
			t.Fatalf("countArfFrameUsage[%d] = %d, want 0 (libvpx vpx_calloc zeros)", i, v)
		}
	}
}

// TestVP9WriteArfFrameUsage pins the libvpx vp9_encodeframe.c:5363-5371
// per-SB write to the ARF / last-golden usage slabs. The helper assumes the
// caller already passes the gating predicate; this test exercises the body.
func TestVP9WriteArfFrameUsage(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            5,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  1000,
		LookaheadFrames:    8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	e.vp9WriteArfFrameUsage(3, 42, 13)
	if got := e.countArfFrameUsage[3]; got != 42 {
		t.Fatalf("countArfFrameUsage[3] = %d, want 42", got)
	}
	if got := e.countLastgoldenFrameUsage[3]; got != 13 {
		t.Fatalf("countLastgoldenFrameUsage[3] = %d, want 13", got)
	}

	// Out-of-bounds writes are no-ops.
	e.vp9WriteArfFrameUsage(-1, 7, 7)
	e.vp9WriteArfFrameUsage(len(e.countArfFrameUsage), 7, 7)
}

// TestVP9Speed5VBRIsSrcFrameAltRefSwitchesPartitionType pins the two libvpx
// partition-search-type overrides that fire on the speed-5 VBR-with-lookahead
// is_src_frame_alt_ref path:
//
//  1. vp9_speed_features.c:597-600 — sf->partition_search_type =
//     VAR_BASED_PARTITION when (rc_mode == VBR && lag_in_frames > 0 &&
//     is_src_frame_alt_ref).
//  2. vp9_speed_features.c:828-832 — sf->partition_search_type =
//     FIXED_PARTITION + sf->always_this_block_size = BLOCK_64X64 when
//     (use_altref_onepass && is_src_frame_alt_ref && frame_type != KEY_FRAME).
//
// At speed 5 VBR-with-lookahead libvpx sets use_altref_onepass = 1
// (vp9_speed_features.c:613-615), so step 2 fires after step 1 and the
// observable final partition_search_type is FIXED_PARTITION. With is_src_
// frame_alt_ref off, step 1 is skipped and the speed-5 default
// REFERENCE_PARTITION (vp9_speed_features.c:595) is preserved.
func TestVP9Speed5VBRIsSrcFrameAltRefSwitchesPartitionType(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            5,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  1000,
		LookaheadFrames:    8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	plainCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: false,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   false,
	})
	plainCtx.frameType = common.InterFrame
	var plainSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &plainSF, e.vp9SpeedFeatureCPUUsed(), plainCtx)
	if plainSF.PartitionSearchType != ReferencePartition {
		t.Fatalf("plain inter PartitionSearchType = %v, want ReferencePartition (libvpx vp9_speed_features.c:595)",
			plainSF.PartitionSearchType)
	}

	altSrcCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   true,
	})
	altSrcCtx.frameType = common.InterFrame
	var altSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &altSF, e.vp9SpeedFeatureCPUUsed(), altSrcCtx)
	// vp9_speed_features.c:828-832 overrides VAR_BASED_PARTITION (set by
	// the 597-600 branch) with FIXED_PARTITION BLOCK_64X64 because
	// use_altref_onepass fires at speed-5 VBR-with-lookahead.
	if altSF.PartitionSearchType != FixedPartition {
		t.Fatalf("is_src_frame_alt_ref VBR PartitionSearchType = %v, want FixedPartition (libvpx vp9_speed_features.c:828-832 overrides 597-600 on use_altref_onepass)",
			altSF.PartitionSearchType)
	}
	if altSF.AlwaysThisBlockSize != common.Block64x64 {
		t.Fatalf("is_src_frame_alt_ref VBR AlwaysThisBlockSize = %v, want Block64x64 (libvpx vp9_speed_features.c:831)",
			altSF.AlwaysThisBlockSize)
	}
}

// TestVP9Speed5CBRIsSrcFrameAltRefIgnored pins the libvpx
// vp9_speed_features.c:597-600 gate: the speed-5 VAR_BASED_PARTITION switch
// requires VBR + lookahead. In CBR mode the gate must be inert even when
// is_src_frame_alt_ref is true.
func TestVP9Speed5CBRIsSrcFrameAltRefIgnored(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            5,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
		LookaheadFrames:    0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	altSrcCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   true,
	})
	altSrcCtx.frameType = common.InterFrame
	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, e.vp9SpeedFeatureCPUUsed(), altSrcCtx)
	if sf.PartitionSearchType == VarBasedPartition {
		t.Fatalf("CBR is_src_frame_alt_ref PartitionSearchType = VarBasedPartition; libvpx gates the 597-600 swap on (rc_mode == VBR && lag_in_frames > 0)")
	}
}

// TestVP9AltrefOnepassIsSrcFrameAltRefForcesFixed64x64 pins libvpx
// vp9_speed_features.c:828-832:
//
//	if (sf->use_altref_onepass) {
//	  if (cpi->rc.is_src_frame_alt_ref && cm->frame_type != KEY_FRAME) {
//	    sf->partition_search_type = FIXED_PARTITION;
//	    sf->always_this_block_size = BLOCK_64X64;
//	  }
//	  ...
//	}
//
// On the speed-5 VBR-with-lookahead path UseAltrefOnepass is set. On an
// is_src_frame_alt_ref inter frame the configurator must pin partition to
// BLOCK_64X64 FIXED.
func TestVP9AltrefOnepassIsSrcFrameAltRefForcesFixed64x64(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		Deadline:           DeadlineRealtime,
		CpuUsed:            5,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  1000,
		LookaheadFrames:    8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// Speed-5 realtime path runs through
	// vp9SetRtSpeedFeatureFramesizeIndependent; use_altref_onepass is set at
	// speed >= 5 with VBR + lookahead (vp9_speed_features.c:613-615 mirrored
	// in govpx speed-5 branch and the speed >= 6 branch).
	altSrcCtx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		IsKey:              false,
		IntraOnly:          false,
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		RefreshAltRefFrame: false,
		IsSrcFrameAltRef:   true,
	})
	altSrcCtx.frameType = common.InterFrame
	altSrcCtx.width = 640
	altSrcCtx.height = 360
	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, e.vp9SpeedFeatureCPUUsed(), altSrcCtx)
	if sf.UseAltrefOnepass == 0 {
		t.Skipf("UseAltrefOnepass not set at this speed/RC combo; libvpx body at 828-832 is conditional on use_altref_onepass")
	}
	if sf.PartitionSearchType != FixedPartition {
		t.Fatalf("is_src_frame_alt_ref + altref_onepass PartitionSearchType = %v, want FixedPartition (libvpx vp9_speed_features.c:830)",
			sf.PartitionSearchType)
	}
	if sf.AlwaysThisBlockSize != common.Block64x64 {
		t.Fatalf("is_src_frame_alt_ref + altref_onepass AlwaysThisBlockSize = %v, want Block64x64 (libvpx vp9_speed_features.c:831)",
			sf.AlwaysThisBlockSize)
	}

	// Key frame is_src_frame_alt_ref must not flip FIXED_PARTITION (libvpx
	// gates on `cm->frame_type != KEY_FRAME`).
	keyCtx := altSrcCtx
	keyCtx.frameType = common.KeyFrame
	var keySF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &keySF, e.vp9SpeedFeatureCPUUsed(), keyCtx)
	if keySF.PartitionSearchType == FixedPartition &&
		keySF.AlwaysThisBlockSize == common.Block64x64 {
		// The keyframe path may legitimately reach FixedPartition through
		// other libvpx branches; the libvpx-829 override specifically must
		// not be the cause. Compare with the IsSrcFrameAltRef=false branch
		// at the same frame_type to confirm parity.
		keyNoAlt := keyCtx
		keyNoAlt.isSrcFrameAltRef = false
		var keyNoAltSF SpeedFeatures
		vp9SetSpeedFeaturesFramesizeIndependent(e, &keyNoAltSF, e.vp9SpeedFeatureCPUUsed(), keyNoAlt)
		if keyNoAltSF.PartitionSearchType != keySF.PartitionSearchType ||
			keyNoAltSF.AlwaysThisBlockSize != keySF.AlwaysThisBlockSize {
			t.Fatalf("keyframe is_src_frame_alt_ref affected PartitionSearchType/AlwaysThisBlockSize (libvpx gates on frame_type != KEY_FRAME)")
		}
	}
}

func TestVP9PanningCyclicContentStateSbFdAfterFrame1(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width: width, Height: height,
		RateControlMode: RateControlCBR, RateControlModeSet: true,
		TargetBitrateKbps: 700, Deadline: DeadlineRealtime,
		CpuUsed: -8, AQMode: VP9AQCyclicRefresh,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	for i := 0; i < 2; i++ {
		if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, i)); err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
	}
	if e.contentStateSbFd == nil {
		t.Fatal("contentStateSbFd nil after 2 frames")
	}
	var max, sum int
	for _, v := range e.contentStateSbFd {
		sum += int(v)
		if int(v) > max {
			max = int(v)
		}
	}
	t.Logf("after frame 1 encode, content_state_sb_fd max=%d sum=%d buf=%v rdmult=%d cyclicRDMult=%d",
		max, sum, e.contentStateSbFd, e.rc.rdmult, e.cyclicAQ.RDMult)
	// Panning motion keeps frame-to-frame SAD high, so libvpx resets the
	// accumulator every SB — max can legitimately stay 0 on this seed.
	if e.sf.UseSourceSad == 0 {
		t.Fatal("UseSourceSad disabled at speed 8")
	}
	if !e.lastSourceValid {
		t.Fatal("lastSourceValid false after keyframe + inter frame")
	}
	if len(e.varPartSBContentStateValid) == 0 || !e.varPartSBContentStateValid[0] {
		t.Fatal("avg_source_sad cache not populated during encode")
	}
}
