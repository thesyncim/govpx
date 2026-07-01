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

func TestVPxOracleTraceDisabledStateTypesHaveZeroSize(t *testing.T) {
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
		{name: "vp9 frame summary", size: unsafe.Sizeof(vp9OracleFrameSummary{})},
	}
	for _, tc := range cases {
		if tc.size != 0 {
			t.Fatalf("%s size = %d, want 0 in default builds", tc.name, tc.size)
		}
	}
}

func TestVPxOracleTraceDisabledFieldsAbsentFromProductionStructs(t *testing.T) {
	cases := []struct {
		name  string
		typ   reflect.Type
		field string
	}{
		{name: "VP8Encoder", typ: reflect.TypeFor[VP8Encoder](), field: "oracleTrace"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "oracleTrace"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "lastSegMapChooserNoPredCost"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "lastSegMapChooserTPredCost"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "lastSegMapChooserTemporal"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "lastSegMapChooserNoPredCounts"},
		{name: "VP9Encoder", typ: reflect.TypeFor[VP9Encoder](), field: "lastSegMapChooserMiHist"},
		{name: "VP9Decoder", typ: reflect.TypeFor[VP9Decoder](), field: "leafTrace"},
	}
	for _, tc := range cases {
		if _, ok := tc.typ.FieldByName(tc.field); ok {
			t.Fatalf("%s exposes %s in default builds", tc.name, tc.field)
		}
	}
}

func TestVPxOracleTraceDisabledZeroSizeFieldsDoNotTrailHotStructs(t *testing.T) {
	cases := []struct {
		name  string
		typ   reflect.Type
		field string
	}{
		{
			name:  "predictedMacroblockCoefficientArgs",
			typ:   reflect.TypeFor[predictedMacroblockCoefficientArgs](),
			field: "trace",
		},
	}
	for _, tc := range cases {
		field, ok := tc.typ.FieldByName(tc.field)
		if !ok {
			t.Fatalf("%s has no %s field", tc.name, tc.field)
		}
		if field.Type.Size() != 0 {
			t.Fatalf("%s.%s size = %d, want 0 in default builds",
				tc.name, tc.field, field.Type.Size())
		}
		if field.Index[0] == tc.typ.NumField()-1 {
			t.Fatalf("%s.%s is the final field; trailing zero-size fields add padding",
				tc.name, tc.field)
		}
		next := tc.typ.Field(field.Index[0] + 1)
		if next.Offset != field.Offset {
			t.Fatalf("%s.%s offset = %d, next field %s offset = %d; want shared offset for zero-size field",
				tc.name, tc.field, field.Offset, next.Name, next.Offset)
		}
	}
}

func TestVPxOracleTraceDisabledMethodsAbsentFromProductionSurface(t *testing.T) {
	cases := []struct {
		name    string
		typ     reflect.Type
		methods []string
	}{
		{
			name: "VP8Encoder",
			typ:  reflect.TypeFor[*VP8Encoder](),
			methods: []string{
				"SetOracleTraceWriter",
				"SetOracleTracePredictorDump",
				"SetOracleTracePretrellisUVDump",
				"SetOracleTraceChromaOptimizeBDump",
				"SetOracleTracePickerUVQuantizeDump",
			},
		},
		{
			name: "VP9Encoder",
			typ:  reflect.TypeFor[*VP9Encoder](),
			methods: []string{
				"SetOracleTraceWriter",
			},
		},
	}
	for _, tc := range cases {
		for _, name := range tc.methods {
			if _, ok := tc.typ.MethodByName(name); ok {
				t.Fatalf("%s exposes %s in default builds", tc.name, name)
			}
		}
	}
}

func TestVPxOracleTraceDisabledHelpersAllocateZero(t *testing.T) {
	var vp8 VP8Encoder
	var vp9 VP9Encoder
	var vp9d VP9Decoder
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

		disabledTraceBoolSink = vp9OracleTraceBuild && vp9.vp9OracleTraceEnabled()
		vp9.resetVP9OracleTraceState()
		vp9.resetVP9OracleRateSelectionTrace()
		vp9.recordVP9OracleRateSelectionTrace(0, 0, 0, false, 0)
		best, worst, correction, recode, loop := vp9.vp9OracleRateSelectionTrace()
		disabledTraceIntSink += best + worst + loop
		disabledTraceFloatSink += correction
		disabledTraceBoolSink = disabledTraceBoolSink || recode
		vp9.emitVP9OracleFrameTrace(vp9OracleFrameSummary{})
		vp9.emitVP9OracleDroppedFrameTrace(0, 0, 0, temporalFrame{}, vp9DropNone)
		vp9.emitVP9OracleEncodedFrameTrace(0, 0, nil, 0, 0, false, VP9EncodeResult{}, 0)
		vp9d.markVP9Unsupported()
	})
	if allocs != 0 {
		t.Fatalf("disabled trace helpers allocated %v times per run, want 0", allocs)
	}
}

func TestVPxOracleTraceDisabledHelpersAreNoops(t *testing.T) {
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
	var vp9d VP9Decoder
	vp9d.markVP9Unsupported()
	if !vp9d.unsupportedReconstruct {
		t.Fatal("markVP9Unsupported did not mark reconstruction unsupported")
	}
}
