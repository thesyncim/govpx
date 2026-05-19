package govpx

import (
	"encoding/binary"
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func clampVP9TxSizeForBlock(tx common.TxSize, bsize common.BlockSize) common.TxSize {
	maxTx := common.MaxTxsizeLookup[bsize]
	if tx > maxTx {
		return maxTx
	}
	return tx
}

func countVP9Skip(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, above, left *vp9dec.NeighborMi, skip uint8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	ctx := vp9dec.GetSkipContext(above, left)
	counts.Skip[ctx][skip]++
}

func countVP9TxSize(counts *encoder.FrameCounts, ctx int,
	maxTxSize, txSize common.TxSize,
) {
	if counts == nil || ctx < 0 || ctx >= vp9dec.TxSizeContexts || txSize >= common.TxSizes {
		return
	}
	switch maxTxSize {
	case common.Tx8x8:
		if txSize <= common.Tx8x8 {
			counts.TxMode.P8x8[ctx][txSize]++
		}
	case common.Tx16x16:
		if txSize <= common.Tx16x16 {
			counts.TxMode.P16x16[ctx][txSize]++
		}
	case common.Tx32x32:
		if txSize <= common.Tx32x32 {
			counts.TxMode.P32x32[ctx][txSize]++
		}
	}
}

func countVP9TxTotals(counts *encoder.FrameCounts, bsize common.BlockSize,
	txSize common.TxSize, planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
) {
	if counts == nil || txSize >= common.TxSizes {
		return
	}
	counts.TxTotals[txSize]++
	if planes == nil {
		return
	}
	uvTx := vp9dec.GetUvTxSize(bsize, txSize, &planes[1])
	if uvTx < common.TxSizes {
		counts.TxTotals[uvTx]++
	}
}

func vp9TxProbsRow(p *vp9dec.TxProbs, maxTxSize common.TxSize, ctx int) []uint8 {
	if p == nil || ctx < 0 || ctx >= vp9dec.TxSizeContexts {
		return nil
	}
	switch maxTxSize {
	case common.Tx8x8:
		return p.P8x8[ctx][:]
	case common.Tx16x16:
		return p.P16x16[ctx][:]
	case common.Tx32x32:
		return p.P32x32[ctx][:]
	default:
		return nil
	}
}

func countVP9IntraInter(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, isInter int,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	counts.IntraInter[ctx][isInter]++
}

func countVP9ReferenceMode(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	frameMode vp9dec.ReferenceMode, refs vp9dec.CompoundFrameRefs,
	above, left *vp9dec.NeighborMi, isCompound bool,
) {
	if counts == nil || frameMode != vp9dec.ReferenceModeSelect ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx := vp9dec.GetReferenceModeContext(above, left, refs)
	bit := 0
	if isCompound {
		bit = 1
	}
	counts.ReferenceMode.CompInter[ctx][bit]++
}

func countVP9SingleRef(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, refFrame int8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	counts.ReferenceMode.SingleRef[ctx0][0][bit0]++
	if bit0 == 0 {
		return
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	counts.ReferenceMode.SingleRef[ctx1][1][bit1]++
}

func countVP9CompoundRef(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, refs vp9dec.CompoundFrameRefs,
	signBias [vp9dec.MaxRefFrames]uint8, refFrame [2]int8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	idx := int(signBias[refs.CompFixedRef])
	if idx < 0 || idx > 1 || refFrame[idx] != refs.CompFixedRef {
		return
	}
	varRef := refFrame[1-idx]
	bit := 0
	switch varRef {
	case refs.CompVarRef[0]:
	case refs.CompVarRef[1]:
		bit = 1
	default:
		return
	}
	ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
	counts.ReferenceMode.CompRef[ctx][bit]++
}

func countVP9InterMode(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, bsize common.BlockSize, ctx int, mode common.PredictionMode,
) {
	if counts == nil || bsize < common.Block8x8 ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	sub := int(mode) - int(common.NearestMv)
	if sub >= 0 && sub < common.InterModes {
		counts.InterMode[ctx][sub]++
	}
}

func countVP9InterSub8Modes(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, bsize common.BlockSize, ctx int, bmi *[4]vp9dec.Bmi,
) {
	if counts == nil || bsize >= common.Block8x8 || bmi == nil ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			j := idy*2 + idx
			sub := int(bmi[j].AsMode) - int(common.NearestMv)
			if sub >= 0 && sub < common.InterModes {
				counts.InterMode[ctx][sub]++
			}
		}
	}
}

func (e *VP9Encoder) countVP9InterSub8NewMvs(counts *encoder.FrameCounts,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, allowHP bool,
	signBias [vp9dec.MaxRefFrames]uint8,
) {
	if counts == nil || mi == nil || bsize >= common.Block8x8 ||
		mi.RefFrame[0] <= vp9dec.IntraFrame {
		return
	}
	halves := 1
	if mi.RefFrame[1] > vp9dec.IntraFrame {
		halves = 2
	}
	var refMv [2]vp9dec.MV
	for ref := 0; ref < halves; ref++ {
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, mi.RefFrame[ref], allowHP,
			signBias)
		if !ok {
			mv = vp9dec.MV{}
		}
		refMv[ref] = mv
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			j := idy*2 + idx
			if mi.Bmi[j].AsMode != common.NewMv {
				continue
			}
			for ref := 0; ref < halves; ref++ {
				countVP9NewMv(counts, mi.Bmi[j].AsMv[ref], refMv[ref])
			}
		}
	}
}

func countVP9InterIntraMode(counts *encoder.FrameCounts, bsize common.BlockSize,
	mode common.PredictionMode,
) {
	if counts == nil || mode < common.DcPred || int(mode) >= common.IntraModes {
		return
	}
	sg := common.SizeGroupLookup[bsize]
	counts.YMode[sg][mode]++
}

func countVP9SwitchableInterp(counts *encoder.FrameCounts,
	above, left *vp9dec.NeighborMi, filter uint8,
) {
	if counts == nil || filter >= uint8(vp9dec.SwitchableFilters) {
		return
	}
	ctx := vp9dec.GetPredContextSwitchableInterp(above, left)
	counts.SwitchableInterp[ctx][filter]++
}

func countVP9NewMv(counts *encoder.FrameCounts, mv, refMv vp9dec.MV) {
	if counts == nil {
		return
	}
	diff := vp9dec.MV{
		Row: mv.Row - refMv.Row,
		Col: mv.Col - refMv.Col,
	}
	vp9IncEncoderMv(diff, &counts.Mv)
}

func vp9IncEncoderMv(mv vp9dec.MV, counts *encoder.NmvContextCounts) {
	joint := vp9dec.GetMvJoint(mv)
	counts.Joints[joint]++
	if joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Row, &counts.Comps[0])
	}
	if joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Col, &counts.Comps[1])
	}
}

func vp9IncEncoderMvComponent(v int16, counts *encoder.NmvComponentCounts) {
	sign := 0
	zv := int(v)
	if zv < 0 {
		sign = 1
		zv = -zv
	}
	counts.Sign[sign]++
	z := zv - 1
	cls, offset := vp9dec.GetMvClass(z)
	counts.Classes[cls]++
	d := offset >> 3
	f := (offset >> 1) & 3
	hp := offset & 1
	if cls == tables.MvClass0 {
		counts.Class0[d]++
		counts.Class0Fp[d][f]++
		counts.Class0Hp[hp]++
		return
	}
	nBits := cls + vp9dec.Class0Bits - 1
	for i := range nBits {
		counts.Bits[i][(d>>i)&1]++
	}
	counts.Fp[f]++
	counts.Hp[hp]++
}

func vp9CoefBranchStats(counts *encoder.FrameCounts) *encoder.FrameCoefBranchStats {
	if counts == nil {
		return nil
	}
	return &counts.CoefBranchStats
}

func (e *VP9Encoder) writeVP9StubModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		txModeForMi(baseMi), vp9ModeTreeKeyframe, nil, nil)
}

func (e *VP9Encoder) writeVP9KeyframeSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	key *vp9KeyframeEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		txModeForMi(baseMi), vp9ModeTreeKeyframeSource, key, nil)
}

func (e *VP9Encoder) writeVP9InterSkipModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		common.TxModeSelect, vp9ModeTreeInterSkip, nil, nil)
}

func (e *VP9Encoder) writeVP9InterSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	inter *vp9InterEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi,
		common.TxModeSelect, vp9ModeTreeInterSource, nil, inter)
}

func (e *VP9Encoder) writeVP9StubModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTileBounds(bw, miRows, miCols, tile, partitionProbs, seg,
		baseMi, txModeForMi(baseMi), vp9ModeTreeKeyframe, nil, nil)
}

func (e *VP9Encoder) writeVP9FrameTiles(output []byte, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) (int, error) {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if e.writeVP9FrameTilesThreadedEnabled(tileRows, tileCols) {
		if totalSize, err, ok := e.writeVP9FrameTilesThreaded(output, miRows, miCols,
			tileInfo, partitionProbs, seg, baseMi, txMode, kind, key, inter); ok {
			return totalSize, err
		}
	}
	totalSize := 0
	nTiles := tileRows * tileCols
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			idx := tileRow*tileCols + tileCol
			isLast := idx == nTiles-1
			offset := totalSize
			if !isLast {
				offset += 4
			}
			if offset >= len(output) {
				return totalSize, encoder.ErrTileBufferFull
			}

			var bw bitstream.Writer
			bw.Start(output[offset:])
			e.writeVP9FrameTile(&bw, miRows, miCols,
				vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols, tileInfo),
				partitionProbs, seg, baseMi, txMode, kind, key, inter)
			size, err := bw.Stop()
			if err != nil {
				if errors.Is(err, bitstream.ErrBufferOverflow) {
					return totalSize, encoder.ErrTileBufferFull
				}
				return totalSize, err
			}
			if !isLast {
				binary.BigEndian.PutUint32(output[totalSize:totalSize+4], uint32(size))
				totalSize += 4
			}
			totalSize += size
		}
	}
	return totalSize, nil
}

func (e *VP9Encoder) writeVP9FrameTile(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	switch kind {
	case vp9ModeTreeKeyframeSource:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, key, nil)
	case vp9ModeTreeInterSource:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, nil, inter)
	default:
		e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
			partitionProbs, seg, baseMi, txMode, kind, nil, nil)
	}
}

func vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
) vp9dec.TileBounds {
	return vp9dec.TileBounds{
		MiRowStart: vp9DecoderTileOffset(tileRow, miRows, tileInfo.Log2TileRows),
		MiRowEnd:   vp9DecoderTileOffset(tileRow+1, miRows, tileInfo.Log2TileRows),
		MiColStart: vp9DecoderTileOffset(tileCol, miCols, tileInfo.Log2TileCols),
		MiColEnd:   vp9DecoderTileOffset(tileCol+1, miCols, tileInfo.Log2TileCols),
	}
}
