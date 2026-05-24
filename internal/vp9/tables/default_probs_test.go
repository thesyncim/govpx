//go:build govpx_oracle_trace

package tables

import (
	"os"
	"strings"
	"testing"
)

// TestDefaultProbsMatchLibvpxSource validates the default-probability
// tables against vp9_entropymode.c byte-for-byte. Uses the existing
// extractBracedArray + extractScanArray helpers from oracle_test.go.
func TestDefaultProbsMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSource("vp9/common/vp9_entropymode.c")
	if srcPath == "" {
		t.Skip("libvpx checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	src := string(raw)

	flatten1D := func(t2 []int) []int { return t2 }
	flatten2DUint8 := func(rows [][]uint8) []int {
		out := make([]int, 0, len(rows)*len(rows[0]))
		for _, r := range rows {
			for _, v := range r {
				out = append(out, int(v))
			}
		}
		return out
	}
	flat1Du8 := func(t []uint8) []int {
		out := make([]int, len(t))
		for i, v := range t {
			out[i] = int(v)
		}
		return out
	}

	flatten3Du8 := func(v [10][10][9]uint8) []int {
		out := make([]int, 0, 10*10*9)
		for i := range v {
			for j := range v[i] {
				for k := range v[i][j] {
					out = append(out, int(v[i][j][k]))
				}
			}
		}
		return out
	}

	cases := []struct {
		marker string
		want   []int
	}{
		{"default_intra_inter_p[", flat1Du8(DefaultIntraInter[:])},
		{"default_comp_inter_p[", flat1Du8(DefaultCompInter[:])},
		{"default_comp_ref_p[", flat1Du8(DefaultCompRef[:])},
		{"default_single_ref_p[", flatten2DUint8([][]uint8{
			DefaultSingleRef[0][:], DefaultSingleRef[1][:],
			DefaultSingleRef[2][:], DefaultSingleRef[3][:], DefaultSingleRef[4][:],
		})},
		{"default_skip_probs[", flat1Du8(DefaultSkipProbs[:])},
		{"vp9_kf_y_mode_prob[", flatten3Du8(KfYModeProb)},
		{"vp9_kf_uv_mode_prob[", flatten2DUint8([][]uint8{
			KfUvModeProb[0][:], KfUvModeProb[1][:], KfUvModeProb[2][:],
			KfUvModeProb[3][:], KfUvModeProb[4][:], KfUvModeProb[5][:],
			KfUvModeProb[6][:], KfUvModeProb[7][:], KfUvModeProb[8][:],
			KfUvModeProb[9][:],
		})},
		{"vp9_kf_partition_probs[", flatten2DUint8([][]uint8{
			KfPartitionProbs[0][:], KfPartitionProbs[1][:], KfPartitionProbs[2][:],
			KfPartitionProbs[3][:], KfPartitionProbs[4][:], KfPartitionProbs[5][:],
			KfPartitionProbs[6][:], KfPartitionProbs[7][:], KfPartitionProbs[8][:],
			KfPartitionProbs[9][:], KfPartitionProbs[10][:], KfPartitionProbs[11][:],
			KfPartitionProbs[12][:], KfPartitionProbs[13][:], KfPartitionProbs[14][:],
			KfPartitionProbs[15][:],
		})},
	}
	_ = flatten1D

	for _, tc := range cases {
		// Strip trailing "[" so extractBracedArray composes correctly.
		marker := strings.TrimSuffix(tc.marker, "[")
		got := extractBracedArray(src, marker)
		if got == nil {
			t.Errorf("%s: marker not found", tc.marker)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %d entries, want %d", tc.marker, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s[%d] = %d, libvpx says %d", tc.marker, i, tc.want[i], got[i])
				break
			}
		}
	}
}
