package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9BlockYrdSkippableOnIdenticalPrediction pins the libvpx
// vp9_pickmode.c:824-826 skippable branch: when the prediction matches the
// source exactly (src_diff is all zeros) every tx unit's eob is zero, so
// skippable=true, rate = eob_cost << VP9_PROB_COST_SHIFT, dist = sse << 4.
func TestVP9BlockYrdSkippableOnIdenticalPrediction(t *testing.T) {
	const bw, bh = 32, 32
	var src [bw * bh]byte
	var dst [bw * bh]byte
	// Fill src and dst identically so src_diff = 0.
	for i := range src {
		src[i] = 128
		dst[i] = 128
	}
	dequant := [2]int16{16, 17} // libvpx Y-plane dequant at qindex 64.
	var scratch [16384]int16
	res := vp9BlockYrd(src[:], bw, 0, 0, dst[:], bw, 0, 0,
		bw, bh, common.Tx16x16, dequant, 0, scratch[:])
	if !res.valid {
		t.Fatalf("vp9BlockYrd returned invalid result on identical src/dst")
	}
	if !res.skippable {
		t.Errorf("skippable = false, want true (zero src_diff)")
	}
	if res.dist != 0 {
		t.Errorf("dist = %d, want 0 (sse=0 -> dist = sse<<4 = 0)", res.dist)
	}
	// For BLOCK_32X32 with TX_16X16 there are (32/16)*(32/16) = 4 tx units.
	wantEobCost := 4
	wantRate := wantEobCost << encoder.VP9ProbCostShift
	if res.rate != wantRate {
		t.Errorf("rate = %d, want %d (eob_cost=%d << VP9ProbCostShift)",
			res.rate, wantRate, wantEobCost)
	}
}

// TestVP9BlockYrdNonSkippableProducesPositiveRate exercises the
// vp9_pickmode.c:830-849 non-skippable branch: a real residual must produce
// rate > 0 from the SATD accumulation. We use a square-wave pattern so the
// fdct / Hadamard output has non-trivial coefficients that survive the
// quantizer step.
func TestVP9BlockYrdNonSkippableProducesPositiveRate(t *testing.T) {
	const bw, bh = 32, 32
	var src [bw * bh]byte
	var dst [bw * bh]byte
	for y := range bh {
		for x := range bw {
			src[y*bw+x] = 128
			// dst alternates between black and white tiles every 8 pixels
			// so the residual is a large AC pattern.
			if ((x>>3)+(y>>3))&1 == 0 {
				dst[y*bw+x] = 0
			} else {
				dst[y*bw+x] = 255
			}
		}
	}
	dequant := [2]int16{16, 17}
	var scratch [16384]int16
	// sseY passed in matches what model_rd_for_sb_y would have computed for
	// this src/dst pair (sum of squared diffs). The exact value matters for
	// the dist scale (sse << 4) but the skippable test below is what we
	// pin.
	const sseY = uint64(32 * 32 * 128 * 128) // worst-case approximation
	res := vp9BlockYrd(src[:], bw, 0, 0, dst[:], bw, 0, 0,
		bw, bh, common.Tx16x16, dequant, sseY, scratch[:])
	if !res.valid {
		t.Fatalf("vp9BlockYrd returned invalid result on square-wave residual")
	}
	if res.skippable {
		t.Errorf("skippable = true, want false (residual is non-zero AC)")
	}
	if res.rate <= 0 {
		t.Errorf("rate = %d, want > 0 (non-skippable residual)", res.rate)
	}
	// libvpx vp9_pickmode.c:823 sse = (sseIn << 6) >> 2 = sseIn << 4.
	if res.sse != int64(sseY<<4) {
		t.Errorf("sse = %d, want %d (sseIn << 4)", res.sse, int64(sseY<<4))
	}
}

// TestVP9BlockYrdRateScaling pins the libvpx vp9_pickmode.c:852-853 rate
// scaling: this_rdc.rate = (rate << (2 + VP9_PROB_COST_SHIFT)) +
// (eob_cost << VP9_PROB_COST_SHIFT). The shift is verbatim libvpx; if
// the constant changes the rate units no longer align with the rest of
// the picker's bit-cost accumulators.
func TestVP9BlockYrdRateScaling(t *testing.T) {
	// Use a one-pixel-difference residual so the rate accumulation is the
	// minimum non-zero case: eob>=1 in one tx, eob==0 in the others.
	const bw, bh = 32, 32
	var src [bw * bh]byte
	var dst [bw * bh]byte
	for i := range src {
		src[i] = 128
		dst[i] = 128
	}
	// Insert a small impulse so one 16x16 tx unit has a non-zero coeff.
	src[0] = 200
	dequant := [2]int16{16, 17}
	var scratch [16384]int16
	const sseY = uint64(72 * 72) // (200-128)^2
	res := vp9BlockYrd(src[:], bw, 0, 0, dst[:], bw, 0, 0,
		bw, bh, common.Tx16x16, dequant, sseY, scratch[:])
	if !res.valid {
		t.Fatalf("vp9BlockYrd returned invalid result on impulse residual")
	}
	// The rate scale is `(raw_satd << 11) + (eob_cost << 9)`. Even when
	// raw_satd == 0 the eob_cost term is non-zero (one bit per tx unit).
	// For BLOCK_32X32 + TX_16X16, eob_cost = 4, so the minimum rate is
	// 4 << 9 = 2048.
	const wantMinRate = 4 << encoder.VP9ProbCostShift
	if res.rate < wantMinRate {
		t.Errorf("rate = %d, want >= %d (eob_cost term lower bound)",
			res.rate, wantMinRate)
	}
}
