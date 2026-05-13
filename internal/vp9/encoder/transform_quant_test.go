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
