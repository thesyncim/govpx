package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func subpelVariance16x16Horizontal(src []byte, ss, x int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[0])
	return sum, int(sse), true
}

func subpelVariance16x16Vertical(src []byte, ss, y int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[0], tables.BilinearFilters[y])
	return sum, int(sse), true
}

func subpelVariance16x16Bilinear(src []byte, ss, x, y int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[y])
	return sum, int(sse), true
}

func subpelVariance16x16Fast(src []byte, ss, x, y int, ref []byte, rs int) (int, int) {
	if !pixel.Enabled {
		return SubpelVariance16x16(src, ss, x, y, ref, rs)
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[y])
	return int(sse) - sum*sum/256, int(sse)
}
