package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func vp9SkipZeroKeyframeForTest(t *testing.T, width, height int, writeResidue bool) []byte {
	t.Helper()
	return vp9SkipResidueKeyframeForTest(t, width, height, writeResidue, 0)
}

func vp9SkipResidueKeyframeForTest(t *testing.T, width, height int,
	writeResidue bool, dcCoeff int16,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(header.Quant.BaseQindex),
		BitDepth:   vp9dec.Bits8,
	}, &dq)

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 16)
	planes[0].LeftContext = make([]uint8, 16)
	planes[1].AboveContext = make([]uint8, 8)
	planes[1].LeftContext = make([]uint8, 8)
	planes[2].AboveContext = make([]uint8, 8)
	planes[2].LeftContext = make([]uint8, 8)

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   0,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	zeroCoeffs := make([]int16, 1024)
	coeffs := make([]int16, 1024)
	coeffs[0] = dcCoeff
	partitionProbs := tables.KfPartitionProbs
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)

	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			mi := baseMi
			vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
				Seg:       &seg,
				Mi:        &mi,
				TxMode:    common.Only4x4,
				SkipProbs: fc.SkipProbs,
			})
			vp9enc.WriteKeyframeUvMode(bw, common.DcPred, mi.Mode)
			if !writeResidue {
				return nil
			}
			return vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
				BSize:    common.Block64x64,
				MiTxSize: common.Tx4x4,
				IsInter:  0,
				Lossless: false,
				Mi:       &mi,
				Planes:   &planes,
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
			})
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterFrameContextUpdatePacketForTest(t *testing.T, width, height int,
	contextIdx uint8, refreshFrameContext bool,
) ([]byte, uint8) {
	t.Helper()
	w := uint32(width)
	h := uint32(height)

	var probs vp9dec.FrameContext
	vp9dec.ResetFrameContext(&probs)
	var counts vp9enc.FrameCounts
	counts.Skip[0] = [2]uint32{1, 4096}
	var seg vp9dec.SegmentationParams
	aboveSegCtx := make([]int8, common.AlignToSB(miColsForSize(w)))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miColsForSize(w)*miColsForSize(h))
	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   refreshFrameContext,
		FrameParallelDecoding: true,
		FrameContextIdx:       contextIdx,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		CountsArgs: &vp9enc.WriteCompressedHeaderFromCountsArgs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
			CoefStepsize:         1,
			Probs:                &probs,
			Counts:               &counts,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			miCols := miColsForSize(w)
			miRows := miColsForSize(h)
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
				PartitionProbs: &probs.PartitionProb,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &probs,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
			})
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	if probs.SkipProbs[0] == tables.DefaultSkipProbs[0] {
		t.Fatalf("compressed-header counts left skip prob at default %d", probs.SkipProbs[0])
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet, probs.SkipProbs[0]
}

func miColsForSize(v uint32) int {
	return int((v + 7) >> 3)
}

func vp9TopRightResidueKeyframeForNewMvTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 64, 64)
}

func vp9InteriorResidueKeyframeForSubpelTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 96, 96)
}

func vp9ColumnResidueKeyframeForMotionTest(t *testing.T, width, height int) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, width, height, 0)
}

func vp9ColumnResidueKeyframeForMotionLoopFilterTest(t *testing.T,
	width, height int, filterLevel uint8,
) []byte {
	t.Helper()
	return vp9ColumnResidueIntraFrameForMotionTest(t, vp9ColumnResidueIntraFrameArgs{
		Width:             width,
		Height:            height,
		KeyFrame:          true,
		ShowFrame:         true,
		RefreshFrameFlags: 0xff,
		FilterLevel:       filterLevel,
		DCCoeff:           32,
	})
}

func vp9ColumnResidueHiddenIntraOnlyFrameForTest(t *testing.T,
	width, height int, refreshFrameFlags uint8, dcCoeff int16,
) []byte {
	t.Helper()
	return vp9ColumnResidueIntraFrameForMotionTest(t, vp9ColumnResidueIntraFrameArgs{
		Width:             width,
		Height:            height,
		KeyFrame:          false,
		ShowFrame:         false,
		RefreshFrameFlags: refreshFrameFlags,
		FilterLevel:       0,
		DCCoeff:           dcCoeff,
	})
}

type vp9ColumnResidueIntraFrameArgs struct {
	Width             int
	Height            int
	KeyFrame          bool
	ShowFrame         bool
	RefreshFrameFlags uint8
	FilterLevel       uint8
	DCCoeff           int16
}

func vp9ColumnResidueIntraFrameForMotionTest(t *testing.T,
	args vp9ColumnResidueIntraFrameArgs,
) []byte {
	t.Helper()
	width := args.Width
	height := args.Height
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

	partitionProbs := tables.KfPartitionProbs
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
	coeffs := make([]int16, 1024)
	coeffs[0] = args.DCCoeff
	zeroCoeffs := make([]int16, 1024)

	frameType := common.InterFrame
	if args.KeyFrame {
		frameType = common.KeyFrame
	}
	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             frameType,
		IntraOnly:             !args.KeyFrame,
		ShowFrame:             args.ShowFrame,
		RefreshFrameFlags:     args.RefreshFrameFlags,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Loopfilter.FilterLevel = args.FilterLevel

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block32x32,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			var writeErr error
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
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
							if writeErr != nil {
								return
							}
							cur := baseMi
							cur.SbType = bsize
							if miCol == 4 {
								cur.Skip = 0
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
								Seg:       &seg,
								Mi:        &cur,
								AboveMi:   vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
								LeftMi:    left,
								TxMode:    common.Only4x4,
								SkipProbs: fc.SkipProbs,
							})
							vp9enc.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
							aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, miRow, miCol)
							if cur.Skip != 0 {
								vp9dec.ResetSkipContext(planes[:], bsize, aboveOffsets[:], leftOffsets[:])
							} else {
								writeErr = vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
									BSize:        bsize,
									MiTxSize:     common.Tx4x4,
									IsInter:      0,
									Lossless:     false,
									Mi:           &cur,
									Planes:       &planes,
									AboveOffsets: aboveOffsets,
									LeftOffsets:  leftOffsets,
									PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
										dq.Y[0],
										dq.Uv[0],
										dq.Uv[0],
									},
									Fc: &fc.CoefProbs,
									GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
										if plane == 0 && r == 0 && c == 0 {
											return coeffs[:vp9dec.MaxEobForTxSize(tx)]
										}
										return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
									},
								})
							}
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return writeErr
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentedAltQKeyframeForTest(t *testing.T) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	seg := vp9SegmentationAltQForTest()
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

	partitionProbs := tables.KfPartitionProbs
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
	coeffsBySeg := [2][]int16{
		make([]int16, 1024),
		make([]int16, 1024),
	}
	for i := range coeffsBySeg {
		coeffsBySeg[i][0] = dq.Y[i][0]
	}
	zeroCoeffs := make([]int16, 1024)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		Seg:                   seg,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block32x32,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   0,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			var writeErr error
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
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
							if writeErr != nil {
								return
							}
							segID := 0
							if miCol >= 4 {
								segID = 1
							}
							cur := baseMi
							cur.SbType = bsize
							cur.SegmentID = uint8(segID)
							cur.SegIDPredicted = uint8(segID)
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
								Seg:       &seg,
								Mi:        &cur,
								AboveMi:   vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
								LeftMi:    left,
								TxMode:    common.Only4x4,
								SkipProbs: fc.SkipProbs,
							})
							vp9enc.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
							aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, miRow, miCol)
							writeErr = vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
								BSize:        bsize,
								MiTxSize:     common.Tx4x4,
								IsInter:      0,
								Lossless:     false,
								Mi:           &cur,
								Planes:       &planes,
								AboveOffsets: aboveOffsets,
								LeftOffsets:  leftOffsets,
								PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
									dq.Y[segID],
									dq.Uv[segID],
									dq.Uv[segID],
								},
								Fc: &fc.CoefProbs,
								GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
									if plane == 0 && r == 0 && c == 0 {
										return coeffsBySeg[segID][:vp9dec.MaxEobForTxSize(tx)]
									}
									return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
								},
							})
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return writeErr
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentationAltQForTest() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   true,
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = 1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[1][vp9dec.SegLvlAltQ] = 96
	return seg
}

func vp9InterIntraFrameForTest(t *testing.T,
	yMode, uvMode common.PredictionMode, skip bool, dcCoeff int16,
) []byte {
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

	skipFlag := uint8(0)
	if skip {
		skipFlag = 1
	}
	mi := vp9dec.NeighborMi{
		SbType:   common.Block64x64,
		Mode:     yMode,
		TxSize:   common.Tx4x4,
		Skip:     skipFlag,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
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
				UvMode: uvMode,
			})
			if skip {
				fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
				return nil
			}
			aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, 0, 0)
			if err := vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
				BSize:        common.Block64x64,
				MiTxSize:     common.Tx4x4,
				IsInter:      0,
				Lossless:     false,
				Mi:           &mi,
				Planes:       &planes,
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
