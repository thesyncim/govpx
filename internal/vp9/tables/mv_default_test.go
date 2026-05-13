package tables

import (
	"os"
	"strings"
	"testing"
)

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

// TestDefaultNmvContextMatchesLibvpxSource validates the seed NMV
// probabilities byte-for-byte against libvpx's vp9_entropymv.c.
func TestDefaultNmvContextMatchesLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vp9/common/vp9_entropymv.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	// default_nmv_context is declared with `= {` rather than the
	// `[size] = {` form extractBracedArray expects. Pre-trim the
	// source so the helper finds the brace block.
	idx := strings.Index(src, "default_nmv_context = {")
	if idx < 0 {
		t.Fatal("default_nmv_context marker not found")
	}
	// Re-anchor by injecting a synthetic "[" so extractBracedArray's
	// `name + "["` lookup matches.
	probe := strings.Replace(src[idx:], "default_nmv_context = {", "default_nmv_context[] = {", 1)
	want := extractBracedArray(probe, "default_nmv_context")
	if want == nil {
		t.Fatal("default_nmv_context body not extractable")
	}

	// Build the expected flat layout matching the C struct order:
	//   joints[3] + per-axis (sign + classes[10] + class0[1] + bits[10]
	//   + class0_fp[2][3] + fp[3] + class0_hp + hp)
	flatGo := make([]int, 0, 64)
	for _, v := range DefaultNmvJoints {
		flatGo = append(flatGo, int(v))
	}
	for _, c := range DefaultNmvComps {
		flatGo = append(flatGo, int(c.Sign))
		for _, v := range c.Classes {
			flatGo = append(flatGo, int(v))
		}
		for _, v := range c.Class0 {
			flatGo = append(flatGo, int(v))
		}
		for _, v := range c.Bits {
			flatGo = append(flatGo, int(v))
		}
		for _, row := range c.Class0Fp {
			for _, v := range row {
				flatGo = append(flatGo, int(v))
			}
		}
		for _, v := range c.Fp {
			flatGo = append(flatGo, int(v))
		}
		flatGo = append(flatGo, int(c.Class0Hp))
		flatGo = append(flatGo, int(c.Hp))
	}

	if len(want) != len(flatGo) {
		t.Fatalf("nmv context: libvpx %d values, Go %d", len(want), len(flatGo))
	}
	for i := range want {
		if want[i] != flatGo[i] {
			t.Errorf("nmv[%d] = %d, libvpx says %d", i, flatGo[i], want[i])
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
	_ = strings.TrimSpace // keep the import used while we restructure
}
