package govpx

import (
	"math"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// Computation walkthrough (see vp8_encoder_firstpass.go for line refs):
//   - 32x32 image -> 4 macroblocks (2x2)
//   - intrapenalty = 256
//   - intra_error = sum(vp8_encode_intra-style predictor SSE + 256) >> 8
//   - simple_weight averages weight_table over the ramp (most pixels above
//     code 64 -> weight 1.0); the actual average is captured in the SSIM
//     constants below.
const (
	firstPassRegressionExpectIntraError0    = 2557.0
	firstPassRegressionExpectIntraError1    = 2243.0
	firstPassRegressionExpectIntraError2    = 2131.0
	firstPassRegressionExpectCodedError1    = 32.0
	firstPassRegressionExpectCodedError2    = 37.0
	firstPassRegressionExpectSSIM0          = 2468.3259118652345
	firstPassRegressionExpectSSIM1          = 31.306640625
	firstPassRegressionExpectSSIM2          = 36.54156494140625
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
		for y := range height {
			for x := range width {
				v := min(32+((x+y)*200/(width+height)), 235)
				frames[i].Y[y*frames[i].YStride+x] = byte(v)
			}
		}
		// Moving 16x16 dark square shifts left by 1 pixel per frame, so
		// the diamond search picks a non-zero MV after frame 0.
		sx := 16 - i
		sy := 16
		for dy := range 16 {
			for dx := range 16 {
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
		for y := range height {
			for x := range width {
				img.Y[y*img.YStride+x] = byte(48 + ((x*5 + y*3) & 127))
			}
		}
		sx := 22 + shift
		sy := 18
		for dy := range 16 {
			for dx := range 16 {
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
// formulas (see vp8_encoder_firstpass.go). Any change to the first-pass scoring
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
		for y := range height {
			for x := range width {
				v := min(max(32+(y+shift)*3+(x+shift)*2, 0), 235)
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
	// vp8_encoder_firstpass.go. Update these in lock-step with the formulas
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

func TestFirstPassMotionSearchSkipsFullPelStats(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 200, 128, 128)
	ref := testVP8Frame(t, 16, 16, 199, 128, 128)
	source := sourceImageFromPublic(src)

	var phase EncoderPhaseStats
	stats := interFrameMotionSearchStats{phase: &phase}
	bestRefMV := vp8enc.MotionVector{}
	bounds := interFrameFullPixelSearchBounds(bestRefMV, 0, 0, 1, 1)
	searcher := newFullPelMotionSearch(source, &ref.Img, 0, 0, bestRefMV, 20,
		bounds, &vp8tables.DefaultMVContext, nil, 0, &stats)
	searcher.firstPassMode = true
	center := bounds.clampEighth(bestRefMV)
	centerCost := searcher.walkCostNoStats(center, maxInt())

	_ = searcher.firstPassSearchSites(center, centerCost,
		libvpxFirstPassSearchStepParam)

	if stats.fullPelSADCalls != 0 || stats.fullPelSADCandidates != 0 ||
		stats.fullPelBatchCalls != 0 || stats.fullPelBoundsRejects != 0 ||
		stats.fullPelEarlyBreaks != 0 {
		t.Fatalf("first-pass full-pel stats = %+v, want zero", stats)
	}
	if phase.FullPelSADCalls != 0 || phase.FullPelSADCandidates != 0 ||
		phase.FullPelBatchCalls != 0 || phase.FullPelBoundsRejects != 0 ||
		phase.FullPelEarlyBreaks != 0 {
		t.Fatalf("first-pass phase full-pel stats = %+v, want zero", phase)
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
