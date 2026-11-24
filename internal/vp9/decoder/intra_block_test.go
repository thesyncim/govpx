package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestGetYMode covers both branches: sub-8x8 reads bmi, ≥8x8 reads
// the block-level mode.
func TestGetYMode(t *testing.T) {
	mi := &NeighborMi{SbType: common.Block4x4, Mode: common.HPred}
	mi.Bmi[0].AsMode = common.DcPred
	mi.Bmi[1].AsMode = common.VPred
	mi.Bmi[2].AsMode = common.TmPred
	mi.Bmi[3].AsMode = common.D45Pred
	for i, want := range []common.PredictionMode{common.DcPred, common.VPred, common.TmPred, common.D45Pred} {
		if got := GetYMode(mi, i); got != want {
			t.Errorf("sub-8x8 block %d: got %d want %d", i, got, want)
		}
	}
	mi.SbType = common.Block16x16
	if got := GetYMode(mi, 2); got != common.HPred {
		t.Errorf("8x8+: got %d want HPred", got)
	}
}

// TestLeftAboveBlockMode covers the four sub-block offsets and the
// inter/missing neighbor → DC fall-through.
func TestLeftAboveBlockMode(t *testing.T) {
	cur := &NeighborMi{}
	cur.Bmi[0].AsMode = common.VPred
	cur.Bmi[1].AsMode = common.HPred
	cur.Bmi[2].AsMode = common.D45Pred
	cur.Bmi[3].AsMode = common.D135Pred

	// Left neighbor: intra, sub-8x8.
	left := &NeighborMi{SbType: common.Block4x4}
	left.Bmi[1].AsMode = common.TmPred  // used for b=0 (b+1)
	left.Bmi[3].AsMode = common.D63Pred // used for b=2

	if got := LeftBlockMode(cur, left, 0); got != common.TmPred {
		t.Errorf("LeftBlockMode b=0 got %d want TmPred", got)
	}
	if got := LeftBlockMode(cur, left, 1); got != cur.Bmi[0].AsMode {
		t.Errorf("LeftBlockMode b=1 got %d want %d", got, cur.Bmi[0].AsMode)
	}
	if got := LeftBlockMode(cur, left, 2); got != common.D63Pred {
		t.Errorf("LeftBlockMode b=2 got %d want D63Pred", got)
	}
	if got := LeftBlockMode(cur, left, 3); got != cur.Bmi[2].AsMode {
		t.Errorf("LeftBlockMode b=3 got %d want %d", got, cur.Bmi[2].AsMode)
	}
	// Missing neighbor → DC.
	if got := LeftBlockMode(cur, nil, 0); got != common.DcPred {
		t.Errorf("LeftBlockMode nil: got %d want DC", got)
	}
	// Inter neighbor → DC.
	inter := &NeighborMi{RefFrame: [2]int8{LastFrame, NoRefFrame}}
	if got := LeftBlockMode(cur, inter, 0); got != common.DcPred {
		t.Errorf("LeftBlockMode inter: got %d want DC", got)
	}

	// Above neighbor: same four-case shape but with b+2 / b-2 indexing.
	above := &NeighborMi{SbType: common.Block4x4}
	above.Bmi[2].AsMode = common.HPred // b=0 → b+2
	above.Bmi[3].AsMode = common.VPred // b=1 → b+2
	if got := AboveBlockMode(cur, above, 0); got != common.HPred {
		t.Errorf("AboveBlockMode b=0 got %d want HPred", got)
	}
	if got := AboveBlockMode(cur, above, 1); got != common.VPred {
		t.Errorf("AboveBlockMode b=1 got %d want VPred", got)
	}
	if got := AboveBlockMode(cur, above, 2); got != cur.Bmi[0].AsMode {
		t.Errorf("AboveBlockMode b=2 got %d want %d", got, cur.Bmi[0].AsMode)
	}
	if got := AboveBlockMode(cur, above, 3); got != cur.Bmi[1].AsMode {
		t.Errorf("AboveBlockMode b=3 got %d want %d", got, cur.Bmi[1].AsMode)
	}
}

// TestGetYModeProbs anchors the (above, left) → row lookup in
// KfYModeProb.
func TestGetYModeProbs(t *testing.T) {
	cur := &NeighborMi{SbType: common.Block16x16}
	left := &NeighborMi{SbType: common.Block16x16, Mode: common.HPred}
	above := &NeighborMi{SbType: common.Block16x16, Mode: common.VPred}
	got := GetYModeProbs(cur, above, left, 0)
	want := tables.KfYModeProb[common.VPred][common.HPred]
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %d want %d", i, got[i], want[i])
		}
	}
}

// TestReadTxSizeFixed: TxMode != Select → result is clamped to the
// frame-level biggest-tx-size cap.
func TestReadTxSizeFixed(t *testing.T) {
	var fc FrameContext
	var r bitstream.Reader
	// TxMode = Allow16x16, max for Block32x32 = Tx32x32 → cap to Tx16x16.
	got := ReadTxSize(&r, &fc, common.Allow16x16, common.Block32x32, nil, nil, true)
	if got != common.Tx16x16 {
		t.Errorf("Block32x32 + Allow16x16: got %d want Tx16x16", got)
	}
	// Allow32x32 + Block16x16 → cap to maxTx = Tx16x16.
	got = ReadTxSize(&r, &fc, common.Allow32x32, common.Block16x16, nil, nil, true)
	if got != common.Tx16x16 {
		t.Errorf("Block16x16 + Allow32x32: got %d want Tx16x16", got)
	}
}

// TestReadTxSizeSelectGated: even with TxModeSelect, sub-8x8 blocks
// skip the cascade and return the max.
func TestReadTxSizeSelectGated(t *testing.T) {
	var fc FrameContext
	var r bitstream.Reader
	got := ReadTxSize(&r, &fc, common.TxModeSelect, common.Block4x4, nil, nil, true)
	if got != common.Tx4x4 {
		t.Errorf("Block4x4 + Select: got %d want Tx4x4", got)
	}
}

// TestReadTxSizeSelectReads exercises the boolean-coded cascade.
func TestReadTxSizeSelectReads(t *testing.T) {
	var fc FrameContext
	for ctx := range fc.TxProbs.P32x32 {
		for i := range fc.TxProbs.P32x32[ctx] {
			fc.TxProbs.P32x32[ctx][i] = 128
		}
	}
	// Block64x64 has max=Tx32x32; ctx=1 (both nil neighbors → max+max>max).
	buf := make([]byte, 16)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, 128) // not 4x4
	w.Write(1, 128) // not 8x8
	w.Write(1, 128) // → 32x32
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	got := ReadTxSize(&r, &fc, common.TxModeSelect, common.Block64x64, nil, nil, true)
	if got != common.Tx32x32 {
		t.Errorf("got %d want Tx32x32", got)
	}
}

// TestReadIntraBlockModeInfoBlock16x16: large partition reads one Y
// mode then one UV mode.
func TestReadIntraBlockModeInfoBlock16x16(t *testing.T) {
	cur := &NeighborMi{SbType: common.Block16x16}
	wantY := common.HPred
	wantUv := common.D135Pred

	probsY := tables.KfYModeProb[common.DcPred][common.DcPred]
	probsUv := tables.KfUvModeProb[wantY]

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	emitIntraMode(t, &w, probsY[:], int(wantY))
	emitIntraMode(t, &w, probsUv[:], int(wantUv))
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])

	uv := ReadIntraBlockModeInfo(&r, cur, nil, nil)
	if cur.Mode != wantY {
		t.Errorf("Y mode = %d, want %d", cur.Mode, wantY)
	}
	if uv != wantUv {
		t.Errorf("UV mode = %d, want %d", uv, wantUv)
	}
}

// TestReadIntraBlockModeInfoBlock4x4: 4 sub-modes encoded in row
// order, mode = bmi[3].
func TestReadIntraBlockModeInfoBlock4x4(t *testing.T) {
	cur := &NeighborMi{SbType: common.Block4x4}
	subs := [4]common.PredictionMode{
		common.HPred, common.VPred, common.TmPred, common.D45Pred,
	}
	wantUv := common.D207Pred

	buf := make([]byte, 96)
	var w bitstream.Writer
	w.Start(buf)
	// libvpx walks blocks in order; the i==1,3 cases use the just-decoded
	// `cur.bmi[i-1]` as the left neighbor for the next iteration. The
	// encoder mirrors this by recomputing the probs row per block.
	for i := range 4 {
		row := GetYModeProbs(cur, nil, nil, i)
		emitIntraMode(t, &w, row, int(subs[i]))
		cur.Bmi[i].AsMode = subs[i]
	}
	uvRow := tables.KfUvModeProb[subs[3]]
	emitIntraMode(t, &w, uvRow[:], int(wantUv))
	size, _ := w.Stop()
	// Reset Bmi so the decoder repopulates them via the boolean coder.
	cur.Bmi = [4]Bmi{}

	var r bitstream.Reader
	r.Init(buf[:size])
	uv := ReadIntraBlockModeInfo(&r, cur, nil, nil)
	for i, want := range subs {
		if cur.Bmi[i].AsMode != want {
			t.Errorf("bmi[%d] = %d, want %d", i, cur.Bmi[i].AsMode, want)
		}
	}
	if cur.Mode != subs[3] {
		t.Errorf("Mode = %d, want bmi[3]=%d", cur.Mode, subs[3])
	}
	if uv != wantUv {
		t.Errorf("UV = %d, want %d", uv, wantUv)
	}
}

// emitIntraMode is a thin shim that emits the boolean-coded
// representation of `leaf` for the canonical IntraModeTree under
// `probs`. Mirrors the helper used in mode_test.go but reusable in
// this composite test.
func emitIntraMode(t *testing.T, w *bitstream.Writer, probs []uint8, leaf int) {
	t.Helper()
	bits, idx, ok := findTreePath(common.IntraModeTree[:], leaf)
	if !ok {
		t.Fatalf("leaf %d not reachable", leaf)
	}
	for k := range bits {
		w.Write(bits[k], uint32(probs[idx[k]]))
	}
}
