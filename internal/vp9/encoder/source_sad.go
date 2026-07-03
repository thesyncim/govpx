package encoder

import vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"

// SourceSADSceneSamplesResult reports the sampled 64x64 source-SAD summary
// used by one-pass scene detection.
type SourceSADSceneSamplesResult struct {
	AverageSAD uint64
	ZeroTemp   int
	Samples    int
}

// SourceSADSceneSamplesArgs carries frame buffers and dimensions for
// SourceSADSceneSamples.
type SourceSADSceneSamplesArgs struct {
	SourceY           []byte
	SourceYStride     int
	LastSourceY       []byte
	LastSourceYStride int
	Width             int
	Height            int
	MIRows            int
	MICols            int
}

// SourceSADSceneSamples ports the sampled 64x64 source-SAD scan used by
// libvpx's one-pass high_source_sad detection.
func SourceSADSceneSamples(args SourceSADSceneSamplesArgs) (SourceSADSceneSamplesResult, bool) {
	if args.Width <= 0 || args.Height <= 0 || args.MIRows <= 0 || args.MICols <= 0 {
		return SourceSADSceneSamplesResult{}, false
	}
	sbCols := (args.MICols + 7) >> 3
	sbRows := (args.MIRows + 7) >> 3
	var avgSAD uint64
	var zeroTemp int
	var samples int
	for sbiRow := range sbRows {
		for sbiCol := range sbCols {
			if !((sbiRow > 0 && sbiCol > 0) &&
				(sbiRow < sbRows-1 && sbiCol < sbCols-1) &&
				((sbiRow%2 == 0 && sbiCol%2 == 0) ||
					(sbiRow%2 != 0 && sbiCol%2 != 0))) {
				continue
			}
			x := sbiCol * 64
			y := sbiRow * 64
			if x+64 > args.Width || y+64 > args.Height {
				continue
			}
			srcOff := y*args.SourceYStride + x
			refOff := y*args.LastSourceYStride + x
			sad := BlockSADOffsets(args.SourceY, srcOff, args.SourceYStride,
				args.LastSourceY, refOff, args.LastSourceYStride,
				64, 64, ^uint64(0))
			avgSAD += sad
			samples++
			if sad == 0 {
				zeroTemp++
			}
		}
	}
	if samples <= 0 {
		return SourceSADSceneSamplesResult{}, false
	}
	return SourceSADSceneSamplesResult{
		AverageSAD: avgSAD / uint64(samples),
		ZeroTemp:   zeroTemp,
		Samples:    samples,
	}, true
}

// AvgSourceSADResult is the per-SB content classification computed by
// libvpx's avg_source_sad.
type AvgSourceSADResult struct {
	ContentState           ContentStateSB
	ZeroTempSADSource      bool
	LowSADForContentState  bool
	SourceSAD              uint64
	SourceVariance         uint64
	SourceReferenceSumDiff uint64
}

// AvgSourceSADArgs carries frame buffers and encoder policy inputs for
// AvgSourceSAD.
type AvgSourceSADArgs struct {
	SourceY           []byte
	SourceYStride     int
	LastSourceY       []byte
	LastSourceYStride int
	Width             int
	Height            int
	MIRow             int
	MICol             int
	ScreenContent     bool
	CBR               bool
}

// AvgSourceSAD ports libvpx avg_source_sad (vp9_encodeframe.c:1201-1248)
// for the 64x64 superblock rooted at (MIRow, MICol).
func AvgSourceSAD(args AvgSourceSADArgs) (AvgSourceSADResult, bool) {
	sbMIRow := args.MIRow &^ 7
	sbMICol := args.MICol &^ 7
	x0 := sbMICol * 8
	y0 := sbMIRow * 8
	if x0 < 0 || y0 < 0 || x0 >= args.Width || y0 >= args.Height ||
		!avgSourceSADPlaneOK(args.SourceY, args.SourceYStride, args.Width, args.Height) ||
		!avgSourceSADPlaneOK(args.LastSourceY, args.LastSourceYStride, args.Width, args.Height) {
		return AvgSourceSADResult{}, false
	}

	tmpSAD, tmpVariance, tmpSSE := avgSourceSAD64(args, x0, y0)
	sumdiffSquare := tmpSSE - tmpVariance

	const avgSourceSADThreshold uint64 = 10000
	const avgSourceSADThreshold2 uint64 = 12000

	contentState := ContentStateHighSadHighSumdiff
	if tmpSAD < avgSourceSADThreshold {
		if sumdiffSquare < 25 {
			contentState = ContentStateLowSadLowSumdiff
		} else {
			contentState = ContentStateLowSadHighSumdiff
		}
	} else if sumdiffSquare < 25 {
		contentState = ContentStateHighSadLowSumdiff
	}

	if !args.ScreenContent && args.CBR && tmpVariance < (tmpSSE>>3) &&
		sumdiffSquare > 10000 {
		contentState = ContentStateLowVarHighSumdiff
	} else if tmpSAD > (avgSourceSADThreshold << 1) {
		contentState = ContentStateVeryHighSad
	}

	return AvgSourceSADResult{
		ContentState:           contentState,
		ZeroTempSADSource:      tmpSAD == 0,
		LowSADForContentState:  tmpSAD < avgSourceSADThreshold2,
		SourceSAD:              tmpSAD,
		SourceVariance:         tmpVariance,
		SourceReferenceSumDiff: sumdiffSquare,
	}, true
}

func avgSourceSADPlaneOK(plane []byte, stride, width, height int) bool {
	return width > 0 && height > 0 && stride >= width &&
		len(plane) >= (height-1)*stride+width
}

func avgSourceSAD64(args AvgSourceSADArgs, x0, y0 int) (sad, variance, sse uint64) {
	if x0+64 <= args.Width && y0+64 <= args.Height {
		srcOff := y0*args.SourceYStride + x0
		refOff := y0*args.LastSourceYStride + x0
		sad = BlockSADOffsets(args.SourceY, srcOff, args.SourceYStride,
			args.LastSourceY, refOff, args.LastSourceYStride, 64, 64, ^uint64(0))
		var sse32 uint32
		variance = uint64(vp9dsp.VpxVariance64x64(args.SourceY, srcOff,
			args.SourceYStride, args.LastSourceY, refOff, args.LastSourceYStride,
			&sse32))
		sse = uint64(sse32)
		return sad, variance, sse
	}

	visibleRows := args.Height - y0
	if visibleRows > 64 {
		visibleRows = 64
	}
	visibleCols := args.Width - x0
	if visibleCols > 64 {
		visibleCols = 64
	}
	rightRepeat := 64 - visibleCols
	var sum int64
	for y := range visibleRows {
		rowSad, rowSum, rowSSE := avgSourceSAD64ClampedRow(args, x0, y0+y,
			visibleCols, rightRepeat)
		sad += rowSad
		sum += rowSum
		sse += rowSSE
	}
	if bottomRepeat := 64 - visibleRows; bottomRepeat > 0 {
		rowSad, rowSum, rowSSE := avgSourceSAD64ClampedRow(args, x0,
			y0+visibleRows-1, visibleCols, rightRepeat)
		sad += rowSad * uint64(bottomRepeat)
		sum += rowSum * int64(bottomRepeat)
		sse += rowSSE * uint64(bottomRepeat)
	}
	meanSquares := uint64((sum * sum) >> 12)
	if sse > meanSquares {
		variance = sse - meanSquares
	}
	return sad, variance, sse
}

func avgSourceSAD64ClampedRow(args AvgSourceSADArgs, x0, y, cols, rightRepeat int) (sad uint64, sum int64, sse uint64) {
	srcOff := y*args.SourceYStride + x0
	refOff := y*args.LastSourceYStride + x0
	lastDiff := 0
	for x := range cols {
		diff := int(args.SourceY[srcOff+x]) - int(args.LastSourceY[refOff+x])
		lastDiff = diff
		if diff < 0 {
			sad += uint64(-diff)
		} else {
			sad += uint64(diff)
		}
		sum += int64(diff)
		sse += uint64(diff * diff)
	}
	if rightRepeat > 0 {
		if lastDiff < 0 {
			sad += uint64(-lastDiff) * uint64(rightRepeat)
		} else {
			sad += uint64(lastDiff) * uint64(rightRepeat)
		}
		sum += int64(lastDiff) * int64(rightRepeat)
		sse += uint64(lastDiff*lastDiff) * uint64(rightRepeat)
	}
	return sad, sum, sse
}
