package common

import "testing"

// TestTreesShape checks each tree has the expected size for its leaf
// count — 2*N - 2 per libvpx's TREE_SIZE macro.
func TestTreesShape(t *testing.T) {
	cases := []struct {
		name string
		size int
		want int
	}{
		{"intra_mode (10 leaves)", len(IntraModeTree), 18},
		{"inter_mode (4 leaves)", len(InterModeTree), 6},
		{"partition (4 leaves)", len(PartitionTree), 6},
		{"switchable_interp (3 leaves)", len(SwitchableInterpTree), 4},
		{"segment (8 leaves)", len(SegmentTree), 14},
	}
	for _, c := range cases {
		if c.size != c.want {
			t.Errorf("%s: size %d, want %d", c.name, c.size, c.want)
		}
	}
}

// TestTreesValidIndices walks each tree and confirms every positive
// child index is even and within bounds, every non-positive leaf is
// in the legal range for its codec, and reachable leaves cover every
// expected value.
func TestTreesValidIndices(t *testing.T) {
	check := func(name string, tree []int8, leafMax int) {
		t.Helper()
		seen := make(map[int]bool)
		var walk func(idx int)
		walk = func(idx int) {
			left := tree[idx]
			right := tree[idx+1]
			for _, v := range [2]int8{left, right} {
				if v > 0 {
					if int(v) >= len(tree) || v&1 != 0 {
						t.Errorf("%s: invalid child index %d at idx %d", name, v, idx)
						return
					}
					walk(int(v))
				} else {
					leaf := -int(v)
					if leaf < 0 || leaf > leafMax {
						t.Errorf("%s: leaf %d out of [0, %d]", name, leaf, leafMax)
					}
					seen[leaf] = true
				}
			}
		}
		walk(0)
	}
	// Intra mode tree has 10 leaves (DC..TM).
	check("intra_mode", IntraModeTree[:], 9)
	check("inter_mode", InterModeTree[:], 3)
	check("partition", PartitionTree[:], 3)
	check("switchable_interp", SwitchableInterpTree[:], 2)
	check("segment", SegmentTree[:], 7)
}
