//go:build !govpx_oracle_trace

package govpx

import (
	"reflect"
	"testing"
	"unsafe"
)

var (
	disabledTraceBoolSink  bool
	disabledTraceIntSink   int
	disabledTraceFloatSink float64
)

func TestDisabledTraceStateZeroSize(t *testing.T) {
	cases := []struct {
		name string
		size uintptr
	}{
		{name: "vp8 oracle state", size: unsafe.Sizeof(oracleTraceState{})},
		{name: "vp8 oracle holder", size: unsafe.Sizeof(oracleTraceHolder{})},
		{name: "vp8 stale y2 snapshot", size: unsafe.Sizeof(staleY2Snapshot{})},
		{name: "vp8 coefficient trace", size: unsafe.Sizeof(predictedMacroblockCoefficientTrace{})},
		{name: "vp9 oracle state", size: unsafe.Sizeof(vp9OracleTraceState{})},
		{name: "vp9 oracle holder", size: unsafe.Sizeof(vp9OracleTraceHolder{})},
	}
	for _, tc := range cases {
		if tc.size != 0 {
			t.Fatalf("%s size = %d, want 0 in default builds", tc.name, tc.size)
		}
	}
}

func TestDisabledTraceFieldsNotInProductionStructs(t *testing.T) {
	cases := []struct {
		name  string
		typ   reflect.Type
		field string
	}{
		{name: "VP8Encoder", typ: reflect.TypeOf(VP8Encoder{}), field: "oracleTrace"},
		{name: "VP9Encoder", typ: reflect.TypeOf(VP9Encoder{}), field: "oracleTrace"},
		{name: "VP9Decoder", typ: reflect.TypeOf(VP9Decoder{}), field: "leafTrace"},
	}
	for _, tc := range cases {
		if _, ok := tc.typ.FieldByName(tc.field); ok {
			t.Fatalf("%s exposes %s in default builds", tc.name, tc.field)
		}
	}
}

func TestDisabledTraceHelpersDoNotAllocate(t *testing.T) {
	var vp8 VP8Encoder
	var vp9 VP9Encoder
	coefTrace := newPretrellisUVTrace(&vp8)
	pickerTrace := newPickerUVQuantizeTrace(&vp8, nil)

	allocs := testing.AllocsPerRun(1000, func() {
		disabledTraceBoolSink = oracleTraceBuild && vp8.oracleTraceEnabled()
		vp8.resetOracleTraceState()
		vp8.resetOracleTraceRecode()
		vp8.incrementOracleTraceRecodeLoop()
		vp8.setOracleTraceRecodeReason("test")
		disabledTraceIntSink += vp8.oracleTraceRecodeLoopCountForTest()
		disabledTraceIntSink += vp8.oracleTraceMBBufferLenForTest()
		vp8.resetOracleMBTraceBuffer()
		vp8.flushOracleMBTraceBuffer()
		vp8.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{})
		vp8.emitOracleLFTrial("test", 0, 0)
		vp8.emitOracleInterPredictorTrace(0, 0, nil)
		vp8.emitOracleInterReconstructedTrace(0, 0, nil)
		vp8.emitOracleLastRefWindow(nil)
		vp8.emitOracleFrameTrace(oracleTraceFrameSummary{})
		vp8.emitOracleDroppedFrameTrace("test")
		vp8.emitOracleRateAndRecodeTrace(0, 0, 0, 0, 0, 0)
		vp8.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{})

		disabledTraceBoolSink = coefTrace.pretrellisUVEnabled(true, false)
		disabledTraceBoolSink = coefTrace.chromaOptimizeBEnabled(true, false)
		disabledTraceBoolSink = pickerTrace.pickerUVQuantizeEnabled()
		coefTrace.emitPretrellisUV(0, 0, 0, nil, nil, nil, 0, 0, 0)
		coefTrace.emitChromaOptimizeB(0, 0, 0, nil, nil, nil, nil, 0, 0, 0, false)
		pickerTrace.emitPickerUVQuantize(0, 0, 0, "", nil, nil, nil, nil, 0, 0, 0)

		disabledTraceBoolSink = vp9OracleTraceBuild && vp9.vp9OracleTraceEnabled()
		vp9.resetVP9OracleTraceState()
		vp9.resetVP9OracleRateSelectionTrace()
		vp9.recordVP9OracleRateSelectionTrace(0, 0, 0, false, 0)
		best, worst, correction, recode, loop := vp9.vp9OracleRateSelectionTrace()
		disabledTraceIntSink += best + worst + loop
		disabledTraceFloatSink += correction
		disabledTraceBoolSink = disabledTraceBoolSink || recode
		vp9.emitVP9OracleFrameTrace(vp9OracleFrameSummary{})
	})
	if allocs != 0 {
		t.Fatalf("disabled trace helpers allocated %v times per run, want 0", allocs)
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
