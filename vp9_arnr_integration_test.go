package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func syntheticVP9PanningFirstPassStats(n int) []VP9FirstPassFrameStats {
	out := make([]VP9FirstPassFrameStats, n)
	for i := range n {
		out[i] = VP9FirstPassFrameStats{
			Frame:            uint64(i),
			Weight:           1.0,
			IntraError:       50000.0,
			CodedError:       10000.0 + float64(i)*100,
			SRCodedError:     11000.0 + float64(i)*120,
			FrameNoiseEnergy: 180.0,
			PcntInter:        0.9,
			PcntMotion:       0.4,
			PcntSecondRef:    0.1,
			PcntNeutral:      0.05,
			PcntIntraLow:     0.02,
			PcntIntraHigh:    0.03,
			IntraSkipPct:     0.02,
			MVr:              1.5,
			MVrAbs:           2.0,
			MVc:              0.5,
			MVcAbs:           1.0,
			MVInOutCount:     0.1,
			Duration:         1.0,
			Count:            1.0,
		}
	}
	return out
}

func TestVP9TwoPassGFGroupFeedsARNRBoost(t *testing.T) {
	stats := FinalizeVP9FirstPassStats(syntheticVP9PanningFirstPassStats(16))
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   600,
		TwoPassStats:        stats,
		LookaheadFrames:     8,
		MaxKeyframeInterval: 30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.twoPass.enabled() {
		t.Fatalf("two-pass not enabled despite stats provided")
	}
	e.refreshVP9GFGroupIfDue(true)
	if e.rc.gfuBoost == 0 {
		t.Fatalf("rc.gfuBoost still 0 after GF refresh")
	}
	if !e.twoPass.gfGroupActive {
		t.Fatalf("gfGroupActive=false after refresh")
	}
}

func TestVP9TwoPassARNRScratchAllocatedWithoutAutoAltRef(t *testing.T) {
	stats := FinalizeVP9FirstPassStats(syntheticVP9PanningFirstPassStats(8))
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		Deadline:           DeadlineGoodQuality,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		TwoPassStats:       stats,
		LookaheadFrames:    4,
		ARNRMaxFrames:      5,
		ARNRStrength:       3,
		ARNRType:           3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.opts.AutoAltRef {
		t.Fatal("test requires AutoAltRef=false")
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("two-pass ARF did not allocate ARNR scratch")
	}
}

func TestVP9SetTwoPassStatsAllocatesARNRScratch(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		Deadline:           DeadlineGoodQuality,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		LookaheadFrames:    4,
		ARNRMaxFrames:      5,
		ARNRStrength:       3,
		ARNRType:           3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) != 0 {
		t.Fatal("ARNR scratch allocated before two-pass stats are set")
	}
	stats := FinalizeVP9FirstPassStats(syntheticVP9PanningFirstPassStats(8))
	if err := e.SetTwoPassStats(stats); err != nil {
		t.Fatalf("SetTwoPassStats: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("SetTwoPassStats did not allocate two-pass ARNR scratch")
	}
}

func TestVP9SetARNRAllocatesTwoPassARNRScratch(t *testing.T) {
	stats := FinalizeVP9FirstPassStats(syntheticVP9PanningFirstPassStats(8))
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		Deadline:           DeadlineGoodQuality,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		TwoPassStats:       stats,
		LookaheadFrames:    4,
		ARNRMaxFrames:      1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) != 0 {
		t.Fatal("ARNR scratch allocated before ARNR is enabled")
	}
	if err := e.SetARNR(5, 3, 3); err != nil {
		t.Fatalf("SetARNR: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("SetARNR did not allocate two-pass ARNR scratch")
	}
}

func TestVP9ApplyARNRUsesAdaptiveStrengthWhenBoostSet(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		Deadline:        DeadlineGoodQuality,
		LookaheadFrames: 6,
		AutoAltRef:      true,
		ARNRMaxFrames:   5,
		ARNRStrength:    6,
		ARNRType:        3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for frame := range 6 {
		src := vp9test.NewYCbCr(width, height, uint8(96+frame*12), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead[%d]: %v", frame, err)
		}
	}
	e.rc.gfuBoost = 1500
	e.rc.avgFrameQIndexInter = 100
	e.rc.avgFrameQIndexKey = 90
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("applyVP9ARNRFilter returned false with adaptive boost set")
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("expected ARNR scratch to be populated")
	}
}

func TestVP9TwoPassARFUsesLookaheadIndexedARNRFilter(t *testing.T) {
	const width, height = 64, 64
	stats := FinalizeVP9FirstPassStats(syntheticVP9PanningFirstPassStats(8))
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		Deadline:           DeadlineGoodQuality,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       stats,
		LookaheadFrames:    4,
		ARNRMaxFrames:      5,
		ARNRStrength:       6,
		ARNRType:           3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	if _, err := e.encodeVP9FrameIntoWithFlagsResult(
		vp9test.NewYCbCr(width, height, 80, 128, 128), dst,
		EncodeForceKeyFrame, false, temporalFrame{LayerCount: 1},
		false); err != nil {
		t.Fatalf("key encode: %v", err)
	}

	e.twoPass.gfGroup = vp9enc.GFGroup{}
	e.twoPass.gfGroupActive = true
	e.twoPass.framesTillGFUpdate = 4
	e.twoPass.gfGroup.Index = 1
	e.twoPass.gfGroup.GFGroupSize = 3
	e.twoPass.gfGroup.UpdateType[1] = vp9enc.ARFUpdate
	e.twoPass.gfGroup.ArfSrcOffset[1] = 2
	e.twoPass.gfGroup.RFLevel[1] = vp9enc.RateFactorGFARFLow
	e.twoPass.gfGroup.GFUBoost[1] = 1500
	e.twoPass.gfGroup.BitAllocation[1] = 1400
	e.twoPass.gfGroup.ARNRStrengthAdjust = 0
	e.twoPass.framePrepared = false
	e.rc.gfuBoost = 1500
	e.rc.avgFrameQIndexInter = 120
	e.rc.avgFrameQIndexKey = 110

	for frame := range 4 {
		src := vp9test.NewPanningYCbCr(width, height, frame+1)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead[%d]: %v", frame, err)
		}
	}
	future, ok := e.peekVP9LookaheadAt(2)
	if !ok {
		t.Fatal("future ARF source missing from lookahead")
	}
	copyVP9LookaheadImage(&e.vp9ARNRScratch, &future.img, width, height)
	rawCenter := append([]byte(nil), future.img.Y...)

	hidden, ok, err := e.maybeEncodeVP9TwoPassARFInto(dst, false)
	if err != nil {
		t.Fatalf("maybeEncodeVP9TwoPassARFInto: %v", err)
	}
	if !ok {
		t.Fatal("two-pass ARF scheduler did not emit")
	}
	if hidden.ShowFrame || hidden.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("hidden ARF = show:%t refresh:%#x, want hidden ALTREF refresh",
			hidden.ShowFrame, hidden.RefreshFrameFlags)
	}
	if bytes.Equal(e.vp9ARNRScratch.Y, rawCenter) {
		t.Fatal("two-pass ARF left ARNR scratch equal to raw ARF source")
	}
	if !bytes.Equal(future.img.Y, rawCenter) {
		t.Fatal("two-pass ARNR mutated queued ARF source")
	}
}
