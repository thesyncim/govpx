package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func vp9MiGridAtForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillVP9MiGridForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before tile parsing or
