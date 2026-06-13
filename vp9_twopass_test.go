package govpx

import (
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
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
		src := vp9test.NewYCbCr(width, height, uint8(96+i*7), 128, 128)
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
		vp9test.NewYCbCr(width, height, 96, 128, 128), dst)
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
		vp9test.NewYCbCr(width, height, 104, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult[1]: %v", err)
	}
	if second.TwoPassFrameTargetBits != 0 {
		t.Fatalf("second two-pass target = %d, want disabled",
			second.TwoPassFrameTargetBits)
	}
}

func TestVP9EncoderFrameQIndexUsesTwoPassQuantizerBounds(t *testing.T) {
	const width, height = 64, 64
	macroblocks := vp9enc.MacroblockCount((height+7)>>3, (width+7)>>3)
	refreshFlags := uint8(1 << vp9GoldenRefSlot)

	manual := newVP9TwoPassQuantizerFixture(t)
	manual.prepareVP9SecondPassFrameTarget(false, refreshFlags)
	wantQ, wantBest, wantWorst, _ := manual.vp9TwoPassQuantizerWithBounds(
		false, 0, refreshFlags, macroblocks, nil, 0)
	oldQ, oldBest, oldWorst, _ := manual.rc.vbrQuantizerWithBounds(false,
		refreshFlags, manual.frameIndex, macroblocks, nil, 0)
	if wantQ == oldQ && wantBest == oldBest && wantWorst == oldWorst {
		t.Fatalf("fixture does not distinguish two-pass q path: q/bounds %d [%d,%d]",
			wantQ, wantBest, wantWorst)
	}

	enc := newVP9TwoPassQuantizerFixture(t)
	got := enc.vp9EncoderFrameQIndex(false, false, 0, refreshFlags, macroblocks)
	if got != wantQ {
		t.Fatalf("frame qindex = %d, want two-pass q %d (bounds [%d,%d], old one-pass q %d bounds [%d,%d])",
			got, wantQ, wantBest, wantWorst, oldQ, oldBest, oldWorst)
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
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
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

func TestVP9TwoPassKeyFrameZeroMotionPctMatchesLibvpxAccumulator(t *testing.T) {
	rows := []VP9FirstPassFrameStats{
		{
			Frame:        0,
			Weight:       1,
			IntraError:   100,
			CodedError:   50,
			SRCodedError: 50,
			PcntInter:    0.80,
			Duration:     1,
			Count:        1,
		},
		{
			Frame:        1,
			Weight:       1,
			IntraError:   100,
			CodedError:   50,
			SRCodedError: 50,
			PcntInter:    0.90,
			PcntMotion:   0.15,
			Duration:     1,
			Count:        1,
		},
	}
	var ts vp9TwoPassState
	ts.configure(FinalizeVP9FirstPassStats(rows), 1000, 50, 0, 0, 64)

	if got := ts.keyFrameZeroMotionPct(0, 2); got != 75 {
		t.Fatalf("zero-motion pct = %d, want 75", got)
	}
}

func TestVP9TwoPassGFGroupIndexAdvancesPostEncode(t *testing.T) {
	var ts vp9TwoPassState
	ts.configure(finalizedVP9TwoPassTestStats(100, 100, 100, 100),
		1000, 50, 0, 0, 64)
	ts.gfGroupActive = true
	ts.gfGroup.GFGroupSize = 3

	for want := uint8(1); want <= 2; want++ {
		ts.frameTargetBits(1000)
		ts.finishFrameWithActual(1000)
		if ts.gfGroup.Index != want {
			t.Fatalf("gf_group.index = %d, want %d after frame %d",
				ts.gfGroup.Index, want, want)
		}
	}
	ts.frameTargetBits(1000)
	ts.finishFrameWithActual(1000)
	if ts.gfGroup.Index != 2 {
		t.Fatalf("gf_group.index = %d after end of group, want capped at 2",
			ts.gfGroup.Index)
	}
}

func TestVP9RefreshGFGroupCarriesLastKeyFrameZeroMotionPct(t *testing.T) {
	rows := []VP9FirstPassFrameStats{
		{
			Frame:        0,
			Weight:       1,
			IntraError:   100,
			CodedError:   50,
			SRCodedError: 50,
			PcntInter:    0.80,
			Duration:     1,
			Count:        1,
		},
		{
			Frame:        1,
			Weight:       1,
			IntraError:   100,
			CodedError:   50,
			SRCodedError: 50,
			PcntInter:    0.90,
			PcntMotion:   0.15,
			Duration:     1,
			Count:        1,
		},
	}
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       FinalizeVP9FirstPassStats(rows),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	enc.twoPass.kfZeroMotionPct = 42

	enc.refreshVP9GFGroupIfDue(true)
	if enc.twoPass.lastKFGroupZeroMotionPct != 42 {
		t.Fatalf("last kf zero-motion pct = %d, want previous 42",
			enc.twoPass.lastKFGroupZeroMotionPct)
	}
	if enc.twoPass.kfZeroMotionPct != 75 {
		t.Fatalf("current kf zero-motion pct = %d, want 75",
			enc.twoPass.kfZeroMotionPct)
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

// TestVP9TwoPassCorpusVBRPinned pins the libvpx corpus-VBR consumer math
// against hand-computed expected values. The libvpx reference is
// vp9/encoder/vp9_firstpass.c:1647-1682 (vp9_init_second_pass corpus branch)
// + vp9/encoder/vp9_ratectrl.c:2734 (skip vbr_rate_correction when corpus is
// enabled).
//
// All four rows have coded_error = 100, weight = 1, active_area = 1 (no
// intra-skip / inactive-zone), so each row's modified_score is the same.
//
// Hand-computed steps for vbrCorpusComplexity != 0:
//
//	av_weight        = total.weight / total.count = 4 / 4 = 1.0
//	mean_mod_score   = vbrCorpusComplexity / 10.0           // corpus forced
//	av_err           = av_weight * mean_mod_score           // get_distribution_av_err
//	modified_score_i = av_err * pow(coded_err / av_err, bias/100)
//	                 * pow(active_area, ACT_AREA_CORRECTION)
//	normalized_i     = clamp(modified_score_i / mean_mod_score,
//	                          min_pct/100, max_pct/100)
//	bits_left        = bitsPerFrame * Nframes * (sum(normalized_i) / count)
//	frame_target     = bits_left * normalized_i / sum(normalized_i)
//	                 = bitsPerFrame * normalized_i
//
// With default bias=50, default min=0, default max=2000, identical rows:
//   - For coded_err = av_err: pow(1, 0.5) = 1, modified_score = av_err,
//     normalized = av_err / mean_mod_score = av_weight = 1.0.
//     Plug in: bits_left = 1000*4*1 = 4000, frame_target = 1000.
//   - For coded_err = 100 vs mean_mod_score = 10 (cc=100):
//     av_err = 10, modified_score = 10 * sqrt(100/10) = 10 * sqrt(10),
//     normalized = (10*sqrt(10)) / 10 = sqrt(10) ≈ 3.16227766.
//     bits_left = 1000*4*3.16227766 ≈ 12649.11, frame_target = bitsLeft *
//     score / total_score = 12649.11 / 4 = 3162.27 → int(3162).
func TestVP9TwoPassCorpusVBRPinned(t *testing.T) {
	// 4 frames, each with coded_error=100, weight=1, no intra-skip.
	stats := finalizedVP9TwoPassTestStats(100, 100, 100, 100)
	const bitsPerFrame = 1000
	const height = 64

	tests := []struct {
		name                    string
		vbrCorpusComplexity     int
		wantMeanModScore        float64
		wantNormalizedScoreLeft float64
		wantBitsLeft            int64
		wantFrameTarget         int
	}{
		{
			// Corpus disabled — the per-clip raw scan derives
			// mean_mod_score and bits_left stays at bitsPerFrame * N.
			// modified_score = av_err * sqrt(coded_err / av_err) =
			//   100 * sqrt(1) = 100. mean_mod_score = 100, normalized = 1.0.
			// bits_left = 1000 * 4 = 4000, frame_target = 1000.
			name:                    "disabled",
			vbrCorpusComplexity:     0,
			wantMeanModScore:        100.0,
			wantNormalizedScoreLeft: 4.0,
			wantBitsLeft:            4000,
			wantFrameTarget:         1000,
		},
		{
			// Corpus matches per-clip complexity (mean_mod_score=10).
			// av_err = 10. modified_score = 10 * sqrt(100/10) = 10*sqrt(10).
			// normalized = sqrt(10) ≈ 3.16227766. scale = 3.16227766.
			// bits_left = 4000 * 3.16227766 ≈ 12649, frame_target ≈ 3162.
			name:                    "complexity_100",
			vbrCorpusComplexity:     100,
			wantMeanModScore:        10.0,
			wantNormalizedScoreLeft: 4.0 * math.Sqrt(10),
			wantBitsLeft:            int64(4000 * math.Sqrt(10)),
			wantFrameTarget:         int(1000 * math.Sqrt(10)),
		},
		{
			// Corpus is 10x the per-clip complexity (mean_mod_score=100).
			// av_err = 100. modified_score = 100 * sqrt(100/100) = 100.
			// normalized = 1.0. scale = 1.0. bits_left = 4000.
			// frame_target = 1000.
			name:                    "complexity_1000",
			vbrCorpusComplexity:     1000,
			wantMeanModScore:        100.0,
			wantNormalizedScoreLeft: 4.0,
			wantBitsLeft:            4000,
			wantFrameTarget:         1000,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ts vp9TwoPassState
			ts.configureWithCorpus(stats, bitsPerFrame, 50, 0, 0, height,
				tc.vbrCorpusComplexity)
			if got := ts.vbrCorpusComplexity; got != tc.vbrCorpusComplexity {
				t.Errorf("vbrCorpusComplexity = %d, want %d",
					got, tc.vbrCorpusComplexity)
			}
			if !floatNear(ts.meanModScore, tc.wantMeanModScore, 1e-9) {
				t.Errorf("meanModScore = %v, want %v",
					ts.meanModScore, tc.wantMeanModScore)
			}
			if !floatNear(ts.normalizedScoreLeft, tc.wantNormalizedScoreLeft, 1e-9) {
				t.Errorf("normalizedScoreLeft = %v, want %v",
					ts.normalizedScoreLeft, tc.wantNormalizedScoreLeft)
			}
			if ts.bitsLeft != tc.wantBitsLeft {
				t.Errorf("bitsLeft = %d, want %d",
					ts.bitsLeft, tc.wantBitsLeft)
			}
			target := ts.frameTargetBits(bitsPerFrame)
			if target != tc.wantFrameTarget {
				t.Errorf("frameTargetBits = %d, want %d (libvpx-pinned)",
					target, tc.wantFrameTarget)
			}
		})
	}
}

// TestVP9TwoPassCorpusVBRSkipsRateCorrection pins the libvpx invariant from
// vp9/encoder/vp9_ratectrl.c:2734: vp9_set_target_rate skips
// vbr_rate_correction when oxcf->vbr_corpus_complexity is non-zero. Even a
// large simulated overshoot/undershoot must not perturb the next per-frame
// target.
func TestVP9TwoPassCorpusVBRSkipsRateCorrection(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 100, 100, 100)
	var ts vp9TwoPassState
	ts.configureWithCorpus(stats, 1000, 50, 0, 0, 64, 100)

	baseline := ts.frameTargetBits(1000)
	if baseline <= 0 {
		t.Fatalf("baseline = %d, want positive", baseline)
	}
	// A 4x overshoot would normally make vbr_rate_correction shrink the
	// next target. With corpus VBR, libvpx skips that correction.
	overshoot := 4 * ts.baseFrameTarget
	ts.finishFrameWithActual(overshoot)
	next := ts.frameTargetBits(1000)
	if next != baseline {
		t.Errorf("after overshoot, next = %d want %d (vbr_rate_correction must be skipped)",
			next, baseline)
	}
}

// TestVP9TwoPassCorpusVBRValidatesRange pins the libvpx range check from
// vp9/vp9_cx_iface.c:206 RANGE_CHECK(cfg, rc_2pass_vbr_corpus_complexity,
// 0, 10000).
func TestVP9TwoPassCorpusVBRValidatesRange(t *testing.T) {
	cases := []struct {
		name  string
		value int
		want  bool // true => accepted
	}{
		{"zero_ok", 0, true},
		{"min_inclusive", 1, true},
		{"max_inclusive", 10000, true},
		{"negative_rejected", -1, false},
		{"above_max_rejected", 10001, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := VP9EncoderOptions{
				Width:               64,
				Height:              64,
				FPS:                 30,
				VBRCorpusComplexity: tc.value,
			}
			err := validateVP9TwoPassOptions(opts)
			if tc.want && err != nil {
				t.Errorf("validate(%d) = %v, want nil", tc.value, err)
			}
			if !tc.want && err == nil {
				t.Errorf("validate(%d) = nil, want non-nil", tc.value)
			}
		})
	}
}

// TestVP9TwoPassCorpusVBRSpeedFeatureRecodeLoop pins the libvpx speed-feature
// fork from vp9/encoder/vp9_speed_features.c:321-324: at speed >= 2 the
// recode loop widens to ALLOW_RECODE_FIRST when oxcf->vbr_corpus_complexity
// is non-zero, else it stays at ALLOW_RECODE_KFARFGF.
func TestVP9TwoPassCorpusVBRSpeedFeatureRecodeLoop(t *testing.T) {
	cases := []struct {
		name                string
		vbrCorpusComplexity int
		wantRecodeLoop      RecodeLoopType
	}{
		{"corpus_disabled", 0, RecodeLoopAllowKfArfGf},
		{"corpus_enabled", 100, RecodeLoopAllowFirst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := NewVP9Encoder(VP9EncoderOptions{
				Width:               64,
				Height:              64,
				FPS:                 30,
				CpuUsed:             2,
				Deadline:            DeadlineGoodQuality,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
				TargetBitrateKbps:   600,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				TwoPassStats:        finalizedVP9TwoPassTestStats(100, 100, 100, 100),
				VBRCorpusComplexity: tc.vbrCorpusComplexity,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			ctx := enc.vp9DefaultSpeedFrameContext()
			enc.vp9ApplySpeedFeatures(ctx)
			if enc.sf.RecodeLoop != tc.wantRecodeLoop {
				t.Errorf("RecodeLoop = %v, want %v",
					enc.sf.RecodeLoop, tc.wantRecodeLoop)
			}
		})
	}
}

func floatNear(a, b, eps float64) bool {
	if a == b {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= eps
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

func newVP9TwoPassQuantizerFixture(t *testing.T) *VP9Encoder {
	t.Helper()
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  900,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats: finalizedVP9TwoPassTestStats(
			100, 25000, 10000, 100),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	enc.frameIndex = 1
	enc.twoPass.frameIndex = 1
	enc.rc.framesSinceKey = 2
	enc.rc.lastQKey = 120
	enc.rc.lastQInter = 180
	enc.rc.lastBoostedQIndex = 170
	enc.rc.avgFrameQIndexInter = 180
	enc.rc.gfuBoost = 300
	enc.rc.beginFrameWithRefresh(false, enc.frameIndex, 1<<vp9GoldenRefSlot)

	enc.twoPass.gfGroup = vp9enc.GFGroup{}
	enc.twoPass.gfGroupActive = true
	enc.twoPass.framesTillGFUpdate = 1
	enc.twoPass.gfGroup.RFLevel[0] = vp9enc.RateFactorGFARFStd
	enc.twoPass.gfGroup.GFUBoost[0] = 4000
	enc.twoPass.gfGroup.GFGroupSize = 2
	enc.twoPass.gfGroup.GFUBoostScalar = 4000
	enc.twoPass.gfGroup.ARFActiveBestQAdjustF = 1.0
	return enc
}
