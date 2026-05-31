package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
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
