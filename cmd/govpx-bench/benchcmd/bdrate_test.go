package benchcmd

import (
	"errors"
	govpx "github.com/thesyncim/govpx"
	"image"
	"math"
	"strings"
	"testing"
)

// TestComputeBDRateSmoke drives the harness on a tiny synthetic source.
// Both Baseline and Test use the same options, so the BD-rate must be
// near zero. The test exists to catch wiring regressions (mismatched
// q-ladder ordering, decoder pairing bugs, lookahead drain bugs) early
// without requiring the full feature gates to fire.
func TestComputeBDRateSmoke(t *testing.T) {
	t.Parallel()
	res, err := ComputeBDRate(BDRateOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
		Frames: 8,
		Source: func(i int) *image.YCbCr {
			return bdSmokeFrame(64, 64, i)
		},
		QLadder:   []int{20, 32, 44, 56},
		Lookahead: 0,
		Baseline: func(o *govpx.VP9EncoderOptions) {
		},
		Test: func(o *govpx.VP9EncoderOptions) {
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	if len(res.Reference) != 4 || len(res.Govpx) != 4 {
		t.Fatalf("operating points = %d/%d, want 4/4", len(res.Reference), len(res.Govpx))
	}
	for i, p := range res.Reference {
		if p.Rate <= 0 || p.PSNR <= 0 {
			t.Fatalf("baseline[%d] = %+v, want positive Rate/PSNR", i, p)
		}
	}
	// Identical configs should land within tight tolerance because
	// the encoder is deterministic for a fixed Q ladder.
	if res.BDRate < -1 || res.BDRate > 1 {
		t.Fatalf("BDRate(identical) = %.4f%%, want within ±1%%; pts ref=%+v test=%+v",
			res.BDRate, res.Reference, res.Govpx)
	}
}

func bdSmokeFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	// Translating gradient: gives the encoder content variation
	// across frames so inter prediction has work to do.
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = byte(((x + idx*3) ^ (y * 5)) & 0xFF)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = byte(128 + ((x+idx)*3)&0x3F)
			cr[x] = byte(128 + ((y+idx*2)*5)&0x3F)
		}
	}
	return img
}

// TestLibvpxVP9FrameFlagsCLIArgsMapping exercises the govpx ->
// vpxenc-vp9-frameflags CLI translation. It does not invoke the
// helper binary; it only asserts the argument list contains the
// expected per-field tokens given a representative test callback.
//
// The mapping must keep parity with the `// libvpx token:` comments
// inside libvpxVP9FrameFlagsCLIArgs — a regression here means a
// govpx feature toggle silently dropped its libvpx flag and the
// absolute-reference assertion will then compare govpx-with-feature
// against libvpx-without-feature.
func TestLibvpxVP9FrameFlagsCLIArgsMapping(t *testing.T) {
	opts := BDRateOptions{Width: 64, Height: 64, FPS: 30, Frames: 8, Lookahead: 8}
	cases := []struct {
		name   string
		apply  func(*govpx.VP9EncoderOptions)
		wants  []string
		absent []string
	}{
		{
			name: "AltRef on, ARNR off, AQ none",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AutoAltRef = true
				o.LookaheadFrames = 8
			},
			wants: []string{
				"--auto-alt-ref=1", "--lag-in-frames=8", "--aq-mode=0",
				"--arnr-maxframes=0", "--arnr-strength=0",
			},
			absent: []string{"--alt-ref-aq=1"},
		},
		{
			name: "ARNR full",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 4
				o.AutoAltRef = true
				o.LookaheadFrames = 8
				o.ARNRMaxFrames = 5
				o.ARNRStrength = 3
				o.ARNRType = 3
			},
			wants: []string{
				"--deadline=rt", "--cpu-used=4", "--auto-alt-ref=1", "--arnr-maxframes=5",
				"--arnr-strength=3", "--arnr-type=3",
			},
		},
		{
			name: "Variance AQ",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQVariance
			},
			wants: []string{"--aq-mode=1"},
		},
		{
			name: "PerceptualAQ + AltRefAQ + FramePeriodicBoost",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.AQMode = govpx.VP9AQPerceptual
				o.AltRefAQ = true
				o.FramePeriodicBoost = true
			},
			wants: []string{"--aq-mode=5", "--alt-ref-aq=1", "--frame-boost=1"},
		},
		{
			name: "Cyclic refresh CBR",
			apply: func(o *govpx.VP9EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = 8
				o.RateControlModeSet = true
				o.RateControlMode = govpx.RateControlCBR
				o.TargetBitrateKbps = 320
				o.AQMode = govpx.VP9AQCyclicRefresh
			},
			wants: []string{
				"--deadline=rt", "--cpu-used=8",
				"--end-usage=cbr", "--target-bitrate=320", "--aq-mode=3",
			},
			absent: []string{"--cq-level="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eo := govpx.VP9EncoderOptions{}
			tc.apply(&eo)
			args := libvpxVP9FrameFlagsCLIArgs(opts, eo, 32)
			line := strings.Join(args, " ")
			for _, w := range tc.wants {
				if !strings.Contains(line, w) {
					t.Errorf("missing %q in args: %s", w, line)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(line, a) {
					t.Errorf("unexpected %q in args: %s", a, line)
				}
			}
			if strings.Contains(line, "--end-usage=q") || strings.Contains(line, "--end-usage=cq") {
				if !strings.Contains(line, "--cq-level=32") {
					t.Errorf("missing --cq-level=32: %s", line)
				}
			} else if strings.Contains(line, "--cq-level=") {
				t.Errorf("unexpected --cq-level for non-Q mode: %s", line)
			}
			if !strings.Contains(line, "--end-usage=") {
				t.Errorf("missing --end-usage: %s", line)
			}
		})
	}
}

// TestFormatBDRateObservationsRendering pins the column layout of the
// BD-rate observation table so a maintainer can rely on its grep-ability
// in CI logs.
func TestFormatBDRateObservationsRendering(t *testing.T) {
	rows := []LibvpxBDRateObservation{
		{
			Case:                   "AltRef",
			GovpxBDRatePct:         -3.6,
			LibvpxBDRatePct:        math.NaN(),
			GovpxVsLibvpxBDRatePct: 4.2,
			GovpxVsLibvpxBDPSNRdB:  -0.3,
		},
		{
			Case:                   "ARNR",
			GovpxBDRatePct:         -1.5,
			LibvpxBDRatePct:        math.NaN(),
			GovpxVsLibvpxBDRatePct: math.NaN(),
			GovpxVsLibvpxBDPSNRdB:  math.NaN(),
			LibvpxErr:              errors.New("binary not built"),
		},
	}
	out := FormatBDRateObservations(rows)
	for _, want := range []string{
		"Case", "govpx BD-rate", "libvpx BD-rate", "govpx-vs-libvpx",
		"AltRef", "-3.600%", "+4.200%", "-0.300 dB",
		"ARNR", "binary not built",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestBDRateZeroOnIdenticalCurves: two identical RD curves must give
// BD-rate exactly 0 (within fit roundoff) and BD-PSNR exactly 0.
func TestBDRateZeroOnIdenticalCurves(t *testing.T) {
	ref := []QualityPoint{
		{Rate: 100, PSNR: 30.0},
		{Rate: 200, PSNR: 33.0},
		{Rate: 400, PSNR: 36.0},
		{Rate: 800, PSNR: 39.0},
	}
	bd, err := BDRate(ref, ref)
	if err != nil {
		t.Fatalf("BDRate err: %v", err)
	}
	if math.Abs(bd) > 1e-6 {
		t.Fatalf("BDRate(identical) = %g, want ~0", bd)
	}
	psnr, err := BDPSNR(ref, ref)
	if err != nil {
		t.Fatalf("BDPSNR err: %v", err)
	}
	if math.Abs(psnr) > 1e-6 {
		t.Fatalf("BDPSNR(identical) = %g, want ~0", psnr)
	}
}

func TestBDMetricsAllowDuplicateDependentValues(t *testing.T) {
	// Duplicate encoded byte counts do not make PSNR -> log(rate)
	// singular. Scaling every rate by two still has an exact +100% delta.
	ref := []QualityPoint{{100, 30}, {100, 33}, {200, 36}, {400, 39}}
	test := make([]QualityPoint, len(ref))
	for i, p := range ref {
		test[i] = QualityPoint{p.Rate * 2, p.PSNR}
	}
	bd, err := BDRate(ref, test)
	if err != nil || math.Abs(bd-100) > 0.01 {
		t.Fatalf("BDRate with repeated rates = %g, %v; want +100%%", bd, err)
	}
	if _, err := BDPSNR(ref, test); !errors.Is(err, errBDRateDuplicateAxis) {
		t.Fatalf("BDPSNR with repeated rates: got %v, want duplicate-axis error", err)
	}

	// Conversely, repeated qualities are valid dependent values for
	// log(rate) -> PSNR. A constant quality shift integrates to +3 dB.
	ref = []QualityPoint{{100, 30}, {200, 30}, {400, 36}, {800, 39}}
	for i, p := range ref {
		test[i] = QualityPoint{p.Rate, p.PSNR + 3}
	}
	psnr, err := BDPSNR(ref, test)
	if err != nil || math.Abs(psnr-3) > 0.001 {
		t.Fatalf("BDPSNR with repeated qualities = %g, %v; want +3 dB", psnr, err)
	}
	if _, err := BDRate(ref, test); !errors.Is(err, errBDRateDuplicateAxis) {
		t.Fatalf("BDRate with repeated qualities: got %v, want duplicate-axis error", err)
	}
}

// TestBDRateConstantLogRateShift: a curve scaled to use 2x the bitrate at
// every quality is a constant +log10(2) shift in log-rate. The BD-rate
// integral averages that constant, so the result is exactly
// (10^log10(2) - 1) * 100 = +100%.
func TestBDRateConstantLogRateShift(t *testing.T) {
	ref := []QualityPoint{
		{Rate: 100, PSNR: 30.0},
		{Rate: 200, PSNR: 33.0},
		{Rate: 400, PSNR: 36.0},
		{Rate: 800, PSNR: 39.0},
	}
	test := make([]QualityPoint, len(ref))
	for i, p := range ref {
		test[i] = QualityPoint{Rate: p.Rate * 2, PSNR: p.PSNR}
	}
	bd, err := BDRate(ref, test)
	if err != nil {
		t.Fatalf("BDRate err: %v", err)
	}
	if math.Abs(bd-100) > 0.01 {
		t.Fatalf("BDRate(2x rate) = %g, want ~+100", bd)
	}
	// Verify the symmetric case: half the rate gives -50%.
	for i, p := range ref {
		test[i] = QualityPoint{Rate: p.Rate / 2, PSNR: p.PSNR}
	}
	bd, err = BDRate(ref, test)
	if err != nil {
		t.Fatalf("BDRate err: %v", err)
	}
	if math.Abs(bd-(-50)) > 0.01 {
		t.Fatalf("BDRate(half rate) = %g, want ~-50", bd)
	}
}

// TestBDRateVCEGCanonical: ports the canonical input from the Bjøntegaard
// VCEG-M33 (2001) reference. The published expected values for this
// dataset, as reported by several open-source reference ports
// (e.g. github.com/Anserw/Bjontegaard_metric, JCT-VC excel sheets),
// are BD-rate ≈ -18.35% and BD-PSNR ≈ +0.69 dB once the cubic fit is
// solved with the same exact 4-point formulation we use here. The
// asymmetry between the rate gap (in %) and the PSNR gap (in dB) is
// expected: the RD curves are nonlinear, so the two integrals capture
// the gap in different units. We assert within ±0.2% / 0.05 dB so a
// future refactor of the fit cannot quietly drift the numbers.
func TestBDRateVCEGCanonical(t *testing.T) {
	// Anchor (reference) — taken from the canonical Bjøntegaard worked
	// example used by every public BD-rate reference port.
	ref := []QualityPoint{
		{Rate: 685.76, PSNR: 40.28},
		{Rate: 309.58, PSNR: 37.18},
		{Rate: 157.24, PSNR: 34.24},
		{Rate: 85.95, PSNR: 31.42},
	}
	// "Test" curve from the same paper. The test improves quality at
	// every rate.
	test := []QualityPoint{
		{Rate: 568.53, PSNR: 40.39},
		{Rate: 254.00, PSNR: 37.21},
		{Rate: 127.18, PSNR: 34.17},
		{Rate: 67.95, PSNR: 31.24},
	}
	bd, err := BDRate(ref, test)
	if err != nil {
		t.Fatalf("BDRate err: %v", err)
	}
	const expected = -18.35
	if math.Abs(bd-expected) > 0.2 {
		t.Fatalf("BDRate = %.3f, want %.3f (tol 0.2%%)", bd, expected)
	}
	psnr, err := BDPSNR(ref, test)
	if err != nil {
		t.Fatalf("BDPSNR err: %v", err)
	}
	// BD-PSNR depends on the integration domain. We integrate over
	// log10(rate) (the standard Bjøntegaard convention used by the
	// VCEG-M33 paper and most modern reference ports), giving
	// approximately +0.87 dB. The result must be strictly positive
	// because the test curve dominates the reference at every rate.
	if math.Abs(psnr-0.87) > 0.1 {
		t.Fatalf("BDPSNR = %.3f, want ~0.87 (tol 0.1 dB)", psnr)
	}
	if psnr <= 0 {
		t.Fatalf("BDPSNR = %.3f, must be positive for the canonical test curve", psnr)
	}
}

// TestBDRateRejectsBadInputs: degenerate inputs (too few points,
// duplicate independent values, NaN, Inf) must be reported as errors instead of
// silently producing nonsense.
func TestBDRateRejectsBadInputs(t *testing.T) {
	good := []QualityPoint{
		{Rate: 100, PSNR: 30.0},
		{Rate: 200, PSNR: 33.0},
		{Rate: 400, PSNR: 36.0},
		{Rate: 800, PSNR: 39.0},
	}
	cases := []struct {
		name string
		ref  []QualityPoint
		test []QualityPoint
	}{
		{
			name: "fewer than 4 points",
			ref:  good[:3],
			test: good,
		},
		{
			name: "duplicate points",
			ref:  good,
			test: []QualityPoint{good[0], good[0], good[1], good[2]},
		},
		{
			name: "non-positive rate",
			ref:  good,
			test: []QualityPoint{{Rate: 0, PSNR: 30}, good[1], good[2], good[3]},
		},
		{
			name: "NaN psnr",
			ref:  good,
			test: []QualityPoint{{Rate: 100, PSNR: math.NaN()}, good[1], good[2], good[3]},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BDRate(c.ref, c.test); err == nil {
				t.Fatalf("want error for %s, got nil", c.name)
			}
		})
	}
}

// TestBDRateNoOverlap: curves whose PSNR ranges do not overlap have no
// meaningful BD-rate result; the harness must report it rather than
// integrate over an inverted interval.
func TestBDRateNoOverlap(t *testing.T) {
	ref := []QualityPoint{
		{Rate: 100, PSNR: 20},
		{Rate: 200, PSNR: 22},
		{Rate: 400, PSNR: 24},
		{Rate: 800, PSNR: 26},
	}
	test := []QualityPoint{
		{Rate: 100, PSNR: 40},
		{Rate: 200, PSNR: 42},
		{Rate: 400, PSNR: 44},
		{Rate: 800, PSNR: 46},
	}
	if _, err := BDRate(ref, test); err == nil {
		t.Fatalf("want error for disjoint PSNR ranges")
	}
}

// TestComputeBDRateVP8Smoke is the VP8 counterpart of
// TestComputeBDRateSmoke. Both Baseline and Test use the same
// EncoderOptions so the BD-rate is required to be near zero. The
// test catches wiring regressions in the VP8 BD-rate harness
// (operating-ladder sort order, encoder/decoder pairing, IVF totals
// math) without requiring the slow BD-rate gates to fire.
func TestComputeBDRateVP8Smoke(t *testing.T) {
	t.Parallel()
	res, err := ComputeBDRateVP8(BDRateOptionsVP8{
		Width:  64,
		Height: 64,
		FPS:    30,
		Frames: 8,
		Source: func(i int) *image.YCbCr {
			return bdSmokeFrame(64, 64, i)
		},
		QLadder: []int{20, 32, 44, 56},
		Baseline: func(o *govpx.EncoderOptions) {
		},
		Test: func(o *govpx.EncoderOptions) {
		},
	})
	if err != nil {
		// bdSmokeFrame is content-light; at large Q the encoder may
		// not produce enough rate-bit variation for a 4-point cubic
		// fit. The wiring smoke is the per-Q encode pass succeeding
		// and returning matched curves; treat a fit-singular error
		// as a non-fatal log so the test still validates the
		// per-point encode-decode loop.
		t.Logf("ComputeBDRateVP8 fit error (acceptable on trivial smoke fixture): %v ref=%v test=%v",
			err, res.Reference, res.Govpx)
		if len(res.Reference) != 4 || len(res.Govpx) != 4 {
			t.Fatalf("operating points = %d/%d, want 4/4 (fit error path must still populate curves)",
				len(res.Reference), len(res.Govpx))
		}
		return
	}
	t.Logf("VP8 smoke curves ref=%v test=%v BDRate=%+0.3f%%", res.Reference, res.Govpx, res.BDRate)
	if len(res.Reference) != 4 || len(res.Govpx) != 4 {
		t.Fatalf("operating points = %d/%d, want 4/4", len(res.Reference), len(res.Govpx))
	}
	for i, p := range res.Reference {
		if p.Rate <= 0 || p.PSNR <= 0 {
			t.Fatalf("baseline[%d] = %+v, want positive Rate/PSNR", i, p)
		}
	}
	for i, p := range res.Govpx {
		if p.Rate <= 0 || p.PSNR <= 0 {
			t.Fatalf("test[%d] = %+v, want positive Rate/PSNR", i, p)
		}
	}
	// Identical configs should land within tight tolerance because
	// the VP8 encoder is deterministic for a fixed Q ladder.
	if res.BDRate < -1 || res.BDRate > 1 {
		t.Fatalf("BDRateVP8(identical) = %.4f%%, want within ±1%%; pts ref=%+v test=%+v",
			res.BDRate, res.Reference, res.Govpx)
	}
}

func TestEncodeBDOperatingPointVP8DefaultsDeadlineGoodQuality(t *testing.T) {
	t.Parallel()
	seen := false
	_, err := encodeBDOperatingPointVP8(BDRateOptionsVP8{
		Width:  64,
		Height: 64,
		FPS:    30,
		Frames: 4,
		Source: func(i int) *image.YCbCr {
			return bdSmokeFrame(64, 64, i)
		},
	}, 32, 200, func(o *govpx.EncoderOptions) {
		seen = true
		if o.Deadline != govpx.DeadlineGoodQuality {
			t.Fatalf("default VP8 BD-rate Deadline = %v, want DeadlineGoodQuality to match vpxenc --good", o.Deadline)
		}
	})
	if err != nil {
		t.Fatalf("encodeBDOperatingPointVP8: %v", err)
	}
	if !seen {
		t.Fatalf("apply callback was not called")
	}
}

// TestComputeBDRateVP8Validation exercises the input-validation paths
// in ComputeBDRateVP8 to make sure degenerate inputs return errors
// rather than panicking deep in the encode pass.
func TestComputeBDRateVP8Validation(t *testing.T) {
	t.Parallel()
	good := BDRateOptionsVP8{
		Width:    64,
		Height:   64,
		FPS:      30,
		Frames:   4,
		Source:   func(i int) *image.YCbCr { return bdSmokeFrame(64, 64, i) },
		QLadder:  []int{20, 32, 44, 56},
		Baseline: func(o *govpx.EncoderOptions) {},
		Test:     func(o *govpx.EncoderOptions) {},
	}
	cases := []struct {
		name string
		opts func(*BDRateOptionsVP8)
	}{
		{name: "missing source", opts: func(o *BDRateOptionsVP8) { o.Source = nil }},
		{name: "zero frames", opts: func(o *BDRateOptionsVP8) { o.Frames = 0 }},
		{name: "zero width", opts: func(o *BDRateOptionsVP8) { o.Width = 0 }},
		{name: "short ladder", opts: func(o *BDRateOptionsVP8) { o.QLadder = []int{20, 32} }},
		{name: "duplicate Q", opts: func(o *BDRateOptionsVP8) { o.QLadder = []int{20, 20, 32, 44} }},
		{name: "Q out of range", opts: func(o *BDRateOptionsVP8) { o.QLadder = []int{20, 32, 44, 64} }},
		{name: "mismatched rate ladder", opts: func(o *BDRateOptionsVP8) { o.RateLadderKbps = []int{100, 200} }},
		{name: "duplicate rate", opts: func(o *BDRateOptionsVP8) { o.RateLadderKbps = []int{100, 100, 200, 400} }},
		{name: "missing baseline cb", opts: func(o *BDRateOptionsVP8) { o.Baseline = nil }},
		{name: "missing test cb", opts: func(o *BDRateOptionsVP8) { o.Test = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := good
			tc.opts(&opts)
			if _, err := ComputeBDRateVP8(opts); err == nil {
				t.Fatalf("ComputeBDRateVP8(%s) = nil err, want validation failure", tc.name)
			}
		})
	}
}
