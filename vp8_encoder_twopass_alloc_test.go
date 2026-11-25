package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"math"
	"testing"
)

// makeDampedActiveWorstQState builds a twoPassState seeded for the
// early-portion-of-clip damped active_worst_quality update path. The
// stats slice is sized so the libvpx window gate is open at frame=1
// with baselineGFInterval=4 and total_stats.count=256: frame 1 <
// (256*255)>>8 = 255 and frame+interval = 5 < 256. Coded error is
// uniform so the per-frame stats produce a deterministic err_per_mb.
// pass2ActiveWorstQ is preseeded so the damped update has a starting
// active_worst_quality to step.
func makeDampedActiveWorstQState(t *testing.T, total int, gfInterval int, seed int) *twoPassState {
	t.Helper()
	stats := make([]FirstPassFrameStats, total)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 5000,
			CodedError: 1000,
			Count:      1,
			PcntInter:  0.8,
		}
	}
	ts := &twoPassState{}
	ts.configure(stats, 50_000, 50, 0, 400)
	ts.configureFrameDims(128, 128)
	ts.configureQuantizerBounds(0, vp8common.MaxQ)
	ts.baselineGFInterval = gfInterval
	ts.pass2ActiveWorstQ = seed
	ts.pass2ActiveWorstQValid = true
	return ts
}

// TestDampedActiveWorstQSkipsFrameZero pins the libvpx
// vp8/encoder/firstpass.c vp8_second_pass behaviour where the damped
// branch is the `else if` from the `current_video_frame == 0` seed
// branch (lines 2328-2393). At frame 0 the damped branch must NOT
// fire; the regulator retains the seed value computed by the `==0`
// branch.
func TestDampedActiveWorstQSkipsFrameZero(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.dampedUpdatePass2ActiveWorstQ(0)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("frame 0 modified pass2ActiveWorstQ: got %d, want unchanged 40", got)
	}
	if !ts.pass2ActiveWorstQValid {
		t.Fatalf("frame 0 cleared pass2ActiveWorstQValid")
	}
}

// TestDampedActiveWorstQGateUpperBound pins the first half of the
// libvpx vp8/encoder/firstpass.c:2372-2373 window gate:
//
//	current_video_frame < ((total_stats.count * 255) >> 8)
//
// For total=256 the threshold is exactly 255. At frame 255 the gate
// is closed; the damped update returns early and leaves
// active_worst_quality unchanged.
func TestDampedActiveWorstQGateUpperBound(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	// Advance frameIndex to 255 (still within stats so totalLeftStats
	// remains valid via fallbacks).
	ts.frameIndex = 255
	ts.dampedUpdatePass2ActiveWorstQ(255)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("frame 255 (upper-gate closed) modified pass2ActiveWorstQ: got %d, want 40", got)
	}
}

// TestDampedActiveWorstQGateTrailingGF pins the second half of the
// libvpx vp8/encoder/firstpass.c:2374-2375 window gate:
//
//	(current_video_frame + baseline_gf_interval) < total_stats.count
//
// When the current frame plus the upcoming GF span equals or exceeds
// total_stats.count, the damped branch is skipped (we are in the
// trailing GF group).
func TestDampedActiveWorstQGateTrailingGF(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 32, 8, 40)
	// frame + baselineGFInterval == total_stats.count -> gate closed.
	ts.frameIndex = 24
	ts.dampedUpdatePass2ActiveWorstQ(24)
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("trailing-GF gate closed but damped path ran: got %d, want 40", got)
	}
	// One frame earlier: gate still open.
	ts2 := makeDampedActiveWorstQState(t, 32, 8, 40)
	ts2.frameIndex = 23
	ts2.dampedUpdatePass2ActiveWorstQ(23)
	if got := ts2.pass2ActiveWorstQ; got == 40 {
		t.Fatalf("trailing-GF gate open at frame 23 but damped path did not run (still %d)", got)
	}
}

// TestDampedActiveWorstQStepUp pins the libvpx
// vp8/encoder/firstpass.c:2385-2386 `++` step then damping average
// when tmp_q > active_worst_quality. We force a very large
// section_target_bandwidth so estimate_max_q lands at best_quality=0
// in one configuration and a very small budget so it pegs at
// worst_quality in another. The latter triggers the `++` branch.
func TestDampedActiveWorstQStepUp(t *testing.T) {
	// Tiny bits_left forces estimate_max_q to peg at worstQuality
	// (target_norm_bits_per_mb stays below baseBitsPerMB at all Q).
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.bitsLeft = 100 // tiny budget -> tmp_q == worstQuality
	ts.frameIndex = 1
	ts.pass2ActiveWorstQ = 40
	// Compute the libvpx tmp_q outside the function to derive the
	// damped output and verify our function matches.
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	if tmpQ <= 40 {
		t.Fatalf("test precondition failed: expected tmpQ > 40 to exercise ++ branch, got tmpQ=%d", tmpQ)
	}
	aw := 40 + 1 // ++ step
	want := (aw*3 + tmpQ + 2) / 4
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != want {
		t.Fatalf("step-up damped update: got %d, want %d (tmpQ=%d, pre-aw=40, post-step-aw=%d)", got, want, tmpQ, aw)
	}
}

// TestDampedActiveWorstQStepDown pins the libvpx
// vp8/encoder/firstpass.c:2387-2388 `--` step then damping average
// when tmp_q < active_worst_quality. We start with a high
// active_worst_quality and a large budget so estimate_max_q returns
// a low tmp_q, exercising the `--` branch.
func TestDampedActiveWorstQStepDown(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 100)
	// Large bits_left so tmp_q comes in low.
	ts.bitsLeft = 50_000_000
	ts.frameIndex = 1
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	if tmpQ >= 100 {
		t.Fatalf("test precondition failed: expected tmpQ < 100 to exercise -- branch, got tmpQ=%d", tmpQ)
	}
	aw := 100 - 1 // -- step
	want := (aw*3 + tmpQ + 2) / 4
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != want {
		t.Fatalf("step-down damped update: got %d, want %d (tmpQ=%d, pre-aw=100, post-step-aw=%d)", got, want, tmpQ, aw)
	}
}

// TestDampedActiveWorstQEqualNoStep pins the libvpx
// vp8/encoder/firstpass.c:2384-2392 path when tmp_q ==
// active_worst_quality: neither ++ nor -- fires, and the damping
// average collapses to ((aw*3 + aw + 2) / 4) == aw exactly. The
// regulator therefore stays put.
func TestDampedActiveWorstQEqualNoStep(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.frameIndex = 1
	framesLeft := int64(len(ts.stats)) - 1
	stb := ts.bitsLeft / framesLeft
	sectionErr := ts.totalLeftStats.CodedError / ts.totalLeftStats.Count
	errPerMB := sectionErr / float64(ts.numMBs)
	overhead := libvpxEstimateModeMVCost(ts.totalLeftStats, ts.numMBs)
	tmpQ := libvpxEstimateMaxQ(ts.numMBs, int(stb), overhead, errPerMB, 1.0, ts.estMaxQCorrection, ts.sectionMaxQFactor, ts.bestQuality, ts.worstQuality)
	// Force the seeded active_worst_quality to match tmp_q so neither
	// step branch fires.
	ts.pass2ActiveWorstQ = tmpQ
	want := (tmpQ*3 + tmpQ + 2) / 4 // == tmpQ
	if want != tmpQ {
		t.Fatalf("damping formula identity broken: %d != %d", want, tmpQ)
	}
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if got := ts.pass2ActiveWorstQ; got != tmpQ {
		t.Fatalf("equal-tmpQ damped update: got %d, want %d", got, tmpQ)
	}
}

// TestDampedActiveWorstQAveragingFormula pins the libvpx
// vp8/encoder/firstpass.c:2391-2392 damping formula:
//
//	cpi->active_worst_quality = ((aw * 3) + tmp_q + 2) / 4
//
// independently of the ++/-- step. We construct a state where the
// step lands aw closer to tmp_q than 0, then verify the integer
// arithmetic exactly.
func TestDampedActiveWorstQAveragingFormula(t *testing.T) {
	cases := []struct {
		aw, tmpQ, want int
	}{
		// step up: aw=40 -> aw=41, then (41*3 + 50 + 2)/4 = (123+52)/4 = 175/4 = 43
		{aw: 40, tmpQ: 50, want: (41*3 + 50 + 2) / 4},
		// step down: aw=80 -> aw=79, then (79*3 + 50 + 2)/4 = (237+52)/4 = 289/4 = 72
		{aw: 80, tmpQ: 50, want: (79*3 + 50 + 2) / 4},
		// equal: aw=50, tmpQ=50, no step, then (50*3+50+2)/4 = 50
		{aw: 50, tmpQ: 50, want: (50*3 + 50 + 2) / 4},
	}
	for _, tc := range cases {
		aw := tc.aw
		if tc.tmpQ > aw {
			aw++
		} else if tc.tmpQ < aw {
			aw--
		}
		aw = (aw*3 + tc.tmpQ + 2) / 4
		if aw != tc.want {
			t.Fatalf("damping formula: aw=%d tmpQ=%d -> got %d, want %d", tc.aw, tc.tmpQ, aw, tc.want)
		}
	}
}

// TestDampedActiveWorstQRequiresValidSeed pins the precondition:
// when pass2ActiveWorstQValid is false (one-pass mode or pre-seed),
// the damped update is a no-op. libvpx's `==0` branch always runs
// before the damped branch, so the validity bit is guaranteed once
// the damped branch can fire.
func TestDampedActiveWorstQRequiresValidSeed(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.frameIndex = 1
	ts.pass2ActiveWorstQValid = false
	ts.pass2ActiveWorstQ = 40
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if ts.pass2ActiveWorstQValid {
		t.Fatalf("damped update flipped pass2ActiveWorstQValid from false to true without seed")
	}
	if got := ts.pass2ActiveWorstQ; got != 40 {
		t.Fatalf("damped update mutated active_worst_quality with invalid seed: got %d, want 40", got)
	}
}

func TestCollectFirstPassStatsAndTwoPassSceneCut(t *testing.T) {
	const (
		width  = 256
		height = 256
	)
	firstPass, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
	})
	if err != nil {
		t.Fatalf("first-pass NewVP8Encoder returned error: %v", err)
	}
	frames := make([]Image, 12)
	stats := make([]FirstPassFrameStats, len(frames))
	fillScene := func(img Image, base int) {
		for y := 0; y < img.Height; y++ {
			for x := 0; x < img.Width; x++ {
				img.Y[y*img.YStride+x] = byte(base + ((x*17 + y*31 + x*y*3) & 63))
			}
		}
		for i := range img.U {
			img.U[i] = 90
			img.V[i] = 170
		}
	}
	for i := range frames {
		frames[i] = testImage(width, height)
		if i < 5 {
			fillScene(frames[i], 20)
		} else {
			fillScene(frames[i], 150)
		}
		stats[i], err = firstPass.CollectFirstPassStats(frames[i], uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats %d returned error: %v", i, err)
		}
	}
	if !libvpxTestCandidateKeyFrame(stats, 5) {
		t.Fatalf("first-pass stats did not satisfy libvpx candidate keyframe test at scene cut: prev=%+v cut=%+v next=%+v", stats[4], stats[5], stats[6])
	}

	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		TwoPassStats:      stats,
		TwoPassMinPct:     50,
		TwoPassMaxPct:     200,
	})
	if err != nil {
		t.Fatalf("second-pass NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 512*1024)
	var result EncodeResult
	for i, frame := range frames[:6] {
		result, err = e.EncodeInto(dst, frame, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
	}
	if !result.KeyFrame || !result.SceneCut || result.PTS != 5 || result.TwoPassFrameTargetBits == 0 {
		t.Fatalf("scene-cut result = key:%t scene:%t pts:%d target:%d, want two-pass scene-cut keyframe", result.KeyFrame, result.SceneCut, result.PTS, result.TwoPassFrameTargetBits)
	}
}

func TestEncoderCloseAllocatesZero(t *testing.T) {
	e := newTestEncoder(t)
	e.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		e.closed = false
		_ = e.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}

func TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetAhead(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	ts.bitsLeft = 20000

	want := libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), ts.maxPct)
	if want != 4000 {
		t.Fatalf("test setup VBR max = %d, want 4000", want)
	}
	if got := ts.frameTargetBits(0, false, 1000); got != want {
		t.Fatalf("two-pass target ahead of budget = %d, want live VBR max %d", got, want)
	}
}

// TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetBehind pins the
// symmetric case: when bits_left has fallen below the initial average
// budget, the libvpx frame_max_bits cap tightens.
func TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetBehind(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	ts.bitsLeft = 5000

	want := libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), ts.maxPct)
	if want != 1000 {
		t.Fatalf("test setup VBR max = %d, want 1000", want)
	}
	if got := ts.frameTargetBits(0, false, 1000); got != want {
		t.Fatalf("two-pass target behind budget = %d, want live VBR max %d", got, want)
	}
}

func TestTwoPassConfigureConsumesTerminalTotalStats(t *testing.T) {
	frames := []FirstPassFrameStats{
		{CodedError: 100, SSIMWeightedPredErr: 100, Count: 1},
		{CodedError: 900, SSIMWeightedPredErr: 900, Count: 1},
	}
	total := FirstPassFrameStats{CodedError: 1000, SSIMWeightedPredErr: 1000, Count: 2}
	var ts twoPassState
	ts.configure(append(frames, total), 1000, 50, 1, 1000)

	if got := len(ts.stats); got != 2 {
		t.Fatalf("two-pass frame stats length = %d, want terminal total excluded", got)
	}
	if ts.bitsLeft != 1980 {
		t.Fatalf("bitsLeft = %d, want two real frames minus min-frame reserve", ts.bitsLeft)
	}
	want := libvpxCalculateModifiedErr(100, 1000, 2, 50) +
		libvpxCalculateModifiedErr(900, 1000, 2, 50)
	if math.Abs(ts.errorLeft-want) > 1e-9 {
		t.Fatalf("errorLeft = %v, want libvpx terminal-total modified error %v", ts.errorLeft, want)
	}
}

func TestTwoPassConfigureSynthesizesTotalStatsWhenMissing(t *testing.T) {
	stats := []FirstPassFrameStats{
		{CodedError: 100, SSIMWeightedPredErr: 100, Count: 1},
		{CodedError: 900, SSIMWeightedPredErr: 900, Count: 1},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 1, 1000)

	if ts.totalStats.SSIMWeightedPredErr != 1000 || ts.totalStats.Count != 2 {
		t.Fatalf("total stats = %+v, want synthesized SSIM=1000 Count=2", ts.totalStats)
	}
	want := libvpxCalculateModifiedErr(100, 1000, 2, 50) +
		libvpxCalculateModifiedErr(900, 1000, 2, 50)
	if math.Abs(ts.errorLeft-want) > 1e-9 {
		t.Fatalf("errorLeft = %v, want synthesized-total modified error %v", ts.errorLeft, want)
	}
}

// TestNiMaxQLimitClampBelow150IsNoOp covers the libvpx firstpass.c
// lines 994-1006 lower gate: when ni_frames <= 150 the clamp must not
// fire even if the count gate would be met. Verifies the persistent
// maxq limits stay at their seeded (best, worst) bounds.
func TestNiMaxQLimitClampBelow150IsNoOp(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     149,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 1000},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 4 || maxLimit != 127 {
		t.Fatalf("ni_frames=149: got (min=%d,max=%d), want (4,127) — clamp must be a no-op below the 150 floor", minLimit, maxLimit)
	}
	if state.maxqMinLimit != 4 || state.maxqMaxLimit != 127 {
		t.Fatalf("ni_frames=149: persistent limits mutated to (%d,%d); want (4,127)", state.maxqMinLimit, state.maxqMaxLimit)
	}
}

// TestNiMaxQLimitClampSmallTotalIsNoOp covers the libvpx firstpass.c
// upper gate: when ni_frames > (total_stats.count >> 8) is false, the
// clamp must not fire. With ni_frames=200 and total=100000, the right-
// shift gives 390, so ni_frames=200 <= 390 means the predicate is
// false and limits stay seeded.
func TestNiMaxQLimitClampSmallTotalIsNoOp(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     200,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 100000},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 4 || maxLimit != 127 {
		t.Fatalf("ni_frames=200, total=100000: got (min=%d,max=%d), want (4,127) — total/256=390 exceeds ni_frames, clamp must not fire", minLimit, maxLimit)
	}
}

// TestNiMaxQLimitClampFiresAndNarrows covers the libvpx firstpass.c
// lines 994-1006 hot path: when both gates are met (ni_frames>150 AND
// ni_frames > total/256), the persistent maxq limits are narrowed to
// ni_av_qi±32, bounded by best/worst. Uses ni_frames=200, total=500
// (total/256=1 << 200) and ni_av_qi=60 so the narrowed band is [28,92].
func TestNiMaxQLimitClampFiresAndNarrows(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		niFrames:     200,
		niAvQi:       60,
		totalStats:   FirstPassFrameStats{Count: 500},
		maxqMinLimit: 4,
		maxqMaxLimit: 127,
	}
	minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
	if minLimit != 28 || maxLimit != 92 {
		t.Fatalf("ni_frames=200, total=500, ni_av_qi=60: got (min=%d,max=%d), want (28,92)", minLimit, maxLimit)
	}
	// libvpx mutates cpi->twopass.maxq_{min,max}_limit in place.
	if state.maxqMinLimit != 28 || state.maxqMaxLimit != 92 {
		t.Fatalf("ni_frames=200, total=500: persistent limits=(%d,%d), want (28,92) — clamp must mutate state", state.maxqMinLimit, state.maxqMaxLimit)
	}
}

// TestNiMaxQLimitClampClampsToWorstAndBest covers the libvpx
// firstpass.c lines 1000-1005 ternary bounds: when ni_av_qi+32 exceeds
// worst_quality, the narrowed maxq_max_limit is pinned at
// worst_quality; symmetrically the lower side is pinned at
// best_quality. Uses ni_av_qi=110 (so +32=142 > worst=127 → 127) and
// ni_av_qi=20 (so -32=-12 < best=4 → 4) in two sub-cases.
func TestNiMaxQLimitClampClampsToWorstAndBest(t *testing.T) {
	t.Run("upper_pinned_at_worst", func(t *testing.T) {
		state := twoPassState{
			bestQuality:  4,
			worstQuality: 127,
			niFrames:     200,
			niAvQi:       110,
			totalStats:   FirstPassFrameStats{Count: 500},
			maxqMinLimit: 4,
			maxqMaxLimit: 127,
		}
		minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
		if maxLimit != 127 {
			t.Fatalf("ni_av_qi=110: maxLimit=%d, want 127 (ni_av_qi+32=142 should clamp to worst_quality)", maxLimit)
		}
		if minLimit != 78 {
			t.Fatalf("ni_av_qi=110: minLimit=%d, want 78", minLimit)
		}
	})
	t.Run("lower_pinned_at_best", func(t *testing.T) {
		state := twoPassState{
			bestQuality:  4,
			worstQuality: 127,
			niFrames:     200,
			niAvQi:       20,
			totalStats:   FirstPassFrameStats{Count: 500},
			maxqMinLimit: 4,
			maxqMaxLimit: 127,
		}
		minLimit, maxLimit := state.applyNiMaxQLimitClamp(state.maxqMinLimit, state.maxqMaxLimit)
		if minLimit != 4 {
			t.Fatalf("ni_av_qi=20: minLimit=%d, want 4 (ni_av_qi-32=-12 should clamp to best_quality)", minLimit)
		}
		if maxLimit != 52 {
			t.Fatalf("ni_av_qi=20: maxLimit=%d, want 52", maxLimit)
		}
	})
}

// TestRecordInterFrameQuantizerSkipsKeyAndGFRefresh covers the libvpx
// vp8/encoder/onyx_if.c lines 4478-4480 update gate: KEY frames and
// (single-layer) golden/alt-ref refresh frames must NOT increment
// ni_frames. Verifies the recorder respects all three skip conditions.
func TestRecordInterFrameQuantizerSkipsKeyAndGFRefresh(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}, {}, {}},
	}
	// Key frame: skip regardless of refresh flags.
	state.recordInterFrameQuantizer(50, true, false, false, 1, true)
	// Single-layer golden refresh: skip.
	state.recordInterFrameQuantizer(50, false, true, false, 1, true)
	// Single-layer altref refresh: skip.
	state.recordInterFrameQuantizer(50, false, false, true, 1, true)
	if state.niFrames != 0 {
		t.Fatalf("recordInterFrameQuantizer skip cases incremented niFrames=%d, want 0", state.niFrames)
	}
	if state.niTotQi != 0 {
		t.Fatalf("recordInterFrameQuantizer skip cases mutated niTotQi=%d, want 0", state.niTotQi)
	}
}

// TestRecordInterFrameQuantizerPass2RunningAverage covers the libvpx
// onyx_if.c lines 4486-4488 pass-2 branch: ni_av_qi tracks the simple
// cumulative average of recorded Q values. Verifies the running mean
// is exact after several increments.
func TestRecordInterFrameQuantizerPass2RunningAverage(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}, {}, {}},
	}
	qs := []int{40, 50, 60, 70}
	want := 0
	for _, q := range qs {
		state.recordInterFrameQuantizer(q, false, false, false, 1, true)
		want += q
	}
	if state.niFrames != len(qs) {
		t.Fatalf("niFrames=%d, want %d", state.niFrames, len(qs))
	}
	if state.niTotQi != want {
		t.Fatalf("niTotQi=%d, want %d", state.niTotQi, want)
	}
	if state.niAvQi != want/len(qs) {
		t.Fatalf("niAvQi=%d, want %d (pass=2: simple cumulative average)", state.niAvQi, want/len(qs))
	}
}

// TestRecordInterFrameQuantizerLayeredCountsGoldenRefresh covers the
// libvpx onyx_if.c line 4479 layered branch:
//
//	if ((frame_type != KEY) &&
//	    ((number_of_layers > 1) ||
//	     (!refresh_golden && !refresh_alt))) { ni_frames++; }
//
// When number_of_layers > 1 the second disjunct is the always-true
// upper branch, so golden / altref refresh frames DO increment
// ni_frames in layered mode.
func TestRecordInterFrameQuantizerLayeredCountsGoldenRefresh(t *testing.T) {
	state := twoPassState{
		bestQuality:  4,
		worstQuality: 127,
		stats:        []FirstPassFrameStats{{}, {}},
	}
	// number_of_layers=2: golden refresh now increments ni_frames.
	state.recordInterFrameQuantizer(50, false, true, false, 2, true)
	if state.niFrames != 1 || state.niAvQi != 50 {
		t.Fatalf("layered golden refresh: niFrames=%d niAvQi=%d, want (1, 50)", state.niFrames, state.niAvQi)
	}
}

// TestApplyEstMaxQRollingRatioGateRequiresPositiveTarget pins the
// outer-half of the libvpx gate at vp8/encoder/firstpass.c lines
// 923-924:
//
//	if ((cpi->rolling_target_bits > 0) &&
//	    (cpi->active_worst_quality < cpi->worst_quality)) { ... }
//
// When rolling_target_bits <= 0 (the initial state before any frame
// has been encoded, or when the rate controller drains the rolling
// averages to zero), the rolling-ratio update is a no-op and the
// est_max_qcorrection_factor is preserved.
func TestApplyEstMaxQRollingRatioGateRequiresPositiveTarget(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      900,
		rollingTargetBits:      0,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("zero-target gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioGateRequiresActiveBelowWorst pins the
// inner-half of the libvpx gate at firstpass.c line 924:
//
//	cpi->active_worst_quality < cpi->worst_quality
//
// When the regulator's active_worst_quality has been pushed all the
// way up to oxcf.worst_allowed_q (the configured ceiling), libvpx
// suppresses the rolling-ratio nudge so the factor cannot drag the
// already-pegged worst-Q regulator any further.
func TestApplyEstMaxQRollingRatioGateRequiresActiveBelowWorst(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      127,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      800,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("active==worst gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioGateRequiresValidSeed pins govpx's
// equivalent of libvpx's `current_video_frame == 0` short-circuit:
// before the pass-2 seed has run, the regulator has not yet pushed
// active_worst_quality below worst_quality (libvpx initializes them
// equal at vp8_init_first_pass / vp8_new_framerate). Without a valid
// seed govpx treats the gate as closed so the very first
// estimate_max_q call keeps the factor at its libvpx-shaped 1.0
// initial value.
func TestApplyEstMaxQRollingRatioGateRequiresValidSeed(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: false,
		rollingActualBits:      800,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("pre-seed gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepDown pins the libvpx
// firstpass.c:930-931 `ratio < 0.95 -> factor -= 0.005` branch when
// the gate is open. Setup mirrors a mid-clip pass-2 frame where the
// rate controller has been underspending: rolling_actual_bits /
// rolling_target_bits = 0.9, so the factor steps down 0.005.
func TestApplyEstMaxQRollingRatioStepDown(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      900,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 0.995 {
		t.Fatalf("ratio<0.95 step: factor=%v, want 0.995", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepUp pins the libvpx
// firstpass.c:932-933 `ratio > 1.05 -> factor += 0.005` branch when
// the gate is open. Setup mirrors a mid-clip pass-2 frame where the
// rate controller has been overspending: rolling_actual_bits /
// rolling_target_bits = 1.1.
func TestApplyEstMaxQRollingRatioStepUp(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      1100,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.005 {
		t.Fatalf("ratio>1.05 step: factor=%v, want 1.005", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepDeadZone pins the libvpx
// firstpass.c:930-934 `else` branch: ratio in [0.95, 1.05] leaves
// the factor unchanged. The gate is open (positive target, active <
// worst) so we are exercising the dead-zone of the libvpx
// piecewise update, not the outer guards.
func TestApplyEstMaxQRollingRatioStepDeadZone(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      1000,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("dead-zone step: factor=%v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioRunsInSeedPass2 pins the integration
// of the rolling-ratio nudge into seedPass2ActiveWorstQ. The libvpx
// equivalent is firstpass.c line 2349 calling estimate_max_q, which
// then runs the rolling-ratio update at lines 920-941 BEFORE the
// Q-search. govpx mirrors this by invoking
// applyEstMaxQRollingRatioAdjustment as the first step of
// seedPass2ActiveWorstQ's q-search prep. We pre-seed
// pass2ActiveWorstQValid=true with active=40<worst=127 so the gate
// is open, supply rolling bits with ratio 0.9, and assert that
// est_max_qcorrection_factor stepped down by 0.005 after the seed
// runs.
func TestApplyEstMaxQRollingRatioRunsInSeedPass2(t *testing.T) {
	stats := make([]FirstPassFrameStats, 32)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 5000,
			CodedError: 1000,
			Count:      1,
			PcntInter:  0.8,
		}
	}
	ts := &twoPassState{}
	ts.configure(stats, 50_000, 50, 0, 400)
	ts.configureFrameDims(128, 128)
	ts.configureQuantizerBounds(0, vp8common.MaxQ)
	// Pre-seed a valid active_worst_quality below the configured
	// worst, so the libvpx gate is open even on this frame.
	ts.pass2ActiveWorstQ = 40
	ts.pass2ActiveWorstQValid = true
	ts.setRollingBits(900, 1000)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("setup: estMaxQCorrection = %v, want 1.0", ts.estMaxQCorrection)
	}
	ts.seedPass2ActiveWorstQ(50_000)
	if ts.estMaxQCorrection != 0.995 {
		t.Fatalf("seed pass-2: estMaxQCorrection = %v, want 0.995 (rolling-ratio step down)", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioRunsInDampedUpdate pins the integration
// of the rolling-ratio nudge into dampedUpdatePass2ActiveWorstQ. The
// libvpx equivalent is firstpass.c line 2381 calling estimate_max_q,
// which then runs the rolling-ratio update at lines 920-941. With
// the damped-branch window gate open and rolling bits indicating
// overshoot (ratio 1.1), the factor must step up by 0.005.
func TestApplyEstMaxQRollingRatioRunsInDampedUpdate(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.setRollingBits(1100, 1000)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("setup: estMaxQCorrection = %v, want 1.0", ts.estMaxQCorrection)
	}
	ts.frameIndex = 1
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if ts.estMaxQCorrection != 1.005 {
		t.Fatalf("damped update: estMaxQCorrection = %v, want 1.005 (rolling-ratio step up)", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioNoOpInTrailingClip pins that the
// rolling-ratio update fires only inside libvpx's estimate_max_q
// — i.e., the seed and damped branches of vp8_second_pass. In the
// trailing portion of the clip (past the (count*255)>>8 window) the
// damped branch returns before invoking estimate_max_q, so the
// factor is preserved across that frame even when rolling bits
// would otherwise nudge it.
func TestApplyEstMaxQRollingRatioNoOpInTrailingClip(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.setRollingBits(1100, 1000)
	ts.frameIndex = 255
	ts.dampedUpdatePass2ActiveWorstQ(255)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("trailing-clip gate closed but factor moved: got %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestSetRollingBitsStoresValues pins the simple field-update
// semantics of setRollingBits. The encoder calls this from
// vp8_encoder_frame.go before frameTargetBitsWithAltRef so the next
// pass-2 frame's estimate_max_q-equivalents see the latest rolling
// stats.
func TestSetRollingBitsStoresValues(t *testing.T) {
	ts := &twoPassState{}
	ts.setRollingBits(1234, 5678)
	if ts.rollingActualBits != 1234 {
		t.Fatalf("rollingActualBits = %d, want 1234", ts.rollingActualBits)
	}
	if ts.rollingTargetBits != 5678 {
		t.Fatalf("rollingTargetBits = %d, want 5678", ts.rollingTargetBits)
	}
}
