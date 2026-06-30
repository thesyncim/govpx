package encoder

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestForwardDCT4x4ConstantKeepsOnlyDC(t *testing.T) {
	var input [16]int16
	for i := range input {
		input[i] = 10
	}
	var got [16]int16
	ForwardDCT4x4(input[:], 4, &got)
	if got[0] != 320 {
		t.Fatalf("constant block DC = %d, want 320; coeffs=%v", got[0], got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("constant block AC[%d] = %d, want 0; coeffs=%v", i, got[i], got)
		}
	}
}

func BenchmarkVP9QuantizeFPLibvpx(b *testing.B) {
	tests := []struct {
		name string
		tx   common.TxSize
		n    int
	}{
		{name: "4x4", tx: common.Tx4x4, n: 16},
		{name: "8x8", tx: common.Tx8x8, n: 64},
		{name: "16x16", tx: common.Tx16x16, n: 256},
	}
	for _, tc := range tests {
		b.Run(fmt.Sprintf("%s/n%d", tc.name, tc.n), func(b *testing.B) {
			scanOrder := common.DefaultScanOrders[tc.tx]
			coeff := make([]int16, tc.n)
			qcoeff := make([]int16, tc.n)
			dqcoeff := make([]int16, tc.n)
			for i := range coeff {
				v := (i*37)%2048 - 1024
				if i%5 == 0 {
					v /= 8
				}
				coeff[i] = int16(v)
			}
			roundFP := [2]int16{6, 6}
			quantFP := [2]int16{9362, 9362}
			dequant := [2]int16{7, 7}
			b.ReportAllocs()
			b.ResetTimer()
			eobSum := 0
			for i := 0; i < b.N; i++ {
				eobSum += QuantizeFPLibvpx(coeff, tc.n, roundFP, quantFP, dequant,
					scanOrder.Scan, scanOrder.IScan, qcoeff, dqcoeff)
			}
			if eobSum == 0 {
				b.Fatal("unexpected zero eob accumulator")
			}
		})
	}
}

func BenchmarkVP9QuantizeFP(b *testing.B) {
	tests := []struct {
		name string
		tx   common.TxSize
		n    int
	}{
		{name: "4x4", tx: common.Tx4x4, n: 16},
		{name: "8x8", tx: common.Tx8x8, n: 64},
		{name: "16x16", tx: common.Tx16x16, n: 256},
	}
	for _, tc := range tests {
		b.Run(fmt.Sprintf("%s/n%d", tc.name, tc.n), func(b *testing.B) {
			scan := common.DefaultScanOrders[tc.tx].Scan
			coeff := make([]int16, tc.n)
			dqcoeff := make([]int16, tc.n)
			for i := range coeff {
				v := (i*37)%2048 - 1024
				if i%5 == 0 {
					v /= 8
				}
				coeff[i] = int16(v)
			}
			dequant := [2]int16{7, 7}
			b.ReportAllocs()
			b.ResetTimer()
			eobSum := 0
			for i := 0; i < b.N; i++ {
				eobSum += QuantizeFP(coeff, dequant, scan, dqcoeff)
			}
			if eobSum == 0 {
				b.Fatal("unexpected zero eob accumulator")
			}
		})
	}
}

func BenchmarkVP9QuantizeFP32x32(b *testing.B) {
	scan := common.DefaultScanOrders[common.Tx32x32].Scan
	coeff := make([]int16, 1024)
	for i := range coeff {
		v := (i*73)%4096 - 2048
		if i%11 == 0 {
			v /= 16
		}
		coeff[i] = int16(v)
	}
	dequant := [2]int16{7, 6}

	for _, tc := range []struct {
		name  string
		withQ bool
	}{
		{name: "dqonly"},
		{name: "withq", withQ: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dqcoeff := make([]int16, 1024)
			var qcoeff []int16
			if tc.withQ {
				qcoeff = make([]int16, 1024)
			}
			b.ReportAllocs()
			b.ResetTimer()
			eobSum := 0
			for i := 0; i < b.N; i++ {
				eobSum += QuantizeFP32x32WithQ(coeff, dequant, scan, qcoeff, dqcoeff)
			}
			if eobSum == 0 {
				b.Fatal("unexpected zero eob accumulator")
			}
		})
	}
}

func BenchmarkVP9QuantizeBScanOrder(b *testing.B) {
	tests := []struct {
		name string
		tx   common.TxSize
		n    int
	}{
		{name: "4x4", tx: common.Tx4x4, n: 16},
		{name: "8x8", tx: common.Tx8x8, n: 64},
		{name: "16x16", tx: common.Tx16x16, n: 256},
	}
	for _, tc := range tests {
		scanOrder := common.DefaultScanOrders[tc.tx]
		coeff := make([]int16, tc.n)
		for i := range coeff {
			v := (i*97)%4096 - 2048
			if i%7 == 0 {
				v /= 12
			}
			coeff[i] = int16(v)
		}
		dequant := [2]int16{38, 44}
		const qindex = 87
		for _, bench := range []struct {
			name string
			fn   func(qcoeff, dqcoeff []int16) int
		}{
			{
				name: "scan",
				fn: func(qcoeff, dqcoeff []int16) int {
					return QuantizeBWithQ(coeff, qindex, dequant,
						scanOrder.Scan, qcoeff, dqcoeff)
				},
			},
			{
				name: "scanorder",
				fn: func(qcoeff, dqcoeff []int16) int {
					return QuantizeBWithQScanOrder(coeff, qindex, dequant,
						scanOrder, qcoeff, dqcoeff)
				},
			},
		} {
			for _, output := range []struct {
				name  string
				withQ bool
			}{
				{name: "dqonly"},
				{name: "withq", withQ: true},
			} {
				b.Run(fmt.Sprintf("%s/%s/%s", tc.name, bench.name, output.name), func(b *testing.B) {
					var qcoeff []int16
					if output.withQ {
						qcoeff = make([]int16, tc.n)
					}
					dqcoeff := make([]int16, tc.n)
					b.ReportAllocs()
					b.ResetTimer()
					eobSum := 0
					for i := 0; i < b.N; i++ {
						eobSum += bench.fn(qcoeff, dqcoeff)
					}
					if eobSum == 0 {
						b.Fatal("unexpected zero eob accumulator")
					}
				})
			}
		}
	}
}

func BenchmarkVP9QuantizeBSparseTailScanOrder(b *testing.B) {
	tests := []struct {
		name string
		tx   common.TxSize
		n    int
	}{
		{name: "4x4", tx: common.Tx4x4, n: 16},
		{name: "8x8", tx: common.Tx8x8, n: 64},
		{name: "16x16", tx: common.Tx16x16, n: 256},
	}
	for _, tc := range tests {
		scanOrder := common.DefaultScanOrders[tc.tx]
		coeff := make([]int16, tc.n)
		coeff[scanOrder.Scan[0]] = 1200
		coeff[scanOrder.Scan[1]] = -900
		coeff[scanOrder.Scan[min(5, tc.n-1)]] = 700
		dequant := [2]int16{38, 44}
		const qindex = 87
		for _, bench := range []struct {
			name string
			fn   func(qcoeff, dqcoeff []int16) int
		}{
			{
				name: "scan",
				fn: func(qcoeff, dqcoeff []int16) int {
					return QuantizeBWithQ(coeff, qindex, dequant,
						scanOrder.Scan, qcoeff, dqcoeff)
				},
			},
			{
				name: "scanorder",
				fn: func(qcoeff, dqcoeff []int16) int {
					return QuantizeBWithQScanOrder(coeff, qindex, dequant,
						scanOrder, qcoeff, dqcoeff)
				},
			},
		} {
			for _, output := range []struct {
				name  string
				withQ bool
			}{
				{name: "dqonly"},
				{name: "withq", withQ: true},
			} {
				b.Run(fmt.Sprintf("%s/%s/%s", tc.name, bench.name, output.name), func(b *testing.B) {
					var qcoeff []int16
					if output.withQ {
						qcoeff = make([]int16, tc.n)
					}
					dqcoeff := make([]int16, tc.n)
					b.ReportAllocs()
					b.ResetTimer()
					eobSum := 0
					for i := 0; i < b.N; i++ {
						eobSum += bench.fn(qcoeff, dqcoeff)
					}
					if eobSum == 0 {
						b.Fatal("unexpected zero eob accumulator")
					}
				})
			}
		}
	}
}

func BenchmarkVP9QuantizeB32x32ScanOrder(b *testing.B) {
	scanOrder := common.DefaultScanOrders[common.Tx32x32]
	coeff := make([]int16, 1024)
	for i := range coeff {
		v := (i*131)%8192 - 4096
		if i%13 == 0 {
			v /= 16
		}
		coeff[i] = int16(v)
	}
	dequant := [2]int16{38, 44}
	const qindex = 87

	for _, bench := range []struct {
		name string
		fn   func(qcoeff, dqcoeff []int16) int
	}{
		{
			name: "scan",
			fn: func(qcoeff, dqcoeff []int16) int {
				return QuantizeB32x32WithQ(coeff, qindex, dequant,
					scanOrder.Scan, qcoeff, dqcoeff)
			},
		},
		{
			name: "scanorder",
			fn: func(qcoeff, dqcoeff []int16) int {
				return QuantizeB32x32WithQScanOrder(coeff, qindex, dequant,
					scanOrder, qcoeff, dqcoeff)
			},
		},
	} {
		b.Run(bench.name, func(b *testing.B) {
			qcoeff := make([]int16, 1024)
			dqcoeff := make([]int16, 1024)
			b.ReportAllocs()
			b.ResetTimer()
			eobSum := 0
			for i := 0; i < b.N; i++ {
				eobSum += bench.fn(qcoeff, dqcoeff)
			}
			if eobSum == 0 {
				b.Fatal("unexpected zero eob accumulator")
			}
		})
	}
}

func BenchmarkForwardDCT16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(10))
	var input [16 * 16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	for _, tc := range []struct {
		name string
		fn   func([]int16, int, *[256]int16)
	}{
		{
			name: "scalar",
			fn: func(input []int16, stride int, output *[256]int16) {
				forwardDCT16x16Scalar(input, stride, output[:])
			},
		},
		{name: "dispatch", fn: ForwardDCT16x16},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var output [16 * 16]int16
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.fn(input[:], 16, &output)
			}
		})
	}
}

func TestForwardWHT4x4MatchesLibvpxSentinels(t *testing.T) {
	var constant [16]int16
	for i := range constant {
		constant[i] = 10
	}
	var got [16]int16
	ForwardWHT4x4Into(constant[:], 4, got[:])
	if got[0] != 160 {
		t.Fatalf("constant WHT DC = %d, want 160; coeffs=%v", got[0], got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("constant WHT AC[%d] = %d, want 0; coeffs=%v", i, got[i], got)
		}
	}

	input := [16]int16{
		0, 1, 2, 3,
		4, 5, 6, 7,
		8, 9, 10, 11,
		12, 13, 14, 15,
	}
	want := [16]int16{
		120, -16, 0, -8,
		-64, 0, 0, 0,
		0, 0, 0, 0,
		-32, 0, 0, 0,
	}
	ForwardWHT4x4Into(input[:], 4, got[:])
	if got != want {
		t.Fatalf("ramp WHT mismatch\ngot  %v\nwant %v", got, want)
	}
}

func TestForwardDCT8x8ConstantKeepsOnlyDC(t *testing.T) {
	var input [64]int16
	for i := range input {
		input[i] = 10
	}
	var got [64]int16
	ForwardDCT8x8(input[:], 8, &got)
	if got[0] != 639 {
		t.Fatalf("constant block DC = %d, want 639; coeffs=%v", got[0], got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("constant block AC[%d] = %d, want 0; coeffs=%v", i, got[i], got)
		}
	}
}

func TestForwardDCT16x16ConstantKeepsOnlyDC(t *testing.T) {
	var input [256]int16
	for i := range input {
		input[i] = 10
	}
	var got [256]int16
	ForwardDCT16x16(input[:], 16, &got)
	if got[0] != 1278 {
		t.Fatalf("constant block DC = %d, want 1278; coeffs=%v", got[0], got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("constant block AC[%d] = %d, want 0; coeffs=%v", i, got[i], got)
		}
	}
}

func TestForwardDCT32x32ConstantKeepsOnlyDC(t *testing.T) {
	var input [1024]int16
	for i := range input {
		input[i] = 10
	}
	var got [1024]int16
	ForwardDCT32x32(input[:], 32, &got)
	if got[0] != 1278 {
		t.Fatalf("constant block DC = %d, want 1278; coeffs=%v", got[0], got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != 0 {
			t.Fatalf("constant block AC[%d] = %d, want 0; coeffs=%v", i, got[i], got)
		}
	}
}

// TestForwardDCT4x4RampPinnedLibvpx pins ForwardDCT4x4 against output
// generated by running the byte-identical libvpx v1.16.0 vpx_fdct4x4_c
// (vpx_dsp/fwd_txfm.c:15) on a ramp residual block. The oracle program
// is at /tmp/fdct_oracle/main.c; rerun there with cc -O2 to regenerate
// the expected values if libvpx is ever upgraded.
func TestForwardDCT4x4RampPinnedLibvpx(t *testing.T) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i)
	}
	var got [16]int16
	ForwardDCT4x4(input[:], 4, &got)
	want := [16]int16{
		240, -36, 0, -3,
		-143, 0, 0, 0,
		0, 0, 0, 0,
		-10, 0, 0, 0,
	}
	if got != want {
		t.Fatalf("fdct4x4 ramp mismatch\ngot  %v\nwant %v", got, want)
	}
}

// TestForwardDCT4x4AlternatingPinnedLibvpx pins ForwardDCT4x4 against
// libvpx v1.16.0 vpx_fdct4x4_c with alternating +/-100 residual values.
func TestForwardDCT4x4AlternatingPinnedLibvpx(t *testing.T) {
	var input [16]int16
	for i := range input {
		if i%2 == 0 {
			input[i] = 100
		} else {
			input[i] = -100
		}
	}
	var got [16]int16
	ForwardDCT4x4(input[:], 4, &got)
	want := [16]int16{
		0, 1225, 0, 2956,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}
	if got != want {
		t.Fatalf("fdct4x4 alt mismatch\ngot  %v\nwant %v", got, want)
	}
}

// TestForwardDCT8x8RampPinnedLibvpx pins ForwardDCT8x8 against libvpx
// v1.16.0 vpx_fdct8x8_c (vpx_dsp/fwd_txfm.c:90) on the residual i-32.
func TestForwardDCT8x8RampPinnedLibvpx(t *testing.T) {
	var input [64]int16
	for i := range input {
		input[i] = int16(i - 32)
	}
	var got [64]int16
	ForwardDCT8x8(input[:], 8, &got)
	want := [64]int16{
		-32, -146, 0, -15, 0, -5, 0, -1,
		-1165, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		-121, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		-37, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		-8, 0, 0, 0, 0, 0, 0, 0,
	}
	if got != want {
		t.Fatalf("fdct8x8 ramp mismatch\ngot  %v\nwant %v", got, want)
	}
}

// TestForwardDCT16x16RampPinnedLibvpx pins ForwardDCT16x16 against
// libvpx v1.16.0 vpx_fdct16x16_c (vpx_dsp/fwd_txfm.c:183) on the
// residual (i-128)/4 with stride 16. Spot-checks key low-frequency and
// boundary indices since pinning all 256 coefficients is unwieldy.
func TestForwardDCT16x16RampPinnedLibvpx(t *testing.T) {
	var input [256]int16
	for i := range input {
		input[i] = int16((i - 128) / 4)
	}
	var got [256]int16
	ForwardDCT16x16(input[:], 16, &got)
	want := map[int]int16{
		0:   -16,
		1:   -146,
		2:   0,
		3:   -12,
		16:  -2299,
		17:  -4,
		32:  0,
		64:  0,
		128: 0,
		255: 2,
	}
	for idx, expected := range want {
		if got[idx] != expected {
			t.Fatalf("fdct16x16[%d] = %d, want %d", idx, got[idx], expected)
		}
	}
}

// TestForwardDCT32x32RampPinnedLibvpx pins ForwardDCT32x32 against
// libvpx v1.16.0 vpx_fdct32x32_c (vpx_dsp/fwd_txfm.c:708) on the
// residual (i-512)/8 with stride 32. Spot-checks key low-frequency and
// boundary indices.
func TestForwardDCT32x32RampPinnedLibvpx(t *testing.T) {
	var input [1024]int16
	for i := range input {
		input[i] = int16((i - 512) / 8)
	}
	var got [1024]int16
	ForwardDCT32x32(input[:], 32, &got)
	want := map[int]int16{
		0:    -8,
		1:    -142,
		2:    0,
		3:    -11,
		4:    0,
		8:    -10,
		16:   -8,
		32:   -4641,
		64:   0,
		128:  0,
		256:  0,
		512:  0,
		1023: 0,
	}
	for idx, expected := range want {
		if got[idx] != expected {
			t.Fatalf("fdct32x32[%d] = %d, want %d", idx, got[idx], expected)
		}
	}
}

// TestForwardDCT32x32RDPinnedLibvpxCOracle pins ForwardDCT32x32RD against
// values produced by the libvpx v1.16.0 C oracle vpx_fdct32x32_rd_c
// (vpx_dsp/fwd_txfm.c:735). The expected coefficients below were captured
// from a direct CGO-free C harness linked against
// internal/coracle/build/libvpx-v1.16.0-vpxdec-vp9/vpx_dsp/fwd_txfm.c.o
// and must remain byte-for-byte identical to the libvpx C output.
func TestForwardDCT32x32RDPinnedLibvpxCOracle(t *testing.T) {
	// Corpus A: (i-512)/8 ramp, stride 32.
	var ramp [1024]int16
	for i := range ramp {
		ramp[i] = int16((i - 512) / 8)
	}
	var gotRamp [1024]int16
	ForwardDCT32x32RD(ramp[:], 32, &gotRamp)
	wantRamp := map[int]int16{
		0:    -8,
		1:    -140,
		2:    0,
		3:    -12,
		4:    1,
		8:    -10,
		16:   -8,
		32:   -4640,
		33:   -3,
		64:   0,
		128:  0,
		256:  0,
		512:  0,
		1023: 0,
	}
	for idx, expected := range wantRamp {
		if gotRamp[idx] != expected {
			t.Fatalf("ramp fdct32x32_rd[%d] = %d, want %d", idx, gotRamp[idx], expected)
		}
	}

	// Corpus B: constant 100, stride 32 — must concentrate in DC.
	var constInput [1024]int16
	for i := range constInput {
		constInput[i] = 100
	}
	var gotConst [1024]int16
	ForwardDCT32x32RD(constInput[:], 32, &gotConst)
	if gotConst[0] != 12801 {
		t.Fatalf("const100 fdct32x32_rd[0] = %d, want 12801", gotConst[0])
	}
	for i := 1; i < 1024; i++ {
		if gotConst[i] != 0 {
			t.Fatalf("const100 fdct32x32_rd[%d] = %d, want 0", i, gotConst[i])
		}
	}

	// Corpus C: alternating-sign rows; energy should land at the row
	// Nyquist (index 32).
	var rowSign [1024]int16
	for r := range 32 {
		v := int16(50)
		if r&1 == 1 {
			v = -50
		}
		for c := range 32 {
			rowSign[r*32+c] = v
		}
	}
	var gotRowSign [1024]int16
	ForwardDCT32x32RD(rowSign[:], 32, &gotRowSign)
	wantRowSign := map[int]int16{
		0:    0,
		1:    0,
		16:   0,
		32:   283,
		33:   0,
		64:   0,
		1023: 0,
	}
	for idx, expected := range wantRowSign {
		if gotRowSign[idx] != expected {
			t.Fatalf("rowsign fdct32x32_rd[%d] = %d, want %d", idx, gotRowSign[idx], expected)
		}
	}
}

// TestForwardDCT32x32RDDiffersFromPrecisionVariant verifies that the RD
// variant produces meaningfully different output from the precision
// vpx_fdct32x32_c on a non-degenerate residual. libvpx documents the RD
// pipeline as a low-precision approximation (vpx_dsp/fwd_txfm.c:732-734).
// The exact divergence count (64 mismatched indices on the (i-512)/8
// ramp) was captured against the C oracle and is asserted here.
func TestForwardDCT32x32RDDiffersFromPrecisionVariant(t *testing.T) {
	var input [1024]int16
	for i := range input {
		input[i] = int16((i - 512) / 8)
	}
	var rd, precise [1024]int16
	ForwardDCT32x32RD(input[:], 32, &rd)
	ForwardDCT32x32(input[:], 32, &precise)
	diff := 0
	for i := range rd {
		if rd[i] != precise[i] {
			diff++
		}
	}
	const want = 64
	if diff != want {
		t.Fatalf("rd vs precision divergence = %d coefficients, want %d", diff, want)
	}
}

func TestForwardDCTCospiConstantsMatchLibvpx(t *testing.T) {
	if fdctCospi26_64 != 4756 {
		t.Fatalf("fdctCospi26_64 = %d, want 4756", fdctCospi26_64)
	}
	if fdctCospi27_64 != 3981 {
		t.Fatalf("fdctCospi27_64 = %d, want 3981", fdctCospi27_64)
	}
}

func TestForwardHTDctDctMatchesForwardDCT(t *testing.T) {
	var in4 [16]int16
	for i := range in4 {
		in4[i] = int16((i*17)%41 - 20)
	}
	var got4, want4 [16]int16
	ForwardHT4x4Into(in4[:], 4, common.DctDct, got4[:])
	ForwardDCT4x4(in4[:], 4, &want4)
	if got4 != want4 {
		t.Fatalf("4x4 DCT_DCT mismatch\ngot  %v\nwant %v", got4, want4)
	}

	var in8 [64]int16
	for i := range in8 {
		in8[i] = int16((i*13)%73 - 36)
	}
	var got8, want8 [64]int16
	ForwardHT8x8Into(in8[:], 8, common.DctDct, got8[:])
	ForwardDCT8x8(in8[:], 8, &want8)
	if got8 != want8 {
		t.Fatalf("8x8 DCT_DCT mismatch\ngot  %v\nwant %v", got8, want8)
	}

	var in16 [256]int16
	for i := range in16 {
		in16[i] = int16((i*11)%97 - 48)
	}
	var got16, want16 [256]int16
	ForwardHT16x16Into(in16[:], 16, common.DctDct, got16[:])
	ForwardDCT16x16(in16[:], 16, &want16)
	if got16 != want16 {
		t.Fatalf("16x16 DCT_DCT mismatch")
	}
}

func TestForwardHTHybridTransformsProduceDirectionalCoefficients(t *testing.T) {
	var in [256]int16
	for y := range 16 {
		for x := range 16 {
			in[y*16+x] = int16((x * (y + 3)) - 60)
		}
	}
	var dct, adstDct, dctAdst [256]int16
	ForwardHT16x16Into(in[:], 16, common.DctDct, dct[:])
	ForwardHT16x16Into(in[:], 16, common.AdstDct, adstDct[:])
	ForwardHT16x16Into(in[:], 16, common.DctAdst, dctAdst[:])
	if adstDct == dct || dctAdst == dct || adstDct == dctAdst {
		t.Fatalf("hybrid transforms collapsed to identical coefficient sets")
	}
	if adstDct[1] == 0 && adstDct[2] == 0 && dctAdst[1] == 0 && dctAdst[2] == 0 {
		t.Fatalf("hybrid transforms produced no early directional coefficients")
	}
}

func TestForwardHT8x8HybridRampMatchesLibvpx(t *testing.T) {
	var in [64]int16
	for i := range in {
		in[i] = int16(((i*37 + 13) % 255) - 127)
	}
	cases := []struct {
		name   string
		txType common.TxType
		want   [64]int16
	}{
		{
			name:   "ADST_DCT",
			txType: common.AdstDct,
			want: [64]int16{
				-261, -375, -796, 220, -171, 167, 380, 52,
				482, 890, -8, -1082, 277, -38, -105, 100,
				-577, -148, -2633, 607, 88, -547, -55, -374,
				-363, -1389, -52, 1356, -122, 277, 38, 344,
				157, 124, 97, 163, -769, -407, 651, -213,
				-297, -477, -576, 33, -1079, 877, -553, -280,
				-126, -557, 150, 257, 646, 630, -128, -134,
				-90, -226, -241, 41, -544, 28, -682, 836,
			},
		},
		{
			name:   "DCT_ADST",
			txType: common.DctAdst,
			want: [64]int16{
				14, 274, -1202, -231, -609, -342, -71, -77,
				76, 696, -53, -309, -45, 37, -348, -182,
				-135, -232, -2635, 405, -434, -423, -331, -578,
				328, -1775, 169, 948, -501, 474, 46, 586,
				-4, 467, 932, 248, -533, -209, 124, -255,
				-40, -243, -273, -299, -853, 1024, -631, -247,
				60, -409, 82, -436, 757, 401, 2, 516,
				-11, 309, 141, 314, -238, -294, -1033, 311,
			},
		},
		{
			name:   "ADST_ADST",
			txType: common.AdstAdst,
			want: [64]int16{
				-20, 28, -863, -143, -463, -313, 115, 39,
				92, 968, 940, -655, 129, 63, -97, 81,
				-256, 1065, -2358, -392, -40, -674, -347, -785,
				215, -1339, -1317, 607, -178, 118, -93, 338,
				89, 120, 170, 657, -285, -760, 381, -119,
				-6, -102, -513, 247, -1383, 474, -395, -583,
				83, -640, -370, -367, 109, 695, 269, 79,
				43, -39, -193, 230, -428, 76, -1093, 321,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got [64]int16
			ForwardHT8x8Into(in[:], 8, tc.txType, got[:])
			if got != tc.want {
				t.Fatalf("ForwardHT8x8Into mismatch\ngot  %v\nwant %v",
					got, tc.want)
			}
		})
	}
}

func TestForwardHTSmallHybridTransformsDoNotAllocate(t *testing.T) {
	var in4 [16]int16
	for i := range in4 {
		in4[i] = int16((i*17)%41 - 20)
	}
	var in8 [64]int16
	for i := range in8 {
		in8[i] = int16((i*13)%73 - 36)
	}
	var out4 [16]int16
	var out8 [64]int16
	txTypes := [...]common.TxType{common.AdstDct, common.DctAdst, common.AdstAdst}

	allocs := testing.AllocsPerRun(1000, func() {
		for _, txType := range txTypes {
			ForwardHT4x4Into(in4[:], 4, txType, out4[:])
			ForwardHT8x8Into(in8[:], 8, txType, out8[:])
		}
	})
	if allocs != 0 {
		t.Fatalf("small hybrid transforms allocs/run = %f, want 0", allocs)
	}
}

func TestForwardTransformsOverwriteOutput(t *testing.T) {
	tests := []struct {
		name string
		n    int
		fn   func(input []int16, output []int16)
	}{
		{
			name: "WHT4x4",
			n:    16,
			fn: func(input, output []int16) {
				ForwardWHT4x4Into(input, 4, output)
			},
		},
		{
			name: "HT4x4_DCT_DCT",
			n:    16,
			fn: func(input, output []int16) {
				ForwardHT4x4Into(input, 4, common.DctDct, output)
			},
		},
		{
			name: "HT4x4_ADST_DCT",
			n:    16,
			fn: func(input, output []int16) {
				ForwardHT4x4Into(input, 4, common.AdstDct, output)
			},
		},
		{
			name: "HT4x4_DCT_ADST",
			n:    16,
			fn: func(input, output []int16) {
				ForwardHT4x4Into(input, 4, common.DctAdst, output)
			},
		},
		{
			name: "HT4x4_ADST_ADST",
			n:    16,
			fn: func(input, output []int16) {
				ForwardHT4x4Into(input, 4, common.AdstAdst, output)
			},
		},
		{
			name: "HT8x8_DCT_DCT",
			n:    64,
			fn: func(input, output []int16) {
				ForwardHT8x8Into(input, 8, common.DctDct, output)
			},
		},
		{
			name: "HT8x8_ADST_DCT",
			n:    64,
			fn: func(input, output []int16) {
				ForwardHT8x8Into(input, 8, common.AdstDct, output)
			},
		},
		{
			name: "HT8x8_DCT_ADST",
			n:    64,
			fn: func(input, output []int16) {
				ForwardHT8x8Into(input, 8, common.DctAdst, output)
			},
		},
		{
			name: "HT8x8_ADST_ADST",
			n:    64,
			fn: func(input, output []int16) {
				ForwardHT8x8Into(input, 8, common.AdstAdst, output)
			},
		},
		{
			name: "HT16x16_DCT_DCT",
			n:    256,
			fn: func(input, output []int16) {
				ForwardHT16x16Into(input, 16, common.DctDct, output)
			},
		},
		{
			name: "HT16x16_ADST_DCT",
			n:    256,
			fn: func(input, output []int16) {
				ForwardHT16x16Into(input, 16, common.AdstDct, output)
			},
		},
		{
			name: "HT16x16_DCT_ADST",
			n:    256,
			fn: func(input, output []int16) {
				ForwardHT16x16Into(input, 16, common.DctAdst, output)
			},
		},
		{
			name: "HT16x16_ADST_ADST",
			n:    256,
			fn: func(input, output []int16) {
				ForwardHT16x16Into(input, 16, common.AdstAdst, output)
			},
		},
		{
			name: "DCT32x32",
			n:    1024,
			fn: func(input, output []int16) {
				ForwardDCT32x32Into(input, 32, output)
			},
		},
		{
			name: "DCT32x32RD",
			n:    1024,
			fn: func(input, output []int16) {
				ForwardDCT32x32RDInto(input, 32, output)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]int16, tc.n)
			output := make([]int16, tc.n)
			for i := range output {
				output[i] = 12345
			}
			tc.fn(input, output)
			for i, got := range output {
				if got != 0 {
					t.Fatalf("output[%d] = %d, want overwritten zero", i, got)
				}
			}
		})
	}
}

func TestQuantizersOverwriteZeroBlockOutputs(t *testing.T) {
	tests := []struct {
		name string
		n    int
		fn   func(coeff, qcoeff, dqcoeff []int16) int
	}{
		{
			name: "QuantizeB4x4",
			n:    16,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeBWithQ(coeff, 37, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx4x4].Scan, qcoeff, dqcoeff)
			},
		},
		{
			name: "QuantizeB4x4ScanOrder",
			n:    16,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeBWithQScanOrder(coeff, 37, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx4x4], qcoeff, dqcoeff)
			},
		},
		{
			name: "QuantizeB32x32",
			n:    1024,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeB32x32WithQ(coeff, 37, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx32x32].Scan, qcoeff, dqcoeff)
			},
		},
		{
			name: "QuantizeB32x32ScanOrder",
			n:    1024,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeB32x32WithQScanOrder(coeff, 37, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx32x32], qcoeff, dqcoeff)
			},
		},
		{
			name: "QuantizeFP4x4",
			n:    16,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeFPWithQScanOrder(coeff, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx4x4], qcoeff, dqcoeff)
			},
		},
		{
			name: "QuantizeFP32x32",
			n:    1024,
			fn: func(coeff, qcoeff, dqcoeff []int16) int {
				return QuantizeFP32x32WithQ(coeff, [2]int16{38, 44},
					common.DefaultScanOrders[common.Tx32x32].Scan, qcoeff, dqcoeff)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			coeff := make([]int16, tc.n)
			qcoeff := make([]int16, tc.n)
			dqcoeff := make([]int16, tc.n)
			for i := range qcoeff {
				qcoeff[i] = 12345
				dqcoeff[i] = 12345
			}
			if eob := tc.fn(coeff, qcoeff, dqcoeff); eob != 0 {
				t.Fatalf("eob = %d, want 0", eob)
			}
			for i := range qcoeff {
				if qcoeff[i] != 0 || dqcoeff[i] != 0 {
					t.Fatalf("coeff %d: q=%d dq=%d, want zeroed outputs",
						i, qcoeff[i], dqcoeff[i])
				}
			}
		})
	}
}

func TestForwardHT16x16AdstDctConstantMatchesLibvpx(t *testing.T) {
	var input [256]int16
	for i := range input {
		input[i] = -127
	}
	var got [256]int16
	ForwardHT16x16Into(input[:], 16, common.AdstDct, got[:])
	wantNonZero := map[int]int16{
		0:   -14640,
		16:  -4899,
		32:  -2953,
		48:  -2127,
		64:  -1686,
		80:  -1392,
		96:  -1211,
		112: -1063,
		128: -973,
		144: -894,
		160: -837,
		176: -792,
		192: -758,
		208: -747,
		224: -724,
		240: -713,
	}
	for i, gotCoeff := range got {
		wantCoeff := wantNonZero[i]
		if gotCoeff != wantCoeff {
			t.Fatalf("coeff[%d] = %d, want %d; coeffs=%v", i, gotCoeff, wantCoeff, got)
		}
	}
}

func TestQuantizeB16x16AdstDctConstantMatchesLibvpx(t *testing.T) {
	var input [256]int16
	for i := range input {
		input[i] = -127
	}
	var coeff [256]int16
	ForwardHT16x16Into(input[:], 16, common.AdstDct, coeff[:])

	scan := common.ScanOrders[common.Tx16x16][common.AdstDct].Scan
	var got [256]int16
	eob := QuantizeB(coeff[:], 37, [2]int16{38, 44}, scan, got[:])
	if eob != 159 {
		t.Fatalf("eob = %d, want 159; dqcoeff=%v", eob, got)
	}
	wantNonZero := map[int]int16{
		0:   -14630,
		16:  -4884,
		32:  -2948,
		48:  -2112,
		64:  -1672,
		80:  -1408,
		96:  -1188,
		112: -1056,
		128: -968,
		144: -880,
		160: -836,
		176: -792,
		192: -748,
		208: -748,
		224: -704,
		240: -704,
	}
	for i, gotCoeff := range got {
		wantCoeff := wantNonZero[i]
		if gotCoeff != wantCoeff {
			t.Fatalf("dqcoeff[%d] = %d, want %d; dqcoeff=%v", i, gotCoeff, wantCoeff, got)
		}
	}
}

func TestQuantizeFP4x4EmitsDequantizedCoefficients(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx4x4].Scan
	var coeff [16]int16
	coeff[0] = 320
	ac := int(scan[1])
	coeff[ac] = -20
	var dqcoeff [16]int16
	eob := QuantizeFP4x4(&coeff, [2]int16{4, 4}, scan, &dqcoeff)
	if eob != 2 {
		t.Fatalf("eob = %d, want 2; dqcoeff=%v", eob, dqcoeff)
	}
	if dqcoeff[0] != 320 || dqcoeff[ac] != -20 {
		t.Fatalf("dqcoeff[0]=%d dqcoeff[%d]=%d, want 320/-20", dqcoeff[0], ac, dqcoeff[ac])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if i != ac && dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

func TestQuantizeFP32x32EmitsHalfDequantizedCoefficients(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx32x32].Scan
	var coeff [1024]int16
	coeff[0] = 1278
	ac := int(scan[130])
	coeff[ac] = -46
	var dqcoeff [1024]int16
	eob := QuantizeFP32x32(coeff[:], [2]int16{7, 6}, scan, dqcoeff[:])
	if eob != 131 {
		t.Fatalf("eob = %d, want 131; dqcoeff=%v", eob, dqcoeff)
	}
	if dqcoeff[0] != 1277 || dqcoeff[ac] != -45 {
		t.Fatalf("dqcoeff[0]=%d dqcoeff[%d]=%d, want 1277/-45", dqcoeff[0], ac, dqcoeff[ac])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if i != ac && dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// TestQuantizeB32x32WithQEmitsQcoeff verifies the libvpx-faithful qcoeff
// emit path from vpx_dsp/quantize.c:216-275. Both qcoeff and
// dqcoeff must be populated in lockstep, and the qcoeff value must equal
// the libvpx vp9_get_token_cost(v, ...) argument — the signed quantized
// magnitude before the Tx32x32 /2 dequant scaling — so cost_coeffs can
// consume it without recovering q from int16-wrapped dqcoeff.
func TestQuantizeB32x32WithQEmitsQcoeff(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx32x32].Scan
	var coeff [1024]int16
	// Choose a high-magnitude coefficient so q*dq overflows int16
	// (q*dq/2 > 32767). libvpx's dqcoeff cast wraps, but qcoeff
	// retains the unwrapped magnitude.
	coeff[0] = 10000
	ac := int(scan[7])
	coeff[ac] = -8000
	dequant := [2]int16{16, 17}
	const qindex = 32
	var qcoeff, dqcoeff [1024]int16
	eob := QuantizeB32x32WithQ(coeff[:], qindex, dequant, scan, qcoeff[:], dqcoeff[:])
	if eob == 0 {
		t.Fatal("eob=0; expected non-zero qcoeff")
	}
	// libvpx writes both arrays from the same tmp/sign; the only allowed
	// divergence is the int16 cast of dqcoeff = qcoeff * dequant / 2.
	for rc := range dqcoeff {
		q := int(qcoeff[rc])
		slot := 1
		if rc == 0 {
			slot = 0
		}
		want := int16(q * int(dequant[slot]) / 2)
		if dqcoeff[rc] != want {
			t.Fatalf("rc=%d qcoeff=%d dqcoeff=%d want=%d", rc, q, dqcoeff[rc], want)
		}
	}
	// Verify the overflow case yields a qcoeff that can NOT be recovered
	// from dqcoeff via the legacy /dq recipe.
	q0 := int(qcoeff[0])
	if q0 == 0 {
		t.Fatal("expected nonzero qcoeff[0]")
	}
	// The libvpx recovery used to be: |q| = (2*|dqcoeff| + dq - 1) / dq.
	// When 2*qcoeff*dequant/2 fits in int16 this matches |q|; outside
	// that range the recovery drifts.
	dq := int(dequant[0])
	absDQ := int(dqcoeff[0])
	if absDQ < 0 {
		absDQ = -absDQ
	}
	rec := (absDQ*2 + dq - 1) / dq
	absQ0 := q0
	if absQ0 < 0 {
		absQ0 = -absQ0
	}
	// True wraparound check: dq*|q|/2 must fit in int16 for the legacy
	// recovery to agree.
	wide := int(dequant[0]) * absQ0 / 2
	if wide > 32767 || wide < -32768 {
		// Recovery is allowed (and expected) to drift here; qcoeff
		// stays correct because it is unscaled by /2.
		if rec == absQ0 {
			t.Fatalf("expected legacy recovery to drift when q*dq/2 overflows int16, "+
				"but rec=%d matched abs(qcoeff)=%d", rec, absQ0)
		}
	}
}

// TestQuantizeBWithQEmitsQcoeff is the non-32x32 sibling of
// TestQuantizeB32x32WithQEmitsQcoeff. libvpx vpx_dsp/quantize.c:71-72
// writes both qcoeff and dqcoeff in lockstep; dqcoeff = qcoeff*dequant
// (no /2). Verifies qcoeff*dequant truncated to int16 matches dqcoeff.
func TestQuantizeBWithQEmitsQcoeff(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx16x16].Scan
	var coeff [256]int16
	coeff[0] = 4000
	ac := int(scan[3])
	coeff[ac] = -2000
	dequant := [2]int16{20, 25}
	const qindex = 60
	var qcoeff, dqcoeff [256]int16
	eob := QuantizeBWithQ(coeff[:], qindex, dequant, scan, qcoeff[:], dqcoeff[:])
	if eob == 0 {
		t.Fatal("eob=0; expected non-zero qcoeff")
	}
	for rc := range dqcoeff {
		q := int(qcoeff[rc])
		slot := 1
		if rc == 0 {
			slot = 0
		}
		want := int16(q * int(dequant[slot]))
		if dqcoeff[rc] != want {
			t.Fatalf("rc=%d qcoeff=%d dqcoeff=%d want=%d", rc, q, dqcoeff[rc], want)
		}
	}
}

func TestQuantizeBWithQScanOrderMatchesScanPath(t *testing.T) {
	for _, tx := range []common.TxSize{common.Tx4x4, common.Tx8x8, common.Tx16x16} {
		n := 16 << (2 * int(tx))
		for txType := range common.TxTypes {
			scanOrder := common.ScanOrders[tx][txType]
			for trial := range 12 {
				name := fmt.Sprintf("tx%d/type%d/trial%d", tx, txType, trial)
				t.Run(name, func(t *testing.T) {
					coeff := make([]int16, n)
					fillVP9QuantizeBenchCoeffs(coeff,
						int64(1000+int(tx)*100+int(txType)*17+trial))
					switch trial % 4 {
					case 0:
						clear(coeff)
					case 1:
						coeff[0] = int16(500 + 13*trial)
					case 2:
						coeff[int(scanOrder.Scan[n-1])] = int16(-900 + trial)
					}

					wantQ := make([]int16, n)
					wantDQ := make([]int16, n)
					gotQ := make([]int16, n)
					gotDQ := make([]int16, n)
					wantEOB := QuantizeBWithQ(coeff, 87, [2]int16{38, 44},
						scanOrder.Scan, wantQ, wantDQ)
					gotEOB := QuantizeBWithQScanOrder(coeff, 87, [2]int16{38, 44},
						scanOrder, gotQ, gotDQ)
					if gotEOB != wantEOB {
						t.Fatalf("eob = %d, want %d", gotEOB, wantEOB)
					}
					for i := range n {
						if gotQ[i] != wantQ[i] || gotDQ[i] != wantDQ[i] {
							t.Fatalf("coeff %d: q=%d/%d dq=%d/%d",
								i, gotQ[i], wantQ[i], gotDQ[i], wantDQ[i])
						}
					}

					gotDQOnly := make([]int16, n)
					gotDQOnlyEOB := QuantizeBWithQScanOrder(coeff, 87,
						[2]int16{38, 44}, scanOrder, nil, gotDQOnly)
					if gotDQOnlyEOB != wantEOB {
						t.Fatalf("dq-only eob = %d, want %d", gotDQOnlyEOB, wantEOB)
					}
					for i := range n {
						if gotDQOnly[i] != wantDQ[i] {
							t.Fatalf("dq-only coeff %d: got %d, want %d",
								i, gotDQOnly[i], wantDQ[i])
						}
					}
				})
			}
		}
	}
}

func TestQuantizeBWithQScanOrderClearsSparseStaleOutputs(t *testing.T) {
	for _, tx := range []common.TxSize{common.Tx4x4, common.Tx8x8, common.Tx16x16} {
		n := 16 << (2 * int(tx))
		t.Run(fmt.Sprintf("tx%d", tx), func(t *testing.T) {
			scanOrder := common.DefaultScanOrders[tx]
			coeff := make([]int16, n)
			coeff[0] = 1000
			coeff[int(scanOrder.Scan[1])] = 1
			qcoeff := make([]int16, n)
			dqcoeff := make([]int16, n)
			for i := range qcoeff {
				qcoeff[i] = 12345
				dqcoeff[i] = -12345
			}

			eob := QuantizeBWithQScanOrder(coeff, 87, [2]int16{38, 44},
				scanOrder, qcoeff, dqcoeff)
			if eob == 0 {
				t.Fatal("eob=0; expected DC coefficient to survive")
			}
			for i := 1; i < n; i++ {
				if qcoeff[i] != 0 || dqcoeff[i] != 0 {
					t.Fatalf("stale coeff %d survived: q=%d dq=%d",
						i, qcoeff[i], dqcoeff[i])
				}
			}

			for i := range dqcoeff {
				dqcoeff[i] = -12345
			}
			eob = QuantizeBWithQScanOrder(coeff, 87, [2]int16{38, 44},
				scanOrder, nil, dqcoeff)
			if eob == 0 {
				t.Fatal("dq-only eob=0; expected DC coefficient to survive")
			}
			for i := 1; i < n; i++ {
				if dqcoeff[i] != 0 {
					t.Fatalf("stale dqcoeff %d survived: dq=%d", i, dqcoeff[i])
				}
			}
		})
	}
}

func TestQuantizeB32x32WithQScanOrderMatchesScanPath(t *testing.T) {
	scanOrder := common.DefaultScanOrders[common.Tx32x32]
	for trial := range 12 {
		t.Run(fmt.Sprintf("trial%d", trial), func(t *testing.T) {
			const n = 1024
			coeff := make([]int16, n)
			fillVP9QuantizeBenchCoeffs(coeff, int64(2000+trial))
			switch trial % 4 {
			case 0:
				clear(coeff)
			case 1:
				coeff[0] = int16(1000 + 31*trial)
			case 2:
				coeff[int(scanOrder.Scan[n-1])] = int16(-2000 + trial)
			}

			wantQ := make([]int16, n)
			wantDQ := make([]int16, n)
			gotQ := make([]int16, n)
			gotDQ := make([]int16, n)
			wantEOB := QuantizeB32x32WithQ(coeff, 87, [2]int16{38, 44},
				scanOrder.Scan, wantQ, wantDQ)
			gotEOB := QuantizeB32x32WithQScanOrder(coeff, 87, [2]int16{38, 44},
				scanOrder, gotQ, gotDQ)
			if gotEOB != wantEOB {
				t.Fatalf("eob = %d, want %d", gotEOB, wantEOB)
			}
			for i := range n {
				if gotQ[i] != wantQ[i] || gotDQ[i] != wantDQ[i] {
					t.Fatalf("coeff %d: q=%d/%d dq=%d/%d",
						i, gotQ[i], wantQ[i], gotDQ[i], wantDQ[i])
				}
			}

			gotDQOnly := make([]int16, n)
			gotDQOnlyEOB := QuantizeB32x32WithQScanOrder(coeff, 87,
				[2]int16{38, 44}, scanOrder, nil, gotDQOnly)
			if gotDQOnlyEOB != wantEOB {
				t.Fatalf("dq-only eob = %d, want %d", gotDQOnlyEOB, wantEOB)
			}
			for i := range n {
				if gotDQOnly[i] != wantDQ[i] {
					t.Fatalf("dq-only coeff %d: got %d, want %d",
						i, gotDQOnly[i], wantDQ[i])
				}
			}
		})
	}
}

func fillVP9QuantizeBenchCoeffs(coeff []int16, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	for i := range coeff {
		v := rng.Intn(4097) - 2048
		if i%5 == 0 {
			v /= 8
		}
		if i%17 == 0 {
			v = 0
		}
		coeff[i] = int16(v)
	}
}

func TestQuantizeFP16x16EmitsDequantizedCoefficients(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx16x16].Scan
	var coeff [256]int16
	coeff[0] = 1278
	ac := int(scan[63])
	coeff[ac] = -46
	var dqcoeff [256]int16
	eob := QuantizeFP(coeff[:], [2]int16{7, 6}, scan, dqcoeff[:])
	if eob != 64 {
		t.Fatalf("eob = %d, want 64; dqcoeff=%v", eob, dqcoeff)
	}
	if dqcoeff[0] != 1274 || dqcoeff[ac] != -42 {
		t.Fatalf("dqcoeff[0]=%d dqcoeff[%d]=%d, want 1274/-42", dqcoeff[0], ac, dqcoeff[ac])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if i != ac && dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

func TestQuantizeFP8x8EmitsDequantizedCoefficients(t *testing.T) {
	scan := common.DefaultScanOrders[common.Tx8x8].Scan
	var coeff [64]int16
	coeff[0] = 639
	ac := int(scan[17])
	coeff[ac] = -31
	var dqcoeff [64]int16
	eob := QuantizeFP(coeff[:], [2]int16{3, 5}, scan, dqcoeff[:])
	if eob != 18 {
		t.Fatalf("eob = %d, want 18; dqcoeff=%v", eob, dqcoeff)
	}
	if dqcoeff[0] != 639 || dqcoeff[ac] != -30 {
		t.Fatalf("dqcoeff[0]=%d dqcoeff[%d]=%d, want 639/-30", dqcoeff[0], ac, dqcoeff[ac])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if i != ac && dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// referenceQuantizeFPC is the byte-identical Go transcription of libvpx
// v1.16.0 vp9_quantize_fp_c (vp9/encoder/vp9_quantize.c:26). It is used
// only by TestVP9QuantizeFPMatchesLibvpxContract as the oracle that the
// govpx port must match exactly. Kept verbatim — do not optimise.
func referenceQuantizeFPC(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan []int16, qcoeff, dqcoeff []int16,
) int {
	for i := range nCoeffs {
		qcoeff[i] = 0
		dqcoeff[i] = 0
	}
	eob := -1
	for i := range nCoeffs {
		rc := int(scan[i])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		tmp := min(absCoeff+int(roundFP[slot]), 32767)
		tmp = (tmp * int(quantFP[slot])) >> 16
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[rc] = int16(q)
		dqcoeff[rc] = int16(q * int(dequant[slot]))
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

// TestVP9QuantizeFPMatchesLibvpxContract is the parity guard for the
// govpx libvpx-shaped quantize entry point (QuantizeFPLibvpx). For five
// representative coefficient layouts drawn from real encoder workloads
// (all-zero, single DC, single high-freq AC, dense random, ±boundary)
// it cross-checks (qcoeff, dqcoeff, eob) against the byte-identical
// libvpx vp9_quantize_fp_c oracle. Any divergence breaks the contract
// the vp9_quantize_fp_neon kernel relies on for byte parity.
func TestVP9QuantizeFPMatchesLibvpxContract(t *testing.T) {
	// Real q=64 dequant table values from libvpx vp9_init_quantizer for
	// the Y plane: y_dequant[64][0..1] = {16, 17}. Derived round_fp and
	// quant_fp follow libvpx: vp9/encoder/vp9_quantize.c:209-210.
	dequant := [2]int16{16, 17}
	roundFP := [2]int16{
		int16((48 * int(dequant[0])) >> 7),
		int16((42 * int(dequant[1])) >> 7),
	}
	quantFP := [2]int16{
		int16((1 << 16) / int(dequant[0])),
		int16((1 << 16) / int(dequant[1])),
	}

	type tc struct {
		name    string
		txSize  common.TxSize
		nCoeffs int
		fill    func(coeff []int16)
	}
	cases := []tc{
		{
			name:    "all-zero 4x4",
			txSize:  common.Tx4x4,
			nCoeffs: 16,
			fill:    func(c []int16) {},
		},
		{
			name:    "single DC 8x8",
			txSize:  common.Tx8x8,
			nCoeffs: 64,
			fill: func(c []int16) {
				c[0] = 312
			},
		},
		{
			name:    "single high-freq AC 16x16",
			txSize:  common.Tx16x16,
			nCoeffs: 256,
			fill: func(c []int16) {
				scan := common.DefaultScanOrders[common.Tx16x16].Scan
				rc := int(scan[200])
				c[rc] = -918
			},
		},
		{
			name:    "dense random +/-1024 8x8",
			txSize:  common.Tx8x8,
			nCoeffs: 64,
			fill: func(c []int16) {
				r := rand.New(rand.NewSource(0xC0FFEE))
				for i := range c {
					c[i] = int16(r.Intn(2049) - 1024)
				}
			},
		},
		{
			name:    "boundary int16 extrema 4x4",
			txSize:  common.Tx4x4,
			nCoeffs: 16,
			fill: func(c []int16) {
				for i := range c {
					switch i % 3 {
					case 0:
						c[i] = 32767
					case 1:
						c[i] = -32767
					default:
						c[i] = -32768
					}
				}
			},
		},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			so := common.DefaultScanOrders[tcase.txSize]
			scan := so.Scan[:tcase.nCoeffs]
			iscan := so.IScan[:tcase.nCoeffs]
			coeff := make([]int16, tcase.nCoeffs)
			tcase.fill(coeff)

			gotQ := make([]int16, tcase.nCoeffs)
			gotDQ := make([]int16, tcase.nCoeffs)
			gotEOB := QuantizeFPLibvpx(coeff, tcase.nCoeffs, roundFP, quantFP, dequant,
				scan, iscan, gotQ, gotDQ)

			wantQ := make([]int16, tcase.nCoeffs)
			wantDQ := make([]int16, tcase.nCoeffs)
			wantEOB := referenceQuantizeFPC(coeff, tcase.nCoeffs, roundFP, quantFP, dequant,
				scan, wantQ, wantDQ)

			if gotEOB != wantEOB {
				t.Fatalf("eob mismatch: got %d, want %d", gotEOB, wantEOB)
			}
			for i := range tcase.nCoeffs {
				if gotQ[i] != wantQ[i] {
					t.Fatalf("qcoeff[%d] mismatch: got %d, want %d", i, gotQ[i], wantQ[i])
				}
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] mismatch: got %d, want %d", i, gotDQ[i], wantDQ[i])
				}
			}

			// Cross-check the legacy QuantizeFP entry point still
			// produces the same dqcoeff + eob even though it doesn't
			// emit qcoeff. This guarantees no regression for the
			// existing hot-path callers.
			legacyDQ := make([]int16, tcase.nCoeffs)
			legacyEOB := QuantizeFP(coeff, dequant, scan, legacyDQ)
			if legacyEOB != wantEOB {
				t.Fatalf("legacy QuantizeFP eob mismatch: got %d, want %d", legacyEOB, wantEOB)
			}
			for i := range tcase.nCoeffs {
				if legacyDQ[i] != wantDQ[i] {
					t.Fatalf("legacy QuantizeFP dqcoeff[%d] mismatch: got %d, want %d",
						i, legacyDQ[i], wantDQ[i])
				}
			}
		})
	}
}

func TestQuantizeFPWithQScanOrderMatchesSynthesizedIScan(t *testing.T) {
	dequant := [2]int16{37, 43}
	so := common.ScanOrders[common.Tx16x16][common.AdstDct]
	var coeff [256]int16
	for i := range coeff {
		if i%5 == 0 {
			coeff[i] = int16((i*37)%2049 - 1024)
		}
	}

	var synthQ, synthDQ [256]int16
	synthEOB := QuantizeFPWithQ(coeff[:], dequant, so.Scan, synthQ[:], synthDQ[:])

	var orderQ, orderDQ [256]int16
	orderEOB := QuantizeFPWithQScanOrder(coeff[:], dequant, so, orderQ[:], orderDQ[:])

	if orderEOB != synthEOB {
		t.Fatalf("eob = %d, want %d", orderEOB, synthEOB)
	}
	if orderQ != synthQ {
		t.Fatalf("qcoeff mismatch\n got %v\nwant %v", orderQ, synthQ)
	}
	if orderDQ != synthDQ {
		t.Fatalf("dqcoeff mismatch\n got %v\nwant %v", orderDQ, synthDQ)
	}
}

// referenceQuantizeFP32x32C is the byte-identical Go transcription of
// libvpx v1.16.0 vp9_quantize_fp_32x32_c (vp9/encoder/vp9_quantize.c:92).
// Kept verbatim — do not optimise.
func referenceQuantizeFP32x32C(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan []int16, qcoeff, dqcoeff []int16,
) int {
	for i := range nCoeffs {
		qcoeff[i] = 0
		dqcoeff[i] = 0
	}
	eob := -1
	for i := range nCoeffs {
		rc := int(scan[i])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		c := int(coeff[rc])
		coeffSign := 0
		if c < 0 {
			coeffSign = -1
		}
		absCoeff := (c ^ coeffSign) - coeffSign
		tmp := 0
		if absCoeff >= int(dequant[slot])>>2 {
			// abs_coeff += ROUND_POWER_OF_TWO(round_ptr[rc != 0], 1)
			absCoeff += (int(roundFP[slot]) + 1) >> 1
			// clamp(abs_coeff, INT16_MIN, INT16_MAX)
			if absCoeff > 32767 {
				absCoeff = 32767
			}
			if absCoeff < -32768 {
				absCoeff = -32768
			}
			tmp = (absCoeff * int(quantFP[slot])) >> 15
			// qcoeff_ptr[rc] = (tmp ^ coeff_sign) - coeff_sign
			q := (tmp ^ coeffSign) - coeffSign
			qcoeff[rc] = int16(q)
			// dqcoeff_ptr[rc] = qcoeff_ptr[rc] * dequant_ptr[..] / 2  (qcoeff is tran_low_t/int16 here)
			dqcoeff[rc] = int16(int(qcoeff[rc]) * int(dequant[slot]) / 2)
		}
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

// referenceQuantizeBC is the byte-identical Go transcription of libvpx
// v1.16.0 vpx_quantize_b_c (vpx_dsp/quantize.c:118). Kept verbatim — do
// not optimise.
func referenceQuantizeBC(coeff []int16, nCoeffs int,
	zbin, round, quant, quantShift, dequant [2]int16,
	scan []int16, qcoeff, dqcoeff []int16,
) int {
	for i := range nCoeffs {
		qcoeff[i] = 0
		dqcoeff[i] = 0
	}
	zbins := [2]int{int(zbin[0]), int(zbin[1])}
	nzbins := [2]int{-zbins[0], -zbins[1]}
	nonZeroCount := nCoeffs
	for i := nCoeffs - 1; i >= 0; i-- {
		rc := int(scan[i])
		c := int(coeff[rc])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		if c < zbins[slot] && c > nzbins[slot] {
			nonZeroCount--
		} else {
			break
		}
	}
	eob := -1
	for i := 0; i < nonZeroCount; i++ {
		rc := int(scan[i])
		c := int(coeff[rc])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		coeffSign := 0
		if c < 0 {
			coeffSign = -1
		}
		absCoeff := (c ^ coeffSign) - coeffSign
		if absCoeff >= zbins[slot] {
			tmp := max(min(absCoeff+int(round[slot]), 32767), -32768)
			tmp = ((((tmp * int(quant[slot])) >> 16) + tmp) *
				int(quantShift[slot])) >> 16
			q := (tmp ^ coeffSign) - coeffSign
			qcoeff[rc] = int16(q)
			dqcoeff[rc] = int16(int(qcoeff[rc]) * int(dequant[slot]))
			if tmp != 0 {
				eob = i
			}
		}
	}
	return eob + 1
}

// referenceQuantizeB32x32C is the byte-identical Go transcription of
// libvpx v1.16.0 vpx_quantize_b_32x32_c (vpx_dsp/quantize.c:216). Kept
// verbatim — do not optimise.
func referenceQuantizeB32x32C(coeff []int16,
	zbin, round, quant, quantShift, dequant [2]int16,
	scan []int16, qcoeff, dqcoeff []int16,
) int {
	const nCoeffs = 32 * 32
	for i := range nCoeffs {
		qcoeff[i] = 0
		dqcoeff[i] = 0
	}
	zbins := [2]int{
		(int(zbin[0]) + 1) >> 1,
		(int(zbin[1]) + 1) >> 1,
	}
	nzbins := [2]int{-zbins[0], -zbins[1]}
	idxArr := make([]int, 0, nCoeffs)
	for i := range nCoeffs {
		rc := int(scan[i])
		c := int(coeff[rc])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		if c >= zbins[slot] || c <= nzbins[slot] {
			idxArr = append(idxArr, i)
		}
	}
	eob := -1
	for _, i := range idxArr {
		rc := int(scan[i])
		c := int(coeff[rc])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		coeffSign := 0
		if c < 0 {
			coeffSign = -1
		}
		absCoeff := (c ^ coeffSign) - coeffSign
		// abs_coeff += ROUND_POWER_OF_TWO(round_ptr[..], 1)
		absCoeff += (int(round[slot]) + 1) >> 1
		if absCoeff > 32767 {
			absCoeff = 32767
		}
		if absCoeff < -32768 {
			absCoeff = -32768
		}
		tmp := ((((absCoeff * int(quant[slot])) >> 16) + absCoeff) *
			int(quantShift[slot])) >> 15
		q := (tmp ^ coeffSign) - coeffSign
		qcoeff[rc] = int16(q)
		dqcoeff[rc] = int16(int(qcoeff[rc]) * int(dequant[slot]) / 2)
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

// TestVP9QuantizeFPPinned1280DCPredict pins QuantizeFP against a hand-
// computed reference for a 4-coefficient block. Hand-traced from libvpx
// v1.16.0 vp9_quantize_fp_c (vp9/encoder/vp9_quantize.c:26-56) using
// dequant=[10,12] (so round_fp=[3,3], quant_fp=[6553,5461]).
func TestVP9QuantizeFPPinned1280DCPredict(t *testing.T) {
	// 4 coeffs, identity scan; dequant=[10,12].
	dequant := [2]int16{10, 12}
	scan := []int16{0, 1, 2, 3}
	coeff := []int16{500, 0, -300, 12}
	dqcoeff := make([]int16, 4)
	eob := QuantizeFP(coeff, dequant, scan, dqcoeff)
	// Hand-traced: round_fp=[(48*10)>>7,(42*12)>>7]=[3,3];
	//   quant_fp=[65536/10,65536/12]=[6553,5461].
	// i=0 rc=0 abs=500 tmp=503 *6553=3296159 >>16 =50 -> dq[0]=500
	// i=1 rc=1 abs=0   tmp=3   *5461=16383   >>16 =0  -> dq[1]=0
	// i=2 rc=2 abs=300 tmp=303 *5461=1654683 >>16 =25 -> dq[2]=-300
	// i=3 rc=3 abs=12  tmp=15  *5461=81915   >>16 =1  -> dq[3]=12
	want := []int16{500, 0, -300, 12}
	if eob != 4 {
		t.Fatalf("eob = %d, want 4; dqcoeff=%v", eob, dqcoeff)
	}
	for i, v := range dqcoeff {
		if v != want[i] {
			t.Fatalf("dqcoeff[%d] = %d, want %d", i, v, want[i])
		}
	}
}

// TestVP9QuantizeFP32x32PinnedDC pins QuantizeFP32x32 against a hand-
// computed reference. Hand-traced from libvpx v1.16.0
// vp9_quantize_fp_32x32_c (vp9/encoder/vp9_quantize.c:92-123).
func TestVP9QuantizeFP32x32PinnedDC(t *testing.T) {
	dequant := [2]int16{20, 24}
	scan := make([]int16, 1024)
	for i := range scan {
		scan[i] = int16(i)
	}
	coeff := make([]int16, 1024)
	coeff[0] = 1000
	dqcoeff := make([]int16, 1024)
	eob := QuantizeFP32x32(coeff, dequant, scan, dqcoeff)
	// Hand-traced: round_fp_base=[(48*20)>>7,(42*24)>>7]=[7,7];
	//   shifted round=[(7+1)>>1,(7+1)>>1]=[4,4]; quant=[65536/20,65536/24]=[3276,2730]
	// i=0 rc=0 abs=1000 >= 5 -> abs+=4=1004; (1004*3276)>>15 = 3289104>>15 = 100
	//   dq[0] = 100*20/2 = 1000
	// other rc: abs=0 < (24>>2=6) -> tmp=0, dq stays 0.
	if eob != 1 {
		t.Fatalf("eob = %d, want 1", eob)
	}
	if dqcoeff[0] != 1000 {
		t.Fatalf("dqcoeff[0] = %d, want 1000", dqcoeff[0])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// TestVP9QuantizeFP32x32PinnedDCWithACPair pins QuantizeFP32x32 with a
// DC + low-frequency AC pair so the AC slot exercises the shifted-round
// + >>15 path (not just the DC slot). Hand-traced from libvpx
// v1.16.0 vp9_quantize_fp_32x32_c.
func TestVP9QuantizeFP32x32PinnedDCWithACPair(t *testing.T) {
	dequant := [2]int16{20, 24}
	scan := make([]int16, 1024)
	for i := range scan {
		scan[i] = int16(i)
	}
	coeff := make([]int16, 1024)
	coeff[0] = 1000
	coeff[1] = -200
	dqcoeff := make([]int16, 1024)
	eob := QuantizeFP32x32(coeff, dequant, scan, dqcoeff)
	// AC slot: round=4, quant=2730, dequant=24.
	// i=1 rc=1 abs=200 >= 24>>2=6 -> abs+=4=204; (204*2730)>>15 = 556920>>15 = 16
	//   q = -16, dq[1] = -16*24/2 = -192
	if eob != 2 {
		t.Fatalf("eob = %d, want 2; dqcoeff[0..2]=%v", eob, dqcoeff[:2])
	}
	if dqcoeff[0] != 1000 {
		t.Fatalf("dqcoeff[0] = %d, want 1000", dqcoeff[0])
	}
	if dqcoeff[1] != -192 {
		t.Fatalf("dqcoeff[1] = %d, want -192", dqcoeff[1])
	}
}

// TestVP9QuantizeBPinned pins QuantizeB against a hand-computed reference.
// Hand-traced from libvpx v1.16.0 vpx_quantize_b_c (vpx_dsp/quantize.c:118)
// using dequant=[10,12] and qindex=37 so qzbin_factor=84 and
// qrounding_factor=48 (non-zero q, no sharpness).
func TestVP9QuantizeBPinned(t *testing.T) {
	// 4 coeffs, identity scan; dequant=[10,12]; qindex=37 (DcQuant<148 -> zbinFactor=84).
	dequant := [2]int16{10, 12}
	scan := []int16{0, 1, 2, 3}
	coeff := []int16{500, 0, -300, 12}
	dqcoeff := make([]int16, 4)
	eob := QuantizeB(coeff, 37, dequant, scan, dqcoeff)
	// Hand-traced: zbin=[ROUND_POWER_OF_TWO(84*10,7), ROUND_POWER_OF_TWO(84*12,7)]
	//                  = [(840+64)>>7, (1008+64)>>7] = [7,8]
	//   round=[(48*10)>>7,(48*12)>>7]=[3,4]
	//   invert_quant(10): msb=3, m=1+(1<<19)/10=52429, quant=int16(52429-65536)=-13107, shift=1<<13=8192
	//   invert_quant(12): msb=3, m=1+(1<<19)/12=43691, quant=int16(43691-65536)=-21845, shift=1<<13=8192
	// i=0 rc=0 abs=500 >= 7 -> tmp=503; ((503*-13107)>>16=-101)+503=402; (402*8192)>>16=50 -> dq[0]=500
	// i=1 rc=1 abs=0   <  8 -> skip
	// i=2 rc=2 abs=300 >= 8 -> tmp=304; ((304*-21845)>>16=-102)+304=202; (202*8192)>>16=25 -> dq[2]=-300
	// i=3 rc=3 abs=12  >= 8 -> tmp=16; ((16*-21845)>>16=-6)+16=10; (10*8192)>>16=1 -> dq[3]=12
	want := []int16{500, 0, -300, 12}
	if eob != 4 {
		t.Fatalf("eob = %d, want 4; dqcoeff=%v", eob, dqcoeff)
	}
	for i, v := range dqcoeff {
		if v != want[i] {
			t.Fatalf("dqcoeff[%d] = %d, want %d", i, v, want[i])
		}
	}
}

// TestVP9QuantizeBPinnedZbinSkipsACTrail pins QuantizeB pre-scan: a
// trailing AC zero run below the zbin should be dropped from non_zero_count
// (libvpx vpx_dsp/quantize.c:135-143). EOB must reflect the trimmed range.
func TestVP9QuantizeBPinnedZbinSkipsACTrail(t *testing.T) {
	// qindex=37 -> zbin=[7,8] (Y). Trailing 1's are below AC zbin=8 -> skipped.
	dequant := [2]int16{10, 12}
	scan := []int16{0, 1, 2, 3}
	coeff := []int16{500, 1, 0, -1}
	dqcoeff := make([]int16, 4)
	eob := QuantizeB(coeff, 37, dequant, scan, dqcoeff)
	// Pre-scan: rc=3 c=-1 in (-8,8) -> nzc=3. rc=2 c=0 in (-8,8) -> nzc=2.
	//   rc=1 c=1 in (-8,8) -> nzc=1. Loop runs i=0 only.
	// i=0: 500 quantizes to 50, dq[0]=500. eob=0 -> EOB=1.
	if eob != 1 {
		t.Fatalf("eob = %d, want 1; dqcoeff=%v", eob, dqcoeff)
	}
	if dqcoeff[0] != 500 {
		t.Fatalf("dqcoeff[0] = %d, want 500", dqcoeff[0])
	}
	for i := 1; i < 4; i++ {
		if dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// TestVP9QuantizeB32x32PinnedDC pins QuantizeB32x32 against a hand-
// computed reference. Hand-traced from libvpx v1.16.0
// vpx_quantize_b_32x32_c (vpx_dsp/quantize.c:216-275).
func TestVP9QuantizeB32x32PinnedDC(t *testing.T) {
	// 1024 coeffs, identity scan; dequant=[20,24]; qindex=37.
	dequant := [2]int16{20, 24}
	scan := make([]int16, 1024)
	for i := range scan {
		scan[i] = int16(i)
	}
	coeff := make([]int16, 1024)
	coeff[0] = 1000
	dqcoeff := make([]int16, 1024)
	eob := QuantizeB32x32(coeff, 37, dequant, scan, dqcoeff)
	// Hand-traced for qindex=37 (assuming DcQuant<148 -> qzbinFactor=84):
	//   zbin_base=[(84*20+64)>>7,(84*24+64)>>7]=[(1744)>>7,(2080)>>7]=[13,16]
	//   shifted zbin=[(13+1)>>1,(16+1)>>1]=[7,8]
	//   round_base=[(48*20)>>7,(48*24)>>7]=[7,9]
	//   invert_quant(20): msb=4, m=1+(1<<20)/20=52429, quant=int16(52429-65536)=-13107, shift=1<<12=4096
	//   invert_quant(24): msb=4, m=1+(1<<20)/24=43691, quant=int16(43691-65536)=-21845, shift=1<<12=4096
	// i=0 rc=0 c=1000: 1000 >= 7 OK -> abs+=(7+1)>>1=4 -> 1004
	//   (1004*-13107)>>16 = -201; -201+1004 = 803; (803*4096)>>15 = 100
	//   q = 100, dq[0] = 100*20/2 = 1000
	// other rc: c=0 in (-8,8) -> not in idx_arr.
	if eob != 1 {
		t.Fatalf("eob = %d, want 1", eob)
	}
	if dqcoeff[0] != 1000 {
		t.Fatalf("dqcoeff[0] = %d, want 1000", dqcoeff[0])
	}
	for i := 1; i < len(dqcoeff); i++ {
		if dqcoeff[i] != 0 {
			t.Fatalf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// TestVP9QuantizeFP32x32MatchesLibvpxContract cross-checks
// QuantizeFP32x32 against the verbatim libvpx oracle
// referenceQuantizeFP32x32C across representative coefficient layouts.
func TestVP9QuantizeFP32x32MatchesLibvpxContract(t *testing.T) {
	dequant := [2]int16{16, 17}
	// Derive the legacy round_fp/quant_fp per libvpx vp9_init_quantizer:
	roundFP := [2]int16{
		int16((48 * int(dequant[0])) >> 7),
		int16((42 * int(dequant[1])) >> 7),
	}
	quantFP := [2]int16{
		int16((1 << 16) / int(dequant[0])),
		int16((1 << 16) / int(dequant[1])),
	}

	scan := common.DefaultScanOrders[common.Tx32x32].Scan[:1024]
	type tc struct {
		name string
		fill func(c []int16)
	}
	cases := []tc{
		{"all-zero", func(c []int16) {}},
		{"single DC", func(c []int16) { c[0] = 1278 }},
		{"DC + mid AC", func(c []int16) {
			c[0] = 800
			c[int(scan[60])] = -46
		}},
		{"high-freq AC", func(c []int16) {
			c[int(scan[700])] = -700
		}},
		{"dense ±256", func(c []int16) {
			r := rand.New(rand.NewSource(0xC0FFEE))
			for i := range c {
				c[i] = int16(r.Intn(513) - 256)
			}
		}},
		{"boundary ±32767 (alternating)", func(c []int16) {
			for i := range c {
				if i%2 == 0 {
					c[i] = 32767
				} else {
					c[i] = -32767
				}
			}
		}},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			coeff := make([]int16, 1024)
			tcase.fill(coeff)

			gotDQ := make([]int16, 1024)
			gotEOB := QuantizeFP32x32(coeff, dequant, scan, gotDQ)

			gotQWithQ := make([]int16, 1024)
			gotDQWithQ := make([]int16, 1024)
			gotEOBWithQ := QuantizeFP32x32WithQ(coeff, dequant, scan, gotQWithQ, gotDQWithQ)

			wantQ := make([]int16, 1024)
			wantDQ := make([]int16, 1024)
			wantEOB := referenceQuantizeFP32x32C(coeff, 1024, roundFP, quantFP, dequant,
				scan, wantQ, wantDQ)

			if gotEOB != wantEOB {
				t.Fatalf("eob mismatch: got %d, want %d", gotEOB, wantEOB)
			}
			for i := range 1024 {
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] mismatch: got %d, want %d",
						i, gotDQ[i], wantDQ[i])
				}
				if gotQWithQ[i] != wantQ[i] {
					t.Fatalf("qcoeff[%d] mismatch: got %d, want %d",
						i, gotQWithQ[i], wantQ[i])
				}
				if gotDQWithQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff with q[%d] mismatch: got %d, want %d",
						i, gotDQWithQ[i], wantDQ[i])
				}
			}
			if gotEOBWithQ != wantEOB {
				t.Fatalf("eob with q mismatch: got %d, want %d", gotEOBWithQ, wantEOB)
			}
		})
	}
}

// TestVP9QuantizeBMatchesLibvpxContract cross-checks QuantizeB against
// referenceQuantizeBC across representative coefficient layouts. zbin/
// round/quant/quant_shift are derived using the same recipe libvpx's
// vp9_init_quantizer uses (vp9/encoder/vp9_quantize.c:185-244).
func TestVP9QuantizeBMatchesLibvpxContract(t *testing.T) {
	type qParams struct {
		zbin, round, quant, quantShift [2]int16
	}
	deriveQ := func(qindex int, dequant [2]int16) qParams {
		qzbinFactor := 80
		if qindex == 0 {
			qzbinFactor = 64
		} else if int(common.DcQuant(qindex, 0, common.Bits8)) < 148 {
			qzbinFactor = 84
		}
		qroundingFactor := 48
		if qindex == 0 {
			qroundingFactor = 64
		}
		invert := func(d int) (q, sh int16) {
			if d <= 0 {
				return 0, 0
			}
			l := 0
			for (1 << uint(l+1)) <= d {
				l++
			}
			m := 1 + (1 << uint(16+l) / d)
			return int16(m - (1 << 16)), int16(1 << uint(16-l))
		}
		var p qParams
		for i := range 2 {
			dq := int(dequant[i])
			p.zbin[i] = int16((qzbinFactor*dq + 64) >> 7)
			p.round[i] = int16((qroundingFactor * dq) >> 7)
			p.quant[i], p.quantShift[i] = invert(dq)
		}
		return p
	}

	type tc struct {
		name    string
		txSize  common.TxSize
		nCoeffs int
		qindex  int
		dequant [2]int16
		fill    func(c []int16)
	}
	cases := []tc{
		{"4x4 single DC q=37", common.Tx4x4, 16, 37, [2]int16{10, 12},
			func(c []int16) { c[0] = 500 }},
		{"8x8 dense q=64", common.Tx8x8, 64, 64, [2]int16{16, 17},
			func(c []int16) {
				r := rand.New(rand.NewSource(0xBEEF))
				for i := range c {
					c[i] = int16(r.Intn(513) - 256)
				}
			}},
		{"16x16 high-freq AC q=128", common.Tx16x16, 256, 128, [2]int16{38, 44},
			func(c []int16) {
				scan := common.DefaultScanOrders[common.Tx16x16].Scan
				c[int(scan[200])] = -918
			}},
		{"4x4 boundary ±32767 q=200", common.Tx4x4, 16, 200, [2]int16{96, 113},
			func(c []int16) {
				for i := range c {
					if i%2 == 0 {
						c[i] = 32767
					} else {
						c[i] = -32767
					}
				}
			}},
		{"16x16 zero-trail q=37", common.Tx16x16, 256, 37, [2]int16{10, 12},
			func(c []int16) {
				c[0] = 320
				c[3] = -7
			}},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			so := common.DefaultScanOrders[tcase.txSize]
			scan := so.Scan[:tcase.nCoeffs]
			coeff := make([]int16, tcase.nCoeffs)
			tcase.fill(coeff)

			gotDQ := make([]int16, tcase.nCoeffs)
			gotEOB := QuantizeB(coeff, tcase.qindex, tcase.dequant, scan, gotDQ)

			p := deriveQ(tcase.qindex, tcase.dequant)
			wantQ := make([]int16, tcase.nCoeffs)
			wantDQ := make([]int16, tcase.nCoeffs)
			wantEOB := referenceQuantizeBC(coeff, tcase.nCoeffs, p.zbin, p.round,
				p.quant, p.quantShift, tcase.dequant, scan, wantQ, wantDQ)

			if gotEOB != wantEOB {
				t.Fatalf("eob mismatch: got %d, want %d", gotEOB, wantEOB)
			}
			for i := range tcase.nCoeffs {
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] mismatch: got %d, want %d",
						i, gotDQ[i], wantDQ[i])
				}
			}
		})
	}
}

// TestVP9QuantizeB32x32MatchesLibvpxContract cross-checks QuantizeB32x32
// against referenceQuantizeB32x32C across representative coefficient
// layouts. Uses the same param-derivation recipe as the 4x4/8x8/16x16
// contract test above (vp9/encoder/vp9_quantize.c:185-244).
func TestVP9QuantizeB32x32MatchesLibvpxContract(t *testing.T) {
	type qParams struct {
		zbin, round, quant, quantShift [2]int16
	}
	deriveQ := func(qindex int, dequant [2]int16) qParams {
		qzbinFactor := 80
		if qindex == 0 {
			qzbinFactor = 64
		} else if int(common.DcQuant(qindex, 0, common.Bits8)) < 148 {
			qzbinFactor = 84
		}
		qroundingFactor := 48
		if qindex == 0 {
			qroundingFactor = 64
		}
		invert := func(d int) (q, sh int16) {
			if d <= 0 {
				return 0, 0
			}
			l := 0
			for (1 << uint(l+1)) <= d {
				l++
			}
			m := 1 + (1 << uint(16+l) / d)
			return int16(m - (1 << 16)), int16(1 << uint(16-l))
		}
		var p qParams
		for i := range 2 {
			dq := int(dequant[i])
			p.zbin[i] = int16((qzbinFactor*dq + 64) >> 7)
			p.round[i] = int16((qroundingFactor * dq) >> 7)
			p.quant[i], p.quantShift[i] = invert(dq)
		}
		return p
	}

	type tc struct {
		name    string
		qindex  int
		dequant [2]int16
		fill    func(c []int16)
	}
	cases := []tc{
		{"all-zero q=37", 37, [2]int16{20, 24}, func(c []int16) {}},
		{"single DC q=37", 37, [2]int16{20, 24},
			func(c []int16) { c[0] = 1000 }},
		{"DC + low AC q=37", 37, [2]int16{20, 24}, func(c []int16) {
			c[0] = 1000
			scan := common.DefaultScanOrders[common.Tx32x32].Scan
			c[int(scan[12])] = -250
		}},
		{"high-freq AC q=128", 128, [2]int16{38, 44}, func(c []int16) {
			scan := common.DefaultScanOrders[common.Tx32x32].Scan
			c[int(scan[700])] = -700
		}},
		{"dense ±256 q=64", 64, [2]int16{16, 17}, func(c []int16) {
			r := rand.New(rand.NewSource(0xC0FFEE))
			for i := range c {
				c[i] = int16(r.Intn(513) - 256)
			}
		}},
	}

	scan := common.DefaultScanOrders[common.Tx32x32].Scan[:1024]
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			coeff := make([]int16, 1024)
			tcase.fill(coeff)

			gotDQ := make([]int16, 1024)
			gotEOB := QuantizeB32x32(coeff, tcase.qindex, tcase.dequant, scan, gotDQ)

			p := deriveQ(tcase.qindex, tcase.dequant)
			wantQ := make([]int16, 1024)
			wantDQ := make([]int16, 1024)
			wantEOB := referenceQuantizeB32x32C(coeff, p.zbin, p.round,
				p.quant, p.quantShift, tcase.dequant, scan, wantQ, wantDQ)

			if gotEOB != wantEOB {
				t.Fatalf("eob mismatch: got %d, want %d", gotEOB, wantEOB)
			}
			for i := range 1024 {
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] mismatch: got %d, want %d",
						i, gotDQ[i], wantDQ[i])
				}
			}
		})
	}
}
