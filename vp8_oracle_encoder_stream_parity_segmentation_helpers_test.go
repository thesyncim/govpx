//go:build govpx_oracle_trace

package govpx

func customQuadrantROIMap(width, height int) *ROIMap {
	roi := quadrantROIMap(width, height)
	roi.DeltaQuantizer = [4]int{0, -10, 8, -20}
	roi.DeltaLoopFilter = [4]int{0, -3, 2, 5}
	roi.StaticThreshold = [4]int{0, 500, 0, 1200}
	return roi
}

func simpleCheckerROIMap(width, height int) *ROIMap {
	roi := roiMapPattern(width, height, "checker")
	roi.DeltaQuantizer = [4]int{0, -10, 0, 0}
	roi.DeltaLoopFilter = [4]int{}
	roi.StaticThreshold = [4]int{}
	return roi
}
