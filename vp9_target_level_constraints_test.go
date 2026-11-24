package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9TargetLevelClampsEffectiveRateControl(t *testing.T) {
	e, err := NewVP9Encoder(vp9TargetLevelCBROptions(30))
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.rc.targetBitrateKbps != 30000 {
		t.Fatalf("target bitrate = %d, want requested 30000",
			e.rc.targetBitrateKbps)
	}
	if e.rc.effectiveBitrateKbps != 5760 {
		t.Fatalf("effective bitrate = %d, want level-3 80%% average 5760",
			e.rc.effectiveBitrateKbps)
	}
	if e.rc.bitsPerFrame != 192000 {
		t.Fatalf("bitsPerFrame = %d, want 192000", e.rc.bitsPerFrame)
	}
	if e.rc.overshootPct != 10 {
		t.Fatalf("overshootPct = %d, want level clamp 10", e.rc.overshootPct)
	}
	if e.rc.bufferSizeBits != 3456000 {
		t.Fatalf("bufferSizeBits = %d, want 3456000", e.rc.bufferSizeBits)
	}
}

func TestVP9SetTargetLevelRecomputesRateControl(t *testing.T) {
	opts := vp9TargetLevelCBROptions(255)
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.rc.effectiveBitrateKbps != 30000 || e.rc.overshootPct != defaultVP9RateControlOvershootPct {
		t.Fatalf("initial RC = %d kbps overshoot %d, want 30000/%d",
			e.rc.effectiveBitrateKbps, e.rc.overshootPct,
			defaultVP9RateControlOvershootPct)
	}
	if err := e.SetTargetLevel(30); err != nil {
		t.Fatalf("SetTargetLevel(30): %v", err)
	}
	if e.rc.effectiveBitrateKbps != 5760 || e.rc.overshootPct != 10 {
		t.Fatalf("level-30 RC = %d kbps overshoot %d, want 5760/10",
			e.rc.effectiveBitrateKbps, e.rc.overshootPct)
	}
	if err := e.SetTargetLevel(255); err != nil {
		t.Fatalf("SetTargetLevel(255): %v", err)
	}
	if e.rc.effectiveBitrateKbps != 30000 || e.rc.overshootPct != defaultVP9RateControlOvershootPct {
		t.Fatalf("unconstrained RC = %d kbps overshoot %d, want 30000/%d",
			e.rc.effectiveBitrateKbps, e.rc.overshootPct,
			defaultVP9RateControlOvershootPct)
	}
}

func TestVP9TargetLevelForcesWorstQuantizer(t *testing.T) {
	opts := vp9TargetLevelCBROptions(31)
	opts.MaxQuantizer = 20
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	want := encoder.PublicQuantizerToQIndex(encoder.MaxPublicQuantizer)
	if int(e.rc.worstQuality) != want {
		t.Fatalf("worstQuality = %d, want public q63 qindex %d",
			e.rc.worstQuality, want)
	}
}

func TestVP9TargetLevelRaisesGFIntervalFloor(t *testing.T) {
	opts := vp9TargetLevelCBROptions(41)
	opts.MinGFInterval = 4
	opts.MaxGFInterval = 4
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.rc.minGFInterval != 6 || e.rc.maxGFInterval != 6 {
		t.Fatalf("GF interval = min:%d max:%d, want target-level floor 6/6",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
	if e.opts.MinGFInterval != 4 || e.opts.MaxGFInterval != 4 {
		t.Fatalf("options GF interval = min:%d max:%d, want requested 4/4",
			e.opts.MinGFInterval, e.opts.MaxGFInterval)
	}
}

func TestVP9SetMinGFIntervalHonorsTargetLevelFloor(t *testing.T) {
	opts := vp9TargetLevelCBROptions(41)
	opts.MinGFInterval = 6
	opts.MaxGFInterval = 12
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if err := e.SetMinGFInterval(4); err != nil {
		t.Fatalf("SetMinGFInterval(4): %v", err)
	}
	if e.opts.MinGFInterval != 4 {
		t.Fatalf("opts.MinGFInterval = %d, want requested 4",
			e.opts.MinGFInterval)
	}
	if e.rc.minGFInterval != 6 || e.rc.maxGFInterval != 12 {
		t.Fatalf("effective GF interval = min:%d max:%d, want 6/12",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
}

func TestVP9TargetLevelClampsTileColumns(t *testing.T) {
	info := vp9EncoderTileInfoForTargetLevel(1024, 8192, 64, 8, 0, 30)
	if info.Log2TileCols != 2 {
		t.Fatalf("level-30 tile cols log2 = %d, want 2",
			info.Log2TileCols)
	}
	info = vp9EncoderTileInfoForTargetLevel(1024, 8192, 64, 8, 0, 10)
	if info.Log2TileCols != 1 {
		t.Fatalf("level-10 tile cols log2 = %d, want minimum legal 1",
			info.Log2TileCols)
	}
}

func TestVP9TargetLevelAutoUsesPictureLevelForGFAndTiles(t *testing.T) {
	opts := vp9TargetLevelCBROptions(VP9TargetLevelAuto)
	opts.Width = 2112
	opts.Height = 1088
	opts.MinGFInterval = 4
	opts.MaxGFInterval = 4
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.rc.minGFInterval != 6 || e.rc.maxGFInterval != 6 {
		t.Fatalf("auto GF interval = min:%d max:%d, want level-5 floor 6/6",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
	info := vp9EncoderTileInfoForTargetLevel(1024, 1920, 1080, 8, 0,
		VP9TargetLevelAuto)
	if info.Log2TileCols != 2 {
		t.Fatalf("auto tile cols log2 = %d, want level-4 cap 2",
			info.Log2TileCols)
	}
}

func TestVP9SetRealtimeTargetAutoTargetLevelRecomputesGFForResize(t *testing.T) {
	opts := vp9TargetLevelCBROptions(VP9TargetLevelAuto)
	opts.Width = 64
	opts.Height = 64
	opts.MinGFInterval = 4
	opts.MaxGFInterval = 4
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if e.rc.minGFInterval != 4 || e.rc.maxGFInterval != 4 {
		t.Fatalf("initial GF interval = min:%d max:%d, want 4/4",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 2112, Height: 1088}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.rc.minGFInterval != 6 || e.rc.maxGFInterval != 6 {
		t.Fatalf("resized GF interval = min:%d max:%d, want auto level-5 floor 6/6",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
}

func vp9TargetLevelCBROptions(level int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:               640,
		Height:              480,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   30000,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		TargetLevel:         level,
	}
}
