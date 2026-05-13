package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestVpxDcQuantClampsAndDispatches: clamps qindex+delta to [0, MaxQ]
// and routes to the per-bit-depth table.
func TestVpxDcQuantClampsAndDispatches(t *testing.T) {
	// Bit-depth 8, qindex=100 + delta=0 → DcQLookup8[100].
	if got := VpxDcQuant(100, 0, BitDepth8); got != tables.DcQLookup8[100] {
		t.Errorf("got %d want %d", got, tables.DcQLookup8[100])
	}
	// Negative clamp.
	if got := VpxDcQuant(-50, 0, BitDepth8); got != tables.DcQLookup8[0] {
		t.Errorf("under-clamp: got %d want %d", got, tables.DcQLookup8[0])
	}
	// Over-clamp.
	if got := VpxDcQuant(300, 0, BitDepth8); got != tables.DcQLookup8[MaxQ] {
		t.Errorf("over-clamp: got %d want %d", got, tables.DcQLookup8[MaxQ])
	}
	// delta adds: 200 + 50 = 250.
	if got := VpxDcQuant(200, 50, BitDepth8); got != tables.DcQLookup8[250] {
		t.Errorf("delta: got %d want %d", got, tables.DcQLookup8[250])
	}
	// bit-depth dispatch.
	if got := VpxDcQuant(100, 0, BitDepth10); got != tables.DcQLookup10[100] {
		t.Errorf("bd=10 dispatch: got %d want %d", got, tables.DcQLookup10[100])
	}
	if got := VpxDcQuant(100, 0, BitDepth12); got != tables.DcQLookup12[100] {
		t.Errorf("bd=12 dispatch: got %d want %d", got, tables.DcQLookup12[100])
	}
}

// TestVpxAcQuantClampsAndDispatches: same as DC but against ac_qlookup.
func TestVpxAcQuantClampsAndDispatches(t *testing.T) {
	if got := VpxAcQuant(150, 0, BitDepth8); got != tables.AcQLookup8[150] {
		t.Errorf("got %d want %d", got, tables.AcQLookup8[150])
	}
	if got := VpxAcQuant(-1, 0, BitDepth8); got != tables.AcQLookup8[0] {
		t.Errorf("under: got %d want %d", got, tables.AcQLookup8[0])
	}
	if got := VpxAcQuant(255, 0, BitDepth8); got != tables.AcQLookup8[MaxQ] {
		t.Errorf("over: got %d", got)
	}
	if got := VpxAcQuant(100, 0, BitDepth12); got != tables.AcQLookup12[100] {
		t.Errorf("bd=12: got %d", got)
	}
}

// TestGetSegmentQindexPassthrough: SEG_LVL_ALT_Q inactive → base.
func TestGetSegmentQindexPassthrough(t *testing.T) {
	seg := &SegmentationParams{}
	if got := GetSegmentQindex(seg, 0, 137); got != 137 {
		t.Errorf("got %d want 137", got)
	}
}

// TestGetSegmentQindexDelta: AbsDelta=false → base + data, clamped.
func TestGetSegmentQindexDelta(t *testing.T) {
	seg := &SegmentationParams{Enabled: true}
	seg.FeatureMask[3] = 1 << SegLvlAltQ
	seg.FeatureData[3][SegLvlAltQ] = 50
	if got := GetSegmentQindex(seg, 3, 100); got != 150 {
		t.Errorf("got %d want 150", got)
	}
	// Negative delta clamps at 0.
	seg.FeatureData[3][SegLvlAltQ] = -200
	if got := GetSegmentQindex(seg, 3, 100); got != 0 {
		t.Errorf("under-clamp: got %d", got)
	}
}

// TestGetSegmentQindexAbsdata: AbsDelta=true → data replaces base
// entirely.
func TestGetSegmentQindexAbsdata(t *testing.T) {
	seg := &SegmentationParams{Enabled: true, AbsDelta: true}
	seg.FeatureMask[2] = 1 << SegLvlAltQ
	seg.FeatureData[2][SegLvlAltQ] = 175
	if got := GetSegmentQindex(seg, 2, 100); got != 175 {
		t.Errorf("got %d want 175", got)
	}
}

// TestSetupSegmentationDequantDisabled: only slot 0 is filled with
// the (base + per-plane deltas) dequant pair.
func TestSetupSegmentationDequantDisabled(t *testing.T) {
	var seg SegmentationParams
	args := SetupSegmentationDequantArgs{
		BaseQindex: 100,
		YDcDeltaQ:  4,
		UvDcDeltaQ: -8,
		UvAcDeltaQ: 12,
		BitDepth:   BitDepth8,
	}
	var out DequantTables
	SetupSegmentationDequant(&seg, args, &out)
	if got, want := out.Y[0][0], tables.DcQLookup8[104]; got != want {
		t.Errorf("Y DC: got %d want %d", got, want)
	}
	if got, want := out.Y[0][1], tables.AcQLookup8[100]; got != want {
		t.Errorf("Y AC: got %d want %d", got, want)
	}
	if got, want := out.Uv[0][0], tables.DcQLookup8[92]; got != want {
		t.Errorf("UV DC: got %d want %d", got, want)
	}
	if got, want := out.Uv[0][1], tables.AcQLookup8[112]; got != want {
		t.Errorf("UV AC: got %d want %d", got, want)
	}
}

// TestSetupSegmentationDequantEnabled: every segment slot is filled,
// the per-segment ALT_Q feature overrides the qindex for that slot.
func TestSetupSegmentationDequantEnabled(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.FeatureMask[1] = 1 << SegLvlAltQ
	seg.FeatureData[1][SegLvlAltQ] = 50
	args := SetupSegmentationDequantArgs{
		BaseQindex: 100,
		YDcDeltaQ:  0,
		UvDcDeltaQ: 0,
		UvAcDeltaQ: 0,
		BitDepth:   BitDepth8,
	}
	var out DequantTables
	SetupSegmentationDequant(&seg, args, &out)
	// Slot 1: qindex 150.
	if got, want := out.Y[1][0], tables.DcQLookup8[150]; got != want {
		t.Errorf("seg 1 Y DC: got %d want %d", got, want)
	}
	// Slot 0: base qindex 100.
	if got, want := out.Y[0][0], tables.DcQLookup8[100]; got != want {
		t.Errorf("seg 0 Y DC: got %d want %d", got, want)
	}
}
