package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// seedDefaultModeProbs mirrors libvpx's seeding of y_mode_prob /
// uv_mode_prob from default_if_y_probs / default_if_uv_probs at
// frame_context init time.
func seedDefaultModeProbs(fc *FrameContext) {
	for g := range tables.DefaultIfYProbs {
		for i := range tables.DefaultIfYProbs[g] {
			fc.YModeProb[g][i] = tables.DefaultIfYProbs[g][i]
		}
	}
	for m := range tables.DefaultIfUvProbs {
		for i := range tables.DefaultIfUvProbs[m] {
			fc.UvModeProb[m][i] = tables.DefaultIfUvProbs[m][i]
		}
	}
}

// TestReadIntraModeYInter walks every size_group and confirms the
// returned mode comes from the matching fc.YModeProb row.
func TestReadIntraModeYInter(t *testing.T) {
	var fc FrameContext
	seedDefaultModeProbs(&fc)
	for sg := range 4 {
		want := common.D135Pred
		row := fc.YModeProb[sg]
		buf := make([]byte, 32)
		var w bitstream.Writer
		w.Start(buf)
		emitIntraMode(t, &w, row[:], int(want))
		size, _ := w.Stop()
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadIntraModeYInter(&r, &fc, sg); got != want {
			t.Errorf("size_group=%d: got %d want %d", sg, got, want)
		}
	}
}

// TestReadIntraModeUvInter exercises the UV mode lookup keyed by Y
// mode.
func TestReadIntraModeUvInter(t *testing.T) {
	var fc FrameContext
	seedDefaultModeProbs(&fc)
	want := common.D207Pred
	yMode := common.HPred
	row := fc.UvModeProb[yMode]
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	emitIntraMode(t, &w, row[:], int(want))
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	if got := ReadIntraModeUvInter(&r, &fc, yMode); got != want {
		t.Errorf("got %d want %d", got, want)
	}
}

// TestReadIntraBlockModeInfoInterLargePartition: ≥8x8 reads one
// size-group-keyed Y mode and one Y-mode-keyed UV mode, then sets
// interp_filter to the sentinel.
func TestReadIntraBlockModeInfoInterLargePartition(t *testing.T) {
	var fc FrameContext
	seedDefaultModeProbs(&fc)
	mi := &NeighborMi{SbType: common.Block16x16}
	wantY := common.HPred
	wantUv := common.D135Pred

	sg := int(common.SizeGroupLookup[common.Block16x16])
	probsY := fc.YModeProb[sg]
	probsUv := fc.UvModeProb[wantY]

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	emitIntraMode(t, &w, probsY[:], int(wantY))
	emitIntraMode(t, &w, probsUv[:], int(wantUv))
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	uv := ReadIntraBlockModeInfoInter(&r, &fc, mi)
	if mi.Mode != wantY || uv != wantUv {
		t.Errorf("got (%d, uv=%d), want (%d, %d)", mi.Mode, uv, wantY, wantUv)
	}
	if mi.InterpFilter != uint8(SwitchableFilters) {
		t.Errorf("interp_filter = %d, want %d (SwitchableFilters sentinel)",
			mi.InterpFilter, SwitchableFilters)
	}
}

// TestReadIntraBlockModeInfoInterSub8x8: BLOCK_4X4 emits four sub-
// modes all keyed to size_group 0.
func TestReadIntraBlockModeInfoInterSub8x8(t *testing.T) {
	var fc FrameContext
	seedDefaultModeProbs(&fc)
	mi := &NeighborMi{SbType: common.Block4x4}
	subs := [4]common.PredictionMode{common.HPred, common.VPred, common.TmPred, common.D45Pred}
	wantUv := common.D63Pred

	row := fc.YModeProb[0]
	buf := make([]byte, 96)
	var w bitstream.Writer
	w.Start(buf)
	for _, s := range subs {
		emitIntraMode(t, &w, row[:], int(s))
	}
	uvRow := fc.UvModeProb[subs[3]]
	emitIntraMode(t, &w, uvRow[:], int(wantUv))
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	uv := ReadIntraBlockModeInfoInter(&r, &fc, mi)
	for i, want := range subs {
		if mi.Bmi[i].AsMode != want {
			t.Errorf("bmi[%d]=%d want %d", i, mi.Bmi[i].AsMode, want)
		}
	}
	if mi.Mode != subs[3] {
		t.Errorf("Mode=%d want bmi[3]=%d", mi.Mode, subs[3])
	}
	if uv != wantUv {
		t.Errorf("UV=%d want %d", uv, wantUv)
	}
}

// TestReadIntraFrameModeInfoDisabledSeg: segmentation off, skip=0,
// fixed tx mode → only Y and UV reads happen.
func TestReadIntraFrameModeInfoDisabledSeg(t *testing.T) {
	var fc FrameContext
	fc.SkipProbs[0] = 128
	var seg SegmentationParams
	maps := IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}

	mi := &NeighborMi{SbType: common.Block16x16}
	wantY := common.VPred
	wantUv := common.HPred

	probsY := tables.KfYModeProb[common.DcPred][common.DcPred]
	probsUv := tables.KfUvModeProb[wantY]

	buf := make([]byte, 128)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128) // skip=0
	// TxMode = Only4x4 → no tx-cascade bits.
	emitIntraMode(t, &w, probsY[:], int(wantY))
	emitIntraMode(t, &w, probsUv[:], int(wantUv))
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])

	out := ReadIntraFrameModeInfo(IntraFrameDriverArgs{
		Reader:   &r,
		Fc:       &fc,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   common.Only4x4,
		MiOffset: 0,
		XMis:     2, YMis: 2,
	}, mi)

	if mi.RefFrame[0] != IntraFrame || mi.RefFrame[1] != NoRefFrame {
		t.Errorf("ref_frame = %v, want (IntraFrame, NoRefFrame)", mi.RefFrame)
	}
	if mi.Skip != 0 {
		t.Errorf("skip = %d, want 0", mi.Skip)
	}
	if mi.TxSize != common.Tx4x4 {
		t.Errorf("tx_size = %d, want Tx4x4", mi.TxSize)
	}
	if mi.Mode != wantY {
		t.Errorf("Y = %d, want %d", mi.Mode, wantY)
	}
	if out.UvMode != wantUv {
		t.Errorf("UV = %d, want %d", out.UvMode, wantUv)
	}
}
