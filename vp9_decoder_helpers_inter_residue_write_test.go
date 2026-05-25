package govpx

import (
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
