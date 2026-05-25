package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func vp9InterResidueFrameForTest(t *testing.T, width, height int, dcCoeff int16) []byte {
	t.Helper()
	return vp9InterResidueFrameLoopFilterForTest(t, width, height, dcCoeff, 0)
}

func vp9InterResidueFrameLoopFilterForTest(t *testing.T,
	width, height int, dcCoeff int16, filterLevel uint8,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	zeroCoeffs := make([]int16, 1024)
	coeffs := make([]int16, 1024)
	coeffs[0] = dcCoeff

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
	header.Loopfilter.FilterLevel = filterLevel

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         0,
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
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			return writeVP9InterResidueTileForTest(bw, miRows, miCols, tile,
				aboveSegCtx, leftSegCtx, miGrid, &partitionProbs, &seg, &fc,
				&planes, &dq, mi, dcCoeff, coeffs, zeroCoeffs)
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

func vp9InterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMotionMvFrameForTest(t, common.ZeroMv)
}

func vp9InterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMotionMvFrameForTest(t, common.NearestMv)
}

func vp9InterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearMv, 64, 64)
}

func vp9InterSubpelNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterSubpelNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, true,
		vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterSubpelBilinearNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpBilinear, vp9dec.InterpBilinear)
}

func vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpSwitchable, vp9dec.InterpEighttapSmooth)
}

func vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, true,
		vp9dec.InterpSwitchable, vp9dec.InterpEighttapSharp)
}

func vp9InterSubpelTopRightBorderNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameForTest(t, 64, 64, 0, 4,
		vp9dec.MV{Row: -4, Col: 260}, vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterIntegerTopRightBorderNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameForTest(t, 64, 64, 0, 4,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9ScaledNewMvInterFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameRefDimsForTest(t, 32, 32, 0, 0,
		vp9dec.MV{Col: 32}, vp9dec.InterpEighttap, vp9dec.InterpEighttap, 64, 64)
}

func vp9ScaledInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearestMv, 128, 128)
}

func vp9ScaledInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearMv, 128, 128)
}

func vp9InterSingleNewMvFrameForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		uint32(width), uint32(height))
}

func vp9InterSingleNewMvFrameRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refWidth, refHeight uint32,
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
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
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

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
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
			InterpFilter:         frameFilter,
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
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					if miRow == targetMiRow && miCol == targetMiCol {
						cur.Mode = common.NewMv
						mv[0] = targetMV
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      above,
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: frameFilter,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
						AllowHP:             false,
						Mv:                  mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
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

func vp9InterMotionMvFrameForTest(t *testing.T, bottomLeftMode common.PredictionMode) []byte {
	t.Helper()
	return vp9InterMotionMvFrameLoopFilterRefDimsForTest(t, bottomLeftMode, 0, 64, 64)
}

func vp9InterMotionMvFrameLoopFilterForTest(t *testing.T,
	bottomLeftMode common.PredictionMode, filterLevel uint8,
) []byte {
	t.Helper()
	return vp9InterMotionMvFrameLoopFilterRefDimsForTest(t, bottomLeftMode,
		filterLevel, 64, 64)
}

func vp9InterMotionMvFrameLoopFilterRefDimsForTest(t *testing.T,
	bottomLeftMode common.PredictionMode, filterLevel uint8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
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
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

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
	header.Loopfilter.FilterLevel = filterLevel

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
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
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					if miRow == 0 && miCol == 0 {
						cur.Mode = common.NewMv
						mv[0] = vp9dec.MV{Col: 256}
					} else if miRow == 4 && miCol == 0 && bottomLeftMode != common.ZeroMv {
						cur.Mode = bottomLeftMode
						mv[0] = vp9dec.MV{Col: 256}
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: vp9dec.InterpEighttap,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP: false,
						Mv:      mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
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

func vp9InterMvReuseFrameRefDimsForTest(t *testing.T,
	reuseMode common.PredictionMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
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
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

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

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	targetMV := vp9dec.MV{Col: 256}
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
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					switch {
					case reuseMode == common.NearestMv && miRow == 0 && miCol == 0:
						cur.Mode = common.NewMv
						mv[0] = targetMV
					case reuseMode == common.NearestMv && miRow == 4 && miCol == 0:
						cur.Mode = common.NearestMv
						mv[0] = targetMV
					case reuseMode == common.NearMv && miRow == 0 && miCol == 4:
						cur.Mode = common.NewMv
						mv[0] = targetMV
					case reuseMode == common.NearMv && miRow == 4 && miCol == 4:
						cur.Mode = common.NearMv
						mv[0] = targetMV
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      above,
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: vp9dec.InterpEighttap,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP: false,
						Mv:      mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
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

func vp9InterSubpelMotionFrameForTest(t *testing.T, nearestReuse bool,
	frameFilter, blockFilter vp9dec.InterpFilter,
) []byte {
	t.Helper()
	const width = 96
	const height = 96
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
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
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

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 131072)
	scratch := make([]byte, 131072)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         frameFilter,
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
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							cur := baseMi
							cur.SbType = bsize
							var mv [2]vp9dec.MV
							if nearestReuse {
								if miRow == 0 && miCol == 0 {
									cur.Mode = common.NewMv
									mv[0] = vp9dec.MV{Col: 260}
								} else if miRow == 4 && miCol == 0 {
									cur.Mode = common.NearestMv
									mv[0] = vp9dec.MV{Col: 260}
								}
							} else if miRow == 4 && miCol == 0 {
								cur.Mode = common.NewMv
								mv[0] = vp9dec.MV{Row: 4, Col: 260}
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
							vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
								Seg:          &seg,
								Mi:           &cur,
								AboveMi:      above,
								LeftMi:       left,
								Fc:           &fc,
								TxMode:       common.Only4x4,
								FrameRefMode: vp9dec.SingleReference,
								InterpFilter: frameFilter,
								InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
									miRows, miRow, miCol, bsize),
								SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
								AllowHP:             false,
								Mv:                  mv,
							})
							cur.Mv = mv
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
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
