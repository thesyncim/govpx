package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// findTreePath returns the bit sequence (and per-step prob indices) that
// drives ReadTree to the requested leaf. Mirrors the libvpx token-tree
// shape: positive entries are next-node indices, non-positive entries
// are -leaf.
func findTreePath(tree []int8, leaf int) (bits []uint32, probIdx []int, ok bool) {
	var rec func(i int8) bool
	rec = func(i int8) bool {
		for b := range uint32(2) {
			next := tree[int(i)+int(b)]
			if next <= 0 {
				if -int(next) == leaf {
					bits = append(bits, b)
					probIdx = append(probIdx, int(i>>1))
					return true
				}
				continue
			}
			bits = append(bits, b)
			probIdx = append(probIdx, int(i>>1))
			if rec(next) {
				return true
			}
			bits = bits[:len(bits)-1]
			probIdx = probIdx[:len(probIdx)-1]
		}
		return false
	}
	if rec(0) {
		return bits, probIdx, true
	}
	return nil, nil, false
}

// encodeLeaf emits the boolean-coded byte stream that decodes back to
// `leaf` when fed to ReadTree with the same `tree` and `probs`.
func encodeLeaf(t *testing.T, tree []int8, probs []uint8, leaf int) []byte {
	t.Helper()
	bits, idx, ok := findTreePath(tree, leaf)
	if !ok {
		t.Fatalf("leaf %d not reachable in tree", leaf)
	}
	buf := make([]byte, 64)
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

// TestReadIntraModeRoundTrip confirms every PREDICTION_MODE leaf round-
// trips through the IntraModeTree.
func TestReadIntraModeRoundTrip(t *testing.T) {
	probs := make([]uint8, 9)
	for i := range probs {
		probs[i] = 128
	}
	leaves := []common.PredictionMode{
		common.DcPred,
		common.VPred,
		common.HPred,
		common.D45Pred,
		common.D135Pred,
		common.D117Pred,
		common.D153Pred,
		common.D207Pred,
		common.D63Pred,
		common.TmPred,
	}
	for _, want := range leaves {
		data := encodeLeaf(t, common.IntraModeTree[:], probs, int(want))
		var r bitstream.Reader
		if err := r.Init(data); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadIntraMode(&r, probs); got != want {
			t.Errorf("mode %d: ReadIntraMode returned %d", want, got)
		}
	}
}

// TestReadInterModeOffsets confirms ReadInterMode adds the NearestMv
// offset so the absolute PredictionMode is returned.
func TestReadInterModeOffsets(t *testing.T) {
	var probs [common.InterModes - 1]uint8
	for i := range probs {
		probs[i] = 128
	}
	// InterModeTree leaves run 0..3 (sub-mode); absolute = NEARESTMV + leaf.
	for sub := range common.InterModes {
		want := common.NearestMv + common.PredictionMode(sub)
		data := encodeLeaf(t, common.InterModeTree[:], probs[:], sub)
		var r bitstream.Reader
		if err := r.Init(data); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadInterMode(&r, probs); got != want {
			t.Errorf("sub=%d: got %d, want %d", sub, got, want)
		}
	}
}

// TestReadSkip exercises the single-bit gate against the boolean coder.
func TestReadSkip(t *testing.T) {
	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128)
	w.Write(1, 128)
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := ReadSkip(&r, 128); got != 0 {
		t.Errorf("first skip: got %d", got)
	}
	if got := ReadSkip(&r, 128); got != 1 {
		t.Errorf("second skip: got %d", got)
	}
}

// TestReadSegmentIdRoundTrip walks all 8 segment IDs through SegmentTree.
func TestReadSegmentIdRoundTrip(t *testing.T) {
	var probs [SegTreeProbs]uint8
	for i := range probs {
		probs[i] = 128
	}
	for seg := range MaxSegments {
		data := encodeLeaf(t, common.SegmentTree[:], probs[:], seg)
		var r bitstream.Reader
		if err := r.Init(data); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadSegmentId(&r, probs); got != seg {
			t.Errorf("seg=%d: ReadSegmentId returned %d", seg, got)
		}
	}
}

// TestReadSelectedTxSizeBounds covers the read_selected_tx_size cascade.
func TestReadSelectedTxSizeBounds(t *testing.T) {
	probs := []uint8{128, 128, 128}

	// max=4x4: only the first bit is consulted, result clamps to 4x4.
	{
		buf := make([]byte, 8)
		var w bitstream.Writer
		w.Start(buf)
		w.Write(0, uint32(probs[0]))
		size, _ := w.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		if got := ReadSelectedTxSize(&r, common.Tx4x4, probs); got != common.Tx4x4 {
			t.Errorf("max=4x4: got %d", got)
		}
	}

	// max=32x32, stop at 16x16: tx becomes 1 then 2 then bit2=0.
	{
		buf := make([]byte, 8)
		var w bitstream.Writer
		w.Start(buf)
		w.Write(1, uint32(probs[0]))
		w.Write(1, uint32(probs[1]))
		w.Write(0, uint32(probs[2]))
		size, _ := w.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		if got := ReadSelectedTxSize(&r, common.Tx32x32, probs); got != common.Tx16x16 {
			t.Errorf("stop at 16x16: got %d", got)
		}
	}

	// max=32x32, go all the way to 32x32.
	{
		buf := make([]byte, 8)
		var w bitstream.Writer
		w.Start(buf)
		w.Write(1, uint32(probs[0]))
		w.Write(1, uint32(probs[1]))
		w.Write(1, uint32(probs[2]))
		size, _ := w.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		if got := ReadSelectedTxSize(&r, common.Tx32x32, probs); got != common.Tx32x32 {
			t.Errorf("max=32x32: got %d", got)
		}
	}
}
