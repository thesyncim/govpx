package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"math/bits"
)

// Ported from libvpx v1.16.0 vp8/encoder/dct.c.

// ForwardDCT4x4 dispatches to the SIMD or scalar 4x4 forward DCT kernel.
// SIMD entry points are plugged in by per-arch dispatch files; on platforms
// without a SIMD port it falls through to forwardDCT4x4Scalar.
func ForwardDCT4x4(input []int16, stride int, output *[16]int16) {
	forwardDCT4x4SIMD(input, stride, output)
}

// forwardDCT4x4Scalar is the canonical scalar port of libvpx
// vp8/encoder/dct.c vp8_short_fdct4x4_c. SIMD ports must produce
// byte-identical output for the encoder's input range.
func forwardDCT4x4Scalar(input []int16, stride int, output *[16]int16) {
	_ = input[3*stride+3]

	r0 := input[:4]
	a0 := (int(r0[0]) + int(r0[3])) * 8
	b0 := (int(r0[1]) + int(r0[2])) * 8
	c0 := (int(r0[1]) - int(r0[2])) * 8
	d0 := (int(r0[0]) - int(r0[3])) * 8
	t0 := a0 + b0
	t2 := a0 - b0
	t1 := (c0*2217 + d0*5352 + 14500) >> 12
	t3 := (d0*2217 - c0*5352 + 7500) >> 12

	r1 := input[stride : stride+4]
	a1 := (int(r1[0]) + int(r1[3])) * 8
	b1 := (int(r1[1]) + int(r1[2])) * 8
	c1 := (int(r1[1]) - int(r1[2])) * 8
	d1 := (int(r1[0]) - int(r1[3])) * 8
	t4 := a1 + b1
	t6 := a1 - b1
	t5 := (c1*2217 + d1*5352 + 14500) >> 12
	t7 := (d1*2217 - c1*5352 + 7500) >> 12

	r2 := input[2*stride : 2*stride+4]
	a2 := (int(r2[0]) + int(r2[3])) * 8
	b2 := (int(r2[1]) + int(r2[2])) * 8
	c2 := (int(r2[1]) - int(r2[2])) * 8
	d2 := (int(r2[0]) - int(r2[3])) * 8
	t8 := a2 + b2
	t10 := a2 - b2
	t9 := (c2*2217 + d2*5352 + 14500) >> 12
	t11 := (d2*2217 - c2*5352 + 7500) >> 12

	r3 := input[3*stride : 3*stride+4]
	a3 := (int(r3[0]) + int(r3[3])) * 8
	b3 := (int(r3[1]) + int(r3[2])) * 8
	c3 := (int(r3[1]) - int(r3[2])) * 8
	d3 := (int(r3[0]) - int(r3[3])) * 8
	t12 := a3 + b3
	t14 := a3 - b3
	t13 := (c3*2217 + d3*5352 + 14500) >> 12
	t15 := (d3*2217 - c3*5352 + 7500) >> 12

	// col 0
	ca := t0 + t12
	cb := t4 + t8
	cc := t4 - t8
	cd := t0 - t12
	output[0] = int16((ca + cb + 7) >> 4)
	output[8] = int16((ca - cb + 7) >> 4)
	v := (cc*2217 + cd*5352 + 12000) >> 16
	if cd != 0 {
		v++
	}
	output[4] = int16(v)
	output[12] = int16((cd*2217 - cc*5352 + 51000) >> 16)

	// col 1
	ca = t1 + t13
	cb = t5 + t9
	cc = t5 - t9
	cd = t1 - t13
	output[1] = int16((ca + cb + 7) >> 4)
	output[9] = int16((ca - cb + 7) >> 4)
	v = (cc*2217 + cd*5352 + 12000) >> 16
	if cd != 0 {
		v++
	}
	output[5] = int16(v)
	output[13] = int16((cd*2217 - cc*5352 + 51000) >> 16)

	// col 2
	ca = t2 + t14
	cb = t6 + t10
	cc = t6 - t10
	cd = t2 - t14
	output[2] = int16((ca + cb + 7) >> 4)
	output[10] = int16((ca - cb + 7) >> 4)
	v = (cc*2217 + cd*5352 + 12000) >> 16
	if cd != 0 {
		v++
	}
	output[6] = int16(v)
	output[14] = int16((cd*2217 - cc*5352 + 51000) >> 16)

	// col 3
	ca = t3 + t15
	cb = t7 + t11
	cc = t7 - t11
	cd = t3 - t15
	output[3] = int16((ca + cb + 7) >> 4)
	output[11] = int16((ca - cb + 7) >> 4)
	v = (cc*2217 + cd*5352 + 12000) >> 16
	if cd != 0 {
		v++
	}
	output[7] = int16(v)
	output[15] = int16((cd*2217 - cc*5352 + 51000) >> 16)
}

func ForwardDCT8x4(input []int16, stride int, output *[32]int16) {
	var left [16]int16
	var right [16]int16
	ForwardDCT4x4(input, stride, &left)
	ForwardDCT4x4(input[4:], stride, &right)
	copy(output[0:16], left[:])
	copy(output[16:32], right[:])
}

// ForwardWalsh4x4 dispatches to the SIMD or scalar 4x4 forward Walsh-Hadamard
// kernel. The SIMD implementations mirror libvpx v1.16.0
// vp8/encoder/arm/neon/vp8_shortwalsh4x4_neon.c (vp8_short_walsh4x4_neon) and
// vp8/encoder/x86/fwalsh_sse2.asm (vp8_short_walsh4x4_sse2). Output is
// byte-identical to forwardWalsh4x4Scalar for the encoder's residual range.
func ForwardWalsh4x4(input []int16, stride int, output *[16]int16) {
	forwardWalsh4x4SIMD(input, stride, output)
}

// forwardWalsh4x4Scalar is the canonical scalar port of libvpx
// vp8/encoder/dct.c vp8_short_walsh4x4_c. SIMD ports must produce
// byte-identical output for the encoder's residual range.
func forwardWalsh4x4Scalar(input []int16, stride int, output *[16]int16) {
	var tmp [16]int

	for row := range 4 {
		ip := row * stride
		op := row * 4
		a1 := (int(input[ip+0]) + int(input[ip+2])) * 4
		d1 := (int(input[ip+1]) + int(input[ip+3])) * 4
		c1 := (int(input[ip+1]) - int(input[ip+3])) * 4
		b1 := (int(input[ip+0]) - int(input[ip+2])) * 4

		tmp[op+0] = a1 + d1 + boolInt(a1 != 0)
		tmp[op+1] = b1 + c1
		tmp[op+2] = b1 - c1
		tmp[op+3] = a1 - d1
	}

	for col := range 4 {
		a1 := tmp[col+0] + tmp[col+8]
		d1 := tmp[col+4] + tmp[col+12]
		c1 := tmp[col+4] - tmp[col+12]
		b1 := tmp[col+0] - tmp[col+8]

		a2 := a1 + d1
		b2 := b1 + c1
		c2 := b1 - c1
		d2 := a1 - d1

		a2 += boolInt(a2 < 0)
		b2 += boolInt(b2 < 0)
		c2 += boolInt(c2 < 0)
		d2 += boolInt(d2 < 0)

		output[col+0] = int16((a2 + 3) >> 3)
		output[col+4] = int16((b2 + 3) >> 3)
		output[col+8] = int16((c2 + 3) >> 3)
		output[col+12] = int16((d2 + 3) >> 3)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Whole-MB batched forward 4x4 DCT entry point. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c vp8_transform_mb / vp8_transform_mbuv.
//
// ForwardDCT4x4Batch processes count consecutive 4x4 int16 blocks. Native
// kernels transform two blocks across eight lanes, with per-block handling
// for the remainder and scalar handling outside the VP8 residual range.
// Input and output may be the same slice. Results match the scalar DCT.
func ForwardDCT4x4Batch(input []int16, output []int16, count int) {
	forwardDCT4x4BatchSIMD(input, output, count)
}

// ForwardDCT4x4BatchResiduals transforms count consecutive 4x4 int16 residual
// blocks whose values are guaranteed in [-255, 255] (e.g. uint8 - uint8 pixel
// differences), bypassing the per-pair peak-magnitude overflow check.
func ForwardDCT4x4BatchResiduals(input []int16, output []int16, count int) {
	done := forwardDCT4BatchResidualsGo(input, output, count)
	if done < count {
		forwardDCT4x4BatchSIMD(input[done*16:], output[done*16:], count-done)
	}
}

const dctValueSignShift = bits.UintSize - 1

// DCTValueToken returns the libvpx v1.16.0 coefficient-token classification
// for value x (mirrors the dct_value_tokens table indexed by signed value).
func DCTValueToken(x int) int {
	mask := x >> dctValueSignShift
	abs := (x ^ mask) - mask
	if abs == 0 {
		return tables.ZeroToken
	}
	token, _, ok := dctCoefficientTokenMagnitude(abs)
	if !ok {
		return tables.ZeroToken
	}
	return token
}

// dctValueBaseCostLUT precomputes libvpx's dct_value_cost table for every
// signed coefficient in [-DCTMaxValue, DCTMaxValue]: extra-bits subtree cost
// plus sign-bit cost. Indexed by abs(x) * 2 + sign_bit so positive and
// negative values share the magnitude-dependent extra-bits cost but pick up the
// correct sign-cost (libvpx's vp8_cost_bit(vp8_prob_half, sign_bit) differs by
// two entropy-bit units between sign=0 and sign=1).
var dctValueBaseCostLUT = buildDCTValueBaseCostLUT()

func buildDCTValueBaseCostLUT() [2 * (tables.DCTMaxValue + 1)]int32 {
	var lut [2 * (tables.DCTMaxValue + 1)]int32
	signCostPositive := dctBoolBitCost(128, 0)
	signCostNegative := dctBoolBitCost(128, 1)
	for abs := 1; abs <= tables.DCTMaxValue; abs++ {
		token := int(dctCoefficientTokenLUT[abs])
		extra := dctCoefficientExtraBitsRate(token, abs)
		lut[abs*2+0] = int32(signCostPositive + extra)
		lut[abs*2+1] = int32(signCostNegative + extra)
	}
	return lut
}

// DCTValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func DCTValueBaseCost(x int) int {
	mask := x >> dctValueSignShift
	abs := (x ^ mask) - mask
	if uint(abs) > uint(tables.DCTMaxValue) {
		return int(^uint(0)>>1) / 4
	}
	return int(dctValueBaseCostLUT[abs*2+(mask&1)])
}

// RDTrunc mirrors the libvpx v1.16.0 encodemb.c RDTRUNC macro used to break
// ties when two trellis paths have equal RDCOST.
func RDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

var dctCoefficientTokenLUT = buildDCTCoefficientTokenLUT()

func buildDCTCoefficientTokenLUT() [tables.DCTMaxValue + 1]uint8 {
	var lut [tables.DCTMaxValue + 1]uint8
	lut[1] = tables.OneToken
	lut[2] = tables.TwoToken
	lut[3] = tables.ThreeToken
	lut[4] = tables.FourToken
	for i := 5; i <= 6; i++ {
		lut[i] = tables.DCTValCategory1
	}
	for i := 7; i <= 10; i++ {
		lut[i] = tables.DCTValCategory2
	}
	for i := 11; i <= 18; i++ {
		lut[i] = tables.DCTValCategory3
	}
	for i := 19; i <= 34; i++ {
		lut[i] = tables.DCTValCategory4
	}
	for i := 35; i <= 66; i++ {
		lut[i] = tables.DCTValCategory5
	}
	for i := 67; i <= tables.DCTMaxValue; i++ {
		lut[i] = tables.DCTValCategory6
	}
	return lut
}

func dctCoefficientTokenMagnitude(coeff int) (int, int, bool) {
	mask := coeff >> dctValueSignShift
	coeff = (coeff ^ mask) - mask
	if uint(coeff-1) >= uint(tables.DCTMaxValue) {
		return 0, 0, false
	}
	return int(dctCoefficientTokenLUT[coeff]), coeff, true
}

func dctCoefficientExtraBitsRate(token int, mag int) int {
	extra := tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += dctBoolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func dctBoolBitCost(prob uint8, bit int) int {
	if bit != 0 {
		return tables.ProbCost[255-int(prob)]
	}
	return tables.ProbCost[prob]
}
