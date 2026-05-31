package govpx

func miColsForSize(v uint32) int {
	miCols := (v + 7) >> 3
	return int(miCols)
}
