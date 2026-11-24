package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 tile-level SB walker. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — write_modes. Iterates the
// super-block grid of one tile in row-major order, resets the
// left-segmentation context at the start of each SB row (mirrors
// the vp9_zero(xd->left_seg_context) libvpx does inline), and
// dispatches to WriteModesSb at Block64x64 for each SB.
//
// The tile boundaries are (mi_row_start, mi_row_end, mi_col_start,
// mi_col_end) — the (mi_col_start, mi_col_end) pair clips the SB
// walk horizontally while WriteModesSb's frame-extent gate
// (a.MiRows / a.MiCols) clips at the frame's right/bottom edge.

// WriteModesTileArgs bundles the inputs WriteModesTile consults.
// The fields the per-SB walker reads are the same WriteModesSbArgs
// fields, plus the per-tile bounds.
type WriteModesTileArgs struct {
	WriteModesSbArgs

	// Tile bounds in mi units. The walk steps MiBlockSize across both
	// rows and columns; libvpx uses the same step for every SB grid
	// cell.
	MiRowStart int
	MiRowEnd   int
	MiColStart int
	MiColEnd   int
}

// WriteModesTile mirrors libvpx's write_modes. Walks all SB cells in
// (mi_row_start..mi_row_end, mi_col_start..mi_col_end) and dispatches
// to WriteModesSb at Block64x64 for each cell. The left-segmentation
// context array is zeroed at the start of each SB row to match
// libvpx's per-row vp9_zero of xd->left_seg_context.
func WriteModesTile(bw *bitstream.Writer, a WriteModesTileArgs) {
	for miRow := a.MiRowStart; miRow < a.MiRowEnd; miRow += common.MiBlockSize {
		for i := range a.LeftSegCtx {
			a.LeftSegCtx[i] = 0
		}
		for miCol := a.MiColStart; miCol < a.MiColEnd; miCol += common.MiBlockSize {
			WriteModesSb(bw, a.WriteModesSbArgs, miRow, miCol, common.Block64x64)
		}
	}
}
