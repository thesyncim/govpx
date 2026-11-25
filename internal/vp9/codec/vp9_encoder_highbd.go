package codec

import (
	"encoding/binary"
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9HighBDEncoderState holds lazily-allocated 10/12-bit reconstruction and
// transform scratch buffers for VP9 Profile 2 encoders. Profile 0/1 encoders
// leave VP9Encoder.highbd nil (zero memory / zero GC scan overhead).
type vp9HighBDEncoderState struct {
	reconY16Full []uint16
	reconU16Full []uint16
	reconV16Full []uint16
	reconY16     []uint16
	reconU16     []uint16
	reconV16     []uint16
	reconFrame   Image
	layout       common.FrameLayout
	dq           vp9dec.DequantTables
	zeroCounts   encoder.FrameCounts

	diff    [64]int16
	coeff   [64]int32
	qcoeff  [64]int32
	dqcoeff [64]int32
	pred    [64]uint16
	tokBuf  [1024]uint8

	compScratch []byte
	tileScratch []byte
	tileOut     []byte
}

// EncodeImage is the allocating wrapper around EncodeImageInto for any
// supported VP9 profile (Profile 0, Profile 1, Profile 2).
func (e *VP9Encoder) EncodeImage(img *Image) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	if img == nil {
		return nil, ErrInvalidConfig
	}
	w, h := e.opts.Width, e.opts.Height
	est := max(w*h*3, 64*1024)
	dst := make([]byte, est)
	n, err := e.EncodeImageInto(img, dst)
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

// EncodeImageInto encodes img into dst for any supported VP9 profile:
// Profile 0 (8-bit 4:2:0), Profile 1 (8-bit 4:2:2 / 4:4:0 / 4:4:4 / sRGB),
// and Profile 2 (10-bit or 12-bit 4:2:0 using img.Y16/U16/V16).
func (e *VP9Encoder) EncodeImageInto(img *Image, dst []byte) (int, error) {
	res, err := e.EncodeImageIntoWithFlagsResult(img, dst, 0)
	return len(res.Data), err
}

// EncodeImageIntoWithFlagsResult encodes img into dst with explicit flags.
func (e *VP9Encoder) EncodeImageIntoWithFlagsResult(img *Image, dst []byte,
	flags EncodeFlags,
) (VP9EncodeResult, error) {
	if e == nil || e.closed {
		return VP9EncodeResult{}, ErrClosed
	}
	if img == nil {
		return VP9EncodeResult{}, ErrInvalidConfig
	}
	if e.opts.Profile < 2 {
		if img.EffectiveBitDepth() != 8 || img.Subsampling != e.opts.Subsampling ||
			!img.ValidForEncode(e.opts.Width, e.opts.Height) {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		if img.UStride != img.VStride {
			return VP9EncodeResult{}, ErrInvalidConfig
		}
		ycbcr := image.YCbCr{
			Y:              img.Y,
			Cb:             img.U,
			Cr:             img.V,
			YStride:        img.YStride,
			CStride:        img.UStride,
			SubsampleRatio: e.opts.vp9YCbCrSubsampleRatio(),
			Rect:           image.Rect(0, 0, img.Width, img.Height),
		}
		return e.EncodeIntoWithFlagsResult(&ycbcr, dst, flags)
	}
	return e.encodeVP9HighBDFrame(img, dst, flags)
}

func (e *VP9Encoder) ensureHighBDState(w, h int, bd uint8, sub ChromaSubsampling) *vp9HighBDEncoderState {
	if e.highbd == nil {
		e.highbd = &vp9HighBDEncoderState{}
	}
	st := e.highbd
	ssX, ssY := sub.Shifts()
	layout := common.NewFrameLayoutSubsampled(w, h, uint8(ssX), uint8(ssY))
	st.layout = layout
	if cap(st.reconY16Full) < layout.YFullLen {
		st.reconY16Full = make([]uint16, layout.YFullLen)
	} else {
		st.reconY16Full = st.reconY16Full[:layout.YFullLen]
	}
	if cap(st.reconU16Full) < layout.UVFullLen {
		st.reconU16Full = make([]uint16, layout.UVFullLen)
	} else {
		st.reconU16Full = st.reconU16Full[:layout.UVFullLen]
	}
	if cap(st.reconV16Full) < layout.UVFullLen {
		st.reconV16Full = make([]uint16, layout.UVFullLen)
	} else {
		st.reconV16Full = st.reconV16Full[:layout.UVFullLen]
	}
	neutral := uint16(128 << uint(bd-8))
	fill16(st.reconY16Full, neutral)
	fill16(st.reconU16Full, neutral)
	fill16(st.reconV16Full, neutral)
	st.reconY16 = st.reconY16Full[layout.YOrigin:]
	st.reconU16 = st.reconU16Full[layout.UVOrigin:]
	st.reconV16 = st.reconV16Full[layout.UVOrigin:]
	st.reconFrame = Image{
		Width:       w,
		Height:      h,
		Y16:         st.reconY16,
		U16:         st.reconU16,
		V16:         st.reconV16,
		YStride:     layout.YStride,
		UStride:     layout.UVStride,
		VStride:     layout.UVStride,
		Subsampling: sub,
		BitDepth:    bd,
	}
	return st
}

func (e *VP9Encoder) encodeVP9HighBDFrame(img *Image, dst []byte,
	flags EncodeFlags,
) (VP9EncodeResult, error) {
	bd := e.opts.vp9BitDepth()
	if img.EffectiveBitDepth() != bd || img.Subsampling != e.opts.Subsampling ||
		!img.ValidForEncode(e.opts.Width, e.opts.Height) {
		return VP9EncodeResult{}, ErrInvalidConfig
	}
	if len(dst) < vp9MinEncodeIntoBuffer {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}

	w, h := e.opts.Width, e.opts.Height
	miRows := (h + 7) >> 3
	miCols := (w + 7) >> 3
	macroblocks := encoder.MacroblockCount(miRows, miCols)
	ssX, ssY := e.opts.vp9SubsamplingShifts()
	vp9dec.SetupBlockPlanes(&e.planes, ssX, ssY)
	e.ensureVP9EncoderModeBuffers(miRows, miCols)
	st := e.ensureHighBDState(w, h, bd, e.opts.Subsampling)

	isKey := e.vp9ShouldEncodeKeyFrame(flags)
	intraOnly := false
	if !isKey && !intraOnly && (!e.refValid[vp9LastRefSlot] || !e.refFrames[vp9LastRefSlot].valid || e.refFrames[vp9LastRefSlot].img.BitDepth != bd) {
		isKey = true
	}
	showFrame := flags&EncodeInvisibleFrame == 0
	if intraOnly {
		showFrame = false
	}

	refreshFlags := uint8(0xff)
	if !isKey {
		refreshFlags = e.vp9InterRefreshFrameFlags(flags)
	}
	if e.rc.enabled {
		e.rc.beginFrameWithRefresh(isKey || intraOnly, e.frameIndex, refreshFlags)
		e.rc.preEncodeFrame(showFrame)
	}

	qindex := e.vp9EncoderFrameQIndex(isKey, intraOnly, flags, refreshFlags, macroblocks)
	if e.opts.Lossless {
		qindex = 0
	}
	if qindex > 255 {
		qindex = 255
	}
	lossless := qindex == 0 && e.opts.DeltaQUV == 0

	filterLevel := uint8(0)
	if !lossless && e.opts.DisableLoopfilter != VP9LoopfilterDisableAll &&
		!(e.opts.DisableLoopfilter == VP9LoopfilterDisableInter && !isKey && !intraOnly) {
		filterLevel = uint8(min(max(qindex/4, 0), 63))
	}

	renderW, renderH := uint32(w), uint32(h)
	if e.opts.RenderWidth > 0 && e.opts.RenderHeight > 0 {
		renderW = uint32(e.opts.RenderWidth)
		renderH = uint32(e.opts.RenderHeight)
	}

	hdr := vp9dec.UncompressedHeader{
		Profile:               e.opts.vp9Profile(),
		FrameType:             common.InterFrame,
		ShowFrame:             showFrame,
		IntraOnly:             intraOnly,
		ErrorResilientMode:    e.opts.ErrorResilient,
		Width:                 uint32(w),
		Height:                uint32(h),
		RefreshFrameFlags:     refreshFlags,
		RefreshFrameContext:   false,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     bd,
			ColorSpace:   vp9CommonColorSpace(e.opts.ColorSpace),
			ColorRange:   vp9CommonColorRange(e.opts.ColorRange),
			SubsamplingX: ssX,
			SubsamplingY: ssY,
		},
		Loopfilter: vp9dec.LoopfilterParams{
			FilterLevel:    filterLevel,
			SharpnessLevel: e.opts.Sharpness,
		},
		Render: vp9dec.RenderSize{Width: renderW, Height: renderH},
		Tile: vp9EncoderTileInfoForTargetLevel(miCols, w, h,
			e.vp9TileWorkerThreadHint(), e.opts.Log2TileRows, e.opts.TargetLevel),
	}
	hdr.Quant.BaseQindex = int16(qindex)
	hdr.Quant.UvDcDeltaQ = int8(e.opts.DeltaQUV)
	hdr.Quant.UvAcDeltaQ = int8(e.opts.DeltaQUV)
	hdr.Quant.Lossless = lossless
	if isKey {
		hdr.FrameType = common.KeyFrame
		hdr.RefreshFrameFlags = 0xff
		e.resetVP9EncoderFrameContexts()
	} else if intraOnly {
		hdr.FrameType = common.InterFrame
		hdr.IntraOnly = true
		e.resetVP9EncoderFrameContexts()
	} else {
		lastIdx := uint8(vp9LastRefSlot)
		goldIdx := uint8(vp9GoldenRefSlot)
		altIdx := uint8(vp9AltRefSlot)
		if !e.refValid[goldIdx] || !e.refFrames[goldIdx].valid {
			goldIdx = lastIdx
		}
		if !e.refValid[altIdx] || !e.refFrames[altIdx].valid {
			altIdx = lastIdx
		}
		hdr.InterRef.RefIndex = [common.RefsPerFrame]uint8{lastIdx, goldIdx, altIdx}
		hdr.InterpFilter = vp9dec.InterpEighttap
	}

	vp9dec.SetupSegmentationDequant(&hdr.Seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: qindex,
		UvDcDeltaQ: e.opts.DeltaQUV,
		UvAcDeltaQ: e.opts.DeltaQUV,
		BitDepth:   vp9dec.BitDepth(bd),
	}, &st.dq)

	txMode := common.Allow8x8
	if lossless {
		txMode = common.Only4x4
	}
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}

	isIntraFrame := isKey || intraOnly

	// Build compressed header (no probability updates; pure default probs).
	st.compScratch = buffers.EnsureLen(st.compScratch, 256)
	compSize, err := encoder.WriteCompressedHeaderFromCounts(st.compScratch, encoder.WriteCompressedHeaderFromCountsArgs{
		Lossless:             hdr.Quant.Lossless,
		TxMode:               txMode,
		IntraOnly:            isIntraFrame,
		InterpFilter:         hdr.InterpFilter,
		ReferenceMode:        vp9dec.SingleReference,
		CompoundRefAllowed:   false,
		AllowHighPrecisionMv: false,
		Probs:                &e.fc,
		Counts:               &st.zeroCounts,
	})
	if err != nil || compSize <= 0 || compSize > 0xffff {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	hdr.FirstPartitionSize = uint16(compSize)

	var partProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8
	if isIntraFrame {
		partProbs = &tables.KfPartitionProbs
	} else {
		partProbs = &e.fc.PartitionProb
	}

	// Encode all tiles (single-tile or multi-tile per hdr.Tile).
	tileRows := 1 << uint(hdr.Tile.Log2TileRows)
	tileCols := 1 << uint(hdr.Tile.Log2TileCols)
	tileCap := max(len(dst), 64*1024)
	st.tileScratch = buffers.EnsureLen(st.tileScratch, tileCap)
	st.tileOut = st.tileOut[:0]

	for tileRow := range tileRows {
		for tileCol := range tileCols {
			tile := vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols, hdr.Tile)
			var tw bitstream.Writer
			tw.Start(st.tileScratch)
			e.resetVP9EncoderAboveEntropyContexts()
			clear(e.aboveSegCtx)
			for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
				e.resetVP9EncoderLeftEntropyContexts()
				clear(e.leftSegCtx)
				for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
					e.encodeHighBDSb(&tw, st, img, &hdr, tile, miRows, miCols, miRow, miCol,
						common.Block64x64, txMode, partProbs, isIntraFrame)
				}
			}
			tSize, err := tw.Stop()
			if err != nil || tSize <= 0 {
				return VP9EncodeResult{}, ErrBufferTooSmall
			}
			isLastTile := tileRow == tileRows-1 && tileCol == tileCols-1
			if !isLastTile {
				var szBuf [4]byte
				binary.BigEndian.PutUint32(szBuf[:], uint32(tSize))
				st.tileOut = append(st.tileOut, szBuf[:]...)
			}
			st.tileOut = append(st.tileOut, st.tileScratch[:tSize]...)
		}
	}
	tilePayloadSize := len(st.tileOut)

	// Apply in-loop filter to reconFrame so reference matches decoder byte-exact.
	if filterLevel != 0 {
		vp9dec.LoopFilterFrameInit(&e.lfi, &hdr.Loopfilter, &hdr.Seg, int(filterLevel))
		lfDec := VP9Decoder{
			lfi:    e.lfi,
			miGrid: e.miGrid,
			highbd: &vp9DecoderHighBDState{
				frameY16Full: st.reconY16Full,
				frameU16Full: st.reconU16Full,
				frameV16Full: st.reconV16Full,
			},
			frameYOrigin:       st.layout.YOrigin,
			frameUOrigin:       st.layout.UVOrigin,
			frameVOrigin:       st.layout.UVOrigin,
			lastFrame:          st.reconFrame,
			vp9LoopFilterMasks: e.vp9LoopFilterMasks,
		}
		lfDec.applyVP9LoopFilterSerial(miRows, miCols)
		e.vp9LoopFilterMasks = lfDec.vp9LoopFilterMasks
	}

	// Refresh reference slots.
	for slot := range common.RefFrames {
		if hdr.RefreshFrameFlags&(1<<uint(slot)) != 0 {
			e.refFrames[slot].storeWithRenderAndBitDepth(st.reconFrame, int(renderW), int(renderH), int(bd))
			e.refValid[slot] = true
		}
	}

	// Assemble packet: uncompressed header + compressed header + tile payload.
	var bw encoder.BitWriter
	bw.Init(dst)
	var uncSize int
	prevLfRef, prevLfMode := e.vp9EncoderLoopFilterPrevDeltas(isIntraFrame || e.opts.ErrorResilient)
	if isKey {
		uncSize = encoder.WriteKeyframeUncompressedHeaderWithLoopfilterPrev(&bw, &hdr, &prevLfRef, &prevLfMode)
	} else if intraOnly {
		uncSize = encoder.WriteIntraOnlyUncompressedHeaderWithLoopfilterPrev(&bw, &hdr, &prevLfRef, &prevLfMode)
	} else {
		uncSize = encoder.WriteInterUncompressedHeaderWithLoopfilterPrev(&bw, &hdr, func(slot uint8) (uint32, uint32) {
			if int(slot) < len(e.refFrames) && e.refFrames[slot].valid {
				return uint32(e.refFrames[slot].img.Width), uint32(e.refFrames[slot].img.Height)
			}
			return uint32(w), uint32(h)
		}, &prevLfRef, &prevLfMode)
	}
	total := uncSize + compSize + tilePayloadSize
	if total > len(dst) {
		return VP9EncodeResult{}, ErrBufferTooSmall
	}
	copy(dst[uncSize:uncSize+compSize], st.compScratch[:compSize])
	copy(dst[uncSize+compSize:total], st.tileOut)

	e.refreshVP9EncoderMvRefs(isIntraFrame, miRows, miCols)
	e.lastVP9HeaderFrameType = hdr.FrameType
	e.lastVP9HeaderValid = true

	if e.rc.enabled {
		e.rc.postEncodeFrame(total, showFrame, qindex, isIntraFrame,
			hdr.RefreshFrameFlags, macroblocks, false, nil, 0)
	}

	e.forceKeyFrame = false
	e.frameIndex++
	e.lastQuantizerInternal = qindex
	e.lastQuantizerPublic = qindex >> 2
	e.lastQuantizerValid = true
	e.lastLoopFilterLevel = filterLevel
	e.lastLoopFilterValid = true

	return VP9EncodeResult{
		Data:                  dst[:total],
		KeyFrame:              isKey,
		IntraOnly:             intraOnly,
		ShowFrame:             showFrame,
		InterPicturePredicted: !isIntraFrame,
		RefreshFrameFlags:     hdr.RefreshFrameFlags,
	}, nil
}

func (e *VP9Encoder) encodeHighBDSb(w *bitstream.Writer, st *vp9HighBDEncoderState,
	src *Image, hdr *vp9dec.UncompressedHeader, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, txMode common.TxMode,
	partProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	isIntraFrame bool,
) {
	if miRow >= tile.MiRowEnd || miCol >= tile.MiColEnd {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4

	var partition common.PartitionType
	if bsize == common.Block8x8 {
		partition = common.PartitionNone
	} else {
		partition = common.PartitionSplit
	}

	encoder.WritePartitionForBlock(w, encoder.WriteModesSbArgs{
		AboveSegCtx:    e.aboveSegCtx,
		LeftSegCtx:     e.leftSegCtx,
		MiRows:         tile.MiRowEnd,
		MiCols:         tile.MiColEnd,
		PartitionProbs: partProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if partition == common.PartitionNone {
		e.encodeHighBDBlock8x8(w, st, src, hdr, tile, miRows, miCols, miRow, miCol, txMode, isIntraFrame)
		vp9dec.UpdatePartitionContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
		return
	}
	e.encodeHighBDSb(w, st, src, hdr, tile, miRows, miCols, miRow, miCol, subsize, txMode, partProbs, isIntraFrame)
	e.encodeHighBDSb(w, st, src, hdr, tile, miRows, miCols, miRow, miCol+bs, subsize, txMode, partProbs, isIntraFrame)
	e.encodeHighBDSb(w, st, src, hdr, tile, miRows, miCols, miRow+bs, miCol, subsize, txMode, partProbs, isIntraFrame)
	e.encodeHighBDSb(w, st, src, hdr, tile, miRows, miCols, miRow+bs, miCol+bs, subsize, txMode, partProbs, isIntraFrame)
}

func (e *VP9Encoder) highbdIntraEdges(st *vp9HighBDEncoderState, plane int,
	tile vp9dec.TileBounds, miRow, miCol, blockRow4x4, blockCol4x4, bs, planeW, planeH, bd int,
) (aboveFull [1 + 64]uint16, left [32]uint16, haveTop, haveLeft bool) {
	var planeData []uint16
	var stride int
	switch plane {
	case 0:
		planeData, stride = st.reconY16, st.reconFrame.YStride
	case 1:
		planeData, stride = st.reconU16, st.reconFrame.UStride
	default:
		planeData, stride = st.reconV16, st.reconFrame.VStride
	}
	pd := &e.planes[plane]
	x0 := ((miCol * common.MiSize) >> pd.SubsamplingX) + blockCol4x4*4
	y0 := ((miRow * common.MiSize) >> pd.SubsamplingY) + blockRow4x4*4
	baseVal := uint16(128 << uint(bd-8))
	haveLeft = blockCol4x4 != 0 || miCol > tile.MiColStart
	haveTop = blockRow4x4 != 0 || miRow > 0
	planeBlock4x4W := 2 >> pd.SubsamplingX
	rightAvail := blockCol4x4+(bs>>2) < planeBlock4x4W

	if haveLeft {
		for i := range bs {
			sy := y0 + i
			if sy >= planeH {
				sy = planeH - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	} else {
		for i := range bs {
			left[i] = baseVal + 1
		}
	}

	if haveTop {
		aboveRow := (y0 - 1) * stride
		for i := range bs {
			sx := x0 + i
			if sx >= planeW {
				sx = planeW - 1
			}
			aboveFull[1+i] = planeData[aboveRow+sx]
		}
		if bs == 4 && rightAvail {
			for i := range bs {
				sx := x0 + bs + i
				if sx >= planeW {
					sx = planeW - 1
				}
				aboveFull[1+bs+i] = planeData[aboveRow+sx]
			}
		} else {
			fill := aboveFull[bs]
			for i := range bs {
				aboveFull[1+bs+i] = fill
			}
		}
		if haveLeft {
			aboveFull[0] = planeData[aboveRow+x0-1]
		} else {
			aboveFull[0] = baseVal + 1
		}
	} else {
		for i := range 1 + 2*bs {
			aboveFull[i] = baseVal - 1
		}
	}
	return aboveFull, left, haveTop, haveLeft
}

func (e *VP9Encoder) highbdCopySrcBlock(src *Image, plane, x0, y0, bs int, dst []uint16) {
	var srcData []uint16
	var stride, maxW, maxH int
	if plane == 0 {
		srcData, stride, maxW, maxH = src.Y16, src.YStride, src.Width, src.Height
	} else {
		maxW, maxH = src.Subsampling.ChromaDimensions(src.Width, src.Height)
		if plane == 1 {
			srcData, stride = src.U16, src.UStride
		} else {
			srcData, stride = src.V16, src.VStride
		}
	}
	for r := range bs {
		sy := min(max(y0+r, 0), maxH-1)
		for c := range bs {
			sx := min(max(x0+c, 0), maxW-1)
			dst[r*bs+c] = srcData[sy*stride+sx]
		}
	}
}

func highbdCopyRefFullPel(ref *Image, plane, x0, y0, bs int, dst []uint16, dstOff, dstStride int) {
	var refData []uint16
	var refStride, maxW, maxH int
	if plane == 0 {
		refData, refStride, maxW, maxH = ref.Y16, ref.YStride, ref.Width, ref.Height
	} else {
		maxW, maxH = ref.Subsampling.ChromaDimensions(ref.Width, ref.Height)
		if plane == 1 {
			refData, refStride = ref.U16, ref.UStride
		} else {
			refData, refStride = ref.V16, ref.VStride
		}
	}
	if x0 >= 0 && y0 >= 0 && x0+bs <= maxW && y0+bs <= maxH {
		dsp.HighbdConvolveCopy(refData, y0*refStride+x0, refStride, dst, dstOff, dstStride, bs, bs)
		return
	}
	for r := range bs {
		sy := min(max(y0+r, 0), maxH-1)
		for c := range bs {
			sx := min(max(x0+c, 0), maxW-1)
			dst[dstOff+r*dstStride+c] = refData[sy*refStride+sx]
		}
	}
}

func highbdSAD(a, b []uint16, n int) int {
	return dsp.HighbdSAD(a, b, n)
}

// highbdSearchFullPelMV runs a fast diamond full-pel motion search around (0,0)
// and returns the best full-pel MV in 1/8-pel VP9 MV units (multiples of 8,
// even for 4:2:0 chroma so chroma MV is also full-pel).
func (e *VP9Encoder) highbdSearchFullPelMV(srcY []uint16, ref *Image,
	x0, y0 int,
) (bestMV vp9dec.MV, bestSAD int) {
	var refBlk [64]uint16
	highbdCopyRefFullPel(ref, 0, x0, y0, 8, refBlk[:], 0, 8)
	bestSAD = highbdSAD(srcY, refBlk[:], 64)
	bestR, bestC := 0, 0
	// Step sizes of 8, 4, 2 luma pixels (even so 4:2:0 chroma shift >> 1 is exact full-pel).
	for _, step := range [...]int{8, 4, 2} {
		improved := true
		for improved {
			improved = false
			curR, curC := bestR, bestC
			for _, d := range [...][2]int{{-step, 0}, {step, 0}, {0, -step}, {0, step}} {
				nr, nc := curR+d[0], curC+d[1]
				if nr < -16 || nr > 16 || nc < -16 || nc > 16 {
					continue
				}
				highbdCopyRefFullPel(ref, 0, x0+nc, y0+nr, 8, refBlk[:], 0, 8)
				sad := highbdSAD(srcY, refBlk[:], 64)
				// Small bias favoring zero/smaller MVs to save MV bits.
				if sad+8 < bestSAD {
					bestSAD = sad
					bestR, bestC = nr, nc
					improved = true
				}
			}
		}
	}
	return vp9dec.MV{Row: int16(bestR * 8), Col: int16(bestC * 8)}, bestSAD
}

func (e *VP9Encoder) encodeHighBDBlock8x8(w *bitstream.Writer, st *vp9HighBDEncoderState,
	src *Image, hdr *vp9dec.UncompressedHeader, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int,
	txMode common.TxMode, isIntraFrame bool,
) {
	bd := int(hdr.BitDepthColor.BitDepth)
	alignedW := buffers.Align(int(hdr.Width), 8)
	alignedH := buffers.Align(int(hdr.Height), 8)

	txSizeY := common.Tx8x8
	if hdr.Quant.Lossless || txMode == common.Only4x4 {
		txSizeY = common.Tx4x4
	}

	// Pick best intra Y mode across all 10 VP9 intra modes.
	var srcY [64]uint16
	e.highbdCopySrcBlock(src, 0, miCol*8, miRow*8, 8, srcY[:])
	aboveY, leftY, haveTopY, haveLeftY := e.highbdIntraEdges(st, 0, tile, miRow, miCol, 0, 0, 8, alignedW, alignedH, bd)
	bestYMode := common.DcPred
	bestYSAD := 1 << 30
	for cand := common.DcPred; cand <= common.TmPred; cand++ {
		dsp.HighbdIntraPredict(st.pred[:], 0, 8, 8, cand, aboveY[:], leftY[:8], haveTopY, haveLeftY, bd)
		sad := highbdSAD(srcY[:], st.pred[:], 64)
		if sad < bestYSAD {
			bestYSAD = sad
			bestYMode = cand
		}
	}

	// Decide inter (ZeroMv / NewMv on LAST) vs intra for inter frames.
	isInter := 0
	var interMV vp9dec.MV
	var bestRefMV [2]vp9dec.MV
	if !isIntraFrame && e.refValid[hdr.InterRef.RefIndex[0]] && e.refFrames[hdr.InterRef.RefIndex[0]].valid {
		ref := &e.refFrames[hdr.InterRef.RefIndex[0]].img
		mv, interSAD := e.highbdSearchFullPelMV(srcY[:], ref, miCol*8, miRow*8)
		if interSAD <= bestYSAD {
			isInter = 1
			interMV = mv
		}
	}

	uvMode := common.DcPred
	if isInter == 0 {
		pdU := &e.planes[1]
		uvTxSize := vp9dec.GetUvTxSize(common.Block8x8, txSizeY, pdU)
		uvBs := 4 << uint(uvTxSize)
		var srcU [64]uint16
		e.highbdCopySrcBlock(src, 1, (miCol*8)>>pdU.SubsamplingX, (miRow*8)>>pdU.SubsamplingY, uvBs, srcU[:])
		aboveU, leftU, haveTopU, haveLeftU := e.highbdIntraEdges(st, 1, tile, miRow, miCol, 0, 0, uvBs,
			alignedW>>pdU.SubsamplingX, alignedH>>pdU.SubsamplingY, bd)
		bestUVSAD := 1 << 30
		for _, cand := range [...]common.PredictionMode{common.DcPred, common.VPred, common.HPred, common.TmPred} {
			dsp.HighbdIntraPredict(st.pred[:], 0, uvBs, uvBs, cand, aboveU[:], leftU[:uvBs], haveTopU, haveLeftU, bd)
			sad := highbdSAD(srcU[:], st.pred[:], uvBs*uvBs)
			if sad < bestUVSAD {
				bestUVSAD = sad
				uvMode = cand
			}
		}
	}

	// Record NeighborMi for subsequent neighbor contexts & loop filter.
	mi := &e.miGrid[miRow*miCols+miCol]
	*mi = vp9dec.NeighborMi{
		SbType: common.Block8x8,
		TxSize: txSizeY,
		Mode:   bestYMode,
	}
	if isInter != 0 {
		mi.RefFrame = [2]int8{vp9dec.LastFrame, -1}
		mi.Mv[0] = interMV
		if interMV.Row == 0 && interMV.Col == 0 {
			mi.Mode = common.ZeroMv
		} else {
			mi.Mode = common.NewMv
			bestRefMV = e.vp9EncoderBestInterRefMvs(tile, miRows, miCols,
				miRow, miCol, common.Block8x8, mi, false, vp9dec.FrameRefSignBias(hdr))
		}
	} else {
		mi.RefFrame = [2]int8{vp9dec.IntraFrame, -1}
	}

	// Build prediction + forward transform + quantize for all 3 planes.
	var blockQ [3][4][64]int32
	var blockEOB [3][4]int
	eobTotal := 0

	for plane := range 3 {
		pd := &e.planes[plane]
		var reconPlane []uint16
		var reconStride int
		var dequant [2]int16
		switch plane {
		case 0:
			reconPlane, reconStride = st.reconY16, st.reconFrame.YStride
			dequant = st.dq.Y[0]
		case 1:
			reconPlane, reconStride = st.reconU16, st.reconFrame.UStride
			dequant = st.dq.Uv[0]
		default:
			reconPlane, reconStride = st.reconV16, st.reconFrame.VStride
			dequant = st.dq.Uv[0]
		}
		txSize := txSizeY
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, txSizeY, pd)
		}
		bs := 4 << uint(txSize)
		step := 1 << uint(txSize)
		num4x4W := 2 >> pd.SubsamplingX
		num4x4H := 2 >> pd.SubsamplingY
		baseX := (miCol * 8) >> pd.SubsamplingX
		baseY := (miRow * 8) >> pd.SubsamplingY
		planeW := alignedW >> pd.SubsamplingX
		planeH := alignedH >> pd.SubsamplingY
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		mode := uvMode
		if plane == 0 {
			mode = bestYMode
		}
		txType := common.DctDct
		if isInter == 0 && plane == 0 && !hdr.Quant.Lossless {
			txType = common.IntraModeToTxType[mode]
		}
		so := common.GetScanPtr(txSize, planeType, isInter, hdr.Quant.Lossless, mode)

		subIdx := 0
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				x0 := baseX + cc*4
				y0 := baseY + rr*4
				if isInter != 0 {
					ref := &e.refFrames[hdr.InterRef.RefIndex[0]].img
					mvX := int(interMV.Col>>3) >> pd.SubsamplingX
					mvY := int(interMV.Row>>3) >> pd.SubsamplingY
					highbdCopyRefFullPel(ref, plane, x0+mvX, y0+mvY, bs,
						reconPlane, y0*reconStride+x0, reconStride)
				} else {
					above, left, haveTop, haveLeft := e.highbdIntraEdges(st, plane, tile,
						miRow, miCol, rr, cc, bs, planeW, planeH, bd)
					dsp.HighbdIntraPredict(reconPlane, y0*reconStride+x0, reconStride, bs,
						mode, above[:], left[:bs], haveTop, haveLeft, bd)
				}

				// Residual = src - pred.
				var srcBlk [64]uint16
				e.highbdCopySrcBlock(src, plane, x0, y0, bs, srcBlk[:])
				for r := range bs {
					for c := range bs {
						st.diff[r*bs+c] = int16(srcBlk[r*bs+c]) - int16(reconPlane[(y0+r)*reconStride+x0+c])
					}
				}

				// Forward transform (WHT for lossless, hybrid DCT/ADST for lossy).
				if hdr.Quant.Lossless {
					dsp.HighbdFwht4x4(st.diff[:], st.coeff[:], 4)
				} else if txSize == common.Tx8x8 {
					dsp.HighbdFht8x8(st.diff[:], st.coeff[:], 8, txType)
				} else {
					dsp.HighbdFht4x4(st.diff[:], st.coeff[:], 4, txType)
				}

				// Quantize + dequantize.
				eob := encoder.HighbdQuantizeBlock(st.coeff[:], st.qcoeff[:], st.dqcoeff[:],
					so.Scan, dequant, txSize, hdr.Quant.Lossless)
				blockEOB[plane][subIdx] = eob
				copy(blockQ[plane][subIdx][:], st.qcoeff[:bs*bs])
				eobTotal += eob

				// Reconstruct = pred + inv_tx(dqcoeff).
				if eob > 0 {
					dsp.HighbdInverseTransformAdd(txSize, txType, hdr.Quant.Lossless,
						st.dqcoeff[:], reconPlane, y0*reconStride+x0, reconStride, eob, bd)
				}
				subIdx++
			}
		}
	}

	skip := eobTotal == 0
	if skip {
		mi.Skip = 1
	}

	// Write block mode header.
	var aboveMi, leftMi *vp9dec.NeighborMi
	if miRow > 0 {
		aboveMi = &e.miGrid[(miRow-1)*miCols+miCol]
	}
	if miCol > tile.MiColStart {
		leftMi = &e.miGrid[miRow*miCols+miCol-1]
	}

	if isIntraFrame {
		encoder.WriteKeyframeBlock(w, encoder.WriteKeyframeBlockArgs{
			Seg:       &hdr.Seg,
			Mi:        mi,
			AboveMi:   aboveMi,
			LeftMi:    leftMi,
			TxMode:    txMode,
			MaxTxSize: txSizeY,
			SkipProbs: e.fc.SkipProbs,
		})
		encoder.WriteKeyframeUvMode(w, uvMode, bestYMode)
	} else {
		interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
			miRows, miRow, miCol, common.Block8x8)
		encoder.WriteInterBlock(w, encoder.WriteInterBlockArgs{
			Seg:          &hdr.Seg,
			Mi:           mi,
			AboveMi:      aboveMi,
			LeftMi:       leftMi,
			Fc:           &e.fc,
			TxMode:       txMode,
			MaxTxSize:    txSizeY,
			FrameRefMode: vp9dec.SingleReference,
			InterpFilter: hdr.InterpFilter,
			InterModeCtx: interModeCtx,
			UvMode:       uvMode,
			Mv:           mi.Mv,
			BestRefMv:    bestRefMV,
		})
	}

	// Write residual tokens & update above/left entropy contexts.
	aboveOff, leftOff := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
	if skip {
		for plane := range 3 {
			pd := &e.planes[plane]
			nw := 2 >> pd.SubsamplingX
			nh := 2 >> pd.SubsamplingY
			for i := range nw {
				pd.AboveContext[aboveOff[plane]+i] = 0
			}
			for i := range nh {
				pd.LeftContext[leftOff[plane]+i] = 0
			}
		}
		return
	}

	for plane := range 3 {
		pd := &e.planes[plane]
		txSize := txSizeY
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, txSizeY, pd)
		}
		step := 1 << uint(txSize)
		num4x4W := 2 >> pd.SubsamplingX
		num4x4H := 2 >> pd.SubsamplingY
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		mode := uvMode
		if plane == 0 {
			mode = bestYMode
		}
		so := common.GetScanPtr(txSize, planeType, isInter, hdr.Quant.Lossless, mode)

		subIdx := 0
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				aboveCtx := pd.AboveContext[aboveOff[plane]+cc : aboveOff[plane]+cc+step]
				leftCtx := pd.LeftContext[leftOff[plane]+rr : leftOff[plane]+rr+step]
				initCtx := vp9dec.GetEntropyContextFull(txSize, aboveCtx, leftCtx)
				eob := blockEOB[plane][subIdx]
				encoder.WriteCoefsHighBD(w, &e.fc.CoefProbs, txSize, planeType, isInter,
					so.Scan, so.Neighbors, blockQ[plane][subIdx][:], eob, initCtx,
					vp9dec.BitDepth(bd), &st.tokBuf, nil)
				hasRes := uint8(0)
				if eob > 0 {
					hasRes = 1
				}
				for i := range step {
					aboveCtx[i] = hasRes
					leftCtx[i] = hasRes
				}
				subIdx++
			}
		}
	}
}
