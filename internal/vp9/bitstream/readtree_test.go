package bitstream

import "testing"

// TestReadTreeBinaryDecodes a 4-leaf tree whose layout follows
// libvpx's convention exactly:
//
//	  [node 0]
//	 0 /     \ 1
//	-L0     [node 2]
//	      0 /    \ 1
//	     -L1   [node 4]
//	          0 /  \ 1
//	        -L2   -L3
//
// Encoded as a flat array each internal node lists its (left, right)
// children at indices i, i+1. Left = -L0 (leaf 0) for node 0; right
// jumps to index 2. Etc.
func TestReadTreeBinaryDecodes(t *testing.T) {
	// tree[0..5] = {-0, 2, -1, 4, -2, -3}
	tree := []int8{0, 2, -1, 4, -2, -3}
	// One probability per pair of children: probs[i] selects node 2*i.
	// Node 0 uses probs[0], node 2 uses probs[1], node 4 uses probs[2].
	// Pick 1 (very low — boolean coder almost always picks the
	// "1" branch) so the encoder writes 1 bits to reach deeper leaves.
	probs := []uint8{1, 1, 1}

	for leaf := 0; leaf < 4; leaf++ {
		buf := make([]byte, 32)
		var w Writer
		w.Start(buf)
		// Walk the tree manually to emit the matching bits.
		i := int8(0)
		for {
			leftIdx := tree[i]
			rightIdx := tree[i+1]
			leftLeaf := -int(leftIdx)
			rightLeaf := -int(rightIdx)
			if leftIdx <= 0 && leftLeaf == leaf {
				w.Write(0, uint32(probs[i>>1]))
				break
			}
			if rightIdx <= 0 && rightLeaf == leaf {
				w.Write(1, uint32(probs[i>>1]))
				break
			}
			// Need to descend. Pick the side whose subtree contains the
			// target leaf — for this synthetic tree the right side
			// always contains higher-numbered leaves.
			if leftIdx > 0 && leaf < rightLeaf {
				w.Write(0, uint32(probs[i>>1]))
				i = leftIdx
				continue
			}
			w.Write(1, uint32(probs[i>>1]))
			i = rightIdx
		}
		size, err := w.Stop()
		if err != nil {
			t.Fatalf("leaf %d: Stop: %v", leaf, err)
		}
		var r Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("leaf %d: Init: %v", leaf, err)
		}
		if got := r.ReadTree(tree, probs); got != leaf {
			t.Errorf("leaf %d: ReadTree returned %d", leaf, got)
		}
	}
}
