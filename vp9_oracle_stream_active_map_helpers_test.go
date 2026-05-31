//go:build govpx_oracle_trace

package govpx

func vp9OracleActiveMap(width int, height int, pattern string) ([]uint8, int, int) {
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			switch pattern {
			case "all":
				activeMap[idx] = 1
			case "checker":
				if (row+col)&1 == 0 {
					activeMap[idx] = 1
				}
			case "left-off":
				if col != 0 {
					activeMap[idx] = 1
				}
			case "right-off":
				if col != cols-1 {
					activeMap[idx] = 1
				}
			case "border-off":
				if row != 0 && col != 0 && row != rows-1 && col != cols-1 {
					activeMap[idx] = 1
				}
			default:
				panic("unknown VP9 active-map pattern")
			}
		}
	}
	return activeMap, rows, cols
}
