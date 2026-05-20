//go:build !govpx_oracle_trace

package govpx

import (
	"testing"
	"unsafe"
)

func TestDisabledTraceStateZeroSize(t *testing.T) {
	cases := []struct {
		name string
		size uintptr
	}{
		{name: "vp8 oracle state", size: unsafe.Sizeof(oracleTraceState{})},
		{name: "vp8 stale y2 snapshot", size: unsafe.Sizeof(staleY2Snapshot{})},
		{name: "vp8 coefficient trace", size: unsafe.Sizeof(predictedMacroblockCoefficientTrace{})},
		{name: "vp9 decoded leaf row", size: unsafe.Sizeof(vp9DecodedLeafTrace{})},
		{name: "vp9 decoded leaf state", size: unsafe.Sizeof(vp9DecodedLeafTraceState{})},
		{name: "vp9 oracle state", size: unsafe.Sizeof(vp9OracleTraceState{})},
	}
	for _, tc := range cases {
		if tc.size != 0 {
			t.Fatalf("%s size = %d, want 0 in default builds", tc.name, tc.size)
		}
	}
}

func TestDisabledTraceHelpersAreNoops(t *testing.T) {
	var vp8 VP8Encoder
	if oracleTraceBuild {
		t.Fatal("oracleTraceBuild = true in default build")
	}
	if vp8.oracleTraceEnabled() {
		t.Fatal("VP8 oracle trace active in default build")
	}
	if got := vp8.oracleTraceMBBufferLenForTest(); got != 0 {
		t.Fatalf("VP8 oracle MB trace length = %d, want 0", got)
	}
	if got := vp8.oracleTraceRecodeLoopCountForTest(); got != 0 {
		t.Fatalf("VP8 oracle recode loop count = %d, want 0", got)
	}

	coefTrace := newPretrellisUVTrace(&vp8)
	if coefTrace.pretrellisUVEnabled(true, false) {
		t.Fatal("pretrellis UV trace enabled in default build")
	}
	if coefTrace.chromaOptimizeBEnabled(true, false) {
		t.Fatal("chroma optimize trace enabled in default build")
	}
	if coefTrace.pickerUVQuantizeEnabled() {
		t.Fatal("picker UV quantize trace enabled in default build")
	}
	coefTrace.emitPretrellisUV(0, 0, 0, nil, nil, nil, 0, 0, 0)
	coefTrace.emitChromaOptimizeB(0, 0, 0, nil, nil, nil, nil, 0, 0, 0, false)
	coefTrace.emitPickerUVQuantize(0, 0, 0, "", nil, nil, nil, nil, 0, 0, 0)

	var vp9d VP9Decoder
	if vp9DecodedLeafTraceBuild {
		t.Fatal("vp9DecodedLeafTraceBuild = true in default build")
	}
	if vp9d.vp9DecodedLeafTraceActive() {
		t.Fatal("VP9 decoded-leaf trace active in default build")
	}
	row := vp9DecodedLeafTraceForMI(nil, 0, 0, nil)
	vp9DecodedLeafTraceSetUVMode(&row, 0)
	vp9DecodedLeafTraceAddCoeffSummary(&row, 1, 16, []int16{1, -2, 0})
	vp9DecodedLeafTraceSetSkip(&row, 1)
	vp9d.emitVP9DecodedLeafTrace(row)

	var vp9e VP9Encoder
	if vp9OracleTraceBuild {
		t.Fatal("vp9OracleTraceBuild = true in default build")
	}
	if vp9e.vp9OracleTraceEnabled() {
		t.Fatal("VP9 oracle trace active in default build")
	}
	best, worst, correction, recode, loop := vp9e.vp9OracleRateSelectionTrace()
	if best != 0 || worst != 0 || correction != 0 || recode || loop != 0 {
		t.Fatalf("VP9 rate trace = (%d,%d,%f,%t,%d), want zero values",
			best, worst, correction, recode, loop)
	}
}
