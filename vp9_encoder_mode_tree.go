package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9ModeTreeKind uint8

const (
	vp9ModeTreeKeyframe vp9ModeTreeKind = iota
	vp9ModeTreeKeyframeSource
	vp9ModeTreeInterSkip
	vp9ModeTreeInterSource
)

type vp9KeyframeEncodeState struct {
	img      *image.YCbCr
	hdr      *vp9dec.UncompressedHeader
	dq       *vp9dec.DequantTables
	lossless bool
	counts   *encoder.FrameCounts
}

type vp9InterEncodeState struct {
	img              *image.YCbCr
	dq               *vp9dec.DequantTables
	ref              *vp9ReferenceFrame
	refMask          uint8
	allowHP          bool
	selectFc         vp9dec.FrameContext
	modeCostFc       vp9dec.FrameContext
	modeCostFcValid  bool
	referenceMode    vp9dec.ReferenceMode
	compoundAllowed  bool
	refSignBias      [vp9dec.MaxRefFrames]uint8
	compoundRefs     vp9dec.CompoundFrameRefs
	interpFilter     vp9dec.InterpFilter
	predInterpFilter vp9dec.InterpFilter
	lossless         bool
	txMode           common.TxMode
	counts           *encoder.FrameCounts
	isSrcFrameAltRef bool
	showFrame        bool
	predFilterValid  bool
	// baseQindex mirrors libvpx's cm->base_qindex for the current frame.
	// Used by encoder.ChoosePartitioning to drive set_vbp_thresholds without
	// reverse-looking up from dq.Y[0][1] (which is wrong when
	// segmentation is enabled and segment 0 has a non-zero delta_q).
	// libvpx ref: vp9_encodeframe.c:1379 (set_vbp_thresholds caller).
	baseQindex int
}

func vp9InterReferenceMode(inter *vp9InterEncodeState) vp9dec.ReferenceMode {
	if inter == nil {
		return vp9dec.SingleReference
	}
	return inter.referenceMode
}

func vp9InterModeCostFrameContext(inter *vp9InterEncodeState) *vp9dec.FrameContext {
	if inter == nil {
		return nil
	}
	if inter.modeCostFcValid {
		return &inter.modeCostFc
	}
	return &inter.selectFc
}

func vp9InterSignBias(inter *vp9InterEncodeState) [vp9dec.MaxRefFrames]uint8 {
	if inter == nil {
		return [vp9dec.MaxRefFrames]uint8{}
	}
	return inter.refSignBias
}

func vp9InterCompoundRefs(inter *vp9InterEncodeState) vp9dec.CompoundFrameRefs {
	if inter == nil {
		return vp9dec.CompoundFrameRefs{}
	}
	return inter.compoundRefs
}

func (e *VP9Encoder) writeVP9ModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	rowMT := e.vp9RowMTSync
	// libvpx: vp9_encodeframe.c:6126-6128 — vp9_cyclic_refresh_update_sb_postencode
	// only runs on inter frames where the seg+aq path is live. govpx
	// invokes writeVP9ModesTileBounds twice per frame: a count
	// pre-pass (collectVP9EncodeFrameCounts at vp9_encoder.go:2404)
	// and the real bitstream pass (writeVP9FrameTiles at 2474). The
	// pre-pass sets inter.counts != nil; the real pass leaves it nil.
	// libvpx's call site only fires at real-encode time, so gate on
	// inter.counts == nil here to avoid double-counting consec_zero_mv
	// / last_coded_q_map per frame.
	doCyclicSbPostencode := kind == vp9ModeTreeInterSource &&
		e.cyclicAQ.Enabled && e.cyclicAQ.Apply && e.cyclicAQ.ContentMode &&
		seg != nil && seg.Enabled && inter != nil && inter.counts == nil
	var cyclicBaseQindex int
	if doCyclicSbPostencode {
		// libvpx uses cm->base_qindex when clamping last_coded_q_map.
		// govpx's inter state pins the corresponding qindex in the
		// dequant tables; we recover it through the encoder header
		// scratch which holds the final header for this frame.
		cyclicBaseQindex = int(e.vp9HeaderScratch.Quant.BaseQindex)
	}
	if rowMT == nil {
		for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
			for i := range e.leftSegCtx {
				e.leftSegCtx[i] = 0
			}
			if kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource {
				e.resetVP9EncoderLeftEntropyContexts()
			}
			for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
				e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
					common.Block64x64, tile, partitionProbs, seg, baseMi,
					txMode, kind, key, inter)
				if doCyclicSbPostencode {
					e.vp9CyclicRefreshUpdateEncodedSb(miRows, miCols,
						miRow, miCol, cyclicBaseQindex)
				}
			}
		}
		return
	}
	tileSbCols := (tile.MiColEnd - tile.MiColStart + common.MiBlockSize - 1) >>
		common.MiBlockSizeLog2
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range e.leftSegCtx {
			e.leftSegCtx[i] = 0
		}
		if kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource {
			e.resetVP9EncoderLeftEntropyContexts()
		}
		sbRow := (miRow - tile.MiRowStart) >> common.MiBlockSizeLog2
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			sbCol := (miCol - tile.MiColStart) >> common.MiBlockSizeLog2
			// Wavefront: wait for the row above to encode the above and
			// above-right SB before consuming their entropy / above-context
			// state when RowMT is armed.
			rowMT.read(sbRow, sbCol)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, partitionProbs, seg, baseMi, txMode,
				kind, key, inter)
			if doCyclicSbPostencode {
				e.vp9CyclicRefreshUpdateEncodedSb(miRows, miCols,
					miRow, miCol, cyclicBaseQindex)
			}
			rowMT.write(sbRow, sbCol, tileSbCols)
		}
	}
}

// vp9CyclicRefreshUpdateEncodedSb mirrors libvpx's per-SB postencode hook
// from vp9/encoder/vp9_encodeframe.c:6126-6134. After all leaf blocks of
// a 64x64 superblock have been written to the bitstream (and their
// miGrid entries populated), this walks the 8x8 grid that backs the SB
// and:
//
//   - bumps consec_zero_mv for LAST_FRAME inter blocks with near-zero MVs
//     (libvpx: update_zeromv_cnt, vp9_encodeframe.c:5999-6022), and
//   - updates last_coded_q_map for refresh-segmented blocks
//     (libvpx: vp9_cyclic_refresh_update_sb_postencode,
//     vp9_aq_cyclicrefresh.c:225-255).
//
// Both feed the next frame's cyclic_refresh_update_map eligibility filter
// (libvpx: vp9_aq_cyclicrefresh.c:437-442).
func (e *VP9Encoder) vp9CyclicRefreshUpdateEncodedSb(miRows, miCols,
	miRow, miCol, baseQindex int,
) {
	if e == nil {
		return
	}
	cr := &e.cyclicAQ
	if cr.MIRows != miRows || cr.MICols != miCols {
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:231-234 — superblock 8x8 block
	// walk. num_8x8_blocks_{wide,high}_lookup[BLOCK_64X64] = 8.
	xmis := min(miCols-miCol, common.MiBlockSize)
	ymis := min(miRows-miRow, common.MiBlockSize)
	if xmis <= 0 || ymis <= 0 {
		return
	}
	// Walk each 8x8 leaf block in raster order; the leaf's MODE_INFO is
	// stored at the (miRow+y, miCol+x) miGrid slot by fillVP9MiGrid.
	for y := range ymis {
		for x := range xmis {
			mi := e.vp9MiAt(miRows, miCols, miRow+y, miCol+x)
			if mi == nil {
				continue
			}
			isInter := mi.RefFrame[0] > vp9dec.IntraFrame
			segID := mi.SegmentID
			skip := mi.Skip != 0
			// libvpx: vp9_aq_cyclicrefresh.c:244-253 — single-cell update.
			cr.UpdateSegmentPostencode(miRow+y, miCol+x,
				1, 1, baseQindex, segID, isInter, skip)
			// libvpx: vp9_encodeframe.c:5999-6022 — update_zeromv_cnt.
			cr.UpdateZeroMVCnt(miRow+y, miCol+x, 1, 1,
				mi.Mv[0].Row, mi.Mv[0].Col, mi.RefFrame[0], isInter, segID)
		}
	}
}

func (e *VP9Encoder) writeVP9ModesSb(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	if miRow >= miRows || miCol >= miCols {
		return
	}
	// libvpx vp9_encodeframe.c:5259-5262 — avg_source_sad runs once per 64x64
	// SB at encode_nonrd_sb_row entry before partition/mode picking.
	if inter != nil && kind == vp9ModeTreeInterSource &&
		bsize == common.Block64x64 && inter.img != nil &&
		e.sf.UseSourceSad != 0 {
		e.vp9EnsureSBLastHighContentCached(miRows, miCols, miRow, miCol)
		_, _ = e.vp9SourceSADState(inter.img, miRows, miCols, miRow, miCol)
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := e.pickVP9BlockSizeForRegion(miRows, miCols, miRow, miCol,
		bsize, tile, partitionProbs, txMode, kind, key, inter)
	partition := common.PartitionLookup[bsl][target]
	if counts := vp9EncodeCountsForState(key, inter); counts != nil {
		ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, bsize)
		counts.Partition[ctx][partition]++
	}
	encoder.WritePartitionForBlock(bw, encoder.WriteModesSbArgs{
		AboveSegCtx:    e.aboveSegCtx,
		LeftSegCtx:     e.leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
			seg, baseMi, txMode, kind, key, inter)
	} else {
		switch partition {
		case common.PartitionNone:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
		case common.PartitionHorz:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
			if miRow+bs < miRows {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, seg, baseMi, txMode, kind, key, inter)
			}
		case common.PartitionVert:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile,
				seg, baseMi, txMode, kind, key, inter)
			if miCol+bs < miCols {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, seg, baseMi, txMode, kind, key, inter)
			}
		default:
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, txMode, kind, key, inter)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
	}
}

var vp9StubBlockSizeOrder = [...]common.BlockSize{
	common.Block64x64,
	common.Block64x32,
	common.Block32x64,
	common.Block32x32,
	common.Block32x16,
	common.Block16x32,
	common.Block16x16,
	common.Block16x8,
	common.Block8x16,
	common.Block8x8,
	common.Block8x4,
	common.Block4x8,
	common.Block4x4,
}

func vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol int, root common.BlockSize) common.BlockSize {
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	for _, bsize := range vp9StubBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= availW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= availH {
			return bsize
		}
	}
	return common.Block4x4
}

func (e *VP9Encoder) pickVP9BlockSizeForRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	txMode common.TxMode, kind vp9ModeTreeKind, key *vp9KeyframeEncodeState,
	inter *vp9InterEncodeState,
) common.BlockSize {
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, root)
	if kind == vp9ModeTreeKeyframeSource {
		if key != nil && key.counts == nil {
			if cached, ok := e.lookupVP9KeyframePartitionDecision(miRow, miCol, root); ok {
				return cached
			}
		}
		commitKeyframeTarget := func(target common.BlockSize) common.BlockSize {
			if key != nil && key.counts != nil {
				e.storeVP9KeyframePartitionDecision(miRow, miCol, root, target)
			}
			return target
		}
		if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() &&
			key != nil && key.img != nil && e.vp9DynamicSegmentMapActive() {
			if segmentSize, ok := e.pickVP9SegmentMapPartitionBlockSize(
				miRows, miCols, miRow, miCol, root, key.img, nil); ok {
				return commitKeyframeTarget(segmentSize)
			}
		}
		if varianceSize, ok := e.pickVP9KeyframeVariancePartitionBlockSize(key,
			miRows, miCols, miRow, miCol, root); ok {
			return commitKeyframeTarget(varianceSize)
		}
		if rdSize, ok := e.pickVP9KeyframeRDPartitionBlockSize(key, tile,
			partitionProbs, miRows, miCols, miRow, miCol, root,
			txMode); ok {
			return commitKeyframeTarget(rdSize)
		}
		if e.sf.UseNonrdPickMode == 0 && e.vp9KeyframeRDRefinementEnabled() {
			if textureSize, ok := e.pickVP9KeyframeTexturePartitionBlockSize(key,
				tile, miRows, miCols, miRow, miCol, root); ok {
				return commitKeyframeTarget(textureSize)
			}
		}
		// Fixed-Q libvpx keeps neutral clipped keyframe edges at the coarse
		// geometry, but uses square leaves once the edge carries luma residue.
		if e.vp9FixedPublicQuantizer() &&
			vp9KeyframeEdgeBlockHasNonNeutralLuma(key, miRows, miCols,
				miRow, miCol, root) {
			return commitKeyframeTarget(vp9KeyframeSquareBlockSizeForRegion(miRows, miCols,
				miRow, miCol, root))
		}
		return commitKeyframeTarget(vp9KeyframeSourceBlockSizeForRegion(miRows, miCols,
			miRow, miCol, root))
	}
	if kind == vp9ModeTreeInterSource && inter != nil {
		if edgeSize, ok := vp9InterEdgeBlockSizeForRegion(miRows, miCols,
			miRow, miCol, root); ok {
			target = edgeSize
		}
	}
	if vp9ModeTreeUsesInterSegmentMap(kind) && e.vp9DynamicSegmentMapActive() {
		if activeMapSize, ok := e.pickVP9SegmentMapPartitionBlockSize(
			miRows, miCols, miRow, miCol, root, nil, inter); ok {
			return activeMapSize
		}
	}
	if kind != vp9ModeTreeInterSource || inter == nil || target != root {
		return target
	}
	return e.pickVP9InterPartitionBlockSize(inter, tile, partitionProbs,
		miRows, miCols, miRow, miCol, root)
}

func (e *VP9Encoder) pickVP9SegmentMapPartitionBlockSize(miRows, miCols, miRow, miCol int,
	root common.BlockSize, img *image.YCbCr, inter *vp9InterEncodeState,
) (common.BlockSize, bool) {
	if e == nil || !e.vp9DynamicSegmentMapActive() || root <= common.Block8x8 {
		return common.BlockInvalid, false
	}
	splitSize := common.SubsizeLookup[common.PartitionSplit][root]
	if splitSize < common.Block8x8 {
		return common.BlockInvalid, false
	}
	blockMiW := int(common.Num8x8BlocksWideLookup[root])
	blockMiH := int(common.Num8x8BlocksHighLookup[root])
	if blockMiW <= 1 && blockMiH <= 1 {
		return common.BlockInvalid, false
	}
	endRow := min(miRows, miRow+blockMiH)
	endCol := min(miCols, miCol+blockMiW)
	if miRow >= endRow || miCol >= endCol {
		return common.BlockInvalid, false
	}
	staticSegID := e.vp9StaticSegmentIDForMap()
	segID := e.vp9PartitionSegmentID(miRow, miCol, staticSegID, img, inter)
	for row := miRow; row < endRow; row++ {
		for col := miCol; col < endCol; col++ {
			if e.vp9PartitionSegmentID(row, col, staticSegID, img, inter) != segID {
				return splitSize, true
			}
		}
	}
	return common.BlockInvalid, false
}
