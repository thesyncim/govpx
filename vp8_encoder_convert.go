package govpx

import "github.com/thesyncim/govpx/internal/vpx/geometry"

func encoderMacroblockCount(width int, height int) int {
	return geometry.MacroblockCount(width, height)
}

func encoderMacroblockRows(height int) int {
	return geometry.MacroblockRows(height)
}

func encoderMacroblockCols(width int) int {
	return geometry.MacroblockCols(width)
}
