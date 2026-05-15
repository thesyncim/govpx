package govpx

import "testing"

func TestVP9TwoPassStateDistributesTargetsByStats(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 10000, 100, 10000)
	var ts vp9TwoPassState
	ts.configure(stats, 1000, 50, 0, 0, 64)

	target0 := ts.frameTargetBits(1000)
	ts.finishFrame()
	target1 := ts.frameTargetBits(1000)
	ts.finishFrame()
	target2 := ts.frameTargetBits(1000)
	ts.finishFrame()
	target3 := ts.frameTargetBits(1000)

	if target0 <= 0 || target1 <= 0 || target2 <= 0 || target3 <= 0 {
		t.Fatalf("two-pass targets = [%d %d %d %d], want positive",
			target0, target1, target2, target3)
	}
	if target1 <= target0 || target3 <= target2 {
		t.Fatalf("two-pass targets = [%d %d %d %d], want high-error rows boosted",
			target0, target1, target2, target3)
	}
}

func TestVP9EncoderConsumesTwoPassStats(t *testing.T) {
	const width, height = 64, 64
	stats := finalizedVP9TwoPassTestStats(100, 10000, 100, 10000)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       stats,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	dst := make([]byte, 1<<20)
	var results [4]VP9EncodeResult
	for i := range results {
		src := newVP9YCbCrForTest(width, height, uint8(96+i*7), 128, 128)
		results[i], err = enc.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", i, err)
		}
		if results[i].TwoPassFrameTargetBits <= 0 {
			t.Fatalf("frame %d two-pass target = %d, want positive",
				i, results[i].TwoPassFrameTargetBits)
		}
		if results[i].FrameTargetBits != results[i].TwoPassFrameTargetBits {
			t.Fatalf("frame %d target = %d, two-pass target = %d",
				i, results[i].FrameTargetBits,
				results[i].TwoPassFrameTargetBits)
		}
		if results[i].FirstPassStats.CodedError != stats[i].CodedError {
			t.Fatalf("frame %d first-pass coded = %.0f, want %.0f",
				i, results[i].FirstPassStats.CodedError,
				stats[i].CodedError)
		}
	}
	if results[1].TwoPassFrameTargetBits <= results[0].TwoPassFrameTargetBits {
		t.Fatalf("targets = frame0 %d frame1 %d, want frame1 boosted",
			results[0].TwoPassFrameTargetBits,
			results[1].TwoPassFrameTargetBits)
	}
}

func TestVP9SetTwoPassStatsCanEnableAndDisable(t *testing.T) {
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := enc.SetTwoPassStats(finalizedVP9TwoPassTestStats(100, 10000)); err != nil {
		t.Fatalf("SetTwoPassStats: %v", err)
	}
	dst := make([]byte, 1<<20)
	first, err := enc.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 96, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult[0]: %v", err)
	}
	if first.TwoPassFrameTargetBits <= 0 {
		t.Fatalf("first two-pass target = %d, want positive",
			first.TwoPassFrameTargetBits)
	}
	if err := enc.SetTwoPassStats(nil); err != nil {
		t.Fatalf("SetTwoPassStats(nil): %v", err)
	}
	second, err := enc.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 104, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult[1]: %v", err)
	}
	if second.TwoPassFrameTargetBits != 0 {
		t.Fatalf("second two-pass target = %d, want disabled",
			second.TwoPassFrameTargetBits)
	}
}

func TestVP9EncoderTwoPassSteadyStateAlloc(t *testing.T) {
	const width, height = 128, 128
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       finalizedVP9TwoPassTestStats(100, 10000, 100),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9YCbCrForTest(width, height, 96, 128, 128)
	dst := make([]byte, 1<<20)
	initialTwoPass := enc.twoPass
	if _, err := enc.EncodeInto(src, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
		enc.frameIndex = 0
		enc.twoPass = initialTwoPass
		n, err = enc.EncodeInto(src, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto two-pass: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto two-pass wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto two-pass steady state: got %v allocs/op, want 0",
			allocs)
	}
}

func finalizedVP9TwoPassTestStats(errors ...float64) []VP9FirstPassFrameStats {
	rows := make([]VP9FirstPassFrameStats, len(errors))
	for i, err := range errors {
		rows[i] = VP9FirstPassFrameStats{
			Frame:        uint64(i),
			Weight:       1,
			IntraError:   err * 2,
			CodedError:   err,
			SRCodedError: err,
			PcntInter:    0.9,
			Duration:     1,
			Count:        1,
		}
	}
	return FinalizeVP9FirstPassStats(rows)
}
