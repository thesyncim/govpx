package tables

import "testing"

// TestMvTreesShape checks the TREE_SIZE(N) = 2*N - 2 invariant for
// each MV token tree.
func TestMvTreesShape(t *testing.T) {
	cases := []struct {
		name string
		size int
		want int
	}{
		{"mv_joint (4 leaves)", len(MvJointTree), 6},
		{"mv_class (11 leaves)", len(MvClassTree), 20},
		{"mv_class0 (2 leaves)", len(MvClass0Tree), 2},
		{"mv_fp (4 leaves)", len(MvFpTree), 6},
	}
	for _, c := range cases {
		if c.size != c.want {
			t.Errorf("%s: size %d, want %d", c.name, c.size, c.want)
		}
	}
}

// TestMvTreesAreCanonical hand-validates the four MV token trees
// against the libvpx layout. The libvpx source defines these with
// preprocessor identifiers (MV_CLASS_*, MV_JOINT_*) so a flat
// digit-grep oracle can't recover the leaves; we duplicate the
// expected literal here from vp9_entropymv.c so the Go tree port
// stays anchored.
func TestMvTreesAreCanonical(t *testing.T) {
	if MvJointTree != [6]int8{0, 2, -1, 4, -2, -3} {
		t.Errorf("MvJointTree = %v", MvJointTree)
	}
	wantClass := [20]int8{
		0, 2, -1, 4, 6,
		8, -2, -3, 10, 12,
		-4, -5, -6, 14, 16,
		18, -7, -8, -9, -10,
	}
	if MvClassTree != wantClass {
		t.Errorf("MvClassTree = %v\n  want %v", MvClassTree, wantClass)
	}
	if MvClass0Tree != [2]int8{0, -1} {
		t.Errorf("MvClass0Tree = %v", MvClass0Tree)
	}
	if MvFpTree != [6]int8{0, 2, -1, 4, -2, -3} {
		t.Errorf("MvFpTree = %v", MvFpTree)
	}
}
