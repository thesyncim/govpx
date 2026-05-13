package tables

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDetokTablesMatchLibvpxSource validates that the generated
// detok_tables.go matches libvpx v1.16.0 vp9/common/vp9_entropy.c
// byte-for-byte: cat-probs, pareto8_full, and the two coefband
// translate maps.
func TestDetokTablesMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSourceVP9("vp9/common/vp9_entropy.c")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	flat := []struct {
		marker string
		got    []uint8
	}{
		{"vp9_cat1_prob[", Cat1Prob[:]},
		{"vp9_cat2_prob[", Cat2Prob[:]},
		{"vp9_cat3_prob[", Cat3Prob[:]},
		{"vp9_cat4_prob[", Cat4Prob[:]},
		{"vp9_cat5_prob[", Cat5Prob[:]},
		{"vp9_cat6_prob[", Cat6Prob[:]},
		{"vp9_coefband_trans_4x4[", CoefbandTrans4x4[:]},
		{"vp9_coefband_trans_8x8plus[", CoefbandTrans8x8Plus[:]},
	}
	for _, tc := range flat {
		want := extractBracedArray(src, tc.marker[:len(tc.marker)-1])
		if want == nil {
			t.Errorf("%s: marker not found in libvpx source", tc.marker)
			continue
		}
		if len(want) != len(tc.got) {
			t.Errorf("%s: got %d entries, want %d", tc.marker, len(tc.got), len(want))
			continue
		}
		for i := range tc.got {
			if int(tc.got[i]) != want[i] {
				t.Errorf("%s[%d] = %d, libvpx says %d", tc.marker, i, tc.got[i], want[i])
				break
			}
		}
	}

	// Pareto8Full: 255 × 8.
	wantP := extractBracedArray(src, "vp9_pareto8_full")
	if wantP == nil {
		t.Fatal("vp9_pareto8_full marker not found in libvpx source")
	}
	if len(wantP) != 255*8 {
		t.Fatalf("vp9_pareto8_full: got %d values from source, want 2040", len(wantP))
	}
	for r := 0; r < 255; r++ {
		for c := 0; c < 8; c++ {
			if int(Pareto8Full[r][c]) != wantP[r*8+c] {
				t.Fatalf("Pareto8Full[%d][%d] = %d, libvpx says %d",
					r, c, Pareto8Full[r][c], wantP[r*8+c])
			}
		}
	}
}

// findLibvpxSourceVP9 walks up from this test file to find the pinned
// VP9-enabled libvpx checkout under internal/coracle/build. Falls back
// to the shared VP8 checkout if the VP9 variant isn't built.
func findLibvpxSourceVP9(rel string) string {
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for _, root := range []string{"libvpx-v1.16.0-vp9", "libvpx-v1.16.0"} {
		d := dir
		for {
			path := filepath.Join(d, "internal", "coracle", "build", root, rel)
			if _, err := os.Stat(path); err == nil {
				return path
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return ""
}
