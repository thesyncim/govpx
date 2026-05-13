package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// InterModeContext mirrors the get_mode_context helper in
// vp9/decoder/vp9_decodemv.c. The inter-mode tree only consults the
// nearest two MV-ref neighbor positions for entropy context; broader MV
// candidate search lands separately when non-zero motion vectors are
// reconstructed.
func InterModeContext(miGrid []NeighborMi, miCols int,
	tile TileBounds, miRows, miRow, miCol int, bsize common.BlockSize,
) int {
	contextCounter := 0
	search := tables.MvRefBlocks[bsize]
	for i := range 2 {
		pos := search[i]
		if !IsInside(tile, miRows, miRow, miCol, int(pos.Row), int(pos.Col)) {
			continue
		}
		r := miRow + int(pos.Row)
		c := miCol + int(pos.Col)
		if r < 0 || c < 0 || r >= miRows || c >= miCols {
			continue
		}
		mode := miGrid[r*miCols+c].Mode
		if int(mode) >= len(tables.Mode2Counter) {
			continue
		}
		contextCounter += int(tables.Mode2Counter[mode])
	}
	return int(tables.CounterToContext[contextCounter])
}
