package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func writeVP9InterResidueTileForTest(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range leftSegCtx {
			leftSegCtx[i] = 0
		}
		for plane := range vp9dec.MaxMbPlane {
			left := planes[plane].LeftContext
			for i := range left {
				left[i] = 0
			}
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeVP9InterResidueSbForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	if miRow >= miRows || miCol >= miCols {
		return nil
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
		AboveSegCtx:    aboveSegCtx,
		LeftSegCtx:     leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
			subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
			dcCoeff, coeffs, zeroCoeffs); err != nil {
			return err
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		case common.PartitionHorz:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miRow+bs < miRows {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		case common.PartitionVert:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miCol+bs < miCols {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		default:
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveSegCtx, leftSegCtx,
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
	}
	return nil
}

func writeVP9InterResidueBlockForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miGrid []vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	cur := baseMi
	cur.SbType = bsize
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = vp9MiGridAtForTest(miGrid, miRows, miCols, miRow, miCol-1)
	}
	vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
		Seg:          seg,
		Mi:           &cur,
		AboveMi:      vp9MiGridAtForTest(miGrid, miRows, miCols, miRow-1, miCol),
		LeftMi:       left,
		Fc:           fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
			miRows, miRow, miCol, bsize),
	})
	aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(planes, miRow, miCol)
	if err := vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
		BSize:        bsize,
		MiTxSize:     common.Tx4x4,
		IsInter:      1,
		Lossless:     false,
		Mi:           &cur,
		Planes:       planes,
		AboveOffsets: aboveOffsets,
		LeftOffsets:  leftOffsets,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			dq.Y[0],
			dq.Uv[0],
			dq.Uv[0],
		},
		Fc: &fc.CoefProbs,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
			if dcCoeff != 0 && plane == 0 && r == 0 && c == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			if dcCoeff == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
		},
	}); err != nil {
		return err
	}
	fillVP9MiGridForTest(miGrid, miRows, miCols, miRow, miCol, bsize, cur)
	return nil
}

func vp9PlaneContextOffsetsForTest(planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	miRow, miCol int,
) (above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func vp9CompoundInterSkipFrameForTest(t *testing.T) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsForTest(&header, [3]uint8{0, 0, 0})

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.CompoundReference,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:              &seg,
				Mi:               &mi,
				Fc:               &fc,
				TxMode:           common.Only4x4,
				FrameRefMode:     vp9dec.CompoundReference,
				InterpFilter:     vp9dec.InterpEighttap,
				CompFixedRef:     refs.CompFixedRef,
				CompVarRef:       refs.CompVarRef,
				RefFrameSignBias: signBias,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
				IsCompound: true,
			})
			fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentedAltrefInterSkipFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9SegmentedAltrefInterSkipFrameUpdateForTest(t, true)
}

func vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9SegmentedAltrefInterSkipFrameUpdateForTest(t, false)
}

func vp9SegmentedAltrefInterSkipFrameUpdateForTest(t *testing.T, updateMap bool) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	seg := vp9SegmentationAltrefSkipForTest()
	if !updateMap {
		seg.UpdateMap = false
		seg.UpdateData = false
	}
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		Seg:                   seg,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.InterRef.RefIndex = [3]uint8{0, 0, vp9CompoundAltrefSlotForTest}

	mi := vp9dec.NeighborMi{
		SbType:         common.Block64x64,
		Mode:           common.ZeroMv,
		TxSize:         common.Tx4x4,
		InterpFilter:   uint8(vp9dec.InterpEighttap),
		Skip:           1,
		SegmentID:      1,
		SegIDPredicted: 1,
		RefFrame:       [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &fc,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
			})
			fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentationAltrefSkipForTest() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = (1 << uint(vp9dec.SegLvlRefFrame)) |
		(1 << uint(vp9dec.SegLvlSkip))
	seg.FeatureData[1][vp9dec.SegLvlRefFrame] = int16(vp9dec.AltrefFrame)
	return seg
}

func vp9InterSkipFrameForTest(t *testing.T, width, height int) []byte {
	t.Helper()
	return vp9InterSkipFrameTilesForTest(t, width, height, 0)
}

func vp9InterSkipFrameTilesForTest(t *testing.T, width, height, log2TileCols int) []byte {
	t.Helper()
	return vp9InterSkipFrameTilesWithFrameParallelForTest(t, width, height,
		log2TileCols, true)
}

func vp9InterSkipFrameTilesWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, frameParallel bool,
) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsWithFrameParallelForTest(t, width, height,
		log2TileCols, uint32(width), uint32(height), frameParallel)
}

func vp9ScaledZeroMvInterFrameForTest(t *testing.T, width, height, refWidth, refHeight int) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsForTest(t, width, height, 0,
		uint32(refWidth), uint32(refHeight))
}

func vp9InterSkipFrameRefDimsForTest(t *testing.T, width, height, log2TileCols int,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsWithFrameParallelForTest(t, width, height,
		log2TileCols, refWidth, refHeight, true)
}

func vp9InterSkipFrameRefDimsWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, refWidth, refHeight uint32,
	frameParallel bool,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: frameParallel,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1 << uint(log2TileCols),
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9dec.TileOffset(tileRow, miRows, header.Tile.Log2TileRows),
				MiRowEnd:   vp9dec.TileOffset(tileRow+1, miRows, header.Tile.Log2TileRows),
				MiColStart: vp9dec.TileOffset(tileCol, miCols, header.Tile.Log2TileCols),
				MiColEnd:   vp9dec.TileOffset(tileCol+1, miCols, header.Tile.Log2TileCols),
			}
			writeVP9InterSkipTileForTest(bw, miRows, miCols, tile,
				aboveSegCtx, leftSegCtx, miGrid, &partitionProbs, &seg, &fc, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func writeVP9InterSkipTileForTest(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range leftSegCtx {
			leftSegCtx[i] = 0
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
		}
	}
}

func writeVP9InterSkipSbForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	if miRow >= miRows || miCol >= miCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
		AboveSegCtx:    aboveSegCtx,
		LeftSegCtx:     leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
			subsize, tile, miGrid, seg, fc, baseMi)
	} else {
		switch partition {
		case common.PartitionNone:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
		case common.PartitionHorz:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
			if miRow+bs < miRows {
				writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, miGrid, seg, fc, baseMi)
			}
		case common.PartitionVert:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
			if miCol+bs < miCols {
				writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, miGrid, seg, fc, baseMi)
			}
		default:
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveSegCtx, leftSegCtx,
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
	}
}

func writeVP9InterSkipBlockForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miGrid []vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	cur := baseMi
	cur.SbType = bsize
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = vp9MiGridAtForTest(miGrid, miRows, miCols, miRow, miCol-1)
	}
	vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
		Seg:          seg,
		Mi:           &cur,
		AboveMi:      vp9MiGridAtForTest(miGrid, miRows, miCols, miRow-1, miCol),
		LeftMi:       left,
		Fc:           fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
			miRows, miRow, miCol, bsize),
	})
	fillVP9MiGridForTest(miGrid, miRows, miCols, miRow, miCol, bsize, cur)
}

func vp9MiGridAtForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillVP9MiGridForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before tile parsing or
