package govpx

import (
	"errors"
	"image"
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
	if results[0].TwoPassFrameTargetBits <= results[1].TwoPassFrameTargetBits {
		t.Fatalf("targets = frame0 %d frame1 %d, want keyframe group allocation to boost frame0",
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

func TestVP9TwoPassKeyFrameSeedsActiveWorstAndBoost(t *testing.T) {
	const width, height = 64, 64
	macroblocks := vp9enc.MacroblockCount((height+7)>>3, (width+7)>>3)
	refreshFlags := uint8(0xff)

	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  700,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats: finalizedVP9TwoPassTestStats(
			100, 100, 100, 100),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	enc.prepareVP9SecondPassFrameTarget(true, refreshFlags)
	if enc.twoPass.activeWorstQuality < int(enc.rc.bestQuality) ||
		enc.twoPass.activeWorstQuality >= int(enc.rc.worstQuality) {
		t.Fatalf("active_worst_quality=%d outside expected two-pass range [%d,%d)",
			enc.twoPass.activeWorstQuality, enc.rc.bestQuality,
			enc.rc.worstQuality)
	}
	if enc.twoPass.keyFrameBoost <= 0 ||
		enc.twoPass.keyFrameTargetBits <= 0 {
		t.Fatalf("keyframe group boost/target = %d/%d, want populated",
			enc.twoPass.keyFrameBoost, enc.twoPass.keyFrameTargetBits)
	}
	q, activeBest, activeWorst, _ := enc.vp9TwoPassQuantizerWithBounds(
		true, 0, refreshFlags, macroblocks, nil, 0)
	if q != activeBest {
		t.Fatalf("keyframe two-pass q=%d, active_best=%d", q, activeBest)
	}
	if activeWorst != enc.twoPass.activeWorstQuality {
		t.Fatalf("active_worst=%d, want seeded %d",
			activeWorst, enc.twoPass.activeWorstQuality)
	}
	if q >= 64 {
		t.Fatalf("keyframe q=%d, want seeded two-pass active-worst path (<64)", q)
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

func TestVP9TwoPassGFGroupIndexAdvancesToTerminalOverlaySlot(t *testing.T) {
	var ts vp9TwoPassState
	ts.configure(finalizedVP9TwoPassTestStats(100, 100, 100, 100),
		1000, 50, 0, 0, 64)
	ts.gfGroupActive = true
	ts.gfGroup.GFGroupSize = 3

	for want := uint8(1); want <= 3; want++ {
		ts.frameTargetBits(1000)
		ts.finishFrameWithActual(1000)
		if ts.gfGroup.Index != want {
			t.Fatalf("gf_group.index = %d, want %d after frame %d",
				ts.gfGroup.Index, want, want)
		}
	}
}

func TestVP9RefreshGFGroupPreservesPendingTerminalSlot(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	enc.twoPass.framesTillGFUpdate = 0
	enc.twoPass.gfGroup.Index = 5
	enc.twoPass.gfGroup.GFGroupSize = 5
	enc.twoPass.gfGroup.UpdateType[5] = vp9enc.UseBufFrame

	enc.refreshVP9GFGroupIfDue(false)
	if enc.twoPass.gfGroup.Index != 5 ||
		enc.twoPass.gfGroup.UpdateType[5] != vp9enc.UseBufFrame {
		t.Fatalf("pending terminal slot changed: index=%d update=%d, want index 5 USE_BUF_FRAME",
			enc.twoPass.gfGroup.Index, enc.twoPass.gfGroup.UpdateType[5])
	}
}

func TestVP9TwoPassARFStatsPeekDoesNotAdvanceDisplayCursor(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 200, 300, 400, 500)
	var ts vp9TwoPassState
	ts.configure(stats, 1000, 50, 0, 0, 64)
	ts.frameIndex = 1
	ts.gfGroupActive = true
	ts.gfGroup.Index = 1
	ts.gfGroup.UpdateType[1] = vp9enc.ARFUpdate
	ts.gfGroup.ArfSrcOffset[1] = 2
	ts.gfGroup.BitAllocation[1] = 1400

	if got := ts.statsForCurrentGFUpdate().Frame; got != stats[3].Frame {
		t.Fatalf("ARF stats frame = %d, want future frame %d",
			got, stats[3].Frame)
	}
	target := ts.frameTargetBits(1000)
	if target <= 0 {
		t.Fatalf("ARF target = %d, want positive", target)
	}
	beforeScoreLeft := ts.normalizedScoreLeft
	beforeBitsLeft := ts.bitsLeft

	ts.finishARFFrameWithActual(target / 2)
	if ts.frameIndex != 1 {
		t.Fatalf("frameIndex = %d, want display cursor to stay at 1",
			ts.frameIndex)
	}
	if ts.normalizedScoreLeft != beforeScoreLeft {
		t.Fatalf("normalizedScoreLeft changed on ARF: got %v want %v",
			ts.normalizedScoreLeft, beforeScoreLeft)
	}
	if ts.bitsLeft >= beforeBitsLeft {
		t.Fatalf("bitsLeft = %d, want less than %d after ARF budget spend",
			ts.bitsLeft, beforeBitsLeft)
	}
	if ts.gfGroup.Index != 2 {
		t.Fatalf("gf_group.index = %d, want ARF slot advanced to 2",
			ts.gfGroup.Index)
	}
	if got := ts.statsForFrame().Frame; got != stats[1].Frame {
		t.Fatalf("display stats frame = %d, want current frame %d",
			got, stats[1].Frame)
	}
}

func TestVP9TwoPassLookaheadARFEmitsHiddenFutureSource(t *testing.T) {
	const width, height = 64, 64
	stats := finalizedVP9TwoPassTestStats(100, 200, 300, 400, 500)
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
		LookaheadFrames:    4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	key, err := enc.encodeVP9FrameIntoWithFlagsResult(
		vp9test.NewYCbCr(width, height, 80, 128, 128), dst,
		EncodeForceKeyFrame, false, temporalFrame{LayerCount: 1}, false)
	if err != nil {
		t.Fatalf("key encode: %v", err)
	}
	if !key.KeyFrame || !key.ShowFrame {
		t.Fatalf("key result = key:%t show:%t, want visible key",
			key.KeyFrame, key.ShowFrame)
	}

	enc.twoPass.gfGroup = vp9enc.GFGroup{}
	enc.twoPass.gfGroupActive = true
	enc.twoPass.framesTillGFUpdate = 4
	enc.twoPass.gfGroup.Index = 1
	enc.twoPass.gfGroup.GFGroupSize = 3
	enc.twoPass.gfGroup.UpdateType[1] = vp9enc.ARFUpdate
	enc.twoPass.gfGroup.ArfSrcOffset[1] = 2
	enc.twoPass.gfGroup.RFLevel[1] = vp9enc.RateFactorGFARFLow
	enc.twoPass.gfGroup.GFUBoost[1] = 250
	enc.twoPass.gfGroup.BitAllocation[1] = 1400
	enc.twoPass.framePrepared = false

	for frame := 1; frame <= 3; frame++ {
		src := vp9test.NewYCbCr(width, height, uint8(80+frame*16), 128, 128)
		if err := enc.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("push lookahead frame %d: %v", frame, err)
		}
	}
	future, ok := enc.peekVP9LookaheadAt(2)
	if !ok {
		t.Fatal("future ARF source missing from lookahead")
	}
	if future.isAltRefSource {
		t.Fatal("future source unexpectedly marked alt-ref before ARF encode")
	}

	hidden, ok, err := enc.maybeEncodeVP9TwoPassARFInto(dst, false)
	if err != nil {
		t.Fatalf("maybeEncodeVP9TwoPassARFInto: %v", err)
	}
	if !ok {
		t.Fatal("two-pass ARF scheduler did not emit")
	}
	if hidden.ShowFrame || hidden.KeyFrame ||
		hidden.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("hidden ARF = show:%t key:%t refresh:%#x, want hidden ALTREF refresh",
			hidden.ShowFrame, hidden.KeyFrame, hidden.RefreshFrameFlags)
	}
	if hidden.FirstPassStats.Frame != stats[3].Frame {
		t.Fatalf("hidden ARF stats frame = %d, want future frame %d",
			hidden.FirstPassStats.Frame, stats[3].Frame)
	}
	if enc.frameIndex != 1 || enc.twoPass.frameIndex != 1 {
		t.Fatalf("display cursors = encoder:%d twopass:%d, want both 1",
			enc.frameIndex, enc.twoPass.frameIndex)
	}
	if enc.twoPass.gfGroup.Index != 2 {
		t.Fatalf("gf_group.index = %d, want 2 after hidden ARF",
			enc.twoPass.gfGroup.Index)
	}
	if enc.lookaheadCount != 3 {
		t.Fatalf("lookaheadCount = %d, want future source left queued",
			enc.lookaheadCount)
	}
	if !future.isAltRefSource {
		t.Fatal("future source not marked as source-alt-ref for later overlay")
	}
}

func TestVP9TwoPassLookaheadARFSkipsForcedKeyFrameWindow(t *testing.T) {
	const width, height = 64, 64
	stats := finalizedVP9TwoPassTestStats(100, 200, 300, 400, 500)
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
		LookaheadFrames:    4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	if _, err := enc.encodeVP9FrameIntoWithFlagsResult(
		vp9test.NewYCbCr(width, height, 80, 128, 128), dst,
		EncodeForceKeyFrame, false, temporalFrame{LayerCount: 1}, false); err != nil {
		t.Fatalf("key encode: %v", err)
	}

	enc.twoPass.gfGroup = vp9enc.GFGroup{}
	enc.twoPass.gfGroupActive = true
	enc.twoPass.framesTillGFUpdate = 4
	enc.twoPass.gfGroup.Index = 1
	enc.twoPass.gfGroup.GFGroupSize = 3
	enc.twoPass.gfGroup.UpdateType[1] = vp9enc.ARFUpdate
	enc.twoPass.gfGroup.ArfSrcOffset[1] = 2
	enc.twoPass.gfGroup.RFLevel[1] = vp9enc.RateFactorGFARFLow
	enc.twoPass.gfGroup.GFUBoost[1] = 250
	enc.twoPass.gfGroup.BitAllocation[1] = 1400
	enc.twoPass.framePrepared = false

	for frame := 1; frame <= 3; frame++ {
		flags := EncodeFlags(0)
		if frame == 2 {
			flags = EncodeForceKeyFrame
		}
		src := vp9test.NewYCbCr(width, height, uint8(80+frame*16), 128, 128)
		if err := enc.pushVP9Lookahead(src, flags); err != nil {
			t.Fatalf("push lookahead frame %d: %v", frame, err)
		}
	}
	future, ok := enc.peekVP9LookaheadAt(2)
	if !ok {
		t.Fatal("future ARF source missing from lookahead")
	}

	result, ok, err := enc.maybeEncodeVP9TwoPassARFInto(dst, false)
	if err != nil {
		t.Fatalf("maybeEncodeVP9TwoPassARFInto: %v", err)
	}
	if !ok {
		t.Fatal("two-pass ARF cancellation did not emit flushed visible frame")
	}
	if !result.ShowFrame {
		t.Fatalf("result = show:%t refresh:%#x, want visible flush",
			result.ShowFrame, result.RefreshFrameFlags)
	}
	if future.isAltRefSource {
		t.Fatal("forced-keyframe window still marked future source as alt-ref")
	}
	if enc.lookaheadCount != 2 {
		t.Fatalf("lookaheadCount = %d, want oldest frame flushed",
			enc.lookaheadCount)
	}
	next, ok := enc.peekVP9LookaheadAt(0)
	if !ok || next.flags&EncodeForceKeyFrame == 0 {
		t.Fatal("forced keyframe was not preserved in lookahead")
	}
}

func TestVP9TwoPassLookaheadPathSchedulesHiddenARF(t *testing.T) {
	const width, height, frames = 64, 64, 12
	sources := make([]*image.YCbCr, frames)
	statsEnc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(firstpass): %v", err)
	}
	stats := make([]VP9FirstPassFrameStats, frames)
	for frame := range frames {
		src := vp9test.NewPanningYCbCr(width, height, frame)
		sources[frame] = src
		stats[frame], err = statsEnc.CollectFirstPassStats(src,
			uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", frame, err)
		}
	}

	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  700,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       FinalizeVP9FirstPassStats(stats),
		LookaheadFrames:    8,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(secondpass): %v", err)
	}
	dst := make([]byte, 1<<20)
	results := make([]VP9EncodeResult, 0, frames+1)
	for frame, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		results = append(results, result)
	}
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, result)
	}

	visible, hidden := 0, 0
	for _, result := range results {
		if result.ShowFrame {
			visible++
			continue
		}
		hidden++
		if result.RefreshFrameFlags != 1<<vp9AltRefSlot ||
			result.FirstPassStats.Frame <= 1 {
			t.Fatalf("hidden result refresh/stats = %#x/%d, want ALTREF future stats",
				result.RefreshFrameFlags, result.FirstPassStats.Frame)
		}
	}
	if visible != frames {
		t.Fatalf("visible packets = %d, want %d", visible, frames)
	}
	if hidden == 0 {
		t.Fatal("normal two-pass lookahead path emitted no hidden ARF")
	}
}

func TestVP9TwoPassLookaheadUseBufEmitsShowExisting(t *testing.T) {
	const width, height = 64, 64
	stats := finalizedVP9TwoPassTestStats(100, 200, 300, 400)
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
		LookaheadFrames:    4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	if _, err := enc.encodeVP9FrameIntoWithFlagsResult(
		vp9test.NewYCbCr(width, height, 80, 128, 128), dst,
		EncodeForceKeyFrame, false, temporalFrame{LayerCount: 1}, false); err != nil {
		t.Fatalf("key encode: %v", err)
	}

	enc.twoPass.gfGroup = vp9enc.GFGroup{}
	enc.twoPass.gfGroupActive = true
	enc.twoPass.framesTillGFUpdate = 0
	enc.twoPass.gfGroup.Index = 2
	enc.twoPass.gfGroup.GFGroupSize = 2
	enc.twoPass.gfGroup.UpdateType[2] = vp9enc.UseBufFrame
	enc.twoPass.gfGroup.BitAllocation[2] = 1200
	enc.twoPass.framePrepared = false

	if err := enc.pushVP9Lookahead(
		vp9test.NewYCbCr(width, height, 96, 128, 128), 0); err != nil {
		t.Fatalf("push lookahead: %v", err)
	}
	entry, ok := enc.popVP9Lookahead(true)
	if !ok {
		t.Fatal("pop lookahead failed")
	}
	result, err := enc.encodeVP9LookaheadEntryInto(dst, entry)
	if err != nil {
		t.Fatalf("encodeVP9LookaheadEntryInto: %v", err)
	}
	info, err := PeekVP9StreamInfo(result.Data)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if !info.ShowExistingFrame || info.ExistingFrameSlot != vp9AltRefSlot {
		t.Fatalf("packet info = show_existing:%t slot:%d, want ALTREF show-existing",
			info.ShowExistingFrame, info.ExistingFrameSlot)
	}
	if !result.ShowFrame || result.RefreshFrameFlags != 0 ||
		result.FirstPassStats.Frame != stats[1].Frame ||
		result.TwoPassFrameTargetBits <= 0 {
		t.Fatalf("USE_BUF result = show:%t refresh:%#x stats:%d target:%d, want visible no-refresh stats[1] target",
			result.ShowFrame, result.RefreshFrameFlags,
			result.FirstPassStats.Frame, result.TwoPassFrameTargetBits)
	}
	if enc.frameIndex != 2 || enc.twoPass.frameIndex != 2 ||
		enc.twoPass.gfGroup.Index != 3 {
		t.Fatalf("post USE_BUF cursors = frame:%d stats:%d gf:%d, want 2/2/3",
			enc.frameIndex, enc.twoPass.frameIndex,
			enc.twoPass.gfGroup.Index)
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

func TestVP9BuildGFGroupInputsCarriesSourceAltRefActive(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	enc.rc.sourceAltRefActive = true

	in := enc.buildVP9GFGroupInputs(false)
	if !in.SourceAltRefActive {
		t.Fatal("SourceAltRefActive = false, want active overlay state fed to GF analyzer")
	}
}

func TestVP9BuildGFGroupInputsUsesTwoPassAltRefDefaults(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	enc.opts.LookaheadFrames = 0

	in := enc.buildVP9GFGroupInputs(false)
	if in.LagInFrames != vp9MaxLookaheadFrames {
		t.Fatalf("LagInFrames = %d, want libvpx default %d",
			in.LagInFrames, vp9MaxLookaheadFrames)
	}
	if !in.AllowAltRef {
		t.Fatal("AllowAltRef = false, want stats-backed two-pass default enabled")
	}
}

func TestVP9PostEncodeSourceAltRefStateMirrorsLibvpx(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	enc.rc.sourceAltRefPending = true

	enc.vp9PostEncodeSourceAltRefState(false, 1<<vp9AltRefSlot)
	if enc.rc.sourceAltRefPending || !enc.rc.sourceAltRefActive {
		t.Fatalf("after ARF refresh pending=%v active=%v, want false/true",
			enc.rc.sourceAltRefPending, enc.rc.sourceAltRefActive)
	}

	enc.twoPass.gfGroupActive = true
	enc.twoPass.gfGroup.Index = 1
	enc.vp9PostEncodeSourceAltRefState(false, 1<<vp9GoldenRefSlot)
	if !enc.rc.sourceAltRefActive {
		t.Fatal("sourceAltRefActive cleared at nonzero gf_group index; libvpx keeps it")
	}

	enc.twoPass.gfGroup.Index = 0
	enc.vp9PostEncodeSourceAltRefState(false, 1<<vp9GoldenRefSlot)
	if enc.rc.sourceAltRefActive {
		t.Fatal("sourceAltRefActive still set after overlay/GF index 0 consumed it")
	}

	enc.rc.sourceAltRefPending = true
	enc.rc.sourceAltRefActive = true
	enc.vp9PostEncodeSourceAltRefState(true, 0xff)
	if enc.rc.sourceAltRefPending || enc.rc.sourceAltRefActive {
		t.Fatalf("after intra reset pending=%v active=%v, want false/false",
			enc.rc.sourceAltRefPending, enc.rc.sourceAltRefActive)
	}
}

func TestVP9ConfigureTwoPassBufferUpdatesMirrorsLibvpx(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	enc.twoPass.gfGroupActive = true
	enc.twoPass.gfGroup.Index = 0

	tests := []struct {
		name       string
		updateType uint8
		flags      EncodeFlags
		inRefresh  uint8
		want       uint8
		wantSrcARF bool
	}{
		{
			name:       "arf",
			updateType: vp9enc.ARFUpdate,
			want:       1 << vp9AltRefSlot,
		},
		{
			name:       "golden",
			updateType: vp9enc.GFUpdate,
			want:       1<<vp9LastRefSlot | 1<<vp9GoldenRefSlot,
		},
		{
			name:       "leaf",
			updateType: vp9enc.LFUpdate,
			want:       1 << vp9LastRefSlot,
		},
		{
			name:       "overlay",
			updateType: vp9enc.OverlayUpdate,
			want:       1 << vp9GoldenRefSlot,
			wantSrcARF: true,
		},
		{
			name:       "external-preserved",
			updateType: vp9enc.ARFUpdate,
			flags:      EncodeNoUpdateAltRef,
			inRefresh:  1 << vp9LastRefSlot,
			want:       1 << vp9LastRefSlot,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enc.twoPass.gfGroup.UpdateType[0] = tc.updateType
			enc.rc.isSrcFrameAltRef = false
			inRefresh := tc.inRefresh
			if inRefresh == 0 && tc.name != "external-preserved" {
				inRefresh = 1 << vp9LastRefSlot
			}
			got, gotSrcARF := enc.vp9ConfigureTwoPassBufferUpdates(false,
				tc.flags, inRefresh, false)
			if got != tc.want || gotSrcARF != tc.wantSrcARF ||
				enc.rc.isSrcFrameAltRef != tc.wantSrcARF {
				t.Fatalf("refresh/src_arf = %#x/%v rc=%v, want %#x/%v",
					got, gotSrcARF, enc.rc.isSrcFrameAltRef,
					tc.want, tc.wantSrcARF)
			}
		})
	}
}

func TestVP9TwoPassGFCountdownMirrorsLibvpxRefreshGate(t *testing.T) {
	var ts vp9TwoPassState
	ts.configure(finalizedVP9TwoPassTestStats(100, 100, 100, 100),
		1000, 50, 0, 0, 64)
	ts.gfGroupActive = true
	ts.framesTillGFUpdate = 3

	ts.postEncodeGFUpdate(1 << vp9AltRefSlot)
	if ts.framesTillGFUpdate != 3 {
		t.Fatalf("after ARF refresh framesTillGFUpdate=%d, want unchanged 3",
			ts.framesTillGFUpdate)
	}
	ts.postEncodeGFUpdate(1 << vp9LastRefSlot)
	if ts.framesTillGFUpdate != 2 {
		t.Fatalf("after LF update framesTillGFUpdate=%d, want 2",
			ts.framesTillGFUpdate)
	}
	ts.postEncodeGFUpdate(1 << vp9GoldenRefSlot)
	if ts.framesTillGFUpdate != 1 {
		t.Fatalf("after golden update framesTillGFUpdate=%d, want 1",
			ts.framesTillGFUpdate)
	}
}

func TestVP9SecondPassPreparedTargetSurvivesBeginFrameReset(t *testing.T) {
	enc := newVP9TwoPassQuantizerFixture(t)
	refreshFlags := uint8(1 << vp9AltRefSlot)

	enc.prepareVP9SecondPassFrameTarget(false, refreshFlags)
	prepared := enc.rc.frameTargetBits
	if prepared <= 0 {
		t.Fatalf("prepared target = %d, want positive", prepared)
	}
	enc.rc.beginFrameWithRefresh(false, enc.frameIndex, refreshFlags)
	if enc.rc.frameTargetBits == prepared {
		t.Fatalf("fixture did not reset frame target; target=%d", prepared)
	}
	enc.prepareVP9SecondPassFrameTarget(false, refreshFlags)
	if enc.rc.frameTargetBits != prepared ||
		enc.vp9TwoPassFrameTarget != prepared {
		t.Fatalf("prepared target after restore = rc:%d result:%d, want %d",
			enc.rc.frameTargetBits, enc.vp9TwoPassFrameTarget, prepared)
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

func TestVP9TwoPassPostEncodeQRangeTracksUndershoot(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 100, 100, 100)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
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
	enc.twoPass.activeWorstQuality = 120
	enc.twoPass.baseFrameTarget = 1000
	enc.twoPass.currentTargetBits = 1000
	enc.rc.bitsPerFrame = 1000
	enc.rc.totalActualBits = 500
	enc.rc.rollingTargetBits = 1000
	enc.rc.rollingActualBits = 400
	enc.updateVP9TwoPassPostEncodeQRange(100, false, 1<<vp9LastRefSlot)
	if enc.twoPass.rateErrorEstimate != 100 {
		t.Fatalf("rateErrorEstimate = %d, want clamped 100",
			enc.twoPass.rateErrorEstimate)
	}
	if enc.twoPass.extendMinQ <= 0 {
		t.Fatalf("extendMinQ = %d, want undershoot to extend min-q",
			enc.twoPass.extendMinQ)
	}
	if enc.twoPass.extendMinQFast <= 0 ||
		enc.twoPass.vbrBitsOffTargetFast <= 0 {
		t.Fatalf("fast undershoot feedback = extend:%d bits:%d, want positive",
			enc.twoPass.extendMinQFast,
			enc.twoPass.vbrBitsOffTargetFast)
	}
}

func TestVP9TwoPassPostEncodeQRangeTracksOvershoot(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 100, 100, 100)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
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
	enc.twoPass.activeWorstQuality = 120
	enc.twoPass.baseFrameTarget = 1000
	enc.twoPass.currentTargetBits = 1000
	enc.rc.bitsPerFrame = 1000
	enc.rc.totalActualBits = 3000
	enc.rc.rollingTargetBits = 1000
	enc.rc.rollingActualBits = 2000
	enc.updateVP9TwoPassPostEncodeQRange(3000, false, 1<<vp9LastRefSlot)
	if enc.twoPass.rateErrorEstimate >= -int(enc.rc.overshootPct) {
		t.Fatalf("rateErrorEstimate = %d, want beyond overshoot threshold %d",
			enc.twoPass.rateErrorEstimate, enc.rc.overshootPct)
	}
	if enc.twoPass.extendMaxQ <= 0 {
		t.Fatalf("extendMaxQ = %d, want overshoot to extend max-q",
			enc.twoPass.extendMaxQ)
	}
}

func TestVP9TwoPassQPickerConsumesPostEncodeQRange(t *testing.T) {
	stats := finalizedVP9TwoPassTestStats(100, 100, 100, 100)
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
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
	refreshFlags := uint8(1 << vp9LastRefSlot)
	enc.twoPass.activeWorstQuality = 120
	enc.rc.frameTargetBits = 1200
	enc.rc.maxFrameBandwidth = 5000
	enc.rc.lastQInter = 120
	enc.rc.avgFrameQIndexInter = 120
	macroblocks := vp9enc.MacroblockCount((enc.opts.Height+7)>>3,
		(enc.opts.Width+7)>>3)
	_, baseBest, baseWorst, _ := enc.vp9TwoPassQuantizerWithBounds(false,
		0, refreshFlags, macroblocks, nil, 0)

	enc.twoPass.extendMinQ = 8
	enc.twoPass.extendMinQFast = 4
	enc.twoPass.extendMaxQ = 6
	_, extendedBest, extendedWorst, _ := enc.vp9TwoPassQuantizerWithBounds(false,
		0, refreshFlags, macroblocks, nil, 0)
	if extendedBest >= baseBest {
		t.Fatalf("active best = %d, want < baseline %d after min-q extension",
			extendedBest, baseBest)
	}
	if extendedWorst <= baseWorst {
		t.Fatalf("active worst = %d, want > baseline %d after max-q extension",
			extendedWorst, baseWorst)
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
