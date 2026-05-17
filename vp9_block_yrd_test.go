package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9BlockYrdSkippableOnIdenticalPrediction pins the libvpx
// vp9_pickmode.c:821-828 skippable branch: when the prediction matches the
// source exactly (src_diff is all zeros) every tx unit's eob is zero, so
// skippable=true, dist = sse << 4. libvpx sets this_rdc->rate=0 at line 821
// BEFORE the *sse=(*sse<<6)>>2 / skippable-return at lines 822-828; the
// caller (vp9_pickmode.c:2364) overwrites this_rdc.rate with
// vp9_cost_bit(skip_prob, 1). So block_yrd's output on the skippable path
// is rate=0 (NOT eob_cost<<VP9_PROB_COST_SHIFT — that finalization at
// libvpx :852-853 runs only in the non-skippable branch).
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
	if res.rate != 0 {
		t.Errorf("rate = %d, want 0 (libvpx vp9_pickmode.c:821 sets rate=0 "+
			"and returns at :826 before the eob_cost finalization)",
			res.rate)
	}
}

func TestVP9BlockYrdSkippableWithUnknownSSEKeepsEobRate(t *testing.T) {
	const bw, bh = 32, 32
	var src [bw * bh]byte
	var dst [bw * bh]byte
	for i := range src {
		src[i] = 128
		dst[i] = 128
	}
	dequant := [2]int16{16, 17}
	var scratch [16384]int16
	res := vp9BlockYrd(src[:], bw, 0, 0, dst[:], bw, 0, 0,
		bw, bh, common.Tx16x16, dequant, vp9BlockYrdUnknownSSE, scratch[:])
	if !res.valid {
		t.Fatalf("vp9BlockYrd returned invalid result on identical src/dst")
	}
	if !res.skippable {
		t.Errorf("skippable = false, want true (zero src_diff)")
	}
	if res.dist != 0 {
		t.Errorf("dist = %d, want 0 without finite SSE", res.dist)
	}
	const wantRate = 4 << encoder.VP9ProbCostShift
	if res.rate != wantRate {
		t.Errorf("rate = %d, want %d (eob_cost retained without finite SSE)",
			res.rate, wantRate)
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

// TestVP9BlockErrorFPZero pins libvpx vp9_rdopt.c:334-345: when coeff ==
// dqcoeff the per-element diff is zero, so error = 0 regardless of
// magnitude. The hand-computed expected value is 0.
func TestVP9BlockErrorFPZero(t *testing.T) {
	coeff := []int16{100, -200, 300, -400, 500, -600, 700, -800,
		900, -1000, 1100, -1200, 1300, -1400, 1500, -1600}
	dqcoeff := make([]int16, len(coeff))
	copy(dqcoeff, coeff)
	got := vp9BlockErrorFP(coeff, dqcoeff)
	if got != 0 {
		t.Errorf("vp9BlockErrorFP(coeff,coeff) = %d, want 0 "+
			"(libvpx vp9_rdopt.c:340-341 — diff=0 -> error += 0)", got)
	}
}

// TestVP9BlockErrorFPSingleNonZero pins libvpx vp9_rdopt.c:334-345 against a
// single-element residue. With coeff[0]=10, dqcoeff[0]=3 (rest zero on both
// sides), diff=7 and error = 7*7 = 49. The trailing zeros contribute 0.
func TestVP9BlockErrorFPSingleNonZero(t *testing.T) {
	const n = 16 // TX_4X4 size
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	coeff[0] = 10
	dqcoeff[0] = 3
	// libvpx: diff = coeff[0]-dqcoeff[0] = 7; error = 49.
	const want = uint64(49)
	if got := vp9BlockErrorFP(coeff, dqcoeff); got != want {
		t.Errorf("vp9BlockErrorFP = %d, want %d "+
			"(hand-computed: diff=7, diff*diff=49)", got, want)
	}
}

// TestVP9BlockErrorFPHandComputed16 pins libvpx vp9_rdopt.c:334-345 against
// a 16-element block with mixed signs. Each diff and its square are listed
// explicitly so the expected value is byte-for-byte verifiable.
//
//	idx  coeff  dqcoeff  diff  diff*diff
//	  0    100      90    10        100
//	  1   -100     -90   -10        100
//	  2     50      48     2          4
//	  3   -200    -150   -50       2500
//	  4    300     310   -10        100
//	  5      0       1    -1          1
//	  6      1       0     1          1
//	  7    500     500     0          0
//	  8  -1000   -1100   100      10000
//	  9   1500    1480    20        400
//	 10  -2000   -2050    50       2500
//	 11   2500    2500     0          0
//	 12  -3000   -2900  -100      10000
//	 13   3500    3450    50       2500
//	 14  -4000   -4001     1          1
//	 15   4500    4499     1          1
//	                            ------
//	                            28208
func TestVP9BlockErrorFPHandComputed16(t *testing.T) {
	coeff := []int16{
		100, -100, 50, -200, 300, 0, 1, 500,
		-1000, 1500, -2000, 2500, -3000, 3500, -4000, 4500,
	}
	dqcoeff := []int16{
		90, -90, 48, -150, 310, 1, 0, 500,
		-1100, 1480, -2050, 2500, -2900, 3450, -4001, 4499,
	}
	const want = uint64(28208)
	if got := vp9BlockErrorFP(coeff, dqcoeff); got != want {
		t.Errorf("vp9BlockErrorFP = %d, want %d "+
			"(hand-computed sum of diff*diff over 16 elements)",
			got, want)
	}
}

// TestVP9BlockErrorFPSymmetry pins libvpx vp9_rdopt.c:340-341 — the
// accumulator is symmetric under (coeff,dqcoeff) -> (dqcoeff,coeff)
// because diff*diff is invariant under sign flip.
func TestVP9BlockErrorFPSymmetry(t *testing.T) {
	coeff := []int16{123, -456, 789, -1011, 1213, -1415, 1617, -1819,
		0, 1, -1, 2, -2, 3, -3, 4}
	dqcoeff := []int16{100, -400, 800, -1000, 1200, -1400, 1600, -1800,
		1, 0, 1, -2, 2, -3, 3, -4}
	a := vp9BlockErrorFP(coeff, dqcoeff)
	b := vp9BlockErrorFP(dqcoeff, coeff)
	if a != b {
		t.Errorf("symmetry broken: f(c,dq)=%d, f(dq,c)=%d "+
			"(diff*diff is sign-invariant; libvpx vp9_rdopt.c:341)", a, b)
	}
}

// TestVP9BlockErrorFPLargeBlock pins libvpx vp9_rdopt.c:334-345 at the
// TX_16X16 block size used by block_yrd (n=256). Every element has the
// same diff=±64, so the expected error is 256 * 64^2 = 1048576. This
// also exercises that the accumulator does not overflow on a full 16x16
// block of moderate-amplitude coefficients (libvpx int64_t headroom is
// preserved by govpx's uint64 accumulator).
func TestVP9BlockErrorFPLargeBlock(t *testing.T) {
	const n = 256 // TX_16X16
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		if i&1 == 0 {
			coeff[i] = 1000
			dqcoeff[i] = 1064
		} else {
			coeff[i] = -1000
			dqcoeff[i] = -936
		}
	}
	// diff = ±64 (|diff|=64 every slot); diff^2 = 4096; n * 4096 = 1048576.
	const want = uint64(256) * 64 * 64
	if got := vp9BlockErrorFP(coeff, dqcoeff); got != want {
		t.Errorf("vp9BlockErrorFP(256 elems, |diff|=64) = %d, want %d",
			got, want)
	}
}

// TestVP9BlockErrorFPMaxInt16Diff pins the worst-case headroom of libvpx's
// int64_t error accumulator: with coeff=INT16_MIN and dqcoeff=INT16_MAX,
// diff = -65535 (fits in int), diff*diff = 4_294_836_225 (fits in int64).
// Over a TX_16X16 block (n=256) the sum is 1_099_478_073_600, which fits
// well within int64 and uint64. This pins that govpx's int->uint64 cast
// doesn't silently truncate the per-element product.
func TestVP9BlockErrorFPMaxInt16Diff(t *testing.T) {
	const n = 256
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		coeff[i] = -32768
		dqcoeff[i] = 32767
	}
	// diff = -65535; diff*diff = 4294836225; n=256 -> 1099478073600.
	const want = uint64(256) * uint64(65535) * uint64(65535)
	if got := vp9BlockErrorFP(coeff, dqcoeff); got != want {
		t.Errorf("vp9BlockErrorFP(INT16_MIN vs INT16_MAX, n=256) = %d, "+
			"want %d (libvpx int64_t headroom check)", got, want)
	}
}

// TestVP9BlockErrorFPMatchesInt64Reference pins that the govpx uint64
// accumulator agrees with a literal transcription of libvpx
// vp9_rdopt.c:334-345 using int64_t (the libvpx accumulator type). Both
// must produce bitwise-identical results because diff*diff is
// non-negative — the only freedom in the port is the return type.
func TestVP9BlockErrorFPMatchesInt64Reference(t *testing.T) {
	coeff := []int16{
		7, -13, 19, -25, 31, -37, 41, -43, 47, -53, 59, -61, 67, -71, 73, -79,
		83, -89, 97, -101, 103, -107, 109, -113, 127, -131, 137, -139, 149, -151, 157, -163,
	}
	dqcoeff := []int16{
		8, -16, 16, -24, 32, -40, 40, -40, 48, -56, 56, -56, 64, -72, 72, -80,
		80, -88, 96, -104, 104, -112, 112, -112, 128, -128, 136, -144, 144, -152, 160, -160,
	}
	want := vp9BlockErrorFP(coeff, dqcoeff)
	// Mirror libvpx vp9_rdopt.c:334-345 with int64_t error / int diff.
	var reference int64
	for j := range coeff {
		d := int(coeff[j]) - int(dqcoeff[j])
		reference += int64(d * d)
	}
	if uint64(reference) != want {
		t.Errorf("int64 reference = %d, govpx uint64 = %d "+
			"(libvpx vp9_rdopt.c:334-345 must match bit-for-bit)",
			reference, want)
	}
}

// TestVP9BlockErrorFPCallSiteShift pins the libvpx vp9_pickmode.c:845
// caller-side shift: dist += vp9_block_error_fp(...) >> 2. The function
// itself does NOT shift — the >> 2 is in the caller. We pin both the raw
// error and the post-shift value with hand-computed expected results.
func TestVP9BlockErrorFPCallSiteShift(t *testing.T) {
	coeff := []int16{20, 30, 40, 50, 60, 70, 80, 90,
		10, -10, 20, -20, 30, -30, 40, -40}
	dqcoeff := []int16{16, 32, 32, 48, 64, 64, 80, 96,
		8, -8, 16, -16, 32, -32, 32, -32}
	// Hand: diffs = 4,-2,8,2,-4,6,0,-6,2,-2,4,-4,-2,2,8,-8
	// squares = 16,4,64,4,16,36,0,36,4,4,16,16,4,4,64,64 -> sum = 352
	const wantRaw = uint64(352)
	got := vp9BlockErrorFP(coeff, dqcoeff)
	if got != wantRaw {
		t.Errorf("raw vp9BlockErrorFP = %d, want %d", got, wantRaw)
	}
	// libvpx vp9_pickmode.c:845 — caller applies >> 2 (floor division by 4).
	const wantShifted = wantRaw >> 2 // 88
	if got>>2 != wantShifted {
		t.Errorf("post-shift = %d, want %d "+
			"(libvpx vp9_pickmode.c:845 — >>2 is caller-side)",
			got>>2, wantShifted)
	}
}
