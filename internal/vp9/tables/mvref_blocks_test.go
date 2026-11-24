//go:build govpx_oracle_trace

package tables

import (
	"os"
	"testing"
)

// TestMvRefBlocksMatchLibvpxSource validates the generated
// MvRefBlocks against libvpx v1.16.0 vp9/common/vp9_mvref_common.h.
// The table is 13 (BLOCK_SIZES) × 8 (MVREF_NEIGHBOURS) × 2 (row,col).
func TestMvRefBlocksMatchLibvpxSource(t *testing.T) {
	srcPath := findLibvpxSourceVP9("vp9/common/vp9_mvref_common.h")
	if srcPath == "" {
		t.Skip("libvpx VP9 checkout not present under internal/coracle/build")
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read libvpx source: %v", err)
	}
	want := extractBracedArray(string(raw), "mv_ref_blocks")
	if want == nil {
		t.Fatal("mv_ref_blocks marker not found in libvpx source")
	}
	const rows, cols = 13, 8
	if len(want) != rows*cols*2 {
		t.Fatalf("got %d ints from source, want %d", len(want), rows*cols*2)
	}
	idx := 0
	for r := range rows {
		for c := range cols {
			if int(MvRefBlocks[r][c].Row) != want[idx] ||
				int(MvRefBlocks[r][c].Col) != want[idx+1] {
				t.Fatalf("MvRefBlocks[%d][%d] = (%d,%d), libvpx says (%d,%d)",
					r, c, MvRefBlocks[r][c].Row, MvRefBlocks[r][c].Col,
					want[idx], want[idx+1])
			}
			idx += 2
		}
	}
}
