package encoder

import (
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
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
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
