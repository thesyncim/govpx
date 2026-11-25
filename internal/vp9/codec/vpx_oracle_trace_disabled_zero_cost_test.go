//go:build !govpx_oracle_trace

package codec

import (
	"reflect"
	"testing"
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
		{name: "vp9 oracle state", size: reflect.TypeFor[vp9OracleTraceState]().Size()},
		{name: "vp9 oracle holder", size: reflect.TypeFor[vp9OracleTraceHolder]().Size()},
		{name: "vp9 frame summary", size: reflect.TypeFor[vp9OracleFrameSummary]().Size()},
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

func TestVPxOracleTraceDisabledMethodsAbsentFromProductionSurface(t *testing.T) {
	cases := []struct {
		name    string
		typ     reflect.Type
		methods []string
	}{
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
	var vp9 VP9Encoder
	var vp9d VP9Decoder

	allocs := testing.AllocsPerRun(1000, func() {
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
