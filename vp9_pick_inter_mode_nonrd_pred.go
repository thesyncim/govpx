package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9NonrdPredBuffer struct {
	data   []byte
	stride int
	inUse  bool
}

func vp9NonrdGetPredBuffer(p *[4]vp9NonrdPredBuffer) int {
	for i := 0; i < 3; i++ {
		if !p[i].inUse {
			p[i].inUse = true
			return i
		}
	}
	return -1
}

func vp9NonrdFreePredBuffer(p *[4]vp9NonrdPredBuffer, idx int) {
	if idx >= 0 && idx < len(p) {
		p[idx].inUse = false
	}
}

func (e *VP9Encoder) vp9NonrdReuseInterPredReady(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
) bool {
	if e.sf.ReuseInterPredSby == 0 || bsize < common.Block8x8 ||
		bsize >= common.BlockSizes {
		return false
	}
	if e.sf.PartitionSearchType == ReferencePartition {
		if !e.varPartFrameValid || len(e.varPartGrid) == 0 {
			return false
		}
		idx := miRow*miCols + miCol
		if miRows <= 0 || miCols <= 0 || miRow < 0 || miCol < 0 ||
			miRow >= miRows || miCol >= miCols ||
			idx < 0 || idx >= len(e.varPartGrid) ||
			e.varPartGrid[idx].SbType != bsize {
			return false
		}
		return bsize == common.Block64x64 ||
			bsize == common.Block64x32 ||
			bsize == common.Block32x64
	}
	if e.sf.PartitionSearchType == VarBasedPartition {
		// libvpx VAR_BASED_PARTITION: choose_partitioning pre-bakes the
		// partition tree, then nonrd_use_partition walks it and seeds
		// ctx->pred_pixel_ready = 1 before EVERY >=8x8 leaf pick — the
		// PARTITION_NONE / VERT / HORZ cases at vp9_encodeframe.c:5019,
		// 5030, 5040, 5052, 5063. The only leaf pick without the seed is
		// the bsize==BLOCK_8X8 PARTITION_SPLIT (sub-8x8) case at
		// vp9_encodeframe.c:5078-5082, whose grid stamp is BLOCK_4X4 and
		// therefore never matches bsize here (bsize < Block8x8 already
		// returned false above). choose_partitioning stamps the top-left
		// mi of every terminal leaf — both rect halves included — via
		// set_block_size (vp9_encodeframe.c:340, set_vt_partitioning), so
		// SbType == bsize at (miRow, miCol) identifies exactly the leaf
		// visits of that walk.
		if !e.varPartFrameValid || len(e.varPartGrid) == 0 {
			return false
		}
		idx := miRow*miCols + miCol
		if miRows <= 0 || miCols <= 0 || miRow < 0 || miCol < 0 ||
			miRow >= miRows || miCol >= miCols ||
			idx < 0 || idx >= len(e.varPartGrid) ||
			e.varPartGrid[idx].SbType != bsize {
			return false
		}
		return true
	}
	if e.sf.PartitionSearchType != MlBasedPartition {
		return false
	}

	// libvpx: vp9_encodeframe.c:4608-4663 and :4673 —
	// nonrd_pick_partition seeds ctx->pred_pixel_ready before calling
	// nonrd_pick_sb_modes. The ML realtime lane reaches this helper
	// through that recursive picker, with x->max/min_partition_size pinned
	// to BLOCK_64X64/BLOCK_8X8 at vp9_encodeframe.c:5315-5316.
	ms := int(common.Num8x8BlocksWideLookup[bsize]) / 2
	forceHorzSplit := miRow+ms >= miRows
	forceVertSplit := miCol+ms >= miCols
	xss := e.planes[1].SubsamplingX
	yss := e.planes[1].SubsamplingY

	partitionNoneAllowed := !forceHorzSplit && !forceVertSplit
	partitionHorzAllowed := !forceVertSplit && yss <= xss && bsize >= common.Block8x8
	partitionVertAllowed := !forceHorzSplit && xss <= yss && bsize >= common.Block8x8
	doSplit := bsize >= common.Block8x8

	if e.sf.AutoMinMaxPartitionSize != AutoMinMaxNotInUse {
		const maxPartitionSize = common.Block64x64
		const minPartitionSize = common.Block8x8
		partitionNoneAllowed = partitionNoneAllowed &&
			bsize <= maxPartitionSize && bsize >= minPartitionSize
		partitionHorzAllowed = partitionHorzAllowed &&
			((bsize <= maxPartitionSize && bsize > minPartitionSize) ||
				forceHorzSplit)
		partitionVertAllowed = partitionVertAllowed &&
			((bsize <= maxPartitionSize && bsize > minPartitionSize) ||
				forceVertSplit)
		doSplit = doSplit && bsize > minPartitionSize
	}
	if e.sf.UseSquarePartitionOnly != 0 {
		partitionHorzAllowed = partitionHorzAllowed && forceHorzSplit
		partitionVertAllowed = partitionVertAllowed && forceVertSplit
	}
	if partitionNoneAllowed && doSplit {
		if mlCtx := e.vp9MLPickPartitionEntry(inter, miRows, miCols,
			miRow, miCol); mlCtx != nil {
			switch vp9MLPredictVarPartitioning(bsize, miRow, miCol, mlCtx) {
			case vp9MLPredictNone:
				doSplit = false
			}
		}
	}
	return !(partitionVertAllowed || partitionHorzAllowed || doSplit)
}

func (e *VP9Encoder) vp9NonrdLumaPredRect(miRow, miCol int,
	bsize common.BlockSize,
) (data []byte, stride, x, y, w, h int, ok bool) {
	data, stride = e.vp9EncoderReconPlane(0)
	if len(data) == 0 || stride <= 0 || bsize < 0 || bsize >= common.BlockSizes {
		return nil, 0, 0, 0, 0, 0, false
	}
	rows := len(data) / stride
	x = miCol * common.MiSize
	y = miRow * common.MiSize
	w = int(common.Num4x4BlocksWideLookup[bsize]) * 4
	h = int(common.Num4x4BlocksHighLookup[bsize]) * 4
	if x < 0 || y < 0 || w <= 0 || h <= 0 ||
		x+w > stride || y+h > rows || w*h > len(e.nonrdOrigPredScratch) {
		return nil, 0, 0, 0, 0, 0, false
	}
	return data, stride, x, y, w, h, true
}

func vp9CopyPredRectToScratch(scratch []byte, src []byte,
	srcStride, x, y, w, h int,
) {
	srcOff := y*srcStride + x
	dstOff := 0
	for range h {
		copy(scratch[dstOff:dstOff+w], src[srcOff:srcOff+w])
		srcOff += srcStride
		dstOff += w
	}
}

func vp9CopyPredRectFromScratch(dst []byte, dstStride, x, y, w, h int,
	scratch []byte,
) {
	dstOff := y*dstStride + x
	srcOff := 0
	for range h {
		copy(dst[dstOff:dstOff+w], scratch[srcOff:srcOff+w])
		dstOff += dstStride
		srcOff += w
	}
}

const vp9NonrdIntMaxSAD = uint64(1<<31 - 1)

func vp9NonrdCBRIntProNewMVPass(tmpSad, predMvSadLast, bestPredSad uint64,
	bsize common.BlockSize,
) bool {
	if tmpSad > predMvSadLast {
		return false
	}
	return tmpSad+(uint64(common.NumPelsLog2Lookup[bsize])<<4) <= bestPredSad
}

func vp9NonrdForceSkipGoldenCandidate(forceSkipLowTempVar bool,
	refFrame int8, mode common.PredictionMode, mv vp9dec.MV, mvValid bool,
) bool {
	if !forceSkipLowTempVar || refFrame != vp9dec.GoldenFrame {
		return false
	}
	if mode == common.NewMv {
		return true
	}
	return mvValid && mv != (vp9dec.MV{})
}

func (e *VP9Encoder) vp9NonrdCBRIntProNewMV(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, refMV vp9dec.MV, predMvSadLast, bestPredSad uint64,
) (vp9dec.MV, bool) {
	if inter == nil || inter.img == nil || inter.ref == nil || !inter.ref.valid ||
		bsize < common.Block16x16 || bsize >= common.BlockSizes {
		return vp9dec.MV{}, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 || srcW <= 0 || srcH <= 0 {
		return vp9dec.MV{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 < 0 || y0 < 0 || x0+blockW > srcW || y0+blockH > srcH {
		return vp9dec.MV{}, false
	}
	if !e.intProSrcBorderedValid ||
		e.intProSrcBordered.W != srcW ||
		e.intProSrcBordered.H != srcH {
		common.YV12BuildBorderedPlane(&e.intProSrcBordered, src, srcStride,
			srcW, srcH, common.VP9EncBorderInPixels)
		e.intProSrcBorderedValid = true
	}
	ref, refStride, refOriginX, refOriginY, refW, refH, refOK :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if !refOK || len(ref) == 0 || refStride <= 0 ||
		x0+blockW > refW || y0+blockH > refH {
		return vp9dec.MV{}, false
	}
	srcOriginX := e.intProSrcBordered.OriginX()
	srcOriginY := e.intProSrcBordered.OriginY()
	srcStrideB := e.intProSrcBordered.Stride
	mvLimits := encoder.EncoderMvLimits(miRows, miCols, miRow, miCol, bsize)
	tmpSad, mv := encoder.IntProEstimate(&encoder.IntProEstimateInput{
		Bsize:     bsize,
		Src:       e.intProSrcBordered.Pixels,
		SrcOff:    (srcOriginY+y0)*srcStrideB + (srcOriginX + x0),
		SrcStride: srcStrideB,
		Ref:       ref,
		RefOff:    (refOriginY+y0)*refStride + (refOriginX + x0),
		RefStride: refStride,
		RefMV:     refMV,
		MvLimits:  mvLimits,
	})
	if !vp9NonrdCBRIntProNewMVPass(uint64(tmpSad), predMvSadLast,
		bestPredSad, bsize) {
		return vp9dec.MV{}, false
	}
	return mv, true
}

func (e *VP9Encoder) vp9NonrdPredMVSAD(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, refFrame int8, mv vp9dec.MV,
) (uint64, bool) {
	if inter == nil || inter.img == nil || inter.ref == nil || !inter.ref.valid {
		return 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	ref, refStride, refOriginX, refOriginY, _, _, ok :=
		e.vp9SubpelReferencePlane(refFrame, inter.ref)
	if !ok || len(ref) == 0 || refStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 < 0 || y0 < 0 || x0+blockW > srcW || y0+blockH > srcH {
		return 0, false
	}
	fpRow := int(mv.Row) >> 3
	fpCol := int(mv.Col) >> 3
	refX := refOriginX + x0 + fpCol
	refY := refOriginY + y0 + fpRow
	refRows := len(ref) / refStride
	if refX < 0 || refY < 0 || refX+blockW > refStride || refY+blockH > refRows {
		return 0, false
	}
	return encoder.BlockSADOffsets(src, y0*srcStride+x0, srcStride,
		ref, refY*refStride+refX, refStride, blockW, blockH,
		^uint64(0)), true
}
