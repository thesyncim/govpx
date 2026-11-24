package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

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
