//go:build !govpx_oracle_trace

package codec

import (
	"reflect"
	"testing"
)

var (
	disabledTraceBoolSink bool
	disabledTraceIntSink  int
)

func TestVP8OracleTraceDisabledStateTypesHaveZeroSize(t *testing.T) {
	cases := []struct {
		name string
		size uintptr
	}{
		{name: "vp8 oracle state", size: reflect.TypeFor[oracleTraceState]().Size()},
		{name: "vp8 oracle holder", size: reflect.TypeFor[oracleTraceHolder]().Size()},
		{name: "vp8 stale y2 snapshot", size: reflect.TypeFor[staleY2Snapshot]().Size()},
		{name: "vp8 coefficient trace", size: reflect.TypeFor[predictedMacroblockCoefficientTrace]().Size()},
	}
	for _, tc := range cases {
		if tc.size != 0 {
			t.Fatalf("%s size = %d, want 0 in default builds", tc.name, tc.size)
		}
	}
}

func TestVP8OracleTraceDisabledFieldsAbsentFromProductionStructs(t *testing.T) {
	if _, ok := reflect.TypeFor[VP8Encoder]().FieldByName("oracleTrace"); ok {
		t.Fatal("VP8Encoder exposes oracleTrace in default builds")
	}
}

func TestVP8OracleTraceDisabledZeroSizeFieldsDoNotTrailHotStructs(t *testing.T) {
	typ := reflect.TypeFor[predictedMacroblockCoefficientArgs]()
	field, ok := typ.FieldByName("trace")
	if !ok {
		t.Fatal("predictedMacroblockCoefficientArgs has no trace field")
	}
	if field.Type.Size() != 0 {
		t.Fatalf("predictedMacroblockCoefficientArgs.trace size = %d, want 0", field.Type.Size())
	}
	if field.Index[0] == typ.NumField()-1 {
		t.Fatal("predictedMacroblockCoefficientArgs.trace is the final field; trailing zero-size fields add padding")
	}
	next := typ.Field(field.Index[0] + 1)
	if next.Offset != field.Offset {
		t.Fatalf("predictedMacroblockCoefficientArgs.trace offset = %d, next field %s offset = %d",
			field.Offset, next.Name, next.Offset)
	}
}

func TestVP8OracleTraceDisabledMethodsAbsentFromProductionSurface(t *testing.T) {
	typ := reflect.TypeFor[*VP8Encoder]()
	for _, name := range []string{
		"SetOracleTraceWriter",
		"SetOracleTracePredictorDump",
		"SetOracleTracePretrellisUVDump",
		"SetOracleTraceChromaOptimizeBDump",
		"SetOracleTracePickerUVQuantizeDump",
	} {
		if _, ok := typ.MethodByName(name); ok {
			t.Fatalf("VP8Encoder exposes %s in default builds", name)
		}
	}
}

func TestVP8OracleTraceDisabledHelpersAllocateZero(t *testing.T) {
	var vp8 VP8Encoder
	coefTrace := newPretrellisUVTrace(&vp8)
	pickerTrace := newPickerUVQuantizeTrace(&vp8, nil)

	allocs := testing.AllocsPerRun(1000, func() {
		disabledTraceBoolSink = oracleTraceBuild && vp8.oracleTraceEnabled()
		enableOracleTraceForTest(&vp8)
		vp8.resetOracleTraceState()
		vp8.resetOracleTraceRecode()
		vp8.incrementOracleTraceRecodeLoop()
		vp8.setOracleTraceRecodeReason("test")
		disabledTraceIntSink += vp8.oracleTraceRecodeLoopCountForTest()
		disabledTraceIntSink += vp8.oracleTraceMBBufferLenForTest()
		vp8.resetOracleMBTraceBuffer()
		vp8.flushOracleMBTraceBuffer()
		vp8.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{})
		vp8.emitFastPickerIntraCandidateTrace(0, 0, 0, 0, 0, 0, false, 0, 0, 0, 0, nil)
		vp8.emitFastPickerInterCandidateTrace(0, 0, 0, 0, 0, 0, 0, 0, false, false, 0, 0, 0, 0, nil, interFrameSearchStart{})
		vp8.emitOracleMBTrace(0, 0, nil, nil, interFrameSearchStart{}, 0, 0)
		vp8.emitOracleKeyFrameMBTrace(0, 0, nil, nil, 0, 0)
		vp8.emitOracleLFTrial("test", 0, 0)
		vp8.emitOracleInterPredictorTrace(0, 0, nil)
		vp8.emitOracleInterReconstructedTrace(0, 0, nil)
		vp8.emitOracleLastRefWindow(nil)
		vp8.emitOracleFrameTrace(oracleTraceFrameSummary{})
		vp8.emitOracleDroppedFrameTrace("test")
		vp8.emitOracleRateAndRecodeTrace(0, 0, 0, 0, 0, 0)
		vp8.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{})
		staleY2 := makeOracleStaleY2Snapshot(0, [16]int16{})
		disabledTraceBoolSink = disabledTraceBoolSink || oracleStaleY2SnapshotSet(staleY2)
		applyOracleStaleY2Snapshot(nil, staleY2)
		recordOracleY1DCEOB1(nil, 0, 0)
		recordOracleStaleY2(nil, 0, [16]int16{})
		disabledTraceIntSink += int(libvpxY1DCWouldQuantizeNonzero(0, nil, 0, 0, 0, false))

		disabledTraceBoolSink = coefTrace.pretrellisUVEnabled(true, false)
		disabledTraceBoolSink = coefTrace.chromaOptimizeBEnabled(true, false)
		disabledTraceBoolSink = pickerTrace.pickerUVQuantizeEnabled()
		coefTrace.emitPretrellisUV(0, 0, 0, nil, nil, nil, 0, 0, 0)
		coefTrace.emitChromaOptimizeB(0, 0, 0, nil, nil, nil, nil, 0, 0, 0, false)
		pickerTrace.emitPickerUVQuantize(0, 0, 0, "", nil, nil, nil, nil, 0, 0, 0)
	})
	if allocs != 0 {
		t.Fatalf("disabled trace helpers allocated %v times per run, want 0", allocs)
	}
}

func TestVP8OracleTraceDisabledHelpersAreNoops(t *testing.T) {
	var vp8 VP8Encoder
	if oracleTraceBuild {
		t.Fatal("oracleTraceBuild = true in default build")
	}
	enableOracleTraceForTest(&vp8)
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
	pickerTrace := newPickerUVQuantizeTrace(&vp8, nil)
	if pickerTrace.pickerUVQuantizeEnabled() {
		t.Fatal("picker UV quantize trace enabled in default build")
	}
	coefTrace.emitPretrellisUV(0, 0, 0, nil, nil, nil, 0, 0, 0)
	coefTrace.emitChromaOptimizeB(0, 0, 0, nil, nil, nil, nil, 0, 0, 0, false)
	pickerTrace.emitPickerUVQuantize(0, 0, 0, "", nil, nil, nil, nil, 0, 0, 0)
}
