package govpx

import "github.com/thesyncim/govpx/internal/vpx/geometry"

func encoderMacroblockRows(height int) int {
	return geometry.MacroblockRows(height)
}

func encoderMacroblockCols(width int) int {
	return geometry.MacroblockCols(width)
}
