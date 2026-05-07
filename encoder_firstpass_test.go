package govpx

import (
	"math"
	"testing"
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
		// IntraError = mbs * (variance + 1000) >> 8. The vertical ramp
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

// firstPassRegression* values are captured from running this implementation
// once. They pin every libvpx-aligned FIRSTPASS_STATS field on the
// deterministic 32x32 ramp clip above. Update in lock-step with the
// formulas in encoder_firstpass.go.
//
// Frame 0 has no LAST so MV stats are zero; coded_error == intra_error.
// Frames 1 and 2 produce the same stats because the 2D ramp shifts by an
// equal amount each step and motion search consistently finds (+1, +1).
//
// Computation walkthrough (see encoder_firstpass.go for line refs):
//   - 32x32 image -> 4 macroblocks (2x2)
//   - intrapenalty = 1000 (govpx)
//   - intra_error = sum(macroblockMeanLumaSSE + 1000) >> 8 = 1120
//   - simple_weight averages weight_table over the ramp (most pixels above
//     code 64 -> weight 1.0); the actual average is captured in the SSIM
//     constants below.
const (
	firstPassRegressionExpectIntraError0 = 1120.0
	firstPassRegressionExpectIntraError1 = 1120.0
	firstPassRegressionExpectIntraError2 = 1120.0
	firstPassRegressionExpectCodedError1 = 4.0
	firstPassRegressionExpectCodedError2 = 4.0
	firstPassRegressionExpectSSIM0       = 1081.1595703125
	firstPassRegressionExpectSSIM1       = 3.913330078125
	firstPassRegressionExpectSSIM2       = 3.950439453125
	firstPassRegressionExpectMVr1        = 8.0
	firstPassRegressionExpectMVrAbs1     = 12.0
	firstPassRegressionExpectMVc1        = 8.0
	firstPassRegressionExpectMVcAbs1     = 16.0
	firstPassRegressionExpectMVrv1       = 188.0
	firstPassRegressionExpectMVcv1       = 348.0
	firstPassRegressionExpectMVInOut1    = -0.5
	firstPassRegressionExpectNewMV1      = 4.0
	firstPassRegressionExpectMVr2        = 8.0
	firstPassRegressionExpectMVrAbs2     = 12.0
	firstPassRegressionExpectMVc2        = 8.0
	firstPassRegressionExpectMVcAbs2     = 16.0
	firstPassRegressionExpectMVrv2       = 188.0
	firstPassRegressionExpectMVcv2       = 348.0
	firstPassRegressionExpectMVInOut2    = -0.5
	firstPassRegressionExpectNewMV2      = 4.0
)
