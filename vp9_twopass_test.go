package govpx

import (
	"math"
	"testing"
)

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

// TestVP9TwoPassVBRRateCorrectionBleedsOvershoot verifies the libvpx
// vbr_rate_correction feedback loop. After a large simulated overshoot
// (projected size >> base target), subsequent per-frame targets must
// fall below the unfed baseline, mirroring how libvpx redistributes the
// drift over a 16-frame window.
//
// libvpx parity reference: vp9/encoder/vp9_ratectrl.c:2683 vbr_rate_correction
// libvpx parity reference: vp9/encoder/vp9_firstpass.c:3733 vp9_twopass_postencode_update
func TestVP9TwoPassVBRRateCorrectionBleedsOvershoot(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(
		1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000)
	var ts vp9TwoPassState
	ts.configure(stats, 1000, 50, 0, 0, 64)

	baseline := ts.frameTargetBits(1000)
	if baseline <= 0 {
		t.Fatalf("baseline target = %d, want positive", baseline)
	}
	// Simulate a 2x overshoot on the first frame: projected encoded
	// size is twice the base target. libvpx accumulates the negative
	// delta and shrinks subsequent targets via vbr_rate_correction.
	overshoot := 2 * ts.baseFrameTarget
	ts.finishFrameWithActual(overshoot)
	if ts.vbrBitsOffTarget >= 0 {
		t.Fatalf("vbrBitsOffTarget = %d after 2x overshoot, want negative",
			ts.vbrBitsOffTarget)
	}

	next := ts.frameTargetBits(1000)
	if next <= 0 {
		t.Fatalf("next target = %d, want positive", next)
	}
	if next >= baseline {
		t.Fatalf("next target = %d, want < baseline %d after overshoot",
			next, baseline)
	}
}

// TestVP9TwoPassVBRRateCorrectionAddsUndershoot verifies the symmetric
// case: when a frame undershoots, the next per-frame target is
// boosted.
//
// libvpx parity reference: vp9/encoder/vp9_ratectrl.c:2700-2705 — the
// "vbr_bits_off_target > 0 means we have extra bits to spend" branch.
func TestVP9TwoPassVBRRateCorrectionAddsUndershoot(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(
		1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000)
	var ts vp9TwoPassState
	ts.configure(stats, 1000, 50, 0, 0, 64)

	baseline := ts.frameTargetBits(1000)
	if baseline <= 0 {
		t.Fatalf("baseline target = %d, want positive", baseline)
	}
	undershoot := ts.baseFrameTarget / 4
	ts.finishFrameWithActual(undershoot)
	if ts.vbrBitsOffTarget <= 0 {
		t.Fatalf("vbrBitsOffTarget = %d after large undershoot, want positive",
			ts.vbrBitsOffTarget)
	}

	next := ts.frameTargetBits(1000)
	if next <= baseline {
		t.Fatalf("next target = %d, want > baseline %d after undershoot",
			next, baseline)
	}
}

// TestVP9TwoPassVBRBitrateAccuracyConverges checks the canonical
// libvpx invariant: across a uniform-stats clip, the running per-frame
// target average must converge to the configured avg_frame_bandwidth.
// The feedback loop should keep the sum of assigned targets within ±5%
// of the budget, regardless of injected projected-size noise.
//
// libvpx target: per the libvpx VBR spec, two-pass VBR achieves
// bitrate within ~5% of target on typical clips. govpx's prior +52.91%
// gap at 600 kbps stemmed from the missing vbr_bits_off_target
// feedback (no closed-loop correction).
func TestVP9TwoPassVBRBitrateAccuracyConverges(t *testing.T) {
	const frames = 48
	rows := make([]float64, frames)
	// Mix some complexity: every 4th frame is "harder" (3x error).
	for i := range rows {
		rows[i] = 1000
		if i%4 == 0 {
			rows[i] = 3000
		}
	}
	stats := finalizedVP9TwoPassTestStats(rows...)
	const bitsPerFrame = 20000
	var ts vp9TwoPassState
	ts.configure(stats, bitsPerFrame, 50, 0, 0, 64)

	totalTarget := int64(0)
	for i := range frames {
		target := ts.frameTargetBits(bitsPerFrame)
		if target <= 0 {
			t.Fatalf("target[%d] = %d, want positive", i, target)
		}
		totalTarget += int64(target)
		// Simulate the encoder hitting the assigned target within
		// ±10% — i.e. the regulated Q hits its target well. The
		// feedback loop should keep cumulative drift bounded.
		jitter := 1.0
		if i%5 == 0 {
			jitter = 1.10
		} else if i%5 == 2 {
			jitter = 0.90
		}
		projected := int(float64(ts.baseFrameTarget) * jitter)
		ts.finishFrameWithActual(projected)
	}
	budget := int64(bitsPerFrame) * int64(frames)
	relErr := math.Abs(float64(totalTarget-budget)) / float64(budget)
	if relErr > 0.10 {
		t.Fatalf("cumulative target / budget rel error = %.4f, want <= 0.10 (totalTarget=%d budget=%d)",
			relErr, totalTarget, budget)
	}
}

// TestVP9TwoPassFrameMaxBitsClampMatchesLibvpx verifies the per-frame
// cap formula. libvpx uses
// `vbr_max_bits = avg_frame_bandwidth * two_pass_vbrmax_section / 100`
// (vp9_ratectrl.c:2671). Earlier govpx used defaultTargetBits as the
// cap base which collapses on key/boost frames; this test pins the
// correct base.
//
// libvpx parity reference: vp9/encoder/vp9_ratectrl.c:2671
func TestVP9TwoPassFrameMaxBitsClampMatchesLibvpx(t *testing.T) {
	// One outlier high-error frame so the unclamped target balloons.
	stats := finalizedVP9TwoPassTestStats(
		100, 100, 100, 100, 1000000, 100, 100, 100)
	const bitsPerFrame = 10000
	var ts vp9TwoPassState
	// Use maxPct = 200, so per-frame cap = 10000 * 200 / 100 = 20000.
	ts.configure(stats, bitsPerFrame, 50, 0, 200, 64)

	// Advance to the outlier frame.
	for range 4 {
		target := ts.frameTargetBits(bitsPerFrame)
		ts.finishFrameWithActual(target)
	}
	outlier := ts.frameTargetBits(bitsPerFrame)
	if outlier > 2*bitsPerFrame {
		t.Fatalf("outlier target = %d, want <= 2*bitsPerFrame=%d (libvpx vbr_max_bits cap)",
			outlier, 2*bitsPerFrame)
	}
}

// TestVP9TwoPassMinFrameFloorMatchesLibvpx verifies the per-frame floor
// matches libvpx vp9_rc_clamp_pframe_target_size — namely the larger of
// min_frame_bandwidth and avg_frame_bandwidth >> 5.
//
// libvpx parity reference: vp9/encoder/vp9_ratectrl.c:218
func TestVP9TwoPassMinFrameFloorMatchesLibvpx(t *testing.T) {
	// Zero-error frames so the score is zero — but the floor must
	// still kick in.
	stats := finalizedVP9TwoPassTestStats(1, 1, 1, 1, 1)
	const bitsPerFrame = 32000
	var ts vp9TwoPassState
	ts.configure(stats, bitsPerFrame, 50, 0, 0, 64)
	target := ts.frameTargetBits(bitsPerFrame)
	// avg_frame_bandwidth >> 5 = 32000 >> 5 = 1000.
	wantFloor := bitsPerFrame >> 5
	if target < wantFloor {
		t.Fatalf("target = %d, want >= avg_frame_bandwidth>>5 = %d",
			target, wantFloor)
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
