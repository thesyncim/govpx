package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestPartitionPlaneContext(t *testing.T) {
	above := make([]int8, 16)
	left := make([]int8, 16)
	// Set partition bits at the layer corresponding to Block16x16:
	// MiWidthLog2Lookup[16x16] = 1, so the bit-1 of each context byte
	// drives the (above, left) flags.
	above[3] = 0b10 // above bit 1 = 1
	left[5] = 0b00  // left bit 1 = 0
	ctx := PartitionPlaneContext(above, left, 5, 3, common.Block16x16)
	// expected: left=0, above=1, bsl=1 → 0*2 + 1 + 1*4 = 5
	if ctx != 5 {
		t.Errorf("ctx = %d, want 5", ctx)
	}

	left[5] = 0b10 // left bit 1 = 1
	ctx = PartitionPlaneContext(above, left, 5, 3, common.Block16x16)
	// expected: left=1, above=1, bsl=1 → 1*2 + 1 + 4 = 7
	if ctx != 7 {
		t.Errorf("ctx = %d, want 7", ctx)
	}
}

func TestReadPartitionHasRowsAndCols(t *testing.T) {
	// has_rows && has_cols → full 4-way decode via ReadTree.
	// Synthesize a stream where the writer emits each partition in
	// turn against MAX_PROB so the tree walk is deterministic.
	probs := []uint8{128, 128, 128}

	cases := []common.PartitionType{
		common.PartitionNone,
		common.PartitionHorz,
		common.PartitionVert,
		common.PartitionSplit,
	}
	for _, want := range cases {
		buf := make([]byte, 16)
		var w bitstream.Writer
		w.Start(buf)
		// Walk PartitionTree manually to emit matching bits.
		i := int8(0)
		for {
			leftLeaf := -common.PartitionTree[i]
			rightLeaf := -common.PartitionTree[i+1]
			leftIdx := common.PartitionTree[i]
			rightIdx := common.PartitionTree[i+1]
			if leftIdx <= 0 && common.PartitionType(leftLeaf) == want {
				w.Write(0, uint32(probs[i>>1]))
				break
			}
			if rightIdx <= 0 && common.PartitionType(rightLeaf) == want {
				w.Write(1, uint32(probs[i>>1]))
				break
			}
			// Need to descend further. Pick the side that contains
			// our leaf. For PartitionTree the right subtree always
			// contains the higher-numbered partitions.
			if rightIdx > 0 && int(want) >= int(rightLeaf) {
				w.Write(1, uint32(probs[i>>1]))
				i = rightIdx
				continue
			}
			w.Write(0, uint32(probs[i>>1]))
			i = leftIdx
		}
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadPartition(&r, probs, true, true); got != want {
			t.Errorf("partition %d: got %d", want, got)
		}
	}
}

func TestReadPartitionBoundary(t *testing.T) {
	probs := []uint8{128, 128, 128}

	// has_cols only — 1 bit, 0 = HORZ, 1 = SPLIT.
	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, uint32(probs[1]))
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := ReadPartition(&r, probs, false, true); got != common.PartitionHorz {
		t.Errorf("has_cols only / bit 0: got %d, want Horz", got)
	}

	// neither — always SPLIT, no bits read.
	r.Init([]byte{0x00})
	if got := ReadPartition(&r, probs, false, false); got != common.PartitionSplit {
		t.Errorf("neither: got %d, want Split", got)
	}
}

// TestUpdatePartitionContext stamps the per-subsize "above" / "left"
// values across the right width and confirms slots outside the bw
// window stay untouched.
func TestUpdatePartitionContext(t *testing.T) {
	above := make([]int8, 16)
	left := make([]int8, 16)
	for i := range above {
		above[i] = -1
		left[i] = -1
	}
	// Block16x16 → {12, 12} per partition_context_lookup.
	UpdatePartitionContext(above, left, 5, 3, common.Block16x16, 2)
	wantAbove := common.PartitionContextLookup[common.Block16x16].Above
	wantLeft := common.PartitionContextLookup[common.Block16x16].Left
	for i := range 2 {
		if above[3+i] != wantAbove {
			t.Errorf("above[%d]=%d want %d", 3+i, above[3+i], wantAbove)
		}
		if left[(5&common.MiMask)+i] != wantLeft {
			t.Errorf("left[%d]=%d want %d", (5&common.MiMask)+i, left[(5&common.MiMask)+i], wantLeft)
		}
	}
	// Outside the window: untouched.
	if above[2] != -1 || above[5] != -1 {
		t.Errorf("above outside window dirtied: %d, %d", above[2], above[5])
	}
}

// TestIsInside covers the tile + frame bound combinations.
func TestIsInside(t *testing.T) {
	tile := TileBounds{MiColStart: 4, MiColEnd: 12}
	cases := []struct {
		miRow, miCol   int
		posRow, posCol int
		want           bool
	}{
		// inside
		{5, 5, 0, 1, true},
		{5, 5, -1, 0, true},
		// row clamp at -1
		{0, 5, -1, 0, false},
		// row >= miRows (10)
		{9, 5, 1, 0, false},
		// col < tile start
		{5, 4, 0, -1, false},
		// col >= tile end
		{5, 11, 0, 1, false},
		// col at exactly start/end
		{5, 5, 0, -1, true}, // (5,4) ok
	}
	for i, c := range cases {
		if got := IsInside(tile, 10, c.miRow, c.miCol, c.posRow, c.posCol); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}
