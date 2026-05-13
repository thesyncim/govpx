package tables

import (
	"os"
	"strings"
	"testing"
)

// TestDefaultCoefProbsMatchLibvpxSource extracts each
// default_coef_probs_{4x4,8x8,16x16,32x32} from libvpx
// vp9/common/vp9_entropy.c and validates the rectangular Go storage
// matches it byte-for-byte after re-applying the band-0 zero pad.
func TestDefaultCoefProbsMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vp9/common/vp9_entropy.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	tables := []struct {
		marker string
		got    [2][2][6][6][3]uint8
	}{
		{"default_coef_probs_4x4", DefaultCoefProbs4x4},
		{"default_coef_probs_8x8", DefaultCoefProbs8x8},
		{"default_coef_probs_16x16", DefaultCoefProbs16x16},
		{"default_coef_probs_32x32", DefaultCoefProbs32x32},
	}

	for _, tc := range tables {
		marker := strings.TrimSuffix(tc.marker, "[")
		want := extractBracedArray(src, marker)
		if want == nil {
			t.Errorf("%s: marker not found in libvpx source", tc.marker)
			continue
		}
		// libvpx's sparse init: 2*2*(3 + 5*6)*3 = 396 entries.
		const expected = 396
		if len(want) != expected {
			t.Errorf("%s: got %d entries, want %d", tc.marker, len(want), expected)
			continue
		}

		// Walk the Go table in the same order and check the non-padded
		// slots match libvpx's flat init list. Band 0 contexts 0..2
		// must equal want[0..8]; bands 1..5 contexts 0..5 must follow.
		idx := 0
		mismatch := 0
		for p := range 2 {
			for r := range 2 {
				for b := range 6 {
					contexts := 6
					if b == 0 {
						contexts = 3
					}
					for c := 0; c < contexts; c++ {
						for n := range 3 {
							if int(tc.got[p][r][b][c][n]) != want[idx] {
								if mismatch < 3 {
									t.Errorf("%s: [%d][%d][%d][%d][%d] = %d, want %d",
										tc.marker, p, r, b, c, n,
										tc.got[p][r][b][c][n], want[idx])
								}
								mismatch++
							}
							idx++
						}
					}
				}
			}
		}
		if mismatch > 0 {
			t.Errorf("%s: %d total mismatches", tc.marker, mismatch)
		}

		// Confirm the padded slots (band 0, contexts 3..5) are zero.
		for p := range 2 {
			for r := range 2 {
				for c := 3; c < 6; c++ {
					for n := range 3 {
						if tc.got[p][r][0][c][n] != 0 {
							t.Errorf("%s: band-0 padding slot [%d][%d][0][%d][%d] = %d, want 0",
								tc.marker, p, r, c, n, tc.got[p][r][0][c][n])
						}
					}
				}
			}
		}
	}
}
