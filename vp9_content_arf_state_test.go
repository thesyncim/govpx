package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9CalcMiSizeMatchesLibvpx pins the libvpx calc_mi_size identity:
//
//	static INLINE int calc_mi_size(int len) {
//	  return len + MI_BLOCK_SIZE;
//	}
//
// libvpx: vp9/common/vp9_onyxc_int.h:416 calc_mi_size with MI_BLOCK_SIZE == 8.
func TestVP9CalcMiSizeMatchesLibvpx(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 8},
		{1, 9},
		{40, 48},
		{80, 88},
		{160, 168},
	}
	for _, c := range cases {
		if got := vp9CalcMiSize(c.in); got != c.want {
			t.Fatalf("vp9CalcMiSize(%d) = %d, want %d (libvpx: vp9_onyxc_int.h:416 len + MI_BLOCK_SIZE)",
				c.in, got, c.want)
		}
	}
}

// TestVP9MiDimensionsForFrameMatchesLibvpx pins the (mi_cols, mi_rows,
// mi_stride) triple libvpx computes in set_mb_mi (vp9_alloccommon.c:21-27):
//
//	*mi_cols   = (aligned_width  + MI_SIZE - 1) >> MI_SIZE_LOG2;
//	*mi_rows   = (aligned_height + MI_SIZE - 1) >> MI_SIZE_LOG2;
//	*mi_stride = calc_mi_size(*mi_cols);
//
// MI_SIZE == 8, MI_SIZE_LOG2 == 3. For a 1280x720 frame:
//
//	mi_cols   = (1280 + 7) >> 3 = 160
//	mi_rows   = (720  + 7) >> 3 = 90
//	mi_stride = 160 + 8         = 168
func TestVP9MiDimensionsForFrameMatchesLibvpx(t *testing.T) {
	cases := []struct {
		w, h                       int
		wantCols, wantRows, wantSt int
	}{
		{16, 16, 2, 2, 10},          // smallest realistic SB
		{320, 240, 40, 30, 48},      // small
		{640, 360, 80, 45, 88},      // VGA-class
		{1280, 720, 160, 90, 168},   // HD
		{1920, 1080, 240, 135, 248}, // FHD
	}
	for _, c := range cases {
		cols, rows, stride := vp9MiDimensionsForFrame(c.w, c.h)
		if cols != c.wantCols || rows != c.wantRows || stride != c.wantSt {
			t.Fatalf("vp9MiDimensionsForFrame(%d,%d) = (%d,%d,%d), want (%d,%d,%d)",
				c.w, c.h, cols, rows, stride,
				c.wantCols, c.wantRows, c.wantSt)
		}
	}
}

// TestVP9ContentStateSbFdSizeMatchesLibvpx pins the libvpx vpx_calloc
// expression at vp9_speed_features.c:680:
//
//	(cm->mi_stride >> 3) * ((cm->mi_rows >> 3) + 1) * sizeof(uint8_t)
//
// For 1280x720: mi_stride=168, mi_rows=90.
//
//	(168 >> 3) * ((90 >> 3) + 1) = 21 * (11+1) = 21 * 12 = 252.
func TestVP9ContentStateSbFdSizeMatchesLibvpx(t *testing.T) {
	cases := []struct {
		w, h     int
		miStride int
		miRows   int
		want     int
	}{
		{320, 240, 48, 30, 6 * 4},     // (48>>3) * ((30>>3)+1) = 6 * 4 = 24
		{640, 360, 88, 45, 11 * 6},    // 11 * 6 = 66
		{1280, 720, 168, 90, 21 * 12}, // 21 * 12 = 252
		{1920, 1080, 248, 135, 31 * 17},
	}
	for _, c := range cases {
		_, rows, stride := vp9MiDimensionsForFrame(c.w, c.h)
		if rows != c.miRows || stride != c.miStride {
			t.Fatalf("dim check %dx%d: mi_rows/mi_stride = (%d,%d), want (%d,%d)",
				c.w, c.h, rows, stride, c.miRows, c.miStride)
		}
		if got := vp9ContentStateSbFdSize(stride, rows); got != c.want {
			t.Fatalf("vp9ContentStateSbFdSize(%d,%d) = %d, want %d (libvpx: vp9_speed_features.c:680)",
				stride, rows, got, c.want)
		}
	}
}

// TestVP9SbOffsetForMiMatchesLibvpx pins the libvpx sboffset addressing used
// in vp9_encodeframe.c:5367 (count_arf_frame_usage write) and 1232
// (content_state_sb_fd update):
//
//	int sboffset = ((cm->mi_cols + 7) >> 3) * (mi_row >> 3) + (mi_col >> 3);
//
// mi_cols=160 (1280-wide frame), miRow=0 / miCol=0 -> 0;
// miRow=8 / miCol=0 -> 20; miRow=0 / miCol=8 -> 1; miRow=8 / miCol=16 -> 22.
func TestVP9SbOffsetForMiMatchesLibvpx(t *testing.T) {
	cases := []struct {
		miRow, miCol, miCols int
		want                 int
	}{
		{0, 0, 160, 0},
		{0, 8, 160, 1},
		{0, 16, 160, 2},
		{8, 0, 160, 20},
		{8, 8, 160, 21},
		{16, 16, 160, 42},
		// Non-multiple-of-8 mi_cols still rounds the column-stride up:
		// ((81 + 7) >> 3) == 11, so miRow=8 / miCol=0 -> 11.
		{8, 0, 81, 11},
	}
	for _, c := range cases {
		if got := vp9SbOffsetForMi(c.miRow, c.miCol, c.miCols); got != c.want {
			t.Fatalf("vp9SbOffsetForMi(miRow=%d, miCol=%d, miCols=%d) = %d, want %d (libvpx: vp9_encodeframe.c:5367)",
				c.miRow, c.miCol, c.miCols, got, c.want)
		}
	}
}

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
	wantSize := vp9ContentStateSbFdSize(168, 90)
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

// TestVP9UpdateContentStateSbFd pins libvpx's
// vp9_encodeframe.c:1238-1244 increment / reset semantics.
func TestVP9UpdateContentStateSbFd(t *testing.T) {
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
		t.Fatalf("contentStateSbFd not allocated")
	}

	// Low SAD repeated: increments, capped at 255.
	for range 300 {
		e.vp9UpdateContentStateSbFd(0, true)
	}
	if got := e.vp9ReadContentStateSbFd(0); got != 255 {
		t.Fatalf("after 300 low-SAD increments, contentStateSbFd[0] = %d, want 255 (libvpx: vp9_encodeframe.c:1240-1242 caps at 255)", got)
	}
	// High SAD: resets to 0.
	e.vp9UpdateContentStateSbFd(0, false)
	if got := e.vp9ReadContentStateSbFd(0); got != 0 {
		t.Fatalf("after high-SAD reset, contentStateSbFd[0] = %d, want 0 (libvpx: vp9_encodeframe.c:1243-1244)", got)
	}

	// Out-of-bounds writes are no-ops (libvpx aborts via memory error; govpx
	// returns silently to keep the helper safe from caller off-by-ones).
	e.vp9UpdateContentStateSbFd(-1, true)
	e.vp9UpdateContentStateSbFd(len(e.contentStateSbFd), true)
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

	src := newVP9YCbCrForTest(64, 64, 100, 128, 128)
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
	wantSize := vp9ContentStateSbFdSize(168, 90)
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

// TestVP9UpdateAltrefUsageMatchesLibvpx pins the libvpx update_altref_usage
// formula (vp9_ratectrl.c:1802-1819):
//
//	altref_count = 100.0 * arf_frame_usage / sum_ref_frame_usage
//	cpi->rc.perc_arf_usage = 0.75 * cpi->rc.perc_arf_usage + 0.25 * altref_count
//
// The gating clause requires
// alt_ref_gf_group && !is_src_frame_alt_ref &&
// !refresh_golden_frame && !refresh_alt_ref_frame.
func TestVP9UpdateAltrefUsageMatchesLibvpx(t *testing.T) {
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

	miCols, miRows, _ := vp9MiDimensionsForFrame(640, 360)
	// Populate the two slabs with deterministic values: half the SBs prefer
	// ARF, the rest prefer last-golden.
	for miRow := 0; miRow < miRows; miRow += vp9SbStateMiBlock {
		for miCol := 0; miCol < miCols; miCol += vp9SbStateMiBlock {
			off := vp9SbOffsetForMi(miRow, miCol, miCols)
			e.countArfFrameUsage[off] = 10
			e.countLastgoldenFrameUsage[off] = 30
		}
	}
	// arf:last_golden ratio = 10:30, so altref_count = 100 * 10 / 40 = 25.
	// percArfUsage starts at 0; new = 0.75*0 + 0.25*25 = 6.25.
	e.rc.percArfUsage = 0.0
	e.vp9UpdateAltrefUsage(true, false, false, false, miCols, miRows)
	if got := e.rc.percArfUsage; got < 6.24 || got > 6.26 {
		t.Fatalf("percArfUsage after first update = %g, want 6.25 (libvpx vp9_ratectrl.c:1814-1815)", got)
	}

	// Second update: percArfUsage = 0.75*6.25 + 0.25*25 = 4.6875 + 6.25 = 10.9375.
	e.vp9UpdateAltrefUsage(true, false, false, false, miCols, miRows)
	if got := e.rc.percArfUsage; got < 10.93 || got > 10.94 {
		t.Fatalf("percArfUsage after second update = %g, want 10.9375 (libvpx vp9_ratectrl.c:1814-1815)", got)
	}

	// Gating clause: is_src_frame_alt_ref true must skip the update.
	prev := e.rc.percArfUsage
	e.vp9UpdateAltrefUsage(true, true, false, false, miCols, miRows)
	if e.rc.percArfUsage != prev {
		t.Fatalf("percArfUsage = %g, want unchanged %g (libvpx gate: !is_src_frame_alt_ref)",
			e.rc.percArfUsage, prev)
	}
	// alt_ref_gf_group false: also skips.
	e.vp9UpdateAltrefUsage(false, false, false, false, miCols, miRows)
	if e.rc.percArfUsage != prev {
		t.Fatalf("percArfUsage = %g, want unchanged %g (libvpx gate: alt_ref_gf_group)",
			e.rc.percArfUsage, prev)
	}
	// refresh_golden_frame true: skips.
	e.vp9UpdateAltrefUsage(true, false, true, false, miCols, miRows)
	if e.rc.percArfUsage != prev {
		t.Fatalf("percArfUsage = %g, want unchanged %g (libvpx gate: !refresh_golden_frame)",
			e.rc.percArfUsage, prev)
	}
	// refresh_alt_ref_frame true: skips.
	e.vp9UpdateAltrefUsage(true, false, false, true, miCols, miRows)
	if e.rc.percArfUsage != prev {
		t.Fatalf("percArfUsage = %g, want unchanged %g (libvpx gate: !refresh_alt_ref_frame)",
			e.rc.percArfUsage, prev)
	}
}

// TestVP9UpdateAltrefUsageZeroDenominator pins libvpx vp9_ratectrl.c:1813:
//
//	if (sum_ref_frame_usage > 0) { ... }
//
// When every per-SB pair is zero, perc_arf_usage is left untouched.
func TestVP9UpdateAltrefUsageZeroDenominator(t *testing.T) {
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
	miCols, miRows, _ := vp9MiDimensionsForFrame(640, 360)
	e.rc.percArfUsage = 42.0
	e.vp9UpdateAltrefUsage(true, false, false, false, miCols, miRows)
	if e.rc.percArfUsage != 42.0 {
		t.Fatalf("percArfUsage = %g, want 42.0 unchanged (libvpx gate: sum_ref_frame_usage > 0)",
			e.rc.percArfUsage)
	}
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
