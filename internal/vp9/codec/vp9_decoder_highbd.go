package codec

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// High-bit-depth (10-bit / 12-bit) VP9 decoder reconstruction & loop filter.
// Supports Profile 2 (10/12-bit 4:2:0) and is subsampling-parameterized for
// future Profile 3 reuse. Leaves 8-bit Profile 0/1 hot paths completely untouched.

func (d *VP9Decoder) vp9OutputPlane16(plane int) ([]uint16, int) {
	hb := d.highbd
	if hb == nil {
		return nil, 0
	}
	switch plane {
	case 0:
		return hb.frameY16, d.lastFrame.YStride
	case 1:
		return hb.frameU16, d.lastFrame.UStride
	case 2:
		return hb.frameV16, d.lastFrame.VStride
	default:
		return nil, 0
	}
}

func vp9ReferencePlane16(ref *vp9ReferenceFrame, plane int) ([]uint16, int) {
	if ref == nil || !ref.valid {
		return nil, 0
	}
	switch plane {
	case 0:
		return ref.img.Y16, ref.img.YStride
	case 1:
		return ref.img.U16, ref.img.UStride
	case 2:
		return ref.img.V16, ref.img.VStride
	default:
		return nil, 0
	}
}

func (d *VP9Decoder) vp9EnsureDQCoeff32() []int32 {
	if d.highbd == nil {
		d.highbd = &vp9DecoderHighBDState{}
	}
	if len(d.highbd.dqcoeff32) < 1024 {
		d.highbd.dqcoeff32 = make([]int32, 1024)
	}
	return d.highbd.dqcoeff32
}

func (d *VP9Decoder) reconstructVP9IntraPredictBlockHighBD(
	hdr *vp9dec.UncompressedHeader,
	mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode,
	tile vp9dec.TileBounds,
	miRow, miCol int,
	bsize common.BlockSize,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			d.markVP9Unsupported()
			return
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		miRows := int((hdr.Height + 7) >> 3)
		miCols := int((hdr.Width + 7) >> 3)
		max4x4W, max4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((num4x4W - max4x4W) >> txSize) * blockStep
		blockIdx := 0
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				if _, _, ok := d.reconstructVP9IntraPredictTxHighBD(hdr, pd, plane, mode, txSize,
					tile, miRow, miCol, bsize, rr, cc); !ok {
					d.markVP9Unsupported()
					return
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
}

func (d *VP9Decoder) reconstructVP9IntraPredictTxHighBD(
	hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane,
	plane int,
	mode common.PredictionMode,
	txSize common.TxSize,
	tile vp9dec.TileBounds,
	miRow, miCol int,
	bsize common.BlockSize,
	blockRow4x4, blockCol4x4 int,
) (dst []uint16, stride int, ok bool) {
	planeData, stride := d.vp9OutputPlane16(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := buffers.Align(int(hdr.Width), 8)
	alignedHeight := buffers.Align(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, false
	}

	bd := int(hdr.BitDepthColor.BitDepth)
	baseVal := uint16(128 << uint(bd-8))
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	upAvailable := blockRow4x4 != 0 || miRow > 0
	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W

	var left [32]uint16
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	} else {
		for i := range bs {
			left[i] = baseVal + 1
		}
	}

	// aboveFull[0] = top-left (above[-1]); aboveFull[1..2*bs] = above[0..2*bs-1].
	var aboveFull [1 + 64]uint16
	if upAvailable {
		aboveRowOff := (y0 - 1) * stride
		// Copy above[0..bs-1] with right-edge clamping.
		if bounds.MbToRightEdge < 0 {
			if x0+bs <= planeWidth {
				copy(aboveFull[1:1+bs], planeData[aboveRowOff+x0:aboveRowOff+x0+bs])
			} else if x0 < planeWidth {
				r := planeWidth - x0
				copy(aboveFull[1:1+r], planeData[aboveRowOff+x0:aboveRowOff+x0+r])
				fill := aboveFull[r]
				for i := r; i < bs; i++ {
					aboveFull[1+i] = fill
				}
			}
			// Above-right for 4x4 (and replicated above[bs-1] for >= 8x8).
			if bs == 4 && rightAvailable {
				if x0+2*bs <= planeWidth {
					copy(aboveFull[1+bs:1+2*bs], planeData[aboveRowOff+x0+bs:aboveRowOff+x0+2*bs])
				} else if x0+bs < planeWidth {
					r := planeWidth - (x0 + bs)
					copy(aboveFull[1+bs:1+bs+r], planeData[aboveRowOff+x0+bs:aboveRowOff+x0+bs+r])
					fill := aboveFull[bs+r]
					for i := r; i < bs; i++ {
						aboveFull[1+bs+i] = fill
					}
				} else {
					fill := aboveFull[bs]
					for i := range bs {
						aboveFull[1+bs+i] = fill
					}
				}
			} else {
				fill := aboveFull[bs]
				for i := range bs {
					aboveFull[1+bs+i] = fill
				}
			}
		} else {
			copy(aboveFull[1:1+bs], planeData[aboveRowOff+x0:aboveRowOff+x0+bs])
			if bs == 4 && rightAvailable {
				copy(aboveFull[1+bs:1+2*bs], planeData[aboveRowOff+x0+bs:aboveRowOff+x0+2*bs])
			} else {
				fill := aboveFull[bs]
				for i := range bs {
					aboveFull[1+bs+i] = fill
				}
			}
		}
		if leftAvailable {
			aboveFull[0] = planeData[aboveRowOff+x0-1]
		} else {
			aboveFull[0] = baseVal + 1
		}
	} else {
		for i := range 1 + 2*bs {
			aboveFull[i] = baseVal - 1
		}
	}

	dst = planeData[y0*stride+x0:]
	dsp.HighbdIntraPredict(dst, 0, stride, bs, mode, aboveFull[:], left[:bs],
		upAvailable, leftAvailable, bd)
	return dst, stride, true
}

func (d *VP9Decoder) reconstructVP9InterPredictBlockHighBD(
	hdr *vp9dec.UncompressedHeader,
	mi *vp9dec.NeighborMi,
	miRow, miCol int,
	bsize common.BlockSize,
) bool {
	if d.unsupportedReconstruct {
		return true
	}
	if !vp9dec.CanReconstructInterBlock(mi) {
		d.markVP9Unsupported()
		return true
	}
	nrefs := 1
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		nrefs = 2
	}
	bd := int(hdr.BitDepthColor.BitDepth)
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			d.markVP9Unsupported()
			return true
		}
		dst, dstStride := d.vp9OutputPlane16(plane)
		if dstStride <= 0 || len(dst) == 0 {
			return false
		}
		dstRows := len(dst) / dstStride
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		if x0 >= dstStride || y0 >= dstRows {
			continue
		}
		bw := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		bh := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		if x0+bw > dstStride || y0+bh > dstRows {
			d.markVP9Unsupported()
			return true
		}
		for refIdx := range nrefs {
			refSlot, ok := vp9dec.InterReferenceSlot(hdr, mi.RefFrame[refIdx])
			if !ok {
				return false
			}
			ref := d.vp9PredictRefFrame(refSlot)
			if ref == nil || !ref.valid {
				return false
			}
			var sf *vp9dec.ScaleFactors
			scaled := false
			if ref.img.Width != int(hdr.Width) || ref.img.Height != int(hdr.Height) {
				sf = d.vp9ScaleFactorsFor(ref.img.Width, ref.img.Height,
					int(hdr.Width), int(hdr.Height))
				if !sf.IsValidScale() {
					d.markVP9Unsupported()
					return true
				}
				scaled = sf.IsScaled()
			}
			src, srcStride := vp9ReferencePlane16(ref, plane)
			if srcStride <= 0 || len(src) == 0 {
				return false
			}
			srcRows := len(src) / srcStride
			if !scaled && (x0 >= srcStride || y0 >= srcRows) {
				continue
			}
			srcWidth := (ref.img.Width + (1 << pd.SubsamplingX) - 1) >> pd.SubsamplingX
			srcHeight := (ref.img.Height + (1 << pd.SubsamplingY) - 1) >> pd.SubsamplingY
			if srcWidth <= 0 || srcHeight <= 0 {
				return false
			}
			avg := refIdx == 1
			if mi.SbType < common.Block8x8 {
				block := 0
				num4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
				num4x4H := int(common.Num4x4BlocksHighLookup[planeBsize])
				for y := range num4x4H {
					for x := range num4x4W {
						mv := vp9dec.AverageSplitMvs(&mi.Bmi, refIdx, block,
							int(pd.SubsamplingX), int(pd.SubsamplingY))
						if !d.reconstructVP9InterPredictPlaneHighBD(hdr, pd, mi, sf,
							dst, dstStride, src, srcStride,
							bsize, miRow, miCol,
							x0, y0, 4*x, 4*y,
							srcWidth, srcHeight, bw, bh, 4, 4, mv, avg, bd) {
							return false
						}
						block++
					}
				}
				continue
			}
			if !d.reconstructVP9InterPredictPlaneHighBD(hdr, pd, mi, sf,
				dst, dstStride, src, srcStride,
				bsize, miRow, miCol,
				x0, y0, 0, 0,
				srcWidth, srcHeight, bw, bh, bw, bh, mi.Mv[refIdx], avg, bd) {
				return false
			}
		}
	}
	return true
}

func (d *VP9Decoder) reconstructVP9InterPredictPlaneHighBD(
	hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane,
	mi *vp9dec.NeighborMi,
	sf *vp9dec.ScaleFactors,
	dst []uint16, dstStride int,
	src []uint16, srcStride int,
	bsize common.BlockSize,
	miRow, miCol int,
	baseX, baseY, blockX, blockY int,
	srcWidth, srcHeight, bw, bh, predW, predH int,
	mv vp9dec.MV, avg bool, bd int,
) bool {
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	edges := vp9dec.BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(edges, mv, bw, bh,
		int(pd.SubsamplingX), int(pd.SubsamplingY))

	dstX := baseX + blockX
	dstY := baseY + blockY
	srcX := baseX + blockX
	srcY := baseY + blockY
	scaledMV := vp9dec.MV32{Row: int32(mvQ4.Row), Col: int32(mvQ4.Col)}
	xStepQ4 := vp9dec.SubpelShifts
	yStepQ4 := vp9dec.SubpelShifts
	if sf != nil && sf.IsScaled() {
		srcX = sf.ScaleValueX(srcX)
		srcY = sf.ScaleValueY(srcY)
		scaledMV = vp9dec.ScaleMv(mvQ4,
			miCol*common.MiSize+blockX, miRow*common.MiSize+blockY, sf)
		xStepQ4 = sf.XStepQ4
		yStepQ4 = sf.YStepQ4
	}
	subpelX := int(scaledMV.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(scaledMV.Row) & (vp9dec.SubpelShifts - 1)
	fullX := srcX + (int(scaledMV.Col) >> vp9dec.SubpelBitsConst)
	fullY := srcY + (int(scaledMV.Row) >> vp9dec.SubpelBitsConst)

	// Build a padded uint16 source window so 8-tap filter taps never read OOB.
	needW := (((predW-1)*xStepQ4 + subpelX) >> 4) + 1
	needH := (((predH-1)*yStepQ4 + subpelY) >> 4) + 1
	padLeft, padRight := 3, 4
	padTop, padBottom := 3, 4
	winW := needW + padLeft + padRight
	winH := needH + padTop + padBottom
	need := winW * winH
	if d.highbd == nil {
		d.highbd = &vp9DecoderHighBDState{}
	}
	if cap(d.highbd.interPredictScratch16) < need {
		d.highbd.interPredictScratch16 = make([]uint16, need)
	}
	win := d.highbd.interPredictScratch16[:need]
	for wy := range winH {
		sy := fullY - padTop + wy
		if sy < 0 {
			sy = 0
		} else if sy >= srcHeight {
			sy = srcHeight - 1
		}
		srcRow := src[sy*srcStride:]
		dstRow := win[wy*winW:]
		for wx := range winW {
			sx := fullX - padLeft + wx
			if sx < 0 {
				sx = 0
			} else if sx >= srcWidth {
				sx = srcWidth - 1
			}
			dstRow[wx] = srcRow[sx]
		}
	}

	srcOff := padTop*winW + padLeft
	dstOff := dstY*dstStride + dstX
	filterIdx := int(mi.InterpFilter)
	if filterIdx < 0 || filterIdx >= len(tables.FilterKernels) {
		filterIdx = 0
	}
	filter := tables.FilterKernels[filterIdx]
	hasX := subpelX != 0 || xStepQ4 != 16
	hasY := subpelY != 0 || yStepQ4 != 16
	switch {
	case !hasX && !hasY:
		if avg {
			dsp.HighbdConvolveAvg(win, srcOff, winW, dst, dstOff, dstStride, predW, predH)
		} else {
			dsp.HighbdConvolveCopy(win, srcOff, winW, dst, dstOff, dstStride, predW, predH)
		}
	case hasX && !hasY:
		if avg {
			dsp.HighbdConvolve8AvgHoriz(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelX, xStepQ4, predW, predH, bd)
		} else {
			dsp.HighbdConvolve8Horiz(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelX, xStepQ4, predW, predH, bd)
		}
	case !hasX && hasY:
		if avg {
			dsp.HighbdConvolve8AvgVert(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelY, yStepQ4, predW, predH, bd)
		} else {
			dsp.HighbdConvolve8Vert(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelY, yStepQ4, predW, predH, bd)
		}
	default:
		if avg {
			dsp.HighbdConvolve8Avg(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelX, xStepQ4, subpelY, yStepQ4, predW, predH, bd)
		} else {
			dsp.HighbdConvolve8(win, srcOff, winW, dst, dstOff, dstStride,
				filter, subpelX, xStepQ4, subpelY, yStepQ4, predW, predH, bd)
		}
	}
	return true
}

func (d *VP9Decoder) readVP9ResidueBlockHighBD(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, tile vp9dec.TileBounds,
	miRow, miCol int, bsize common.BlockSize, segID int, isInter int,
) bool {
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	if isInter != 0 && !d.unsupportedReconstruct {
		if !d.reconstructVP9InterPredictBlockHighBD(hdr, mi, miRow, miCol, bsize) {
			return false
		}
	}
	var coefCounts *vp9dec.CoefCounts
	if !hdr.FrameParallelDecoding {
		coefCounts = &d.counts.Coef
	}
	bd := vp9dec.BitDepth(hdr.BitDepthColor.BitDepth)
	dqBuf := d.vp9EnsureDQCoeff32()
	eobTotal := 0

	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeType := 0
		dequant := d.dq.Y[segID]
		if plane > 0 {
			planeType = 1
			dequant = d.dq.Uv[segID]
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4W, num4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
			bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - num4x4W) >> txSize) * blockStep
		aboveBase := aboveOffsets[plane]
		leftBase := leftOffsets[plane]
		blockIdx := 0

		dqShift := uint(0)
		if txSize == common.Tx32x32 {
			dqShift = 1
		}
		maxEob := vp9dec.MaxEobForTxSize(txSize)
		coeffs := dqBuf[:maxEob]

		var interPlane []uint16
		var interStride, interRows, interBaseX, interBaseY, interBS int
		if isInter != 0 && !d.unsupportedReconstruct {
			interPlane, interStride = d.vp9OutputPlane16(plane)
			if interStride > 0 && len(interPlane) > 0 {
				interRows = len(interPlane) / interStride
				interBaseX = (miCol * common.MiSize) >> pd.SubsamplingX
				interBaseY = (miRow * common.MiSize) >> pd.SubsamplingY
				interBS = 4 << uint(txSize)
			}
		}

		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				txType := common.DctDct
				if isInter == 0 && planeType == 0 && !hdr.Quant.Lossless {
					txType = common.IntraModeToTxType[mode]
				}
				aboveCtx := pd.AboveContext[aboveBase+cc : aboveBase+cc+step]
				leftCtx := pd.LeftContext[leftBase+rr : leftBase+rr+step]
				initCtx := vp9dec.GetEntropyContextFull(txSize, aboveCtx, leftCtx)
				so := common.GetScanPtr(txSize, planeType, isInter, hdr.Quant.Lossless, mode)

				eob := vp9dec.DecodeCoefsHighBD(r, &d.fc.CoefProbs, txSize,
					planeType, isInter, so.Scan, so.Neighbors, dequant,
					dqShift, initCtx, bd, coeffs, &d.coefTokenCache, coefCounts)
				eobTotal += eob

				if isInter == 0 && !d.unsupportedReconstruct {
					dst, stride, ok := d.reconstructVP9IntraPredictTxHighBD(hdr, pd, plane,
						mode, txSize, tile, miRow, miCol, bsize, rr, cc)
					if !ok {
						d.markVP9Unsupported()
					} else if eob > 0 && dst != nil {
						dsp.HighbdInverseTransformAdd(txSize, txType, hdr.Quant.Lossless,
							coeffs, dst, 0, stride, eob, int(bd))
					}
				} else if isInter != 0 && !d.unsupportedReconstruct {
					x0 := interBaseX + cc*4
					y0 := interBaseY + rr*4
					if len(interPlane) == 0 || x0+interBS > interStride || y0+interBS > interRows {
						d.markVP9Unsupported()
					} else if eob > 0 {
						dsp.HighbdInverseTransformAdd(txSize, common.DctDct, hdr.Quant.Lossless,
							coeffs, interPlane, y0*interStride+x0, interStride, eob, int(bd))
					}
				}

				if eob > 0 {
					clear(coeffs)
				}
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for i := range step {
					aboveCtx[i] = hasResidue
					leftCtx[i] = hasResidue
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	if eobTotal == 0 {
		mi.Skip = 1
	}
	return !r.HasError()
}

// --- High-bit-depth Loop Filter ---

func (d *VP9Decoder) vp9FilterLoopBlockHighBD(miRows, miCols, miRow, miCol int,
	lfm *vp9LoopFilterMask,
) bool {
	hb := d.highbd
	if hb == nil {
		return false
	}
	bd := int(d.lastFrame.BitDepth)
	if !d.vp9FilterLoopBlockPlaneSS00HighBD(hb.frameY16Full, d.frameYOrigin, d.lastFrame.YStride,
		miRows, miRow, miCol, lfm, bd) {
		return false
	}
	ssX, ssY := d.lastFrame.Subsampling.Shifts()
	switch {
	case ssX == 1 && ssY == 1:
		if !d.vp9FilterLoopBlockPlaneSS11HighBD(hb.frameU16Full, d.frameUOrigin, d.lastFrame.UStride,
			miRows, miRow, miCol, lfm, bd) {
			return false
		}
		return d.vp9FilterLoopBlockPlaneSS11HighBD(hb.frameV16Full, d.frameVOrigin, d.lastFrame.VStride,
			miRows, miRow, miCol, lfm, bd)
	case ssX == 0 && ssY == 0:
		if !d.vp9FilterLoopBlockPlaneSS00HighBD(hb.frameU16Full, d.frameUOrigin, d.lastFrame.UStride,
			miRows, miRow, miCol, lfm, bd) {
			return false
		}
		return d.vp9FilterLoopBlockPlaneSS00HighBD(hb.frameV16Full, d.frameVOrigin, d.lastFrame.VStride,
			miRows, miRow, miCol, lfm, bd)
	default:
		return false
	}
}

func (d *VP9Decoder) vp9FilterLoopBlockPlaneHighBD(miRows, miCols, miRow, miCol int,
	plane vp9LoopFilterPlane, lfm *vp9LoopFilterMask,
) bool {
	hb := d.highbd
	if hb == nil {
		return false
	}
	bd := int(d.lastFrame.BitDepth)
	switch plane {
	case vp9LoopFilterPlaneY:
		return d.vp9FilterLoopBlockPlaneSS00HighBD(hb.frameY16Full, d.frameYOrigin,
			d.lastFrame.YStride, miRows, miRow, miCol, lfm, bd)
	case vp9LoopFilterPlaneU:
		return d.vp9FilterLoopBlockPlaneSS11HighBD(hb.frameU16Full, d.frameUOrigin,
			d.lastFrame.UStride, miRows, miRow, miCol, lfm, bd)
	case vp9LoopFilterPlaneV:
		return d.vp9FilterLoopBlockPlaneSS11HighBD(hb.frameV16Full, d.frameVOrigin,
			d.lastFrame.VStride, miRows, miRow, miCol, lfm, bd)
	default:
		return false
	}
}

func (d *VP9Decoder) vp9FilterLoopBlockPlaneSS00HighBD(plane []uint16, origin, stride int,
	miRows, miRow, miCol int, lfm *vp9LoopFilterMask, bd int,
) bool {
	if stride <= 0 || len(plane) == 0 {
		return false
	}
	base := origin + miRow*common.MiSize*stride + miCol*common.MiSize
	if base < 0 || base >= len(plane) {
		return false
	}
	mask16 := lfm.leftY[common.Tx16x16]
	mask8 := lfm.leftY[common.Tx8x8]
	mask4 := lfm.leftY[common.Tx4x4]
	mask4Int := lfm.int4x4Y
	offset := base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 2 {
		vp9FilterSelectivelyVertRow2HighBD(0, plane, offset, stride,
			uint32(mask16), uint32(mask8), uint32(mask4), uint32(mask4Int),
			&d.lfi, lfm.lflY[r<<3:], bd)
		offset += 16 * stride
		mask16 >>= 16
		mask8 >>= 16
		mask4 >>= 16
		mask4Int >>= 16
	}

	mask16 = lfm.aboveY[common.Tx16x16]
	mask8 = lfm.aboveY[common.Tx8x8]
	mask4 = lfm.aboveY[common.Tx4x4]
	mask4Int = lfm.int4x4Y
	offset = base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r++ {
		var mask16R, mask8R, mask4R uint32
		if miRow+r != 0 {
			mask16R = uint32(mask16 & 0xff)
			mask8R = uint32(mask8 & 0xff)
			mask4R = uint32(mask4 & 0xff)
		}
		vp9FilterSelectivelyHorizHighBD(plane, offset, stride,
			mask16R, mask8R, mask4R, uint32(mask4Int&0xff),
			&d.lfi, lfm.lflY[r<<3:], bd)
		offset += 8 * stride
		mask16 >>= 8
		mask8 >>= 8
		mask4 >>= 8
		mask4Int >>= 8
	}
	return true
}

func (d *VP9Decoder) vp9FilterLoopBlockPlaneSS11HighBD(plane []uint16, origin, stride int,
	miRows, miRow, miCol int, lfm *vp9LoopFilterMask, bd int,
) bool {
	if stride <= 0 || len(plane) == 0 {
		return false
	}
	base := origin + (miRow*(common.MiSize>>1))*stride + miCol*(common.MiSize>>1)
	if base < 0 || base >= len(plane) {
		return false
	}
	var lflUV [16]uint8
	mask16 := lfm.leftUV[common.Tx16x16]
	mask8 := lfm.leftUV[common.Tx8x8]
	mask4 := lfm.leftUV[common.Tx4x4]
	mask4Int := lfm.int4x4UV
	offset := base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 4 {
		for c := range common.MiBlockSize >> 1 {
			lflUV[(r<<1)+c] = lfm.lflY[(r<<3)+(c<<1)]
			lflUV[((r+2)<<1)+c] = lfm.lflY[((r+2)<<3)+(c<<1)]
		}
		vp9FilterSelectivelyVertRow2HighBD(1, plane, offset, stride,
			uint32(mask16), uint32(mask8), uint32(mask4), uint32(mask4Int),
			&d.lfi, lflUV[r<<1:], bd)
		offset += 16 * stride
		mask16 >>= 8
		mask8 >>= 8
		mask4 >>= 8
		mask4Int >>= 8
	}

	mask16 = lfm.aboveUV[common.Tx16x16]
	mask8 = lfm.aboveUV[common.Tx8x8]
	mask4 = lfm.aboveUV[common.Tx4x4]
	mask4Int = lfm.int4x4UV
	offset = base
	for r := 0; r < common.MiBlockSize && miRow+r < miRows; r += 2 {
		mask4IntR := uint32(mask4Int & 0xf)
		if miRow+r == miRows-1 {
			mask4IntR = 0
		}
		var mask16R, mask8R, mask4R uint32
		if miRow+r != 0 {
			mask16R = uint32(mask16 & 0xf)
			mask8R = uint32(mask8 & 0xf)
			mask4R = uint32(mask4 & 0xf)
		}
		vp9FilterSelectivelyHorizHighBD(plane, offset, stride,
			mask16R, mask8R, mask4R, mask4IntR,
			&d.lfi, lflUV[r<<1:], bd)
		offset += 8 * stride
		mask16 >>= 4
		mask8 >>= 4
		mask4 >>= 4
		mask4Int >>= 4
	}
	return true
}

func vp9FilterSelectivelyVertRow2HighBD(subsamplingFactor int,
	plane []uint16, offset, pitch int,
	mask16, mask8, mask4, mask4Int uint32,
	lfi *vp9dec.LoopFilterInfoN, lfl []uint8, bd int,
) {
	dualMaskCutoff := uint32(0xffff)
	lflForward := 8
	if subsamplingFactor != 0 {
		dualMaskCutoff = 0xff
		lflForward = 4
	}
	dualOne := uint32(1 | (1 << uint(lflForward)))
	ss0 := offset
	for mask := (mask16 | mask8 | mask4 | mask4Int) & dualMaskCutoff; mask != 0; mask = (mask &^ dualOne) >> 1 {
		if mask&dualOne != 0 {
			l0 := &lfi.Lfthr[lfl[0]]
			l1 := &lfi.Lfthr[lfl[lflForward]]
			ss1 := ss0 + 8*pitch

			if mask16&dualOne != 0 {
				if mask16&dualOne == dualOne {
					dsp.HighbdLpfVertical16Dual(plane, ss0, pitch,
						l0.Mblim, l0.Lim, l0.HevThr, bd)
				} else {
					l, ss := l0, ss0
					if mask16&1 == 0 {
						l, ss = l1, ss1
					}
					dsp.HighbdLpfVertical16(plane, ss, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
				}
			}
			if mask8&dualOne != 0 {
				if mask8&dualOne == dualOne {
					dsp.HighbdLpfVertical8Dual(plane, ss0, pitch,
						l0.Mblim, l0.Lim, l0.HevThr,
						l1.Mblim, l1.Lim, l1.HevThr, bd)
				} else {
					l, ss := l0, ss0
					if mask8&1 == 0 {
						l, ss = l1, ss1
					}
					dsp.HighbdLpfVertical8(plane, ss, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
				}
			}
			if mask4&dualOne != 0 {
				if mask4&dualOne == dualOne {
					dsp.HighbdLpfVertical4Dual(plane, ss0, pitch,
						l0.Mblim, l0.Lim, l0.HevThr,
						l1.Mblim, l1.Lim, l1.HevThr, bd)
				} else {
					l, ss := l0, ss0
					if mask4&1 == 0 {
						l, ss = l1, ss1
					}
					dsp.HighbdLpfVertical4(plane, ss, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
				}
			}
			if mask4Int&dualOne != 0 {
				if mask4Int&dualOne == dualOne {
					dsp.HighbdLpfVertical4Dual(plane, ss0+4, pitch,
						l0.Mblim, l0.Lim, l0.HevThr,
						l1.Mblim, l1.Lim, l1.HevThr, bd)
				} else {
					l, ss := l0, ss0+4
					if mask4Int&1 == 0 {
						l, ss = l1, ss1+4
					}
					dsp.HighbdLpfVertical4(plane, ss, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
				}
			}
		}
		ss0 += 8
		lfl = lfl[1:]
		mask16 >>= 1
		mask8 >>= 1
		mask4 >>= 1
		mask4Int >>= 1
	}
}

func vp9FilterSelectivelyHorizHighBD(plane []uint16, offset, pitch int,
	mask16, mask8, mask4, mask4Int uint32,
	lfi *vp9dec.LoopFilterInfoN, lfl []uint8, bd int,
) {
	for mask := mask16 | mask8 | mask4 | mask4Int; mask != 0; {
		count := 1
		if mask&1 != 0 {
			l := &lfi.Lfthr[lfl[0]]
			switch {
			case mask16&1 != 0:
				if mask16&3 == 3 {
					dsp.HighbdLpfHorizontal16Dual(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
					count = 2
				} else {
					dsp.HighbdLpfHorizontal16(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
				}
			case mask8&1 != 0:
				if mask8&3 == 3 {
					n := &lfi.Lfthr[lfl[1]]
					dsp.HighbdLpfHorizontal8Dual(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr,
						n.Mblim, n.Lim, n.HevThr, bd)
					if mask4Int&3 == 3 {
						dsp.HighbdLpfHorizontal4Dual(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr,
							n.Mblim, n.Lim, n.HevThr, bd)
					} else if mask4Int&1 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr, bd)
					} else if mask4Int&2 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+8+4*pitch, pitch,
							n.Mblim, n.Lim, n.HevThr, bd)
					}
					count = 2
				} else {
					dsp.HighbdLpfHorizontal8(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
					if mask4Int&1 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr, bd)
					}
				}
			case mask4&1 != 0:
				if mask4&3 == 3 {
					n := &lfi.Lfthr[lfl[1]]
					dsp.HighbdLpfHorizontal4Dual(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr,
						n.Mblim, n.Lim, n.HevThr, bd)
					if mask4Int&3 == 3 {
						dsp.HighbdLpfHorizontal4Dual(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr,
							n.Mblim, n.Lim, n.HevThr, bd)
					} else if mask4Int&1 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr, bd)
					} else if mask4Int&2 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+8+4*pitch, pitch,
							n.Mblim, n.Lim, n.HevThr, bd)
					}
					count = 2
				} else {
					dsp.HighbdLpfHorizontal4(plane, offset, pitch,
						l.Mblim, l.Lim, l.HevThr, bd)
					if mask4Int&1 != 0 {
						dsp.HighbdLpfHorizontal4(plane, offset+4*pitch, pitch,
							l.Mblim, l.Lim, l.HevThr, bd)
					}
				}
			default:
				dsp.HighbdLpfHorizontal4(plane, offset+4*pitch, pitch,
					l.Mblim, l.Lim, l.HevThr, bd)
			}
		}
		offset += 8 * count
		lfl = lfl[count:]
		mask >>= uint(count)
		mask16 >>= uint(count)
		mask8 >>= uint(count)
		mask4 >>= uint(count)
		mask4Int >>= uint(count)
	}
}
