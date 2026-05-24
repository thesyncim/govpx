//go:build govpx_oracle_trace

package tables

import (
	"os"
	"strings"
	"testing"
)

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
	probe := strings.Replace(src[idx:], "default_nmv_context = {", "default_nmv_context[] = {", 1)
	want := extractBracedArray(probe, "default_nmv_context")
	if want == nil {
		t.Fatal("default_nmv_context body not extractable")
	}

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
