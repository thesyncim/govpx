package encoder_test

import (
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestConvertMacroblockCoefficientsOverwritesActiveSkippedDCBlock(t *testing.T) {
	var src vp8enc.MacroblockCoefficients
	var dst vp8dec.MacroblockTokens
	src.SetBlockEOB(0, 0)
	dst.QCoeff[0][0] = 99
	dst.QCoeff[0][1] = 77
	dst.EOB[0] = 2

	vp8enc.ConvertMacroblockCoefficients(&src, false, &dst)

	if got := dst.EOB[0]; got != 1 {
		t.Fatalf("EOB[0] = %d, want skipped-DC EOB 1", got)
	}
	if got := dst.QCoeff[0][0]; got != 0 {
		t.Fatalf("QCoeff[0][0] = %d, want active skipped DC overwritten", got)
	}
}

func BenchmarkConvertMacroblockCoefficientsSparse(b *testing.B) {
	var src vp8enc.MacroblockCoefficients
	var dst vp8dec.MacroblockTokens
	src.QCoeff[0][0] = 3
	src.SetBlockEOB(0, 1)
	src.QCoeff[24][0] = 4
	src.SetBlockEOB(24, 1)
	src.QCoeff[16][0] = -2
	src.SetBlockEOB(16, 1)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vp8enc.ConvertMacroblockCoefficients(&src, false, &dst)
	}
	if dst.EOB[0] != 1 || dst.QCoeff[0][0] != 3 || dst.EOB[24] != 1 || dst.QCoeff[24][0] != 4 || dst.EOB[16] != 1 || dst.QCoeff[16][0] != -2 {
		b.Fatalf("converted tokens = %+v", dst)
	}
}
