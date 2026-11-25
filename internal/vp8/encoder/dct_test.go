package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"math/rand"
	randv2 "math/rand/v2"
	"testing"
)

func TestForwardDCTBatchIndependentScalar(t *testing.T) {
	r := randv2.New(randv2.NewPCG(187, 621))
	for _, count := range []int{0, 1, 2, 3, 8, 16, 25} {
		for trial := range 80 {
			input := make([]int16, count*16)
			for i := range input {
				input[i] = int16(r.IntN(511) - 255)
				if trial%7 == 0 {
					input[i] = int16(r.Uint32())
				}
			}
			want, got := make([]int16, len(input)), make([]int16, len(input))
			for block := range count {
				forwardDCT4x4Scalar(input[block*16:block*16+16], 4, (*[16]int16)(want[block*16:block*16+16]))
			}
			ForwardDCT4x4Batch(input, got, count)
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("count%d trial%d lane%d: %d != %d", count, trial, i, got[i], want[i])
				}
			}
			alias := append([]int16(nil), input...)
			ForwardDCT4x4Batch(alias, alias, count)
			for i := range alias {
				if alias[i] != want[i] {
					t.Fatalf("alias count%d trial%d lane%d: %d != %d", count, trial, i, alias[i], want[i])
				}
			}
		}
	}
}

func BenchmarkForwardDCTBatchResidual25(b *testing.B) {
	var input, output [25 * 16]int16
	for i := range input {
		input[i] = int16((i*37+11)%511 - 255)
	}
	b.ReportAllocs()
	for b.Loop() {
		ForwardDCT4x4Batch(input[:], output[:], 25)
	}
}

// TestForwardDCT4x4BatchMatchesPerBlock verifies the batched 4x4 DCT
// dispatcher produces byte-identical output to the per-block
// ForwardDCT4x4 entry point for every realistic VP8 residual range.
// The libvpx reference (vp8_short_fdct8x4 looping vp8_short_fdct4x4)
// has the same per-block byte-identity invariant.
func TestForwardDCT4x4BatchMatchesPerBlock(t *testing.T) {
	r := rand.New(rand.NewSource(0xBADC0DE))
	for _, count := range []int{1, 2, 8, 16, 24, 25} {
		t.Run("", func(t *testing.T) {
			input := make([]int16, count*16)
			for i := range input {
				input[i] = int16(r.Intn(512) - 256)
			}
			outBatch := make([]int16, count*16)
			outPer := make([]int16, count*16)
			ForwardDCT4x4Batch(input, outBatch, count)
			for i := range count {
				var out [16]int16
				ForwardDCT4x4(input[i*16:i*16+16], 4, &out)
				copy(outPer[i*16:i*16+16], out[:])
			}
			for i := range outBatch {
				if outBatch[i] != outPer[i] {
					t.Fatalf("count=%d block=%d lane=%d: batch=%d per=%d", count, i/16, i%16, outBatch[i], outPer[i])
				}
			}
		})
	}
}

// TestForwardDCT4x4BatchSentinels covers the sentinel inputs the
// per-block tests already check, applied across multiple blocks at
// once so the asm loop's inter-iteration state is exercised.
func TestForwardDCT4x4BatchSentinels(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
		{name: "dc_neg", in: [16]int16{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{255, -255, 255, -255, -255, 255, -255, 255, 255, -255, 255, -255, -255, 255, -255, 255}},
		{name: "max_pos", in: [16]int16{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}},
		{name: "max_neg", in: [16]int16{-256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256, -256}},
		{name: "single_top_left", in: [16]int16{255}},
		{name: "single_bottom_right", in: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255}},
	}

	const blocks = 25
	input := make([]int16, blocks*16)
	for bi, tc := range cases {
		for i := range blocks {
			copy(input[i*16:i*16+16], tc.in[:])
		}
		outBatch := make([]int16, blocks*16)
		outPer := make([]int16, blocks*16)
		ForwardDCT4x4Batch(input, outBatch, blocks)
		for i := range blocks {
			var out [16]int16
			ForwardDCT4x4(input[i*16:i*16+16], 4, &out)
			copy(outPer[i*16:i*16+16], out[:])
		}
		for i := range outBatch {
			if outBatch[i] != outPer[i] {
				t.Fatalf("case=%d(%s) block=%d lane=%d: batch=%d per=%d", bi, tc.name, i/16, i%16, outBatch[i], outPer[i])
			}
		}
	}
}

// BenchmarkForwardDCT4x4Batch25 measures the batched cost of the 25
// blocks an MB worth of forward DCTs, which is the call pattern
// libvpx's vp8_transform_mb hides behind a single dispatch.
func BenchmarkForwardDCT4x4Batch25(b *testing.B) {
	input := make([]int16, 25*16)
	for i := range input {
		input[i] = int16((i * 7) - 200)
	}
	output := make([]int16, 25*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardDCT4x4Batch(input, output, 25)
	}
}

// BenchmarkForwardDCT4x4PerBlock25 measures the same 25 blocks but
// dispatched one at a time, the layout of the existing govpx
// encoder paths before R9-11.
func BenchmarkForwardDCT4x4PerBlock25(b *testing.B) {
	input := make([]int16, 25*16)
	for i := range input {
		input[i] = int16((i * 7) - 200)
	}
	output := make([]int16, 25*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := range 25 {
			var out [16]int16
			ForwardDCT4x4(input[j*16:j*16+16], 4, &out)
			copy(output[j*16:j*16+16], out[:])
		}
	}
}

func TestForwardDCT4x4Sentinels(t *testing.T) {
	cases := []struct {
		name  string
		input [16]int16
		want  [16]int16
	}{
		{name: "zero", want: [16]int16{0, 1}},
		{
			name:  "dc",
			input: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
			want:  [16]int16{40, 1},
		},
		{
			name:  "ramp",
			input: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7},
			want:  [16]int16{-4, -8, 0, 0, -35, 0, 0, 0, 0, 0, 0, 0, -2, 0, 0, 0},
		},
	}

	for _, tc := range cases {
		var got [16]int16
		ForwardDCT4x4(tc.input[:], 4, &got)
		if got != tc.want {
			t.Fatalf("%s DCT = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestForwardDCT8x4MatchesTwoDCT4x4Blocks(t *testing.T) {
	input := [32]int16{
		-8, -7, -6, -5, -4, -3, -2, -1,
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}
	var got [32]int16
	var left, right [16]int16

	ForwardDCT8x4(input[:], 8, &got)
	ForwardDCT4x4(input[:], 8, &left)
	ForwardDCT4x4(input[4:], 8, &right)

	var want [32]int16
	copy(want[0:16], left[:])
	copy(want[16:32], right[:])
	if got != want {
		t.Fatalf("DCT8x4 = %v, want %v", got, want)
	}
}

func TestForwardWalsh4x4Sentinels(t *testing.T) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var got [16]int16
	ForwardWalsh4x4(input[:], 4, &got)
	want := [16]int16{-3, -8, 0, -4, -32, 0, 0, 0, 0, 0, 0, 0, -16, 0, 0, 0}
	if got != want {
		t.Fatalf("Walsh4x4 = %v, want %v", got, want)
	}
}

func TestForwardTransformsAllocateZero(t *testing.T) {
	input := [32]int16{
		-8, -7, -6, -5, -4, -3, -2, -1,
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}
	var dct4 [16]int16
	var dct8 [32]int16
	var walsh [16]int16
	allocs := testing.AllocsPerRun(1000, func() {
		ForwardDCT4x4(input[:], 8, &dct4)
		ForwardDCT8x4(input[:], 8, &dct8)
		ForwardWalsh4x4(input[:], 8, &walsh)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkForwardDCT4x4(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardDCT4x4(input[:], 4, &output)
	}
}

func BenchmarkForwardWalsh4x4(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ForwardWalsh4x4(input[:], 4, &output)
	}
}

// TestDCTValueCostFullRangeMatchesLibvpxFillValueTokens audits the
// per-coefficient base-cost lookup that optimize_b consults on every Viterbi
// keep/drop step. Libvpx caches this in `dct_value_cost[2048*2]` and indexes
// it by `vp8_dct_value_cost_ptr + x` where x is the signed quantized
// coefficient (vp8/encoder/encodemb.c:233 and :290). Govpx ports the table
// inside dctValueBaseCostLUT.
//
// The libvpx algorithm in vp8/encoder/tokenize.c fill_value_tokens
// (commented out at lines 39-98 - the generator that produced the embedded
// dct_value_cost.h and dct_value_tokens.h headers) is, for each signed
// coefficient i in [-DCT_MAX_VALUE, DCT_MAX_VALUE):
//
//	const int a = sign ? -i : i;    // a = |i|
//	int eb = sign;                  // bit 0 of Extra is the sign bit
//	if (a > 4) {
//	    int j = 4;
//	    while (++j < 11  &&  vp8_extra_bits[j].base_val <= a) {}
//	    t[i].Token = --j;
//	    eb |= (a - vp8_extra_bits[j].base_val) << 1;
//	} else {
//	    t[i].Token = a;
//	}
//	t[i].Extra = eb;
//
//	// dct_value_cost
//	cost = 0
//	if (p->base_val) {   // skip ZERO_TOKEN
//	    if (p->Len) cost += vp8_treed_cost(p->tree, p->prob, extra >> 1, p->Len)
//	    cost += vp8_cost_bit(vp8_prob_half=128, extra & 1)  // sign cost
//	    dct_value_cost[i + DCT_MAX_VALUE] = cost
//	}
//
// This test reimplements that algorithm byte-for-byte in Go and compares
// every entry against govpx's lookup. Any divergence - magnitude, sign,
// or boundary cell - is a per-coefficient rate drift inside optimize_b
// that flips trellis decisions on small-best ARNR cold-segment cases and
// corrupts byte-faithful parity with libvpx.
func TestDCTValueCostFullRangeMatchesLibvpxFillValueTokens(t *testing.T) {
	// Build the reference table the same way libvpx's commented
	// fill_value_tokens does. Loop signed i covers [-DCT_MAX_VALUE,
	// DCT_MAX_VALUE) - the same range optimize_b probes via the
	// 2048-centered pointer.
	const dctMaxValue = vp8tables.DCTMaxValue
	for i := -dctMaxValue; i < dctMaxValue; i++ {
		token, extra := libvpxFillValueTokens(i)
		wantCost := libvpxFillValueCost(token, extra)
		gotCost := DCTValueBaseCost(i)
		if gotCost != wantCost {
			t.Fatalf("DCTValueBaseCost(%d) = %d, want %d (libvpx fill_value_tokens) - token=%d extra=%d",
				i, gotCost, wantCost, token, extra)
		}
		gotToken := DCTValueToken(i)
		if i == 0 {
			// Libvpx t[0].Token = 0 (ZERO_TOKEN); govpx returns
			// ZERO_TOKEN for x==0 from DCTValueToken.
			if gotToken != vp8tables.ZeroToken {
				t.Fatalf("DCTValueToken(0) = %d, want ZeroToken=%d", gotToken, vp8tables.ZeroToken)
			}
			continue
		}
		if gotToken != token {
			t.Fatalf("DCTValueToken(%d) = %d, want %d", i, gotToken, token)
		}
	}
}

func TestRDTruncMatchesLibvpxMacro(t *testing.T) {
	got := RDTrunc(551, 12345)
	want := (128 + 12345*551) & 0xFF
	if got != want {
		t.Fatalf("RDTrunc(551, 12345) = %d, want %d", got, want)
	}
}

// libvpxFillValueTokens is a Go transliteration of the libvpx
// vp8/encoder/tokenize.c fill_value_tokens body for one signed coefficient i.
// It returns the token classification and the Extra word that fill_value_cost
// then consumes.
func libvpxFillValueTokens(i int) (token int, extra int) {
	sign := 1
	if i >= 0 {
		sign = 0
	}
	a := i
	if sign != 0 {
		a = -i
	}
	eb := sign
	if a > 4 {
		j := 4
		for {
			j++
			if j >= 11 {
				break
			}
			if int(vp8tables.ExtraBitsTable[j].BaseVal) > a {
				break
			}
		}
		j--
		token = j
		eb |= (a - int(vp8tables.ExtraBitsTable[j].BaseVal)) << 1
	} else {
		token = a
	}
	return token, eb
}

// libvpxFillValueCost is a Go transliteration of fill_value_tokens's cost
// computation for one (token, extra) pair. p->base_val == 0 (ZERO_TOKEN or
// the trailing { 0, 0, 0, 0 } sentinel) returns 0 - libvpx writes nothing
// to dct_value_cost in that case so the C array's pre-zero-initialized
// slot is returned (and govpx's LUT explicitly stores 0 for abs==0 by
// leaving the array element at its zero value).
func libvpxFillValueCost(token int, extra int) int {
	p := vp8tables.ExtraBitsTable[token]
	if p.BaseVal == 0 {
		// Either token 0 (ZERO_TOKEN) or 11 (sentinel) - libvpx skips
		// the cost write. But token 0 still has the sign-cost branch
		// inside optimize_b suppressed by the qcoeff==0 outer guard.
		// Mirror by returning 0; the trellis never reads this slot
		// for a non-zero coefficient (verified by the loop range in
		// the calling test).
		return 0
	}
	cost := 0
	if p.Len != 0 {
		cost += libvpxTreedCost(p.Tree, p.Prob, extra>>1, int(p.Len))
	}
	cost += libvpxCostBit(128, extra&1)
	return cost
}

// libvpxTreedCost mirrors the static `vp8_treed_cost` in
// vp8/encoder/treewriter.h line 78 - degenerate-binary tree walk used by
// the extra-bits coding for DCT_VAL_CATEGORY1..6.
func libvpxTreedCost(tree []int16, prob []uint8, v int, n int) int {
	c := 0
	i := 0
	for {
		n--
		b := (v >> n) & 1
		c += libvpxCostBit(prob[i>>1], b)
		i = int(tree[i+b])
		if n == 0 {
			return c
		}
	}
}

// libvpxCostBit mirrors vp8_cost_bit / vp8_cost_bit0 / vp8_cost_one - the
// 256-entry ProbCost LUT keyed by `prob` for bit=0 and `255-prob` for bit=1.
func libvpxCostBit(prob uint8, bit int) int {
	if bit != 0 {
		return vp8tables.ProbCost[255-int(prob)]
	}
	return vp8tables.ProbCost[prob]
}

func TestFDCTZeroLiteral(t *testing.T) {
	var c [32]int16
	var want [32]int16
	forwardDCT4x4Scalar(c[:16], 4, (*[16]int16)(want[:16]))
	forwardDCT4x4Scalar(c[16:], 4, (*[16]int16)(want[16:]))
	if want != ([32]int16{1: 1, 17: 1}) {
		t.Fatalf("unexpected scalar zero transform: %v", want)
	}
	for _, alias := range []bool{false, true} {
		got := make([]int16, 32)
		in := c[:]
		if alias {
			in = got
		}
		ForwardDCT4x4Batch(in, got, 2)
		for i, x := range got {
			if x != want[i] {
				t.Fatalf("alias%v lane%d got%d want%d", alias, i, x, want[i])
			}
		}
	}
}
func BenchmarkFDCTZeroScreen(b *testing.B) {
	for _, pattern := range []string{"zero", "sparse", "dense"} {
		b.Run(pattern, func(b *testing.B) {
			var in, out [16 * 16]int16
			for i := range in {
				if pattern == "dense" {
					in[i] = int16((i*37+11)%511 - 255)
				} else if pattern == "sparse" && i%32 == 0 {
					in[i] = 17
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				ForwardDCT4x4Batch(in[:], out[:], 16)
			}
		})
	}
}

// Preserve the original two-block load-before-store semantics on overlapping windows.
func TestFDCTZeroPacketAliasOrder(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("two-block SIMD packet ordering")
	}
	for _, count := range []int{1, 2, 3, 8, 16, 25} {
		for delta := -17; delta <= 17; delta++ {
			const off = 64
			got := make([]int16, off+25*16+80)
			for i := range got {
				got[i] = int16(0x1357)
			}
			for i := 0; i < count*16; i++ {
				got[off+i] = 0
			}
			want := append([]int16(nil), got...)
			in, out := want[off:off+count*16], want[off+delta:off+delta+count*16]
			i := 0
			for ; i+2 <= count; i += 2 {
				var stage, result [32]int16
				copy(stage[:], in[i*16:i*16+32])
				peak := 0
				for _, v := range stage {
					x := int(v)
					if x < 0 {
						x = -x
					}
					if x > peak {
						peak = x
					}
				}
				if peak > 255 {
					forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
					forwardDCT4x4Scalar(in[i*16+16:], 4, (*[16]int16)(out[i*16+16:]))
				} else {
					forwardDCT4x4Scalar(stage[:16], 4, (*[16]int16)(result[:16]))
					forwardDCT4x4Scalar(stage[16:], 4, (*[16]int16)(result[16:]))
					copy(out[i*16:i*16+32], result[:])
				}
			}
			for ; i < count; i++ {
				forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
			}
			ForwardDCT4x4Batch(got[off:off+count*16], got[off+delta:off+delta+count*16], count)
			for lane, x := range got {
				if x != want[lane] {
					t.Fatalf("count%d delta%d lane%d got%d want%d", count, delta, lane, x, want[lane])
				}
			}
		}
	}
}
