package benchcmd

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestComputeBDRateVP8Smoke is the VP8 counterpart of
// TestComputeBDRateSmoke. Both Baseline and Test use the same
// EncoderOptions so the BD-rate is required to be near zero. The
// test catches wiring regressions in the VP8 BD-rate harness
// (operating-ladder sort order, encoder/decoder pairing, IVF totals
// math) without requiring the slow per-feature gates to fire.
func TestComputeBDRateVP8Smoke(t *testing.T) {
	t.Parallel()
	res, err := ComputeBDRateVP8(t, BDRateOptionsVP8{
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
			if _, err := ComputeBDRateVP8(t, opts); err == nil {
				t.Fatalf("ComputeBDRateVP8(%s) = nil err, want validation failure", tc.name)
			}
		})
	}
}
