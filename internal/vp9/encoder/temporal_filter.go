package encoder

import "image"

const (
	temporalFilterThreshLow  = 10000
	temporalFilterThreshHigh = 20000
	temporalFilterHexRange   = 127
	temporalFilterDiaRange   = 8
)

// TemporalFilterFrame is the planar frame view consumed by the VP9 temporal
// filter. Slices alias the caller-owned image buffers.
type TemporalFilterFrame struct {
	width   int
	height  int
	y       []byte
	u       []byte
	v       []byte
	yStride int
	uStride int
	vStride int
}

// TemporalFilterFrameFromYCbCr adapts an image.YCbCr to the VP9 temporal
// filter's planar view without copying.
func TemporalFilterFrameFromYCbCr(img *image.YCbCr) TemporalFilterFrame {
	return TemporalFilterFrame{
		width:   img.Rect.Dx(),
		height:  img.Rect.Dy(),
		y:       img.Y,
		u:       img.Cb,
		v:       img.Cr,
		yStride: img.YStride,
		uStride: img.CStride,
		vStride: img.CStride,
	}
}

// TemporalFilterWindow returns the backward and forward ARNR reference counts
// for the requested VP9 ARNR filter type.
func TemporalFilterWindow(distance int, lookaheadCount int, maxFrames int, filterType int) (int, int, bool) {
	if distance < 0 || lookaheadCount <= 0 || maxFrames <= 1 {
		return 0, 0, false
	}
	numFramesBackward := distance
	numFramesForward := lookaheadCount - (numFramesBackward + 1)
	if numFramesForward < 0 {
		return 0, 0, false
	}
	framesBackward := 0
	framesForward := 0
	switch filterType {
	case 1:
		framesBackward = numFramesBackward
		if framesBackward >= maxFrames {
			framesBackward = maxFrames - 1
		}
	case 2:
		framesForward = numFramesForward
		if framesForward >= maxFrames {
			framesForward = maxFrames - 1
		}
	case 3:
		// libvpx VP9 places the alt-ref at the end of the GF
		// group, so when the lookahead-driven driver picks the
		// newest queued frame as the alt-ref source we have no
		// forward refs available. The previous symmetric clamp
		// (forward = backward = min(forward,backward)) collapsed
		// both sides to 0 in that case, which silently disabled
		// the temporal filter pass. Match libvpx's
		// vp9_temporal_filter.c behavior: when one side is short,
		// use what is available on the other side capped to
		// maxFrames-1 so the filter still runs.
		framesForward = numFramesForward
		framesBackward = numFramesBackward
		if framesForward == 0 {
			if framesBackward > maxFrames-1 {
				framesBackward = maxFrames - 1
			}
			break
		}
		if framesBackward == 0 {
			if framesForward > maxFrames-1 {
				framesForward = maxFrames - 1
			}
			break
		}
		if framesForward > framesBackward {
			framesForward = framesBackward
		}
		if framesBackward > framesForward {
			framesBackward = framesForward
		}
		if framesForward > (maxFrames-1)/2 {
			framesForward = (maxFrames - 1) / 2
		}
		if framesBackward > maxFrames/2 {
			framesBackward = maxFrames / 2
		}
	default:
		return 0, 0, false
	}
	return framesBackward, framesForward, true
}

func gatherTemporalFilterBlock(dst []byte, dstStride int, src []byte, srcStride, srcX, srcY, srcW, srcH, size int) {
	for j := range size {
		yy := min(max(srcY+j, 0), srcH-1)
		row := src[yy*srcStride:]
		for i := range size {
			xx := min(max(srcX+i, 0), srcW-1)
			dst[j*dstStride+i] = row[xx]
		}
	}
}

var temporalFilterFixedDivide = func() [512]uint32 {
	var t [512]uint32
	for i := 1; i < 512; i++ {
		t[i] = 0x80000 / uint32(i)
	}
	return t
}()

func writeTemporalFilterBlock(dst []byte, dstStride, dstX, dstY, dstW, dstH, size int, accumulator []uint32, count []uint32) {
	n := size * size
	if len(count) < n || len(accumulator) < n {
		return
	}
	count = count[:n:n]
	accumulator = accumulator[:n:n]
	for j := range size {
		yy := dstY + j
		if uint(yy) >= uint(dstH) {
			continue
		}
		end := yy*dstStride + dstW
		if end > len(dst) {
			continue
		}
		row := dst[yy*dstStride : end : end]
		for i := range size {
			xx := dstX + i
			if uint(xx) >= uint(dstW) {
				continue
			}
			k := j*size + i
			c := count[k]
			if c == 0 {
				continue
			}
			if c >= uint32(len(temporalFilterFixedDivide)) {
				row[xx] = byte(min((accumulator[k]+c/2)/c, 255))
				continue
			}
			pval := min((accumulator[k]+c>>1)*temporalFilterFixedDivide[c]>>19, 255)
			row[xx] = byte(pval)
		}
	}
}

func clampTemporalFilterMV(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func temporalFilterInBounds(col, row, colMin, colMax, rowMin, rowMax int) bool {
	return col >= colMin && col <= colMax && row >= rowMin && row <= rowMax
}

// IterateTemporalFilter applies the VP9 32x32 ARNR temporal filter to dst.
func IterateTemporalFilter(dst *TemporalFilterFrame, refs []TemporalFilterFrame, centerIdx int, strength int) {
	if uint(centerIdx) >= uint(len(refs)) {
		return
	}
	if dst == nil {
		return
	}

	blockCols := (dst.width + 31) >> 5
	blockRows := (dst.height + 31) >> 5
	if blockCols|blockRows == 0 {
		return
	}

	var accumulator [1536]uint32
	var count [1536]uint32
	for blockRow := range blockRows {
		blockY := blockRow << 5
		for blockCol := range blockCols {
			blockX := blockCol << 5
			processVP9ARNRBlock32(dst, refs, centerIdx, blockRow,
				blockCol, blockRows, blockCols, blockX, blockY, strength,
				accumulator[:], count[:])
		}
	}
}

func processVP9ARNRBlock32(dst *TemporalFilterFrame, refs []TemporalFilterFrame, centerIdx int, blockRow int, blockCol int, blockRows int, blockCols int, blockX, blockY, strength int, accumulator []uint32, count []uint32) {
	accumulator = accumulator[:1536:1536]
	count = count[:1536:1536]
	for i := range accumulator {
		accumulator[i] = 0
		count[i] = 0
	}

	var srcY [1024]byte
	gatherTemporalFilterBlock(srcY[:], 32, dst.y, dst.yStride, blockX, blockY,
		dst.width, dst.height, 32)
	blockUVX := blockX >> 1
	blockUVY := blockY >> 1
	uvW := (dst.width + 1) >> 1
	uvH := (dst.height + 1) >> 1
	var srcU, srcV [256]byte
	gatherTemporalFilterBlock(srcU[:], 16, dst.u, dst.uStride, blockUVX, blockUVY, uvW, uvH, 16)
	gatherTemporalFilterBlock(srcV[:], 16, dst.v, dst.vStride, blockUVX, blockUVY, uvW, uvH, 16)
	bounds := vp9ARNRBlock32MVBounds(blockRow, blockCol, blockRows, blockCols)

	for fi, ref := range refs {
		var blkFW [4]int
		var blkMVs [4]vp9ARNRMV
		use32 := false
		refMV := vp9ARNRMV{}
		if fi == centerIdx {
			blkFW = [4]int{2, 2, 2, 2}
			use32 = true
		} else {
			err, blkErr, mv32, mvs16 := vp9ARNRFindMatchingBlock32(
				srcY[:], ref, blockX, blockY, bounds)
			refMV = mv32
			blkMVs = mvs16
			err16 := blkErr[0] + blkErr[1] + blkErr[2] + blkErr[3]
			minErr, maxErr := blkErr[0], blkErr[0]
			for k := 1; k < len(blkErr); k++ {
				minErr = min(minErr, blkErr[k])
				maxErr = max(maxErr, blkErr[k])
			}
			if ((err*15 < (err16 << 4)) && maxErr-minErr < 10000) ||
				((err*14 < (err16 << 4)) && maxErr-minErr < 5000) {
				use32 = true
				fw := vp9ARNRFilterWeight(err, temporalFilterThreshLow<<2,
					temporalFilterThreshHigh<<2)
				blkFW = [4]int{fw, fw, fw, fw}
			} else {
				for k := range blkFW {
					blkFW[k] = vp9ARNRFilterWeight(blkErr[k],
						temporalFilterThreshLow, temporalFilterThreshHigh)
				}
			}
			capWeight := 2
			switch vp9ARNRAbsInt(fi - centerIdx) {
			case 2, 3:
				capWeight = 1
			}
			for k := range blkFW {
				blkFW[k] = min(blkFW[k], capWeight)
			}
		}
		if blkFW[0]|blkFW[1]|blkFW[2]|blkFW[3] == 0 {
			continue
		}

		var predY [1024]byte
		var predU, predV [256]byte
		vp9ARNRBuildPredictor32(predY[:], predU[:], predV[:], ref,
			blockX, blockY, blockUVX, blockUVY, use32, refMV, blkMVs)
		applyVP9TemporalFilter32(srcY[:], predY[:], srcU[:], predU[:],
			srcV[:], predV[:], strength, blkFW, use32,
			accumulator[:1024], count[:1024],
			accumulator[1024:1280], count[1024:1280],
			accumulator[1280:1536], count[1280:1536])
	}

	writeTemporalFilterBlock(dst.y, dst.yStride, blockX, blockY, dst.width, dst.height, 32, accumulator[:1024], count[:1024])
	writeTemporalFilterBlock(dst.u, dst.uStride, blockUVX, blockUVY, uvW, uvH, 16, accumulator[1024:1280], count[1024:1280])
	writeTemporalFilterBlock(dst.v, dst.vStride, blockUVX, blockUVY, uvW, uvH, 16, accumulator[1280:1536], count[1280:1536])
}

type vp9ARNRMV struct {
	col int
	row int
}

type vp9ARNRMVBounds struct {
	colMin int
	colMax int
	rowMin int
	rowMax int
}

func vp9ARNRBlock32MVBounds(blockRow, blockCol, blockRows, blockCols int) vp9ARNRMVBounds {
	const border = 17 - 2*6
	return vp9ARNRMVBounds{
		colMin: -((blockCol << 5) + border),
		colMax: ((blockCols - 1 - blockCol) << 5) + border,
		rowMin: -((blockRow << 5) + border),
		rowMax: ((blockRows - 1 - blockRow) << 5) + border,
	}
}

func vp9ARNRFindMatchingBlock32(srcY []byte, ref TemporalFilterFrame, blockX, blockY int, bounds vp9ARNRMVBounds) (int, [4]int, vp9ARNRMV, [4]vp9ARNRMV) {
	_, fullX, fullY := vp9ARNRFindMatchingBlock(srcY, 32, ref,
		blockX, blockY, 32, bounds, 0, 0)
	err, mvX, mvY := vp9ARNRSubpelRefineBlock(srcY, 32, ref,
		blockX, blockY, 32, bounds, fullX, fullY)
	mv32 := vp9ARNRMV{col: mvX, row: mvY}

	var blkErr [4]int
	var blkMVs [4]vp9ARNRMV
	k := 0
	for yOff := 0; yOff < 32; yOff += 16 {
		for xOff := 0; xOff < 32; xOff += 16 {
			var sub [16 * 16]byte
			for y := range 16 {
				copy(sub[y*16:y*16+16],
					srcY[(yOff+y)*32+xOff:(yOff+y)*32+xOff+16])
			}
			_, subFullX, subFullY := vp9ARNRFindMatchingBlock(sub[:],
				16, ref, blockX+xOff, blockY+yOff, 16, bounds,
				mv32.col>>3, mv32.row>>3)
			subErr, subMVX, subMVY := vp9ARNRSubpelRefineBlock(sub[:],
				16, ref, blockX+xOff, blockY+yOff, 16, bounds,
				subFullX, subFullY)
			blkErr[k] = subErr
			blkMVs[k] = vp9ARNRMV{col: subMVX, row: subMVY}
			k++
		}
	}
	return err, blkErr, mv32, blkMVs
}

func vp9ARNRFilterWeight(err, low, high int) int {
	switch {
	case err < low:
		return 2
	case err < high:
		return 1
	default:
		return 0
	}
}

func vp9ARNRFindMatchingBlock(src []byte, srcStride int, ref TemporalFilterFrame, x, y, size int, bounds vp9ARNRMVBounds, seedX, seedY int) (int, int, int) {
	br := clampTemporalFilterMV(seedY, bounds.rowMin, bounds.rowMax)
	bc := clampTemporalFilterMV(seedX, bounds.colMin, bounds.colMax)
	hex := [6][2]int{
		{-1, -2}, {1, -2}, {2, 0}, {1, 2}, {-1, 2}, {-2, 0},
	}
	nextChkpts := [6][3][2]int{
		{{-2, 0}, {-1, -2}, {1, -2}},
		{{-1, -2}, {1, -2}, {2, 0}},
		{{1, -2}, {2, 0}, {1, 2}},
		{{2, 0}, {1, 2}, {-1, 2}},
		{{1, 2}, {-1, 2}, {-2, 0}},
		{{-1, 2}, {-2, 0}, {-1, -2}},
	}
	neighbors := [4][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}}
	bestSAD := vp9ARNRSADAt(src, srcStride, ref, x, y, size, bc, br)
	bestSite := -1
	for i, step := range hex {
		row := br + step[0]
		col := bc + step[1]
		if !temporalFilterInBounds(col, row, bounds.colMin, bounds.colMax,
			bounds.rowMin, bounds.rowMax) {
			continue
		}
		sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
		if sad < bestSAD {
			bestSAD = sad
			bestSite = i
		}
	}
	if bestSite >= 0 {
		br += hex[bestSite][0]
		bc += hex[bestSite][1]
		k := bestSite
		for j := 1; j < temporalFilterHexRange; j++ {
			bestSite = -1
			for i, step := range nextChkpts[k] {
				row := br + step[0]
				col := bc + step[1]
				if !temporalFilterInBounds(col, row, bounds.colMin, bounds.colMax,
					bounds.rowMin, bounds.rowMax) {
					continue
				}
				sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
				if sad < bestSAD {
					bestSAD = sad
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br += nextChkpts[k][bestSite][0]
			bc += nextChkpts[k][bestSite][1]
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}
	for range temporalFilterDiaRange {
		bestSite = -1
		for i, step := range neighbors {
			row := br + step[0]
			col := bc + step[1]
			if !temporalFilterInBounds(col, row, bounds.colMin, bounds.colMax,
				bounds.rowMin, bounds.rowMax) {
				continue
			}
			sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
			if sad < bestSAD {
				bestSAD = sad
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br += neighbors[bestSite][0]
		bc += neighbors[bestSite][1]
	}
	return bestSAD, bc, br
}

func vp9ARNRSubpelRefineBlock(src []byte, srcStride int, ref TemporalFilterFrame, x, y, size int, bounds vp9ARNRMVBounds, fullX, fullY int) (int, int, int) {
	minCol := bounds.colMin << 3
	maxCol := bounds.colMax << 3
	minRow := bounds.rowMin << 3
	maxRow := bounds.rowMax << 3
	bestRow := fullY << 3
	bestCol := fullX << 3
	bestSAD := vp9ARNRSADAtSubpel(src, srcStride, ref, x, y, size, bestCol, bestRow)
	steps := [3]int{4, 2, 1}
	for _, step := range steps {
		for range 4 {
			startRow := bestRow
			startCol := bestCol
			leftSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow, startCol-step, minRow, maxRow, minCol, maxCol)
			rightSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow, startCol+step, minRow, maxRow, minCol, maxCol)
			upSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow-step, startCol, minRow, maxRow, minCol, maxCol)
			downSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow+step, startCol, minRow, maxRow, minCol, maxCol)
			if leftSAD < bestSAD {
				bestSAD = leftSAD
				bestRow = startRow
				bestCol = startCol - step
			}
			if rightSAD < bestSAD {
				bestSAD = rightSAD
				bestRow = startRow
				bestCol = startCol + step
			}
			if upSAD < bestSAD {
				bestSAD = upSAD
				bestRow = startRow - step
				bestCol = startCol
			}
			if downSAD < bestSAD {
				bestSAD = downSAD
				bestRow = startRow + step
				bestCol = startCol
			}
			dr := -step
			dc := -step
			if downSAD < upSAD {
				dr = step
			}
			if rightSAD < leftSAD {
				dc = step
			}
			diagSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow+dr, startCol+dc, minRow, maxRow, minCol, maxCol)
			if diagSAD < bestSAD {
				bestSAD = diagSAD
				bestRow = startRow + dr
				bestCol = startCol + dc
			}
			if bestRow == startRow && bestCol == startCol {
				break
			}
		}
	}
	return bestSAD, bestCol, bestRow
}

func vp9ARNRSubpelProbe(src []byte, srcStride int, ref TemporalFilterFrame, x, y, size int, row, col, minRow, maxRow, minCol, maxCol int) int {
	if row < minRow || row > maxRow || col < minCol || col > maxCol {
		return 1<<30 - 1
	}
	return vp9ARNRSADAtSubpel(src, srcStride, ref, x, y, size, col, row)
}

func vp9ARNRSADAt(src []byte, srcStride int, ref TemporalFilterFrame, x, y, size, mvX, mvY int) int {
	var pred [1024]byte
	gatherTemporalFilterBlock(pred[:size*size], size, ref.y, ref.yStride, x+mvX, y+mvY,
		ref.width, ref.height, size)
	return vp9ARNRSAD(src, srcStride, pred[:], size, size)
}

func vp9ARNRSADAtSubpel(src []byte, srcStride int, ref TemporalFilterFrame, x, y, size, col, row int) int {
	if (row|col)&7 == 0 {
		return vp9ARNRSADAt(src, srcStride, ref, x, y, size, col>>3, row>>3)
	}
	var pred [1024]byte
	vp9ARNRPredictLuma(pred[:size*size], size, ref, x, y, col, row, size, size)
	return vp9ARNRSAD(src, srcStride, pred[:], size, size)
}

func vp9ARNRSAD(src []byte, srcStride int, pred []byte, predStride int, size int) int {
	sad := 0
	for y := range size {
		srcRow := src[y*srcStride:]
		predRow := pred[y*predStride:]
		for x := range size {
			d := int(srcRow[x]) - int(predRow[x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
	}
	return sad
}

func vp9ARNRBuildPredictor32(predY, predU, predV []byte, ref TemporalFilterFrame, blockX, blockY, blockUVX, blockUVY int, use32 bool, refMV vp9ARNRMV, blkMVs [4]vp9ARNRMV) {
	if use32 {
		vp9ARNRPredictLuma(predY, 32, ref, blockX, blockY, refMV.col,
			refMV.row, 32, 32)
		vp9ARNRPredictChroma(predU, 16, ref.u, ref.uStride,
			(ref.width+1)>>1, (ref.height+1)>>1, blockUVX, blockUVY,
			refMV.col, refMV.row, 16, 16)
		vp9ARNRPredictChroma(predV, 16, ref.v, ref.vStride,
			(ref.width+1)>>1, (ref.height+1)>>1, blockUVX, blockUVY,
			refMV.col, refMV.row, 16, 16)
		return
	}
	k := 0
	for yOff := 0; yOff < 32; yOff += 16 {
		for xOff := 0; xOff < 32; xOff += 16 {
			mv := blkMVs[k]
			var subY [256]byte
			vp9ARNRPredictLuma(subY[:], 16, ref, blockX+xOff,
				blockY+yOff, mv.col, mv.row, 16, 16)
			for y := range 16 {
				copy(predY[(yOff+y)*32+xOff:(yOff+y)*32+xOff+16],
					subY[y*16:y*16+16])
			}
			uvXOff := xOff >> 1
			uvYOff := yOff >> 1
			vp9ARNRPredictChroma(predU[uvYOff*16+uvXOff:], 16,
				ref.u, ref.uStride, (ref.width+1)>>1, (ref.height+1)>>1,
				blockUVX+uvXOff, blockUVY+uvYOff, mv.col, mv.row, 8, 8)
			vp9ARNRPredictChroma(predV[uvYOff*16+uvXOff:], 16,
				ref.v, ref.vStride, (ref.width+1)>>1, (ref.height+1)>>1,
				blockUVX+uvXOff, blockUVY+uvYOff, mv.col, mv.row, 8, 8)
			k++
		}
	}
}

func vp9ARNRPredictLuma(dst []byte, dstStride int, ref TemporalFilterFrame, x, y, mvColQ3, mvRowQ3, w, h int) {
	mvColQ4 := mvColQ3 << 1
	mvRowQ4 := mvRowQ3 << 1
	vp9ARNRPredict12(dst, dstStride, ref.y, ref.yStride, ref.width,
		ref.height, x, y, mvColQ4, mvRowQ4, w, h)
}

func vp9ARNRPredictChroma(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, x, y, mvColQ3, mvRowQ3, w, h int) {
	vp9ARNRPredict12(dst, dstStride, plane, planeStride, planeW, planeH,
		x, y, mvColQ3, mvRowQ3, w, h)
}

func vp9ARNRPredict12(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, x, y, mvColQ4, mvRowQ4, w, h int) {
	intCol := mvColQ4 >> 4
	intRow := mvRowQ4 >> 4
	fracCol := mvColQ4 & 15
	fracRow := mvRowQ4 & 15
	if (fracCol | fracRow) == 0 {
		gatherTemporalFilterBlock(dst, dstStride, plane, planeStride, x+intCol, y+intRow,
			planeW, planeH, w)
		return
	}
	const extend = 5
	gatherW := w + 11
	gatherH := h + 11
	var scratchBuf [43 * 43]byte
	scratch := scratchBuf[:gatherW*gatherH]
	gatherTemporalFilterBlock(scratch, gatherW, plane, planeStride, x+intCol-extend,
		y+intRow-extend, planeW, planeH, gatherW)
	var tempBuf [32 * 43]byte
	temp := tempBuf[:w*gatherH]
	xFilter := &vp9TemporalSubpelFilters12[fracCol]
	for yy := range gatherH {
		for xx := range w {
			sum := 0
			base := yy*gatherW + xx
			for k := range 12 {
				sum += int(scratch[base+k]) * int(xFilter[k])
			}
			temp[yy*w+xx] = vp9ClipPixel(vp9RoundPowerOfTwo(sum, 7))
		}
	}
	yFilter := &vp9TemporalSubpelFilters12[fracRow]
	for xx := range w {
		for yy := range h {
			sum := 0
			base := yy*w + xx
			for k := range 12 {
				sum += int(temp[base+k*w]) * int(yFilter[k])
			}
			dst[yy*dstStride+xx] = vp9ClipPixel(vp9RoundPowerOfTwo(sum, 7))
		}
	}
}

var vp9TemporalSubpelFilters12 = [16][12]int16{
	{0, 0, 0, 0, 0, 128, 0, 0, 0, 0, 0, 0},
	{0, 1, -2, 3, -7, 127, 8, -4, 2, -1, 1, 0},
	{-1, 2, -3, 6, -13, 124, 18, -8, 4, -2, 2, -1},
	{-1, 3, -4, 8, -18, 120, 28, -12, 7, -4, 2, -1},
	{-1, 3, -6, 10, -21, 115, 38, -15, 8, -5, 3, -1},
	{-2, 4, -6, 12, -24, 108, 49, -18, 10, -6, 3, -2},
	{-2, 4, -7, 13, -25, 100, 60, -21, 11, -7, 4, -2},
	{-2, 4, -7, 13, -26, 91, 71, -24, 13, -7, 4, -2},
	{-2, 4, -7, 13, -25, 81, 81, -25, 13, -7, 4, -2},
	{-2, 4, -7, 13, -24, 71, 91, -26, 13, -7, 4, -2},
	{-2, 4, -7, 11, -21, 60, 100, -25, 13, -7, 4, -2},
	{-2, 3, -6, 10, -18, 49, 108, -24, 12, -6, 4, -2},
	{-1, 3, -5, 8, -15, 38, 115, -21, 10, -6, 3, -1},
	{-1, 2, -4, 7, -12, 28, 120, -18, 8, -4, 3, -1},
	{-1, 2, -2, 4, -8, 18, 124, -13, 6, -3, 2, -1},
	{0, 1, -1, 2, -4, 8, 127, -7, 3, -2, 1, 0},
}

var vp9TemporalFilterIndexMult = [...]uint32{
	0, 0, 0, 0, 49152, 39322, 32768, 28087, 24576, 21846,
	19661, 17874, 0, 15124,
}

func applyVP9TemporalFilter32(srcY, predY, srcU, predU, srcV, predV []byte,
	strength int, blkFW [4]int, use32 bool,
	yAccumulator, yCount, uAccumulator, uCount, vAccumulator, vCount []uint32,
) {
	if blkFW[0]|blkFW[1]|blkFW[2]|blkFW[3] == 0 {
		return
	}
	if strength < 0 {
		strength = 0
	}
	if strength > 6 {
		strength = 6
	}
	rounding := (1 << uint(strength)) >> 1
	var yDiff [1024]int
	var uDiff, vDiff [256]int
	for y := range 32 {
		for x := range 32 {
			diff := int(srcY[y*32+x]) - int(predY[y*32+x])
			yDiff[y*32+x] = diff * diff
		}
	}
	for y := range 16 {
		for x := range 16 {
			u := int(srcU[y*16+x]) - int(predU[y*16+x])
			v := int(srcV[y*16+x]) - int(predV[y*16+x])
			uDiff[y*16+x] = u * u
			vDiff[y*16+x] = v * v
		}
	}
	for y := range 32 {
		for x := range 32 {
			sum := 0
			used := 0
			for dy := -1; dy <= 1; dy++ {
				yy := y + dy
				if yy < 0 || yy >= 32 {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					xx := x + dx
					if xx < 0 || xx >= 32 {
						continue
					}
					sum += yDiff[yy*32+xx]
					used++
				}
			}
			uvIdx := (y>>1)*16 + (x >> 1)
			sum += uDiff[uvIdx] + vDiff[uvIdx]
			used += 2
			filterWeight := vp9ARNRBlockFilterWeight(y, x, 32, 32,
				blkFW, use32)
			modifier := vp9TemporalFilterModIndex(sum, used, rounding,
				strength, filterWeight)
			k := y*32 + x
			yCount[k] += uint32(modifier)
			yAccumulator[k] += uint32(modifier) * uint32(predY[k])
		}
	}
	for uvY := range 16 {
		for uvX := range 16 {
			uSum, vSum := 0, 0
			used := 0
			for dy := -1; dy <= 1; dy++ {
				yy := uvY + dy
				if yy < 0 || yy >= 16 {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					xx := uvX + dx
					if xx < 0 || xx >= 16 {
						continue
					}
					idx := yy*16 + xx
					uSum += uDiff[idx]
					vSum += vDiff[idx]
					used++
				}
			}
			ySum := 0
			for yy := uvY << 1; yy < (uvY<<1)+2; yy++ {
				for xx := uvX << 1; xx < (uvX<<1)+2; xx++ {
					ySum += yDiff[yy*32+xx]
					used++
				}
			}
			uSum += ySum
			vSum += ySum
			filterWeight := vp9ARNRBlockFilterWeight(uvY, uvX, 16, 16,
				blkFW, use32)
			uMod := vp9TemporalFilterModIndex(uSum, used, rounding,
				strength, filterWeight)
			vMod := vp9TemporalFilterModIndex(vSum, used, rounding,
				strength, filterWeight)
			uv := uvY*16 + uvX
			uCount[uv] += uint32(uMod)
			uAccumulator[uv] += uint32(uMod) * uint32(predU[uv])
			vCount[uv] += uint32(vMod)
			vAccumulator[uv] += uint32(vMod) * uint32(predV[uv])
		}
	}
}

func vp9ARNRBlockFilterWeight(y, x, h, w int, blkFW [4]int, use32 bool) int {
	if use32 {
		return blkFW[0]
	}
	if y < h/2 {
		if x < w/2 {
			return blkFW[0]
		}
		return blkFW[1]
	}
	if x < w/2 {
		return blkFW[2]
	}
	return blkFW[3]
}

func vp9ARNRAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func vp9ClipPixel(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func vp9TemporalFilterModIndex(sumDist, index, rounding, strength, filterWeight int) int {
	if index < 0 || index >= len(vp9TemporalFilterIndexMult) ||
		vp9TemporalFilterIndexMult[index] == 0 {
		return 0
	}
	if sumDist < 0 {
		sumDist = 0
	}
	if sumDist > 0xffff {
		sumDist = 0xffff
	}
	modifier := (uint32(sumDist) * vp9TemporalFilterIndexMult[index]) >> 16
	mod := min((int(modifier)+rounding)>>uint(strength), 16)
	return (16 - mod) * filterWeight
}
