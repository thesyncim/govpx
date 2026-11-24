package geometry

func MacroblockRows(height int) int {
	return (height + 15) >> 4
}

func MacroblockCols(width int) int {
	return (width + 15) >> 4
}

func MacroblockCount(width int, height int) int {
	return MacroblockRows(height) * MacroblockCols(width)
}
