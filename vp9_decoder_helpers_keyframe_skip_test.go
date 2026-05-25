package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
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

func miColsForSize(v uint32) int {
	return int((v + 7) >> 3)
}
