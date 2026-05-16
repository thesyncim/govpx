package encoder

import (
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
			name:    "boundary +/-32767 4x4",
			txSize:  common.Tx4x4,
			nCoeffs: 16,
			fill: func(c []int16) {
				for i := range c {
					if i%2 == 0 {
						c[i] = 32767
					} else {
						c[i] = -32767
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
