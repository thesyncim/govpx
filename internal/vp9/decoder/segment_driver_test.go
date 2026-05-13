package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestCopySegmentIdRect copies a 2×3 window with an offset and stride
// of 5, and confirms only the window slots are written.
func TestCopySegmentIdRect(t *testing.T) {
	miCols := 5
	last := []uint8{
		9, 9, 9, 9, 9,
		9, 1, 2, 3, 9,
		9, 4, 5, 6, 9,
		9, 9, 9, 9, 9,
	}
	cur := make([]uint8, len(last))
	for i := range cur {
		cur[i] = 0xff
	}
	off := 1*miCols + 1
	CopySegmentId(cur, last, miCols, off, 3, 2)
	// Inside the window: copied from last.
	if cur[off] != 1 || cur[off+1] != 2 || cur[off+2] != 3 {
		t.Errorf("row 0 = %v, want 1 2 3", cur[off:off+3])
	}
	if cur[off+miCols] != 4 || cur[off+miCols+1] != 5 || cur[off+miCols+2] != 6 {
		t.Errorf("row 1 = %v, want 4 5 6", cur[off+miCols:off+miCols+3])
	}
	// Outside the window: sentinel preserved.
	if cur[0] != 0xff || cur[4] != 0xff {
		t.Error("outside-window slots were overwritten")
	}
}

// TestCopySegmentIdNil: nil source zeros the window.
func TestCopySegmentIdNil(t *testing.T) {
	miCols := 4
	cur := []uint8{
		7, 7, 7, 7,
		7, 7, 7, 7,
	}
	CopySegmentId(cur, nil, miCols, 0, 4, 2)
	for i, v := range cur {
		if v != 0 {
			t.Errorf("cur[%d]=%d, want 0 (nil source should zero-fill)", i, v)
		}
	}
}

// TestSetSegmentId fills a window with a single id.
func TestSetSegmentId(t *testing.T) {
	miCols := 3
	cur := make([]uint8, 9)
	SetSegmentId(cur, miCols, 0, 2, 2, 4)
	want := []uint8{4, 4, 0, 4, 4, 0, 0, 0, 0}
	for i, v := range cur {
		if v != want[i] {
			t.Errorf("cur[%d]=%d, want %d", i, v, want[i])
		}
	}
}

// TestReadIntraSegmentIdDisabled: disabled segmentation returns 0 and
// touches nothing.
func TestReadIntraSegmentIdDisabled(t *testing.T) {
	var seg SegmentationParams
	maps := IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}
	var r bitstream.Reader
	if got := ReadIntraSegmentId(&r, &seg, &maps, 0, 4, 4); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// TestReadIntraSegmentIdCopyMode: UpdateMap=false triggers a copy
// from last_frame_seg_map (or zero if nil) and returns 0.
func TestReadIntraSegmentIdCopyMode(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = false
	last := []uint8{5, 5, 5, 5}
	cur := []uint8{0, 0, 0, 0}
	maps := IntraSegmentMaps{
		CurrentFrameSegMap: cur,
		LastFrameSegMap:    last,
		MiCols:             4,
	}
	var r bitstream.Reader
	if got := ReadIntraSegmentId(&r, &seg, &maps, 0, 4, 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	for i, v := range cur {
		if v != 5 {
			t.Errorf("cur[%d]=%d, want 5 (copied)", i, v)
		}
	}
}

// TestReadIntraSegmentIdRead: UpdateMap=true reads the segment id
// from the boolean coder and writes it into the current-frame map.
func TestReadIntraSegmentIdRead(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = true
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	maps := IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 16),
		MiCols:             4,
	}
	want := 5
	data := encodeLeafSeg(t, want, seg.TreeProbs[:])
	var r bitstream.Reader
	r.Init(data)
	got := ReadIntraSegmentId(&r, &seg, &maps, 0, 2, 2)
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if maps.CurrentFrameSegMap[y*4+x] != uint8(want) {
				t.Errorf("seg map [%d,%d] not written", y, x)
			}
		}
	}
}

// TestReadInterSegmentIdTemporalPredicted: UpdateMap=true,
// TemporalUpdate=true, bit=1 → reuse predicted id without reading
// the tree.
func TestReadInterSegmentIdTemporalPredicted(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = true
	seg.TemporalUpdate = true
	for i := range seg.PredProbs {
		seg.PredProbs[i] = 128
	}
	last := []uint8{7, 7, 7, 7}
	var spred uint8
	maps := InterSegmentMaps{
		IntraSegmentMaps: IntraSegmentMaps{
			CurrentFrameSegMap: make([]uint8, 4),
			LastFrameSegMap:    last,
			MiCols:             4,
		},
		SegIDPredictedOut: &spred,
	}
	// Encode a single bit=1 against pred prob 128.
	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, 128)
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	got := ReadInterSegmentId(&r, &seg, &maps, 0, 4, 1, nil, nil)
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}
	if spred != 1 {
		t.Errorf("seg_id_predicted = %d, want 1", spred)
	}
}

// TestReadInterSegmentIdTemporalRead: bit=0 → fall through to tree
// read.
func TestReadInterSegmentIdTemporalRead(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = true
	seg.TemporalUpdate = true
	for i := range seg.PredProbs {
		seg.PredProbs[i] = 128
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	maps := InterSegmentMaps{
		IntraSegmentMaps: IntraSegmentMaps{
			CurrentFrameSegMap: make([]uint8, 4),
			MiCols:             4,
		},
	}
	want := 3
	// Bit=0 for the pred-prob, then segment tree path for `want`.
	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128)
	// Re-use the same tree-path encoder used in mode_test.go.
	bits, idx, _ := findTreePath(common.SegmentTree[:], want)
	for k := range bits {
		w.Write(bits[k], uint32(seg.TreeProbs[idx[k]]))
	}
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	got := ReadInterSegmentId(&r, &seg, &maps, 0, 4, 1, nil, nil)
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

// TestReadSwitchableInterpFilterRoundTrip walks every filter leaf.
func TestReadSwitchableInterpFilterRoundTrip(t *testing.T) {
	var fc FrameContext
	for i := range fc.SwitchableInterpProb {
		for j := range fc.SwitchableInterpProb[i] {
			fc.SwitchableInterpProb[i][j] = 128
		}
	}
	wants := []InterpFilter{InterpEighttap, InterpEighttapSmooth, InterpEighttapSharp}
	for _, want := range wants {
		bits, idx, _ := findTreePath(common.SwitchableInterpTree[:], int(want))
		buf := make([]byte, 16)
		var w bitstream.Writer
		w.Start(buf)
		for k := range bits {
			// ctx=SwitchableFilters (both neighbors nil → "no filter" sentinel ctx).
			w.Write(bits[k], uint32(fc.SwitchableInterpProb[SwitchableFilters][idx[k]]))
		}
		size, _ := w.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		if got := ReadSwitchableInterpFilter(&r, &fc, nil, nil); got != want {
			t.Errorf("want %d, got %d", want, got)
		}
	}
}

// encodeLeafSeg reuses findTreePath against the canonical 8-leaf
// segment tree.
func encodeLeafSeg(t *testing.T, leaf int, probs []uint8) []byte {
	t.Helper()
	bits, idx, _ := findTreePath(common.SegmentTree[:], leaf)
	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	for k := range bits {
		w.Write(bits[k], uint32(probs[idx[k]]))
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	return buf[:size]
}
