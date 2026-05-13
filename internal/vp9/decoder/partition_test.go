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
