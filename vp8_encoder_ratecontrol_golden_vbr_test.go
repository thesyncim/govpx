package govpx

import (
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestEncodeIntoOnePassVBRDoesNotRefreshGoldenImmediatelyAfterKey(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 64*64*3)
	if _, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	state := packetState(t, inter.Data)
	if state.Refresh.RefreshGolden {
		t.Fatalf("frame-1 VBR refresh_golden = true, want false until libvpx DEFAULT_GF_INTERVAL countdown expires")
	}
	if e.rc.framesTillGFUpdateDue != libvpxDefaultGFInterval-2 {
		t.Fatalf("framesTillGFUpdateDue = %d, want %d after key decrement plus one inter frame",
			e.rc.framesTillGFUpdateDue, libvpxDefaultGFInterval-2)
	}
}

func TestEncodeIntoOnePassVBRRefreshesGoldenOnLibvpxIntervals(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 64*64*3)
	for frame := range 15 {
		result, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", frame, err)
		}
		if frame == 0 {
			continue
		}
		state := packetState(t, result.Data)
		switch frame {
		case libvpxDefaultGFInterval, 2 * libvpxDefaultGFInterval:
			if !state.Refresh.RefreshGolden {
				t.Fatalf("frame %d VBR refresh_golden = false, want true on libvpx GF interval", frame)
			}
			if state.Refresh.CopyBufferToAltRef != 2 {
				t.Fatalf("frame %d copy-to-alt = %d, want old-GF-to-ARF copy", frame, state.Refresh.CopyBufferToAltRef)
			}
		default:
			if state.Refresh.RefreshGolden {
				t.Fatalf("frame %d VBR refresh_golden = true, want false between default GF intervals", frame)
			}
		}
	}
}

func TestOnePassAutoGoldenPreservesStartupModeAcrossRuntimeReconfigure(t *testing.T) {
	newEncoder := func(t *testing.T, mode RateControlMode) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:             32,
			Height:            32,
			FPS:               30,
			RateControlMode:   mode,
			TargetBitrateKbps: 700,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			CQLevel:           20,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			KeyFrameInterval:  999,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		return e
	}
	forceOpportunity := func(e *VP8Encoder) (int, int) {
		rows := geometry.MacroblockRows(e.opts.Height)
		cols := geometry.MacroblockCols(e.opts.Width)
		e.rc.framesTillGFUpdateDue = 0
		e.rc.thisFramePercentIntra = 10
		e.rc.recentRefFrameUsageIntra = rows * cols / 4
		e.rc.recentRefFrameUsageLast = rows * cols
		e.rc.gfActiveCount = rows * cols
		return rows, cols
	}

	cbrStart := newEncoder(t, RateControlCBR)
	if err := cbrStart.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q) from CBR returned error: %v", err)
	}
	rows, cols := forceOpportunity(cbrStart)
	if cbrStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CBR-start runtime Q auto-golden refresh = true, want false")
	}

	cqStart := newEncoder(t, RateControlCQ)
	if err := cqStart.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(Q) from CQ returned error: %v", err)
	}
	rows, cols = forceOpportunity(cqStart)
	if !cqStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CQ-start runtime Q auto-golden refresh = false, want true")
	}
	if err := cqStart.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR) from CQ returned error: %v", err)
	}
	rows, cols = forceOpportunity(cqStart)
	if !cqStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("CQ-start runtime CBR auto-golden refresh = false, want true")
	}
	if got := cqStart.libvpxKeyFrameSetupGFInterval(rows, cols); got != 1 {
		t.Fatalf("CQ-start runtime CBR post-key GF interval = %d, want next-inter refresh", got)
	}

	qStart := newEncoder(t, RateControlQ)
	rows, cols = forceOpportunity(qStart)
	if !qStart.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("Q-start auto-golden refresh = false, want true")
	}
	if err := qStart.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR) from Q returned error: %v", err)
	}
	if got := qStart.libvpxKeyFrameSetupGFInterval(rows, cols); got != 1 {
		t.Fatalf("Q-start runtime CBR post-key GF interval = %d, want next-inter refresh", got)
	}
}

func TestOnePassAutoGoldenDisabledForTwoPassStartup(t *testing.T) {
	sources := []Image{
		encoderValidationPanningFrame(32, 32, 0),
		encoderValidationPanningFrame(32, 32, 1),
		encoderValidationPanningFrame(32, 32, 2),
	}
	stats := collectRuntimeControlFirstPassStats(t, EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		KeyFrameInterval:  999,
	}, sources)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		TwoPassStats:      stats,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	e.rc.framesTillGFUpdateDue = 0
	e.rc.thisFramePercentIntra = 10
	e.rc.recentRefFrameUsageIntra = rows * cols / 4
	e.rc.recentRefFrameUsageLast = rows * cols
	e.rc.gfActiveCount = rows * cols

	if e.shouldRefreshGoldenFrameOnePassNonCBR(false, false, 0, rows, cols) {
		t.Fatalf("two-pass startup auto-golden refresh = true, want false")
	}
}
