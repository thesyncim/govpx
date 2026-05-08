package govpx

import (
	"math"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestFirstPassStatsPopulatesLibvpxFields runs CollectFirstPassStats on a
// small synthetic clip with luma motion and asserts the libvpx-aligned
// stats fields (ssim_weighted_pred_err, MVr/MVc/MVrAbs/MVcAbs/MVrv/MVcv,
// MVInOutCount, NewMVCount, PcntMotion) get populated to plausible values
// once motion search and simple_weight wiring is in place.
func TestFirstPassStatsPopulatesLibvpxFields(t *testing.T) {
	const (
		width  = 64
		height = 64
		count  = 6
	)
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 800,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	// Build a clip where every frame contains a clearly non-flat luma
	// pattern (luma gradient + a moving square) so simple_weight, motion
	// search, and MV accumulation all have signal to record.
	frames := make([]Image, count)
	for i := range frames {
		frames[i] = testImage(width, height)
		// Background gradient: ramps from 16..200 across the row, which
		// puts most pixels above the 64-code knee in the libvpx
		// weight_table so the simple_weight average lands near 1.0.
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				v := 32 + ((x + y) * 200 / (width + height))
				if v > 235 {
					v = 235
				}
				frames[i].Y[y*frames[i].YStride+x] = byte(v)
			}
		}
		// Moving 16x16 dark square shifts left by 1 pixel per frame, so
		// the diamond search picks a non-zero MV after frame 0.
		sx := 16 - i
		sy := 16
		for dy := 0; dy < 16; dy++ {
			for dx := 0; dx < 16; dx++ {
				ix := sx + dx
				iy := sy + dy
				if ix < 0 || ix >= width || iy < 0 || iy >= height {
					continue
				}
				frames[i].Y[iy*frames[i].YStride+ix] = 8
			}
		}
		// Constant chroma keeps the test focused on luma stats.
		for j := range frames[i].U {
			frames[i].U[j] = 128
		}
		for j := range frames[i].V {
			frames[i].V[j] = 128
		}
	}

	stats := make([]FirstPassFrameStats, count)
	for i := range frames {
		stats[i], err = enc.CollectFirstPassStats(frames[i], uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", i, err)
		}
	}

	// Frame 0 has no LAST: motion search is skipped, so MV-related fields
	// are all zero and PcntInter is zero too.
	if stats[0].PcntInter != 0 || stats[0].PcntMotion != 0 || stats[0].NewMVCount != 0 {
		t.Fatalf("frame 0 should have zero inter/motion stats; got %+v", stats[0])
	}
	if stats[0].SSIMWeightedPredErr <= 0 {
		t.Fatalf("frame 0 ssim-weighted err should be > 0; got %f", stats[0].SSIMWeightedPredErr)
	}

	// Subsequent frames must have non-zero motion stats because the dark
	// square shifted by 1 pixel and motion search picks (0,-1) or similar.
	for i := 1; i < count; i++ {
		if stats[i].PcntInter <= 0 {
			t.Fatalf("frame %d PcntInter should be > 0; got %f", i, stats[i].PcntInter)
		}
		if stats[i].PcntMotion < 0 || stats[i].PcntMotion > 1 {
			t.Fatalf("frame %d PcntMotion out of [0,1]: %f", i, stats[i].PcntMotion)
		}
		if math.IsNaN(stats[i].MVrv) || math.IsNaN(stats[i].MVcv) {
			t.Fatalf("frame %d MV variance is NaN: row=%f col=%f", i, stats[i].MVrv, stats[i].MVcv)
		}
		if math.Abs(stats[i].MVInOutCount) > 1.0 {
			t.Fatalf("frame %d MVInOutCount magnitude should be <= 1; got %f", i, stats[i].MVInOutCount)
		}
		// simple_weight on a luma gradient with most pixels above 64 must
		// be close to 1.0; ssim_weighted_pred_err >= 0.1 * coded_error.
		if stats[i].SSIMWeightedPredErr < 0.1*stats[i].CodedError-1e-9 {
			t.Fatalf("frame %d ssim-weighted err below the libvpx 0.1 weight floor: ssim=%f coded=%f",
				i, stats[i].SSIMWeightedPredErr, stats[i].CodedError)
		}
	}

	// At least one frame should report a non-zero NewMVCount because the
	// shifting square produces a non-zero MV that differs from the prior
	// frame's last seen MV (which starts at 0).
	sawNewMV := false
	for i := 1; i < count; i++ {
		if stats[i].NewMVCount > 0 {
			sawNewMV = true
			break
		}
	}
	if !sawNewMV {
		t.Fatalf("expected at least one frame with NewMVCount > 0; stats=%+v", stats)
	}
}

func TestFirstPassStaticThresholdFeedsEncodeBreakout(t *testing.T) {
	const (
		width  = 64
		height = 64
	)
	newEncoder := func(staticThreshold int) *VP8Encoder {
		enc, err := NewVP8Encoder(EncoderOptions{
			Width:             width,
			Height:            height,
			FPS:               30,
			RateControlMode:   RateControlVBR,
			TargetBitrateKbps: 800,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  60,
			StaticThreshold:   staticThreshold,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder(staticThreshold=%d): %v", staticThreshold, err)
		}
		return enc
	}
	frame := func(shift int) Image {
		img := testImage(width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.Y[y*img.YStride+x] = byte(48 + ((x*5 + y*3) & 127))
			}
		}
		sx := 22 + shift
		sy := 18
		for dy := 0; dy < 16; dy++ {
			for dx := 0; dx < 16; dx++ {
				x := sx + dx
				y := sy + dy
				if x >= 0 && x < width && y >= 0 && y < height {
					img.Y[y*img.YStride+x] = 220
				}
			}
		}
		for i := range img.U {
			img.U[i] = 128
			img.V[i] = 128
		}
		return img
	}

	motion := newEncoder(0)
	breakout := newEncoder(1 << 30)
	frames := []Image{frame(0), frame(1)}
	var motionStats, breakoutStats FirstPassFrameStats
	var err error
	for i, f := range frames {
		motionStats, err = motion.CollectFirstPassStats(f, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("motion CollectFirstPassStats[%d]: %v", i, err)
		}
		breakoutStats, err = breakout.CollectFirstPassStats(f, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("breakout CollectFirstPassStats[%d]: %v", i, err)
		}
	}
	if motionStats.PcntMotion <= 0 || motionStats.NewMVCount <= 0 {
		t.Fatalf("default first-pass stats should run motion search and find a non-zero MV: %+v", motionStats)
	}
	if breakoutStats.PcntMotion != 0 || breakoutStats.NewMVCount != 0 {
		t.Fatalf("static-threshold first-pass stats should skip motion search via encode_breakout: %+v", breakoutStats)
	}
}

func TestFirstPassZeroMotionErrorDoesNotAddBias(t *testing.T) {
	const width, height = 32, 16
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 400,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	frame := testImage(width, height)
	fillImage(frame, 128, 128, 128)

	if _, err := enc.CollectFirstPassStats(frame, 0, 1, 0); err != nil {
		t.Fatalf("CollectFirstPassStats[0]: %v", err)
	}
	stats, err := enc.CollectFirstPassStats(frame, 1, 1, 0)
	if err != nil {
		t.Fatalf("CollectFirstPassStats[1]: %v", err)
	}
	if stats.CodedError != 0 {
		t.Fatalf("identical-frame coded_error = %v, want 0 (libvpx zero-motion MSE has no +128 bias)", stats.CodedError)
	}
}

func TestFirstPassGoldenDoesNotResetOnAllIntraFallback(t *testing.T) {
	const width, height = 16, 16
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 400,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(width, height)
	fillImage(first, 16, 128, 128)
	second := testImage(width, height)
	fillImage(second, 240, 128, 128)

	if _, err := enc.CollectFirstPassStats(first, 0, 1, 0); err != nil {
		t.Fatalf("CollectFirstPassStats[0]: %v", err)
	}
	stats, err := enc.CollectFirstPassStats(second, 1, 1, 0)
	if err != nil {
		t.Fatalf("CollectFirstPassStats[1]: %v", err)
	}
	if stats.PcntInter >= 0.05 {
		t.Fatalf("test setup PcntInter = %v, want all-intra hard cut", stats.PcntInter)
	}
	golden := &enc.firstPassGoldenRef.Img
	last := &enc.firstPassLastRef.Img
	if planeMatches(golden.Y, golden.YStride, last.Y, last.YStride, width, height) &&
		planeMatches(golden.U, golden.UStride, last.U, last.UStride, width/2, height/2) &&
		planeMatches(golden.V, golden.VStride, last.V, last.VStride, width/2, height/2) {
		t.Fatalf("GOLDEN reset to the all-intra current LAST frame; libvpx only seeds GOLDEN on frame 0 or via the post-stats copy heuristic")
	}
}

// TestFirstPassStatsRegression32x32 pins the per-frame FirstPassFrameStats
// for a deterministic 32x32 (4 macroblock) clip. The expected values were
// captured from this implementation against the libvpx vp8_first_pass
// formulas (see encoder_firstpass.go). Any change to the first-pass scoring
// must update these constants explicitly.
//
// libvpx references for each pinned field (vp8/encoder/firstpass.c):
//   - intra_error: macroblockMeanLumaSSE + intrapenalty
//   - coded_error: min(intra, motion_error) per MB, motion_error from
//     first_pass_motion_search (zero-MV + NSTEP diamond + new_mv_mode_penalty)
//   - ssim_weighted_pred_err: coded_error * simple_weight(source) (>=0.1)
//   - pcnt_inter: intercount / MBs
//   - pcnt_neutral: neutral_count / MBs (low-and-close intra/inter heuristic)
//   - MVr/MVc/MVrAbs/MVcAbs: q3-scaled MV component sums / mvcount
//   - MVrv/MVcv: (sum_squares - mean*sum)/mvcount per axis
//   - mv_in_out_count: sum_in_vectors / (mvcount * 2)
//   - new_mv_count: count of mvcount entries that differed from the
//     previous non-zero MV
//   - pcnt_motion: mvcount / MBs
func TestFirstPassStatsRegression32x32(t *testing.T) {
	const (
		width  = 32
		height = 32
	)
	enc, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 400,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	// 2D luma ramp shifted by `shift` pixels in both row and column. A
	// strict 2D ramp makes the integer-pel SSE search pick a unique
	// best MV, which keeps the regression assertions stable.
	frame := func(shift int) Image {
		img := testImage(width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				v := 32 + (y+shift)*3 + (x+shift)*2
				if v < 0 {
					v = 0
				}
				if v > 235 {
					v = 235
				}
				img.Y[y*img.YStride+x] = byte(v)
			}
		}
		for j := range img.U {
			img.U[j] = 128
		}
		for j := range img.V {
			img.V[j] = 128
		}
		return img
	}

	frames := []Image{frame(0), frame(1), frame(2)}
	stats := make([]FirstPassFrameStats, len(frames))
	for i, f := range frames {
		stats[i], err = enc.CollectFirstPassStats(f, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", i, err)
		}
	}

	type expect struct {
		intraError          float64
		codedError          float64
		ssimWeightedPredErr float64
		pcntInter           float64
		pcntMotion          float64
		pcntSecondRef       float64
		pcntNeutral         float64
		mvR                 float64
		mvRAbs              float64
		mvC                 float64
		mvCAbs              float64
		mvRv                float64
		mvCv                float64
		mvInOut             float64
		newMVCount          float64
	}
	// Pinned values captured from the libvpx-aligned implementation in
	// encoder_firstpass.go. Update these in lock-step with the formulas
	// above when libvpx-parity work touches the first-pass scoring.
	want := []expect{
		// Frame 0: no LAST yet, so all motion-related fields are zero.
		// IntraError = mbs * (variance + 256) >> 8. The vertical ramp
		// is constant within each row, so each MB's mean-luma SSE is
		// nonzero; the values below are what the implementation
		// produces.
		{
			intraError: firstPassRegressionExpectIntraError0,
			codedError: firstPassRegressionExpectIntraError0,
			// simple_weight on a ramp 16..140 averages well below 1.0
			// because codes <=32 pin to 0.02 in weight_table[]. The
			// libvpx clamp ensures weight >= 0.1.
			ssimWeightedPredErr: firstPassRegressionExpectSSIM0,
		},
		{
			intraError:          firstPassRegressionExpectIntraError1,
			codedError:          firstPassRegressionExpectCodedError1,
			ssimWeightedPredErr: firstPassRegressionExpectSSIM1,
			pcntInter:           1.0,
			pcntMotion:          1.0,
			pcntNeutral:         0.0,
			mvR:                 firstPassRegressionExpectMVr1,
			mvRAbs:              firstPassRegressionExpectMVrAbs1,
			mvC:                 firstPassRegressionExpectMVc1,
			mvCAbs:              firstPassRegressionExpectMVcAbs1,
			mvRv:                firstPassRegressionExpectMVrv1,
			mvCv:                firstPassRegressionExpectMVcv1,
			mvInOut:             firstPassRegressionExpectMVInOut1,
			newMVCount:          firstPassRegressionExpectNewMV1,
		},
		{
			intraError:          firstPassRegressionExpectIntraError2,
			codedError:          firstPassRegressionExpectCodedError2,
			ssimWeightedPredErr: firstPassRegressionExpectSSIM2,
			pcntInter:           1.0,
			pcntMotion:          1.0,
			pcntSecondRef:       firstPassRegressionExpectPcntSecondRef2,
			pcntNeutral:         0.0,
			mvR:                 firstPassRegressionExpectMVr2,
			mvRAbs:              firstPassRegressionExpectMVrAbs2,
			mvC:                 firstPassRegressionExpectMVc2,
			mvCAbs:              firstPassRegressionExpectMVcAbs2,
			mvRv:                firstPassRegressionExpectMVrv2,
			mvCv:                firstPassRegressionExpectMVcv2,
			mvInOut:             firstPassRegressionExpectMVInOut2,
			newMVCount:          firstPassRegressionExpectNewMV2,
		},
	}

	const tol = 1e-6
	closeTo := func(got, exp float64) bool {
		if math.IsNaN(got) || math.IsNaN(exp) {
			return got == exp
		}
		return math.Abs(got-exp) <= tol*math.Max(1.0, math.Abs(exp))
	}

	for i := range want {
		w := want[i]
		g := stats[i]
		if !closeTo(g.IntraError, w.intraError) {
			t.Errorf("frame %d IntraError = %v, want %v", i, g.IntraError, w.intraError)
		}
		if !closeTo(g.CodedError, w.codedError) {
			t.Errorf("frame %d CodedError = %v, want %v", i, g.CodedError, w.codedError)
		}
		if !closeTo(g.SSIMWeightedPredErr, w.ssimWeightedPredErr) {
			t.Errorf("frame %d SSIMWeightedPredErr = %v, want %v", i, g.SSIMWeightedPredErr, w.ssimWeightedPredErr)
		}
		if !closeTo(g.PcntInter, w.pcntInter) {
			t.Errorf("frame %d PcntInter = %v, want %v", i, g.PcntInter, w.pcntInter)
		}
		if !closeTo(g.PcntMotion, w.pcntMotion) {
			t.Errorf("frame %d PcntMotion = %v, want %v", i, g.PcntMotion, w.pcntMotion)
		}
		if !closeTo(g.PcntSecondRef, w.pcntSecondRef) {
			t.Errorf("frame %d PcntSecondRef = %v, want %v", i, g.PcntSecondRef, w.pcntSecondRef)
		}
		if !closeTo(g.PcntNeutral, w.pcntNeutral) {
			t.Errorf("frame %d PcntNeutral = %v, want %v", i, g.PcntNeutral, w.pcntNeutral)
		}
		if !closeTo(g.MVr, w.mvR) {
			t.Errorf("frame %d MVr = %v, want %v", i, g.MVr, w.mvR)
		}
		if !closeTo(g.MVrAbs, w.mvRAbs) {
			t.Errorf("frame %d MVrAbs = %v, want %v", i, g.MVrAbs, w.mvRAbs)
		}
		if !closeTo(g.MVc, w.mvC) {
			t.Errorf("frame %d MVc = %v, want %v", i, g.MVc, w.mvC)
		}
		if !closeTo(g.MVcAbs, w.mvCAbs) {
			t.Errorf("frame %d MVcAbs = %v, want %v", i, g.MVcAbs, w.mvCAbs)
		}
		if !closeTo(g.MVrv, w.mvRv) {
			t.Errorf("frame %d MVrv = %v, want %v", i, g.MVrv, w.mvRv)
		}
		if !closeTo(g.MVcv, w.mvCv) {
			t.Errorf("frame %d MVcv = %v, want %v", i, g.MVcv, w.mvCv)
		}
		if !closeTo(g.MVInOutCount, w.mvInOut) {
			t.Errorf("frame %d MVInOutCount = %v, want %v", i, g.MVInOutCount, w.mvInOut)
		}
		if !closeTo(g.NewMVCount, w.newMVCount) {
			t.Errorf("frame %d NewMVCount = %v, want %v", i, g.NewMVCount, w.newMVCount)
		}
	}
}

func TestFirstPassMotionSearchReturnsLibvpxSSECost(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 200, 128, 128)
	ref := testVP8Frame(t, 16, 16, 199, 128, 128)
	source := sourceImageFromPublic(src)

	mv, cost, ok := firstPassMotionSearch(source, &ref.Img, 0, 0, vp8enc.MotionVector{}, 20)

	if !ok {
		t.Fatalf("firstPassMotionSearch returned ok=false")
	}
	if !mv.IsZero() {
		t.Fatalf("first-pass MV = %+v, want zero", mv)
	}
	want := macroblockLumaSSE(source, &ref.Img, 0, 0, vp8enc.MotionVector{}) +
		interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, 20, &vp8tables.DefaultMVContext)
	if cost != want {
		t.Fatalf("first-pass cost = %d, want SSE cost %d", cost, want)
	}
	variance, _ := macroblockLumaMotionVarianceSSE(source, &ref.Img, 0, 0, vp8enc.MotionVector{})
	if cost == variance {
		t.Fatalf("first-pass cost = variance %d, want libvpx vpx_mse16x16/SSE", variance)
	}
}

// TestSimpleWeightLumaMatchesLibvpxTable spot-checks the weight_table
// boundaries against vp8/encoder/firstpass.c weight_table[256]:
//   - codes 0..32 pin to 0.02
//   - codes 33..63 ramp linearly: weight[i] = (i-32)/32
//   - codes 64..255 pin to 1.0
func TestSimpleWeightLumaMatchesLibvpxTable(t *testing.T) {
	cases := []struct {
		code byte
		want float64
	}{
		{0, 0.02},
		{32, 0.02},
		{33, 1.0 / 32.0},
		{48, 16.0 / 32.0},
		{63, 31.0 / 32.0},
		{64, 1.0},
		{200, 1.0},
		{255, 1.0},
	}
	for _, tc := range cases {
		if firstPassWeightTable[tc.code] != tc.want {
			t.Errorf("firstPassWeightTable[%d] = %v, want %v", tc.code, firstPassWeightTable[tc.code], tc.want)
		}
	}
}

// TestTwoPassFramesToKeyReturnsZeroWhenStatsMissing pins the libvpx
// fallback when stats are not loaded.
func TestTwoPassFramesToKeyReturnsZeroWhenStatsMissing(t *testing.T) {
	var ts twoPassState
	if got := ts.framesToKey(0, 60); got != 0 {
		t.Fatalf("framesToKey with no stats = %d, want 0", got)
	}
}

// TestTwoPassFramesToKeyClampsAtKeyFrameInterval pins the libvpx
// `if (frames_to_key >= keyFrameInterval) break;` clamp: with no
// scene-cut signal in the synthetic stats, framesToKey should not
// exceed the configured interval.
func TestTwoPassFramesToKeyClampsAtKeyFrameInterval(t *testing.T) {
	stats := make([]FirstPassFrameStats, 100)
	for i := range stats {
		// Boring stats that never trigger libvpxTestCandidateKeyFrame.
		stats[i] = FirstPassFrameStats{IntraError: 1000, CodedError: 1000, PcntInter: 0.95}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	got := ts.framesToKey(0, 30)
	if got > 31 {
		t.Fatalf("framesToKey with 30-frame interval = %d, want <= 31", got)
	}
	if got < 30 {
		t.Fatalf("framesToKey with 30-frame interval = %d, want >= 30 (no early KF predicate fires)", got)
	}
}

// TestTwoPassFramesToKeyClampsAtTwoIntervalsForAutoKey pins the libvpx
// `if (frames_to_key >= 2*key_freq) break;` outer clamp by passing
// keyFrameInterval=10 and verifying the result is <= 20.
func TestTwoPassFramesToKeyClampsAtTwoIntervalsForAutoKey(t *testing.T) {
	stats := make([]FirstPassFrameStats, 100)
	for i := range stats {
		stats[i] = FirstPassFrameStats{IntraError: 1000, CodedError: 1000, PcntInter: 0.95}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	if got := ts.framesToKey(0, 10); got > 20 {
		t.Fatalf("framesToKey with 10-frame interval = %d, want <= 20", got)
	}
}

// TestTwoPassAltRefBitChargeDoesNotAdvanceStats pins libvpx Pass2Encode:
// hidden ARF packets subtract from twopass.bits_left, but because
// refresh_alt_ref_frame skips vp8_second_pass and show_frame is false they do
// not consume the visible-frame first-pass stats index.
func TestTwoPassAltRefBitChargeDoesNotAdvanceStats(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)

	initialBits := ts.bitsLeft
	initialError := ts.errorLeft
	frame0Error := ts.modifiedError(stats[0])

	ts.chargeAltRefFrameBits(123)
	if ts.frameIndex != 0 {
		t.Fatalf("frameIndex after hidden ARF charge = %d, want 0", ts.frameIndex)
	}
	if ts.errorLeft != initialError {
		t.Fatalf("errorLeft after hidden ARF charge = %v, want unchanged %v", ts.errorLeft, initialError)
	}
	if ts.bitsLeft != initialBits-123 {
		t.Fatalf("bitsLeft after hidden ARF charge = %d, want %d", ts.bitsLeft, initialBits-123)
	}

	ts.finishFrame(77)
	if ts.frameIndex != 1 {
		t.Fatalf("frameIndex after visible frame = %d, want 1", ts.frameIndex)
	}
	if ts.errorLeft != initialError-frame0Error {
		t.Fatalf("errorLeft after visible frame = %v, want %v", ts.errorLeft, initialError-frame0Error)
	}
	wantBitsLeft := initialBits - 123 - 77 + int64(vbrMinFrameBandwidthBits(1000, 50))
	if ts.bitsLeft != wantBitsLeft {
		t.Fatalf("bitsLeft after visible frame = %d, want %d", ts.bitsLeft, wantBitsLeft)
	}
}

// TestTwoPassKFGroupModifiedErrorMatchesSumOfFrames pins libvpx's
// inner accumulator: `kf_group_err += calculate_modified_err(this_frame)`
// across the KF group.
func TestTwoPassKFGroupModifiedErrorMatchesSumOfFrames(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	want := twoPassModifiedError(stats[0], 50) + twoPassModifiedError(stats[1], 50) + twoPassModifiedError(stats[2], 50)
	if got := ts.kfGroupModifiedError(0, 3); got != want {
		t.Fatalf("kfGroupModifiedError = %v, want %v", got, want)
	}
}

// TestTwoPassKFGroupBitsAllocatesByErrorRatio pins the libvpx allocation
//
//	kf_group_bits = bits_left * (kf_group_err / modified_error_left)
//
// clamped at max_bits_per_frame * frames_to_key.
func TestTwoPassKFGroupBitsAllocatesByErrorRatio(t *testing.T) {
	stats := []FirstPassFrameStats{
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
		{IntraError: 1500, CodedError: 200, PcntInter: 0.85},
		{IntraError: 800, CodedError: 50, PcntInter: 0.95},
		{IntraError: 1000, CodedError: 100, PcntInter: 0.9},
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	groupErr := ts.kfGroupModifiedError(0, 3)
	want := int64(float64(ts.bitsLeft) * (groupErr / ts.errorLeft))
	if got := ts.kfGroupBits(0, 3, 0); got != want {
		t.Fatalf("kfGroupBits without cap = %d, want %d", got, want)
	}
	// With max_bits_per_frame=100 and frames_to_key=3, the cap is 300.
	if got := ts.kfGroupBits(0, 3, 100); got > 300 {
		t.Fatalf("kfGroupBits with cap=100*3 = %d, want <= 300", got)
	}
}

// TestLibvpxEstimateMaxQReturnsMaxOnZeroBudget pins libvpx's
// `if (section_target_bandwidth <= 0) return maxq_max_limit` early
// exit.
func TestLibvpxEstimateMaxQReturnsMaxOnZeroBudget(t *testing.T) {
	got := libvpxEstimateMaxQ(1500, 0, 0, 100.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 127 {
		t.Fatalf("estimate_max_q with zero budget = %d, want maxq_max_limit=127", got)
	}
}

// TestLibvpxEstimateMaxQFindsLowestQAcceptingBudget pins libvpx's
// loop semantics: walk Q from min upward, return the first Q whose
// bits_per_mb_at_q <= target_norm_bits_per_mb. Use a generous budget
// so a low Q satisfies it.
func TestLibvpxEstimateMaxQFindsLowestQAcceptingBudget(t *testing.T) {
	// num_mbs=1500, section_target_bandwidth=10_000_000 -> very large
	// per-MB budget; even Q=0 should pass.
	got := libvpxEstimateMaxQ(1500, 10_000_000, 0, 50.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 0 {
		t.Fatalf("estimate_max_q with very large budget = %d, want 0", got)
	}
}

// TestLibvpxEstimateMaxQReturnsMaxWhenBudgetTooSmall pins the
// fall-through return when no Q satisfies the per-MB target.
func TestLibvpxEstimateMaxQReturnsMaxWhenBudgetTooSmall(t *testing.T) {
	// Tiny target bits relative to err_per_mb=10000 forces fallthrough.
	got := libvpxEstimateMaxQ(1500, 1500, 0, 10000.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 127 {
		t.Fatalf("estimate_max_q with tight budget = %d, want maxq_max_limit=127", got)
	}
}

// TestLibvpxEstimateMaxQHonoursMinLimitAsFloor pins libvpx's
// `for (Q = maxq_min_limit; Q < maxq_max_limit; ...)` floor: the
// search never returns below maxq_min_limit.
func TestLibvpxEstimateMaxQHonoursMinLimitAsFloor(t *testing.T) {
	got := libvpxEstimateMaxQ(1500, 10_000_000, 0, 50.0, 1.0, 1.0, 1.0, 30, 127)
	if got != 30 {
		t.Fatalf("estimate_max_q with min_limit=30 = %d, want 30", got)
	}
}

// TestLibvpxGetPredictionDecayRateMatchesLibvpxFormula pins the libvpx
// vp8/encoder/firstpass.c get_prediction_decay_rate computation.
// With pcnt_inter=0.9, pcnt_motion=0.2, mvr_abs=10, mvc_abs=10:
//
//	rate = 0.9
//	motion_decay = 1.0 - 0.2/20 = 0.99 (no clamp, 0.9 < 0.99).
//	mv_rabs = |10 * 0.2| = 2; mv_cabs = 2.
//	distance_factor = sqrt(4+4)/250 = 2.828/250 = 0.01131.
//	distance_factor = 1.0 - 0.01131 = 0.9887.
//	rate stays at 0.9 (rate < distance_factor).
func TestLibvpxGetPredictionDecayRateMatchesLibvpxFormula(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.9,
		PcntMotion: 0.2,
		MVrAbs:     10,
		MVcAbs:     10,
	}
	got := libvpxGetPredictionDecayRate(stats)
	if math.Abs(got-0.9) > 1e-9 {
		t.Fatalf("prediction_decay_rate = %v, want ~0.9", got)
	}
}

// TestLibvpxGetPredictionDecayRateLargeMVZerosOut pins the libvpx
// `(distance_factor > 1.0) ? 0.0 : (1.0 - distance_factor)` clamp.
// Large MVs produce distance_factor > 1, which becomes 0.0 and
// dominates the min.
func TestLibvpxGetPredictionDecayRateLargeMVZerosOut(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.9,
		PcntMotion: 1.0,
		MVrAbs:     500,
		MVcAbs:     500,
	}
	got := libvpxGetPredictionDecayRate(stats)
	if got != 0.0 {
		t.Fatalf("large-MV decay rate = %v, want 0.0", got)
	}
}

// TestLibvpxGetPredictionDecayRateMotionDecayClamps pins the libvpx
// `motion_decay = 1.0 - pcnt_motion/20` floor when motion_decay
// becomes the dominant term. With pcnt_inter=0.99, pcnt_motion=10,
// motion_decay=0.5 -> rate clamps to 0.5.
func TestLibvpxGetPredictionDecayRateMotionDecayClamps(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.99,
		PcntMotion: 10.0,
		MVrAbs:     1,
		MVcAbs:     1,
	}
	got := libvpxGetPredictionDecayRate(stats)
	// motion_decay = 1.0 - 10/20 = 0.5.
	// distance_factor = sqrt(100+100)/250 = 14.14/250 = 0.0566 -> 0.9434.
	// rate = min(0.99, 0.5, 0.9434) = 0.5.
	if math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("motion-decay clamp = %v, want 0.5", got)
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnShortInterval pins
// the libvpx `frame_interval > MIN_GF_INTERVAL` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnShortInterval(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(libvpxMinGFInterval, 3, 0.999, 0.5, rates) {
		t.Fatalf("frame_interval == MIN_GF_INTERVAL should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnLowDecayRate pins
// the libvpx `loop_decay_rate >= 0.999` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnLowDecayRate(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.95, 0.5, rates) {
		t.Fatalf("loop_decay_rate < 0.999 should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnHighDecayAccum pins
// the libvpx `decay_accumulator < 0.9` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnHighDecayAccum(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.999, 0.95, rates) {
		t.Fatalf("decay_accumulator >= 0.9 should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsTrueOnAllStill pins the
// happy-path: long interval, low accumulator, high decay rate, and
// all next-still rates >= 0.999 -> transition_to_still=true.
func TestLibvpxDetectTransitionToStillReturnsTrueOnAllStill(t *testing.T) {
	rates := []float64{0.999, 1.0, 1.0}
	if !libvpxDetectTransitionToStill(10, 3, 0.999, 0.5, rates) {
		t.Fatalf("all-still lookahead should fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnLookaheadDip pins the
// libvpx loop break: any lookahead frame with decay_rate < 0.999
// breaks the transition-still detection.
func TestLibvpxDetectTransitionToStillReturnsFalseOnLookaheadDip(t *testing.T) {
	rates := []float64{1.0, 0.95, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.999, 0.5, rates) {
		t.Fatalf("middle dip should break the lookahead loop")
	}
}

// TestLibvpxCalculateModifiedErrMatchesLibvpxFormula pins libvpx's
//
//	modified_err = av_err * pow(this_err/av_err, vbrbias/100)
//
// where av_err = total_ssim/count.
func TestLibvpxCalculateModifiedErrMatchesLibvpxFormula(t *testing.T) {
	got := libvpxCalculateModifiedErr(200.0, 1000.0, 10, 50)
	avErr := 1000.0 / 10
	want := avErr * math.Pow(200.0/avErr, 0.5)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calculate_modified_err = %v, want ~%v", got, want)
	}
}

// TestLibvpxCalculateModifiedErrZeroCountReturnsZero pins libvpx's
// safe-fallback when count==0 (governed by govpx; libvpx does not
// guard, but govpx's helper protects against /0).
func TestLibvpxCalculateModifiedErrZeroCountReturnsZero(t *testing.T) {
	if got := libvpxCalculateModifiedErr(200.0, 1000.0, 0, 50); got != 0 {
		t.Fatalf("count=0 = %v, want 0", got)
	}
}

// TestLibvpxCalculateModifiedErrZeroAvErrUsesDoubleDivideCheck pins
// the libvpx DOUBLE_DIVIDE_CHECK fallback: when av_err is ~0, the
// helper substitutes 1.0 in the denominator (so modified_err =
// av_err * pow(this_err, vbrbias/100), but with av_err near 0 the
// product is also near 0).
func TestLibvpxCalculateModifiedErrZeroAvErrUsesDoubleDivideCheck(t *testing.T) {
	got := libvpxCalculateModifiedErr(50.0, 0.0, 10, 50)
	// av_err = 0 -> modified = 0 * pow(50/1, 0.5) = 0. Doesn't blow up.
	if got != 0 {
		t.Fatalf("av_err=0 = %v, want 0", got)
	}
}

// TestLibvpxEstimateQReturnsMaxOnZeroBudget pins libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ` early exit (govpx
// uses vp8MaxQIndex as the libvpx MAXQ analog).
func TestLibvpxEstimateQReturnsMaxOnZeroBudget(t *testing.T) {
	got := libvpxEstimateQ(1500, 0, 100.0, 1.0, 1.0)
	if got != vp8MaxQIndex {
		t.Fatalf("estimate_q with zero budget = %d, want %d", got, vp8MaxQIndex)
	}
}

// TestLibvpxEstimateQFindsLowestQAcceptingBudget pins the libvpx
// estimate_q loop returning the lowest Q whose bits_per_mb_at_q is
// at or below the per-MB target.
func TestLibvpxEstimateQFindsLowestQAcceptingBudget(t *testing.T) {
	got := libvpxEstimateQ(1500, 10_000_000, 50.0, 1.0, 1.0)
	if got != 0 {
		t.Fatalf("estimate_q with very large budget = %d, want 0", got)
	}
}

// TestLibvpxEstimateKFGroupQReturnsDoubleMaxOnEmptyBudget pins libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ * 2;` early exit.
func TestLibvpxEstimateKFGroupQReturnsDoubleMaxOnEmptyBudget(t *testing.T) {
	got := libvpxEstimateKFGroupQ(1500, 0, 100.0, 5.0, 50, 0, 0, 1.0)
	want := (vp8MaxQIndex + 1) * 2
	if got != want {
		t.Fatalf("estimate_kf_group_q with zero budget = %d, want %d", got, want)
	}
}

// TestLibvpxEstimateKFGroupQOvershootIncrementsBeyondMax pins the
// libvpx tail loop that bumps Q (and shrinks bits_per_mb_at_q by
// 0.96 each step) when no Q in [0, MAXQ) satisfies the budget.
func TestLibvpxEstimateKFGroupQOvershootIncrementsBeyondMax(t *testing.T) {
	// Use a tiny budget with high err_per_mb so even at Q=MAXQ the
	// bits are still above target. Q should overshoot MAXQ.
	got := libvpxEstimateKFGroupQ(1500, 1500, 100000.0, 5.0, 50, 1000, 1000, 1.0)
	if got <= vp8MaxQIndex {
		t.Fatalf("estimate_kf_group_q overshoot = %d, want > MAXQ=%d", got, vp8MaxQIndex)
	}
	if got >= (vp8MaxQIndex+1)*2 {
		t.Fatalf("estimate_kf_group_q overshoot = %d, want < MAXQ*2", got)
	}
}

// TestLibvpxEstimateKFGroupQSpendRatioFallback pins the libvpx
// `if (long_rolling_target_bits <= 0) current_spend_ratio = 10.0`
// fallback: caller passes 0 for long_rolling_target_bits and the
// helper still returns a sane Q.
func TestLibvpxEstimateKFGroupQSpendRatioFallback(t *testing.T) {
	got := libvpxEstimateKFGroupQ(1500, 100_000_000, 50.0, 5.0, 50, 0, 0, 1.0)
	if got < 0 || got > (vp8MaxQIndex+1)*2 {
		t.Fatalf("estimate_kf_group_q with long_rolling_target=0 returned out-of-range Q=%d", got)
	}
}

// TestLibvpxCalcCorrectionFactorMatchesLibvpxFormula pins the libvpx
// vp8/encoder/firstpass.c calc_correction_factor:
//
//	error_term = err_per_mb / err_devisor
//	power_term = min(pt_low + Q*0.01, pt_high)
//	cf = pow(error_term, power_term)
//	clamp(cf, 0.05, 5.0)
func TestLibvpxCalcCorrectionFactorMatchesLibvpxFormula(t *testing.T) {
	// err_per_mb=300, err_devisor=150 -> error_term=2.0.
	// Q=20 -> power_term = 0.40 + 20*0.01 = 0.60 (< 0.90 cap).
	// cf = pow(2.0, 0.60) ~ 1.5157.
	got := libvpxCalcCorrectionFactor(300.0, 150.0, 0.40, 0.90, 20)
	want := math.Pow(2.0, 0.60)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calc_correction_factor = %v, want ~%v", got, want)
	}
}

// TestLibvpxCalcCorrectionFactorClampsBelow005 pins the lower clamp
// at 0.05.
func TestLibvpxCalcCorrectionFactorClampsBelow005(t *testing.T) {
	// err_per_mb=1, err_devisor=1e6 -> error_term=1e-6.
	// power_term=0.4. cf = pow(1e-6, 0.4) ~ 0.00398 -> clamped to 0.05.
	got := libvpxCalcCorrectionFactor(1.0, 1e6, 0.40, 0.90, 0)
	if got != 0.05 {
		t.Fatalf("calc_correction_factor lower clamp = %v, want 0.05", got)
	}
}

// TestLibvpxCalcCorrectionFactorClampsAbove50 pins the upper clamp at 5.0.
func TestLibvpxCalcCorrectionFactorClampsAbove50(t *testing.T) {
	// err_per_mb=1e6, err_devisor=1 -> error_term=1e6. cf will exceed 5.0.
	got := libvpxCalcCorrectionFactor(1e6, 1.0, 0.40, 0.90, 100)
	if got != 5.0 {
		t.Fatalf("calc_correction_factor upper clamp = %v, want 5.0", got)
	}
}

// TestLibvpxCalcCorrectionFactorClampsPowerTermAtPtHigh pins the
// `power_term = (power_term > pt_high) ? pt_high : power_term` cap.
// At Q=200, raw power_term = 0.4 + 2.0 = 2.4, clamped to pt_high=0.90.
func TestLibvpxCalcCorrectionFactorClampsPowerTermAtPtHigh(t *testing.T) {
	// err_per_mb=300, err_devisor=150 -> error_term=2.0.
	// raw power_term=0.4+2.0=2.4 -> clamped to 0.90.
	// cf = pow(2.0, 0.90) ~ 1.866.
	got := libvpxCalcCorrectionFactor(300.0, 150.0, 0.40, 0.90, 200)
	want := math.Pow(2.0, 0.90)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calc_correction_factor with clamped power = %v, want ~%v", got, want)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioBelowOneShrinks
// pins the libvpx
// `if (rolling_ratio < 0.95) est_max_qcorrection_factor -= 0.005`
// branch.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioBelowOneShrinks(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 900, 1000)
	if got != 0.995 {
		t.Fatalf("ratio<0.95 update = %v, want 0.995", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioAboveOneGrows pins
// the libvpx `if (rolling_ratio > 1.05) factor += 0.005` branch.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioAboveOneGrows(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 1100, 1000)
	if got != 1.005 {
		t.Fatalf("ratio>1.05 update = %v, want 1.005", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentClamps01To10 pins the
// libvpx `clamp(factor, 0.1, 10.0)` clamp.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentClamps01To10(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(0.05, 900, 1000)
	if got != 0.1 {
		t.Fatalf("lower clamp = %v, want 0.1", got)
	}
	got = libvpxEstimateMaxQRollingRatioAdjustment(20.0, 1100, 1000)
	if got != 10.0 {
		t.Fatalf("upper clamp = %v, want 10.0", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentInRangeNoChange pins
// the libvpx `else` branch: 0.95 <= ratio <= 1.05 leaves the factor
// unchanged.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentInRangeNoChange(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 1000, 1000)
	if got != 1.0 {
		t.Fatalf("in-range update = %v, want 1.0", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentZeroTargetIsNoOp pins
// the libvpx `if (rolling_target_bits > 0)` outer guard.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentZeroTargetIsNoOp(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(2.5, 1000, 0)
	if got != 2.5 {
		t.Fatalf("zero-target update = %v, want 2.5 unchanged", got)
	}
}

// TestLibvpxSectionStatsAccumulatesAndAverages pins the libvpx
// FIRSTPASS_STATS accumulate_stats/avg_stats pattern: addFrame sums,
// avg divides by count.
func TestLibvpxSectionStatsAccumulatesAndAverages(t *testing.T) {
	var s libvpxSectionStats
	s.addFrame(1000, 100)
	s.addFrame(2000, 200)
	s.addFrame(3000, 300)
	s.avg()
	if s.sectionIntra != 2000 || s.sectionCoded != 200 {
		t.Fatalf("avg = (%v, %v), want (2000, 200)", s.sectionIntra, s.sectionCoded)
	}
}

// TestLibvpxSectionIntraRatingDivisionByZeroFallback pins libvpx's
// DOUBLE_DIVIDE_CHECK fallback: when sectionCoded is ~0, the helper
// substitutes 1.0 so the division does not blow up.
func TestLibvpxSectionIntraRatingDivisionByZeroFallback(t *testing.T) {
	if got := libvpxSectionIntraRating(100.0, 0.0); got != 100 {
		t.Fatalf("intra_rating with coded=0 = %d, want 100 (DOUBLE_DIVIDE_CHECK fallback)", got)
	}
}

// TestLibvpxSectionIntraRatingTruncatesToInt pins the libvpx
// (unsigned int) cast.
func TestLibvpxSectionIntraRatingTruncatesToInt(t *testing.T) {
	if got := libvpxSectionIntraRating(1500.0, 200.0); got != 7 {
		t.Fatalf("intra_rating(1500,200) = %d, want 7 (truncated 7.5)", got)
	}
}

// TestLibvpxSectionMaxQFactorMatchesLibvpxFormula pins the libvpx
// section_max_qfactor formula with the 0.80 floor:
//
//	factor = 1.0 - (Ratio - 10.0) * 0.025
//	clamp(factor, 0.80, +inf)
func TestLibvpxSectionMaxQFactorMatchesLibvpxFormula(t *testing.T) {
	// Ratio=12 -> factor = 1.0 - 2.0*0.025 = 0.95 (no floor).
	if got := libvpxSectionMaxQFactor(1200.0, 100.0); got != 0.95 {
		t.Fatalf("section_max_qfactor(ratio=12) = %v, want 0.95", got)
	}
	// Ratio=10 -> factor = 1.0 (no scaling).
	if got := libvpxSectionMaxQFactor(1000.0, 100.0); got != 1.0 {
		t.Fatalf("section_max_qfactor(ratio=10) = %v, want 1.0", got)
	}
	// Ratio=20 -> factor = 1.0 - 10*0.025 = 0.75 -> floored to 0.80.
	if got := libvpxSectionMaxQFactor(2000.0, 100.0); got != 0.80 {
		t.Fatalf("section_max_qfactor(ratio=20) = %v, want 0.80 (floored)", got)
	}
}

// TestLibvpxSectionMaxQFactorDivisionByZeroFallback pins the
// DOUBLE_DIVIDE_CHECK fallback path.
func TestLibvpxSectionMaxQFactorDivisionByZeroFallback(t *testing.T) {
	if got := libvpxSectionMaxQFactor(10.0, 0.0); got != 1.0 {
		t.Fatalf("section_max_qfactor(coded=0, intra=10) = %v, want 1.0", got)
	}
}

// TestLibvpxAssignStdFrameBitsErrorFraction pins the libvpx
// vp8/encoder/firstpass.c assign_std_frame_bits formula:
//
//	target = gf_group_bits * (modified_err / gf_group_error_left)
//	      + min_frame_bandwidth
//
// With modified_err=20, gf_group_error_left=100, gf_group_bits=10000,
// target = int(10000 * 0.2) + 0 = 2000.
func TestLibvpxAssignStdFrameBitsErrorFraction(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 0, 0, 0)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits = %d, want 2000", got)
	}
}

// TestLibvpxAssignStdFrameBitsClampAtMaxBits pins the libvpx
// `if (target > max_bits) target = max_bits` clamp.
func TestLibvpxAssignStdFrameBitsClampAtMaxBits(t *testing.T) {
	got := libvpxAssignStdFrameBits(50.0, 100.0, 10000, 1500, 0, 0, 0, 0)
	if got != 1500 {
		t.Fatalf("assign_std_frame_bits with max_bits cap = %d, want 1500", got)
	}
}

// TestLibvpxAssignStdFrameBitsClampAtGFGroupBits pins the libvpx
// `if (target > gf_group_bits) target = gf_group_bits` clamp.
func TestLibvpxAssignStdFrameBitsClampAtGFGroupBits(t *testing.T) {
	got := libvpxAssignStdFrameBits(200.0, 100.0, 10000, 0, 0, 0, 0, 0)
	if got != 10000 {
		t.Fatalf("assign_std_frame_bits with gf_group_bits cap = %d, want 10000", got)
	}
}

// TestLibvpxAssignStdFrameBitsAddsMinFrameBandwidth pins the libvpx
// `target += min_frame_bandwidth` add.
func TestLibvpxAssignStdFrameBitsAddsMinFrameBandwidth(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 500, 0, 0, 0)
	if got != 2500 {
		t.Fatalf("assign_std_frame_bits with min_frame_bandwidth = %d, want 2500", got)
	}
}

// TestLibvpxAssignStdFrameBitsAltExtraOnOddFrames pins the libvpx
// `if ((frames_since_golden & 1) && frames_till_gf_update_due > 0)`
// alt_extra_bits add.
func TestLibvpxAssignStdFrameBitsAltExtraOnOddFrames(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 1, 5, 100)
	if got != 2100 {
		t.Fatalf("assign_std_frame_bits with alt_extra (odd) = %d, want 2100", got)
	}
	got = libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 2, 5, 100)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits with alt_extra (even) = %d, want 2000", got)
	}
	got = libvpxAssignStdFrameBits(20.0, 100.0, 10000, 0, 0, 1, 0, 100)
	if got != 2000 {
		t.Fatalf("assign_std_frame_bits with alt_extra (no update due) = %d, want 2000", got)
	}
}

// TestLibvpxAssignStdFrameBitsReturnsZeroOnEmptyGroup pins the libvpx
// `if (gf_group_bits <= 0) return 0` guard.
func TestLibvpxAssignStdFrameBitsReturnsZeroOnEmptyGroup(t *testing.T) {
	if got := libvpxAssignStdFrameBits(20.0, 100.0, 0, 1500, 500, 0, 0, 0); got != 0 {
		t.Fatalf("assign_std_frame_bits with gf_group_bits=0 = %d, want 0", got)
	}
}

// TestLibvpxAssignStdFrameBitsZeroErrorFractionUsesMinOnly pins the
// libvpx `if (gf_group_error_left <= 0) err_fraction = 0` branch.
func TestLibvpxAssignStdFrameBitsZeroErrorFractionUsesMinOnly(t *testing.T) {
	got := libvpxAssignStdFrameBits(20.0, 0, 10000, 0, 500, 0, 0, 0)
	if got != 500 {
		t.Fatalf("assign_std_frame_bits with err_left=0 = %d, want 500", got)
	}
}

// TestLibvpxFrameMaxBitsCBRBasicAllocation pins the libvpx CBR
// branch of frame_max_bits when buffer is at optimal:
//
//	max_bits = av_per_frame_bandwidth * vbrmax / 100.
func TestLibvpxFrameMaxBitsCBRBasicAllocation(t *testing.T) {
	got := libvpxFrameMaxBitsCBR(1000, 200, 5000, 5000)
	if got != 2000 {
		t.Fatalf("frame_max_bits CBR optimal = %d, want 2000", got)
	}
}

// TestLibvpxFrameMaxBitsCBRScalesWithBufferRatio pins the libvpx
// buffer-fullness scaling: when buffer_level < optimal, max_bits is
// scaled by (buffer_level / optimal), with a floor of
// min(av_per_frame_bandwidth>>2, max_bits>>2 (pre-scale)).
func TestLibvpxFrameMaxBitsCBRScalesWithBufferRatio(t *testing.T) {
	// av=1000, vbrmax=200 -> max_bits=2000 pre-scale.
	// buffer=2500, optimal=5000 -> ratio=0.5 -> max_bits=1000.
	// min_floor = min(1000>>2=250, 2000>>2=500) = 250. 1000 > 250.
	got := libvpxFrameMaxBitsCBR(1000, 200, 2500, 5000)
	if got != 1000 {
		t.Fatalf("frame_max_bits CBR half-buffer = %d, want 1000", got)
	}
}

// TestLibvpxFrameMaxBitsCBRFloorsAtMinMaxBits pins the libvpx
// `if (max_bits < min_max_bits) max_bits = min_max_bits` floor.
func TestLibvpxFrameMaxBitsCBRFloorsAtMinMaxBits(t *testing.T) {
	// av=1000, vbrmax=200 -> max_bits=2000 pre-scale.
	// buffer=100, optimal=5000 -> ratio=0.02 -> max_bits=40.
	// min_floor = min(250, 500) = 250. 40 < 250 -> 250.
	got := libvpxFrameMaxBitsCBR(1000, 200, 100, 5000)
	if got != 250 {
		t.Fatalf("frame_max_bits CBR low-buffer floor = %d, want 250", got)
	}
}

// TestLibvpxFrameMaxBitsVBRBasicAllocation pins the libvpx VBR branch:
//
//	max_bits = (bits_left / frames_left) * vbrmax / 100.
func TestLibvpxFrameMaxBitsVBRBasicAllocation(t *testing.T) {
	// bits_left=100000, frames_left=100 -> per-frame=1000.
	// vbrmax=200 -> max_bits = int(1000 * 2.0) = 2000.
	got := libvpxFrameMaxBitsVBR(100000, 100, 200)
	if got != 2000 {
		t.Fatalf("frame_max_bits VBR = %d, want 2000", got)
	}
}

// TestTwoPassFrameTargetBitsUsesLiveVBRMaxWhenBudgetAhead pins the
// libvpx second-pass VBR ceiling:
//
//	max_bits = (bits_left / frames_left) * two_pass_vbrmax_section / 100
//
// When the encode is ahead of budget, the cap rises with bits_left
// instead of staying pinned to the initial average frame target.
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
	if ts.bitsLeft != 2000 {
		t.Fatalf("bitsLeft = %d, want two real frames only", ts.bitsLeft)
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

func makeTwoPassSpikyStats(count int) []FirstPassFrameStats {
	stats := make([]FirstPassFrameStats, count)
	for i := range stats {
		stats[i] = FirstPassFrameStats{CodedError: 1}
	}
	if len(stats) > 0 {
		stats[0].CodedError = 1000000
	}
	return stats
}

// TestLibvpxFrameMaxBitsReturnsZeroForExhaustedInputs pins the
// guards: zero/negative bits_left, frames_left, vbrmax_section, or
// av_per_frame_bandwidth all return 0.
func TestLibvpxFrameMaxBitsReturnsZeroForExhaustedInputs(t *testing.T) {
	if got := libvpxFrameMaxBitsCBR(0, 200, 5000, 5000); got != 0 {
		t.Fatalf("CBR av=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsCBR(1000, 0, 5000, 5000); got != 0 {
		t.Fatalf("CBR vbrmax=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsVBR(0, 100, 200); got != 0 {
		t.Fatalf("VBR bits_left=0 -> %d, want 0", got)
	}
	if got := libvpxFrameMaxBitsVBR(100000, 0, 200); got != 0 {
		t.Fatalf("VBR frames_left=0 -> %d, want 0", got)
	}
}

// TestLibvpxGFGroupBitsAllocatesByErrorRatio pins the libvpx
// gf_group_bits = kf_group_bits * (gf_group_err / kf_group_error_left)
// with the kf_group_bits ceiling.
func TestLibvpxGFGroupBitsAllocatesByErrorRatio(t *testing.T) {
	got := libvpxGFGroupBits(10000, 30.0, 100.0, 0, 0)
	want := int64(10000.0 * (30.0 / 100.0))
	if got != want {
		t.Fatalf("libvpxGFGroupBits = %d, want %d", got, want)
	}
}

// TestLibvpxGFGroupBitsCapsAtKFGroupBits pins the libvpx clamp:
// gf_group_bits cannot exceed kf_group_bits even if the error ratio
// exceeds 1.0.
func TestLibvpxGFGroupBitsCapsAtKFGroupBits(t *testing.T) {
	got := libvpxGFGroupBits(1000, 200.0, 100.0, 0, 0)
	if got != 1000 {
		t.Fatalf("libvpxGFGroupBits with err_ratio>1 = %d, want kf_group_bits=1000", got)
	}
}

// TestLibvpxGFGroupBitsClampsAtMaxBits pins the libvpx
// `max_bits * baseline_gf_interval` ceiling.
func TestLibvpxGFGroupBitsClampsAtMaxBits(t *testing.T) {
	got := libvpxGFGroupBits(100000, 50.0, 100.0, 1000, 8)
	// raw = 50000, max=8000.
	if got != 8000 {
		t.Fatalf("libvpxGFGroupBits with max_bits cap = %d, want 8000", got)
	}
}

// TestLibvpxGFGroupBitsReturnsZeroWhenInputsZero pins the libvpx
// `if (kf_group_bits > 0 && kf_group_error_left > 0)` gate.
func TestLibvpxGFGroupBitsReturnsZeroWhenInputsZero(t *testing.T) {
	if got := libvpxGFGroupBits(0, 50.0, 100.0, 0, 0); got != 0 {
		t.Fatalf("kf_group_bits=0 -> %d, want 0", got)
	}
	if got := libvpxGFGroupBits(1000, 50.0, 0, 0, 0); got != 0 {
		t.Fatalf("kf_group_error_left=0 -> %d, want 0", got)
	}
}

// TestLibvpxGFBitsAllocationGoldenFrameMatchesLibvpx pins the libvpx
// GF (non-ARF) allocation. With gfu_boost=200, gfq_adjustment=128,
// baseline_gf_interval=8:
//
//	Boost = (200 * 128) / 100 = 256.
//	cap = 8 * 150 = 1200; 256 < 1200; floor 125 not active.
//	allocation_chunks = 8 * 100 + (256 - 100) = 956.
//	gf_bits = int(256 * (10000/956)) = int(256 * 10.46) = 2677.
func TestLibvpxGFBitsAllocationGoldenFrameMatchesLibvpx(t *testing.T) {
	got := libvpxGFBitsAllocation(false, 200, 128, 10000, 8)
	wantF := 256.0 * (10000.0 / 956.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation GF = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationARFMatchesLibvpx pins the libvpx ARF
// allocation: Boost = (gfu_boost * 3 * gfq_adjustment) / (2 * 100) +
// interval*50. With gfu_boost=200, gfq_adjustment=128, interval=8:
//
//	Boost = (200 * 3 * 128) / 200 + 400 = 384 + 400 = 784.
//	cap = (8+1)*200 = 1800; 784 < cap; floor 125 not active.
//	allocation_chunks = (8+1)*100 + 784 = 1684.
//	gf_bits = int(784 * (10000/1684)) = int(784 * 5.937) = 4654.
func TestLibvpxGFBitsAllocationARFMatchesLibvpx(t *testing.T) {
	got := libvpxGFBitsAllocation(true, 200, 128, 10000, 8)
	wantF := 784.0 * (10000.0 / 1684.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation ARF = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationAppliesBoostFloor pins the libvpx 125
// floor on Boost.
func TestLibvpxGFBitsAllocationAppliesBoostFloor(t *testing.T) {
	// Boost = (10 * 50) / 100 = 5; floor -> 125.
	got := libvpxGFBitsAllocation(false, 10, 50, 10000, 8)
	// allocation_chunks = 800 + (125-100) = 825. gf_bits = int(125 * 10000/825).
	wantF := 125.0 * (10000.0 / 825.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation with boost floor = %d, want %d", got, want)
	}
}

// TestLibvpxGFBitsAllocationHalvesLargeBoost pins the libvpx
// `while (Boost > 1000) Boost /= 2; allocation_chunks /= 2;` overflow
// guard.
func TestLibvpxGFBitsAllocationHalvesLargeBoost(t *testing.T) {
	// gfu_boost=2000, gfq_adjustment=200 -> Boost=4000 (before clamp).
	// Cap is interval*150 = 8*150 = 1200, so Boost clamps to 1200 first
	// (libvpx applies the cap *before* the halving). After cap=1200,
	// halving runs once: Boost=600, alloc_chunks=(800+1100)/2=950.
	got := libvpxGFBitsAllocation(false, 2000, 200, 10000, 8)
	// Boost=1200 (cap), alloc=800+1100=1900. Halve: B=600, alloc=950.
	// gf_bits = int(600*10000/950).
	wantF := 600.0 * (10000.0 / 950.0)
	want := int(wantF)
	if got != want {
		t.Fatalf("libvpxGFBitsAllocation halved = %d, want %d", got, want)
	}
}

// TestTwoPassKFGroupBitsReturnsZeroWhenBitsExhausted pins the libvpx
// `if (bits_left > 0 && modified_error_left > 0.0)` gate.
func TestTwoPassKFGroupBitsReturnsZeroWhenBitsExhausted(t *testing.T) {
	stats := []FirstPassFrameStats{{IntraError: 1000, CodedError: 100, PcntInter: 0.9}}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	ts.bitsLeft = 0
	if got := ts.kfGroupBits(0, 1, 0); got != 0 {
		t.Fatalf("kfGroupBits with bits_left=0 = %d, want 0", got)
	}
}

// TestTwoPassFramesToKeyHonoursTestCandidateKF pins the
// libvpxTestCandidateKeyFrame predicate firing inside framesToKey.
// Build stats where frame 6 is a clear scene cut (low intra/coded
// ratio drop) so the predicate fires after the MIN_GF_INTERVAL=4
// gate.
func TestTwoPassFramesToKeyHonoursTestCandidateKF(t *testing.T) {
	stats := make([]FirstPassFrameStats, 50)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 10000,
			CodedError: 100,
			PcntInter:  0.99,
		}
	}
	// Frame 6: simulate a scene cut by inverting intra/coded.
	for i := 6; i <= 12; i++ {
		stats[i] = FirstPassFrameStats{
			IntraError:    100,
			CodedError:    9000,
			PcntInter:     0.05,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
		}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	got := ts.framesToKey(0, 30)
	// Predicate-driven KF should fire well before the 30-frame floor.
	if got > 20 {
		t.Fatalf("framesToKey with scene cut at frame 6 = %d, want <= 20", got)
	}
	if got < libvpxMinGFInterval {
		t.Fatalf("framesToKey = %d, want >= MIN_GF_INTERVAL=%d", got, libvpxMinGFInterval)
	}
}

// firstPassRegression* values pin every libvpx-aligned FIRSTPASS_STATS field
// on the deterministic 32x32 ramp clip above. TestOracleFirstPassStatsCompare
// separately gates these values against empirical libvpx output with the small
// quality-equivalent tolerance used for predictor-residual rounding.
//
// Frame 0 has no LAST so MV stats are zero; coded_error == intra_error.
// LAST searches run against the reconstructed first-pass reference at libvpx's
// fixed pass-1 q=26, while encode_breakout raw checks use the separate prior
// source buffer. Frame 2 also sees the initial GOLDEN reference for the
// second-ref experiment.
//
// Computation walkthrough (see encoder_firstpass.go for line refs):
//   - 32x32 image -> 4 macroblocks (2x2)
//   - intrapenalty = 256
//   - intra_error = sum(vp8_encode_intra-style predictor SSE + 256) >> 8
//   - simple_weight averages weight_table over the ramp (most pixels above
//     code 64 -> weight 1.0); the actual average is captured in the SSIM
//     constants below.
const (
	firstPassRegressionExpectIntraError0    = 2557.0
	firstPassRegressionExpectIntraError1    = 2243.0
	firstPassRegressionExpectIntraError2    = 2132.0
	firstPassRegressionExpectCodedError1    = 32.0
	firstPassRegressionExpectCodedError2    = 39.0
	firstPassRegressionExpectSSIM0          = 2468.3259118652345
	firstPassRegressionExpectSSIM1          = 31.306640625
	firstPassRegressionExpectSSIM2          = 38.51678466796875
	firstPassRegressionExpectMVr1           = 18.0
	firstPassRegressionExpectMVrAbs1        = 18.0
	firstPassRegressionExpectMVc1           = -6.0
	firstPassRegressionExpectMVcAbs1        = 14.0
	firstPassRegressionExpectMVrv1          = 475.75
	firstPassRegressionExpectMVcv1          = 429.75
	firstPassRegressionExpectMVInOut1       = -0.5
	firstPassRegressionExpectNewMV1         = 4.0
	firstPassRegressionExpectPcntSecondRef2 = 0.75
	firstPassRegressionExpectMVr2           = 8.0
	firstPassRegressionExpectMVrAbs2        = 8.0
	firstPassRegressionExpectMVc2           = 8.0
	firstPassRegressionExpectMVcAbs2        = 8.0
	firstPassRegressionExpectMVrv2          = 92.0
	firstPassRegressionExpectMVcv2          = 92.0
	firstPassRegressionExpectMVInOut2       = -0.375
	firstPassRegressionExpectNewMV2         = 4.0
)

// TestPass2VBRSectionLimitClampsTarget pins the libvpx
// vp8/encoder/firstpass.c Pass2Encode VBR section-limit application:
// per-frame target is clamped to [section_min_bits, section_max_bits]
// where the section bounds derive from
// `cpi->oxcf.two_pass_vbrmin_section / two_pass_vbrmax_section`
// percentages applied to the live VBR per-frame budget. The test
// builds a synthetic two-pass state with a known frame target and
// section bounds and asserts the clamped output for both the
// upward-clamp (modified_err >> avg) and downward-clamp
// (modified_err << avg) directions.
func TestPass2VBRSectionLimitClampsTarget(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	const (
		perFrame = 1000
		biasPct  = 100
		minPct   = 50
		maxPct   = 150
	)
	var ts twoPassState
	ts.configure(stats, perFrame, biasPct, minPct, maxPct)
	// First frame error is huge (Spiky), so the err-fraction target
	// blows past sectionMax and must be clamped down.
	highMin, highMax := ts.pass2VBRSectionLimits(0, perFrame)
	if highMin != int64(perFrame*minPct/100) {
		t.Fatalf("section min = %d, want %d", highMin, perFrame*minPct/100)
	}
	wantMax := int64(libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), maxPct))
	if highMax != wantMax {
		t.Fatalf("section max = %d, want live VBR max %d", highMax, wantMax)
	}
	if got := ts.frameTargetBits(0, false, perFrame); int64(got) != wantMax {
		t.Fatalf("frame target with high err = %d, want clamped to section max %d", got, wantMax)
	}
	// Frame 1+ has tiny CodedError relative to total, so the
	// err-fraction target falls below sectionMin and must be clamped
	// up to the section min floor.
	lowTarget := ts.frameTargetBits(1, false, perFrame)
	wantLowMin, _ := ts.pass2VBRSectionLimits(1, perFrame)
	if int64(lowTarget) != wantLowMin {
		t.Fatalf("frame target with low err = %d, want clamped to section min %d", lowTarget, wantLowMin)
	}
}

// TestPass2ARFPendingTriggersFromHighMotionSection pins the libvpx
// vp8/encoder/firstpass.c `define_gf_group` / `select_arf_period`
// ARF-pending decision. A synthetic stats sequence with a stable
// high-prediction-quality (high intra/coded ratio, high pcnt_inter)
// section coming up should trigger sourceAltRefPending and arm
// framesTillAltRefFrame to a positive value via scheduleAltRefSource.
func TestPass2ARFPendingTriggersFromHighMotionSection(t *testing.T) {
	const sectionLen = 16
	stats := make([]FirstPassFrameStats, sectionLen)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:    20000,
			CodedError:    200,
			PcntInter:     0.95,
			PcntMotion:    0.4,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
			MVrAbs:        5,
			MVcAbs:        5,
			Count:         1,
		}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	interval, pending := ts.pass2DetectARFPending(0, sectionLen, true, libvpxMinGFInterval+8)
	if !pending {
		t.Fatalf("pass2DetectARFPending returned pending=false on high-motion section")
	}
	if interval < libvpxMinGFInterval {
		t.Fatalf("ARF interval = %d, want >= MIN_GF_INTERVAL=%d", interval, libvpxMinGFInterval)
	}

	// Wire the encoder side: pass2MaybeArmAltRefPending should call
	// scheduleAltRefSource so sourceAltRefPending and
	// framesTillAltRefFrame both transition to "armed" state.
	enc := &VP8Encoder{
		opts: EncoderOptions{
			AutoAltRef:       true,
			LookaheadFrames:  sectionLen + 1,
			KeyFrameInterval: 0,
		},
	}
	enc.twoPass = ts
	enc.pass2MaybeArmAltRefPending(0, 0, false)
	if !enc.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending = false after high-motion section, want true")
	}
	if enc.framesTillAltRefFrame <= 0 {
		t.Fatalf("framesTillAltRefFrame = %d, want > 0", enc.framesTillAltRefFrame)
	}
	if !enc.altRefSourceValid {
		t.Fatalf("altRefSourceValid = false, scheduleAltRefSource must record the future PTS")
	}
}
