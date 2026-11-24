package govpx

import (
	"math"
	"testing"
)

// TestFirstPassY4MCorpusSectionAccumulators exercises the libvpx
// terminal "total stats" packet path on a deterministic, in-memory
// Y4M-shaped corpus (4 frames at 32x32, 4:2:0 planar, with controlled
// luma motion). The test mirrors the libvpx vp8/encoder/firstpass.c
// pipeline:
//
//   - Per-frame `vp8_first_pass` -> FIRSTPASS_STATS records (govpx
//     `CollectFirstPassStats`).
//   - End-of-encode `vp8_end_first_pass` -> the running aggregate
//     `cpi->twopass.total_stats` rolled in via `accumulate_stats` is
//     emitted as the trailing packet on the output stats list. govpx
//     models that as `FinalizeFirstPassStats`, which appends the
//     aggregate as a sentinel `IsTotal=true` entry computed by
//     `accumulateFirstPassStats` (mirroring libvpx's `accumulate_stats`
//     per-field summation).
//
// The pinned per-frame and total values are tied to the libvpx first-pass
// model in vp8_encoder_firstpass.go: predictor-residual intra scoring and fixed
// pass-1 q=26. Any change to first-pass scoring or to the section accumulator
// must update them in lock-step with the oracle gate.
func TestFirstPassY4MCorpusSectionAccumulators(t *testing.T) {
	const (
		width  = 32
		height = 32
		count  = 4
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

	// Synthesize a small Y4M-shaped corpus. Each frame is a 32x32 4:2:0
	// planar image with a 2D luma ramp shifted by `shift` pixels in
	// both row and column, plus a moving 8x8 dark patch that drifts by
	// (1,1) pixels per frame. The combination guarantees:
	//
	//   - Every macroblock has signal for `simple_weight` (luma codes
	//     stay in the >=64 region for most pixels, which is the
	//     1.0-weight bucket of the libvpx weight_table[256]).
	//   - The motion-search picks a non-zero MV after frame 0, so
	//     PcntInter / PcntMotion / NewMVCount are non-zero.
	//   - Differing per-frame error magnitudes so the rolled-up totals
	//     are not just a multiple of a single frame.
	frame := func(shift int) Image {
		img := Image{
			Width:   width,
			Height:  height,
			Y:       make([]byte, width*height),
			U:       make([]byte, (width/2)*(height/2)),
			V:       make([]byte, (width/2)*(height/2)),
			YStride: width,
			UStride: width / 2,
			VStride: width / 2,
		}
		for y := range height {
			for x := range width {
				v := min(max(64+(y+shift)*3+(x+shift)*2, 0), 235)
				img.Y[y*img.YStride+x] = byte(v)
			}
		}
		// Moving 8x8 dark patch starting at (4,4), drifting by
		// (1,1) per frame.
		px := 4 + shift
		py := 4 + shift
		for dy := range 8 {
			for dx := range 8 {
				ix := px + dx
				iy := py + dy
				if ix < 0 || ix >= width || iy < 0 || iy >= height {
					continue
				}
				img.Y[iy*img.YStride+ix] = 16
			}
		}
		// Constant chroma keeps the test focused on luma stats.
		for j := range img.U {
			img.U[j] = 128
		}
		for j := range img.V {
			img.V[j] = 128
		}
		return img
	}

	frames := make([]Image, count)
	for i := range frames {
		frames[i] = frame(i)
	}

	stats := make([]FirstPassFrameStats, count)
	for i, f := range frames {
		stats[i], err = enc.CollectFirstPassStats(f, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d]: %v", i, err)
		}
	}

	// Append the libvpx terminal total-stats packet at end-of-encode.
	finalized := FinalizeFirstPassStats(stats)
	if got, want := len(finalized), count+1; got != want {
		t.Fatalf("FinalizeFirstPassStats len = %d, want %d", got, want)
	}
	if !finalized[count].IsTotal {
		t.Fatalf("FinalizeFirstPassStats: last entry IsTotal=false, want true")
	}
	for i := range count {
		if finalized[i].IsTotal {
			t.Errorf("frame %d entry unexpectedly marked IsTotal", i)
		}
	}

	// Idempotence: calling Finalize twice MUST NOT re-append.
	if got := FinalizeFirstPassStats(finalized); len(got) != len(finalized) {
		t.Errorf("FinalizeFirstPassStats not idempotent: len=%d want %d", len(got), len(finalized))
	}

	// Print captured values to assist updating the pins below when the
	// scoring formulas change. Wrapped in t.Logf so it only surfaces
	// with `go test -v`.
	for i, s := range finalized {
		t.Logf("stats[%d] frame=%d intra=%.6f coded=%.6f ssim=%.6f pcntInter=%.6f pcntMotion=%.6f mvr=%.6f mvc=%.6f mvrAbs=%.6f mvcAbs=%.6f mvrv=%.6f mvcv=%.6f mvInOut=%.6f newMV=%.1f duration=%.1f count=%.1f isTotal=%v",
			i, s.Frame, s.IntraError, s.CodedError, s.SSIMWeightedPredErr,
			s.PcntInter, s.PcntMotion, s.MVr, s.MVc, s.MVrAbs, s.MVcAbs,
			s.MVrv, s.MVcv, s.MVInOutCount, s.NewMVCount,
			s.Duration, s.Count, s.IsTotal)
	}

	// Pinned per-frame values captured from the libvpx-aligned
	// implementation in vp8_encoder_firstpass.go. Each value follows the
	// formulas documented there:
	//   - IntraError      = sum_mb(vp8_encode_intra-style predictor SSE + intrapenalty) >> 8
	//   - CodedError      = sum_mb(min(intra, motion_error)) >> 8
	//   - SSIMWeightedPredErr = CodedError * simple_weight(source) (>=0.1)
	//   - PcntInter       = intercount / MBs
	//   - PcntMotion      = mvcount / MBs
	//   - PcntSecondRef   = secondrefcount / MBs
	//   - MVr/MVc         = sum_mvr / mvcount, sum_mvc / mvcount (q3)
	//   - MVrAbs/MVcAbs   = sum_|mvr| / mvcount, sum_|mvc| / mvcount
	//   - MVrv/MVcv       = (sum_sq - mean*sum)/mvcount per axis
	//   - MVInOutCount    = sum_in_vectors / (mvcount * 2)
	//   - NewMVCount      = count of MV transitions
	//   - Duration        = 1 (cpi->source->ts_end - ts_start)
	//   - Count           = 1 per frame
	type frameExpect struct {
		intra     float64
		coded     float64
		ssim      float64
		pcntInter float64
		duration  float64
		count     float64
	}
	frameWant := []frameExpect{
		{intra: y4mExpectIntra0, coded: y4mExpectCoded0, ssim: y4mExpectSSIM0, pcntInter: 0.0, duration: 1, count: 1},
		{intra: y4mExpectIntra1, coded: y4mExpectCoded1, ssim: y4mExpectSSIM1, pcntInter: y4mExpectPcntInter1, duration: 1, count: 1},
		{intra: y4mExpectIntra2, coded: y4mExpectCoded2, ssim: y4mExpectSSIM2, pcntInter: y4mExpectPcntInter2, duration: 1, count: 1},
		{intra: y4mExpectIntra3, coded: y4mExpectCoded3, ssim: y4mExpectSSIM3, pcntInter: y4mExpectPcntInter3, duration: 1, count: 1},
	}

	const tol = 1e-6
	closeTo := func(got, exp float64) bool {
		if math.IsNaN(got) || math.IsNaN(exp) {
			return got == exp
		}
		return math.Abs(got-exp) <= tol*math.Max(1.0, math.Abs(exp))
	}

	for i := range frameWant {
		w := frameWant[i]
		g := finalized[i]
		if !closeTo(g.IntraError, w.intra) {
			t.Errorf("frame %d IntraError = %v, want %v", i, g.IntraError, w.intra)
		}
		if !closeTo(g.CodedError, w.coded) {
			t.Errorf("frame %d CodedError = %v, want %v", i, g.CodedError, w.coded)
		}
		if !closeTo(g.SSIMWeightedPredErr, w.ssim) {
			t.Errorf("frame %d SSIMWeightedPredErr = %v, want %v", i, g.SSIMWeightedPredErr, w.ssim)
		}
		if !closeTo(g.PcntInter, w.pcntInter) {
			t.Errorf("frame %d PcntInter = %v, want %v", i, g.PcntInter, w.pcntInter)
		}
		if !closeTo(g.Duration, w.duration) {
			t.Errorf("frame %d Duration = %v, want %v", i, g.Duration, w.duration)
		}
		if !closeTo(g.Count, w.count) {
			t.Errorf("frame %d Count = %v, want %v", i, g.Count, w.count)
		}
	}

	// Pinned terminal total-stats packet. Per libvpx accumulate_stats,
	// each field is the per-frame sum across the whole sequence. The
	// per-frame deltas above sum to these aggregates (see Print loop
	// in this test for the captured values used to seed the pins).
	total := finalized[count]
	if !closeTo(total.IntraError, y4mExpectTotalIntra) {
		t.Errorf("total IntraError = %v, want %v", total.IntraError, y4mExpectTotalIntra)
	}
	if !closeTo(total.CodedError, y4mExpectTotalCoded) {
		t.Errorf("total CodedError = %v, want %v", total.CodedError, y4mExpectTotalCoded)
	}
	if !closeTo(total.SSIMWeightedPredErr, y4mExpectTotalSSIM) {
		t.Errorf("total SSIMWeightedPredErr = %v, want %v", total.SSIMWeightedPredErr, y4mExpectTotalSSIM)
	}
	if !closeTo(total.PcntInter, y4mExpectTotalPcntInter) {
		t.Errorf("total PcntInter = %v, want %v", total.PcntInter, y4mExpectTotalPcntInter)
	}
	if !closeTo(total.Duration, float64(count)) {
		t.Errorf("total Duration = %v, want %v", total.Duration, float64(count))
	}
	if !closeTo(total.Count, float64(count)) {
		t.Errorf("total Count = %v, want %v", total.Count, float64(count))
	}
	if !total.IsTotal {
		t.Errorf("total IsTotal = false, want true")
	}

	// Cross-check: the per-field running aggregate must equal the
	// terminal packet exactly (libvpx accumulate_stats invariant).
	var manual FirstPassFrameStats
	for i := range count {
		accumulateFirstPassStats(&manual, finalized[i])
	}
	if !closeTo(manual.IntraError, total.IntraError) ||
		!closeTo(manual.CodedError, total.CodedError) ||
		!closeTo(manual.SSIMWeightedPredErr, total.SSIMWeightedPredErr) ||
		!closeTo(manual.PcntInter, total.PcntInter) ||
		!closeTo(manual.PcntMotion, total.PcntMotion) ||
		!closeTo(manual.PcntSecondRef, total.PcntSecondRef) ||
		!closeTo(manual.PcntNeutral, total.PcntNeutral) ||
		!closeTo(manual.MVr, total.MVr) ||
		!closeTo(manual.MVc, total.MVc) ||
		!closeTo(manual.MVrAbs, total.MVrAbs) ||
		!closeTo(manual.MVcAbs, total.MVcAbs) ||
		!closeTo(manual.MVrv, total.MVrv) ||
		!closeTo(manual.MVcv, total.MVcv) ||
		!closeTo(manual.MVInOutCount, total.MVInOutCount) ||
		!closeTo(manual.NewMVCount, total.NewMVCount) ||
		!closeTo(manual.Duration, total.Duration) ||
		!closeTo(manual.Count, total.Count) {
		t.Errorf("manual accumulate disagrees with terminal total: manual=%+v total=%+v", manual, total)
	}

	// SetTwoPassStats must accept a finalized stats slice (with the
	// trailing IsTotal entry) and surface the aggregate via
	// twoPassState.totalStats. This pins the libvpx
	// `cpi->twopass.total_stats = *cpi->twopass.stats_in_end` wiring
	// at second-pass init.
	enc2, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 400,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
		TwoPassStats:      finalized,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(TwoPassStats) returned error: %v", err)
	}
	if got := enc2.twoPass.totalStats; !closeTo(got.CodedError, total.CodedError) || !closeTo(got.Count, total.Count) {
		t.Errorf("twoPass.totalStats = %+v, want CodedError=%v Count=%v",
			got, total.CodedError, total.Count)
	}
	if got := len(enc2.twoPass.stats); got != count {
		t.Errorf("twoPass.stats len = %d, want %d (terminal entry must be peeled)", got, count)
	}
}

// Pinned values for TestFirstPassY4MCorpusSectionAccumulators. These
// follow the libvpx-aligned implementation in vp8_encoder_firstpass.go (the same
// formulas TestFirstPassStatsMatchLibvpxFormulas32x32 pins for its 32x32 ramp
// clip).
// Update them in lock-step when the first-pass scoring or section accumulator
// changes.
//
// Per-frame formulas (vp8/encoder/firstpass.c vp8_first_pass):
//   - IntraError      = sum_mb(vp8_encode_intra-style predictor SSE + intrapenalty) >> 8
//   - CodedError      = sum_mb(min(intra, motion_error)) >> 8
//   - SSIMWeightedPredErr = CodedError * simple_weight(source) (>=0.1)
//   - PcntInter       = intercount / MBs
//
// Section/total formulas (vp8/encoder/firstpass.c accumulate_stats):
//   - section.X = sum_frame(X) for every per-frame field.
const (
	// Frame 0: keyframe analog, no LAST yet, intra==coded.
	y4mExpectIntra0 = 3226.0
	y4mExpectCoded0 = 3226.0
	y4mExpectSSIM0  = 3028.4075000000007

	// Frame 1: 1px diagonal motion -> motion search wins on every MB.
	y4mExpectIntra1     = 3281.0
	y4mExpectCoded1     = 91.0
	y4mExpectSSIM1      = 85.42624999999998
	y4mExpectPcntInter1 = 1.0

	y4mExpectIntra2     = 3289.0
	y4mExpectCoded2     = 114.0
	y4mExpectSSIM2      = 107.01749999999994
	y4mExpectPcntInter2 = 1.0

	y4mExpectIntra3     = 2998.0
	y4mExpectCoded3     = 111.0
	y4mExpectSSIM3      = 104.20124999999992
	y4mExpectPcntInter3 = 1.0

	// Terminal total-stats packet: per-field sum across all four
	// frames per libvpx accumulate_stats.
	y4mExpectTotalIntra     = y4mExpectIntra0 + y4mExpectIntra1 + y4mExpectIntra2 + y4mExpectIntra3
	y4mExpectTotalCoded     = y4mExpectCoded0 + y4mExpectCoded1 + y4mExpectCoded2 + y4mExpectCoded3
	y4mExpectTotalSSIM      = y4mExpectSSIM0 + y4mExpectSSIM1 + y4mExpectSSIM2 + y4mExpectSSIM3
	y4mExpectTotalPcntInter = y4mExpectPcntInter1 + y4mExpectPcntInter2 + y4mExpectPcntInter3
)

// TestFinalizeFirstPassStatsEmpty mirrors libvpx's vp8_end_first_pass
// no-op behavior on an empty corpus: the function returns the input
// unchanged rather than emitting a synthetic zero-total record.
func TestFinalizeFirstPassStatsEmpty(t *testing.T) {
	if got := FinalizeFirstPassStats(nil); got != nil {
		t.Errorf("FinalizeFirstPassStats(nil) = %v, want nil", got)
	}
	empty := []FirstPassFrameStats{}
	if got := FinalizeFirstPassStats(empty); len(got) != 0 {
		t.Errorf("FinalizeFirstPassStats(empty) len = %d, want 0", len(got))
	}
}

// TestAccumulateFirstPassStatsMatchesLibvpx pins the per-field
// summation in accumulateFirstPassStats against the libvpx
// vp8/encoder/firstpass.c accumulate_stats reference.
func TestAccumulateFirstPassStatsMatchesLibvpx(t *testing.T) {
	a := FirstPassFrameStats{
		Frame: 1, IntraError: 100, CodedError: 50, SSIMWeightedPredErr: 25,
		PcntInter: 0.5, PcntMotion: 0.4, PcntSecondRef: 0.1, PcntNeutral: 0.05,
		MVr: 1, MVrAbs: 2, MVc: 3, MVcAbs: 4, MVrv: 5, MVcv: 6,
		MVInOutCount: 0.2, NewMVCount: 7,
		Duration: 1, Count: 1,
	}
	b := FirstPassFrameStats{
		Frame: 2, IntraError: 200, CodedError: 75, SSIMWeightedPredErr: 30,
		PcntInter: 0.6, PcntMotion: 0.5, PcntSecondRef: 0.05, PcntNeutral: 0.03,
		MVr: 2, MVrAbs: 3, MVc: 4, MVcAbs: 5, MVrv: 6, MVcv: 7,
		MVInOutCount: 0.3, NewMVCount: 9,
		Duration: 1, Count: 1,
	}
	var section FirstPassFrameStats
	accumulateFirstPassStats(&section, a)
	accumulateFirstPassStats(&section, b)

	want := FirstPassFrameStats{
		Frame: 3, IntraError: 300, CodedError: 125, SSIMWeightedPredErr: 55,
		PcntInter: 1.1, PcntMotion: 0.9, PcntSecondRef: 0.15, PcntNeutral: 0.08,
		MVr: 3, MVrAbs: 5, MVc: 7, MVcAbs: 9, MVrv: 11, MVcv: 13,
		MVInOutCount: 0.5, NewMVCount: 16,
		Duration: 2, Count: 2,
	}

	if math.Abs(section.IntraError-want.IntraError) > 1e-9 ||
		math.Abs(section.CodedError-want.CodedError) > 1e-9 ||
		math.Abs(section.SSIMWeightedPredErr-want.SSIMWeightedPredErr) > 1e-9 ||
		math.Abs(section.PcntInter-want.PcntInter) > 1e-9 ||
		math.Abs(section.PcntMotion-want.PcntMotion) > 1e-9 ||
		math.Abs(section.PcntSecondRef-want.PcntSecondRef) > 1e-9 ||
		math.Abs(section.PcntNeutral-want.PcntNeutral) > 1e-9 ||
		math.Abs(section.MVr-want.MVr) > 1e-9 ||
		math.Abs(section.MVrAbs-want.MVrAbs) > 1e-9 ||
		math.Abs(section.MVc-want.MVc) > 1e-9 ||
		math.Abs(section.MVcAbs-want.MVcAbs) > 1e-9 ||
		math.Abs(section.MVrv-want.MVrv) > 1e-9 ||
		math.Abs(section.MVcv-want.MVcv) > 1e-9 ||
		math.Abs(section.MVInOutCount-want.MVInOutCount) > 1e-9 ||
		math.Abs(section.NewMVCount-want.NewMVCount) > 1e-9 ||
		math.Abs(section.Duration-want.Duration) > 1e-9 ||
		math.Abs(section.Count-want.Count) > 1e-9 ||
		section.Frame != want.Frame {
		t.Errorf("accumulate mismatch:\n got=%+v\nwant=%+v", section, want)
	}

	// nil receiver is a no-op (defensive guard).
	accumulateFirstPassStats(nil, a)
}
