package encoder

import "testing"

func TestAnalysisBlockOffsetsUseVP8ScanOrder(t *testing.T) {
	if got, want := AnalysisYBlockOffset(5, 20), 4*20+4; got != want {
		t.Fatalf("AnalysisYBlockOffset = %d, want %d", got, want)
	}
	if got, want := AnalysisUVBlockOffset(3, 10), 4*10+4; got != want {
		t.Fatalf("AnalysisUVBlockOffset = %d, want %d", got, want)
	}
}

func TestAddQuantizedBlockResidualUsesEOBCases(t *testing.T) {
	pred := [16]byte{}
	for i := range pred {
		pred[i] = 128
	}

	dstNone := pred
	AddQuantizedBlockResidual(0, &[16]int16{0: 64}, dstNone[:], 4)
	if dstNone != pred {
		t.Fatalf("eob=0 changed destination: got %v want %v", dstNone, pred)
	}

	dstDC := pred
	AddQuantizedBlockResidual(1, &[16]int16{0: 64}, dstDC[:], 4)
	if dstDC == pred {
		t.Fatalf("eob=1 did not apply DC residual")
	}

	dstAC := pred
	AddQuantizedBlockResidual(2, &[16]int16{0: 64, 1: 16}, dstAC[:], 4)
	if dstAC == pred {
		t.Fatalf("eob=2 did not apply full residual")
	}
}
