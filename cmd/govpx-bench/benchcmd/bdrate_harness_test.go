package benchcmd

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestComputeBDRateSmoke drives the harness on a tiny synthetic source.
// Both Baseline and Test use the same options, so the BD-rate must be
// near zero. The test exists to catch wiring regressions (mismatched
// q-ladder ordering, decoder pairing bugs, lookahead drain bugs) early
// without requiring the full feature gates to fire.
func TestComputeBDRateSmoke(t *testing.T) {
	t.Parallel()
	res, err := ComputeBDRate(t, BDRateOptions{
		Codec:  "vp9",
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
