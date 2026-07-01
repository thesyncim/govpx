package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) collectVP9EncodeFrameCounts(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	isKey, intraOnly bool, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	counts := &e.frameCounts
	*counts = encoder.FrameCounts{}
	e.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}

	var countKey *vp9KeyframeEncodeState
	if key != nil {
		tmp := *key
		tmp.counts = counts
		countKey = &tmp
	}
	var countInter *vp9InterEncodeState
	if inter != nil {
		tmp := *inter
		tmp.counts = counts
		countInter = &tmp
	}

	// libvpx vp9/encoder/vp9_bitstream.c:378-403 — write_modes_b services
	// KEY_FRAME and intra-only frames through the same write_mb_modes_kf +
	// pack_mb_tokens dispatch (the frame_is_intra_only(cm) predicate at
	// :395-396). The govpx wire-side bitstream pass at
	// :2883-2887 already routes both `isKey` and `intraOnly` through
	// vp9ModeTreeKeyframeSource for the same reason. The count pass must
	// match so the per-block coef-counts accumulator (WriteCoefSb at
	// :6912/:7001) runs for intra-only frames too — the
	// vp9ModeTreeKeyframe fallback path at :7024+ writes the keyframe
	// block header but does not invoke WriteCoefSb, leaving
	// FrameCounts.CoefBranchStats empty for intra-only frames.
	// build_tree_distribution + update_coef_probs_common at
	// vp9_bitstream.c:519-682 ingest those branch counts, so dropping the
	// counts here starves the savings-search the same way the
	// FuzzVP9OracleEncoderRuntimeControls deferred citation describes for
	// other sparse-counts cases.
	kind := vp9ModeTreeInterSource
	if isKey || intraOnly {
		kind = vp9ModeTreeKeyframeSource
		e.resetVP9LeafKeyframeDecisionCache()
	}
	cyclicSnap := e.saveVP9CyclicRefreshMapsForCounts()
	miGridValid := e.collectVP9FrameTileCounts(width, height, miRows, miCols, tileInfo,
		partitionProbs, seg, baseMi, txMode, kind, countKey, countInter)
	if miGridValid && e.vp9ActiveSegmentMapCodingChooser() {
		e.vp9ChooseSegmentMapCodingMethod(seg, miRows, miCols, tileInfo,
			isKey || intraOnly)
	}
	e.restoreVP9CyclicRefreshMapsAfterCounts(cyclicSnap)

	e.resetVP9EncoderCodingState(width, height)
	return counts
}

func vp9SkipEncodeFrameFromCounts(header *vp9dec.UncompressedHeader, counts *encoder.FrameCounts) int {
	if header == nil || counts == nil || header.FrameType == common.KeyFrame || !header.ShowFrame {
		return 0
	}
	var intraCount, interCount uint32
	for i := range common.IntraInterContexts {
		intraCount += counts.IntraInter[i][0]
		interCount += counts.IntraInter[i][1]
	}
	if intraCount<<2 < interCount {
		return 1
	}
	return 0
}

func (e *VP9Encoder) collectVP9FrameTileCounts(width, height, miRows, miCols int,
	tileInfo vp9dec.TileInfo,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, txMode common.TxMode,
	kind vp9ModeTreeKind, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) bool {
	tileRows := 1 << uint(tileInfo.Log2TileRows)
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	if e.vp9TileWorkerThreadHint() > 1 && tileRows == 1 && tileCols > 1 {
		seed := vp9CountTileSeedForState(key, inter)
		if e.collectVP9FrameTileCountsThreaded(width, height, miRows, miCols,
			tileInfo, partitionProbs, seg, baseMi, txMode, kind, seed) {
			e.vp9TokenCollect = vp9TokenCollectState{}
			e.vp9TokenReplay = vp9TokenReplayState{}
			e.vp9TokenFrame.Reset()
			return e.vp9ActiveSegmentMapCodingChooser()
		}
	}
	collectTokens := e.beginVP9CountTokenCollection(miRows, miCols, tileRows, tileCols,
		kind)
	for tileRow := range tileRows {
		for tileCol := range tileCols {
			if collectTokens {
				e.vp9TokenCollect.tileRow = tileRow
				e.vp9TokenCollect.tileCol = tileCol
			}
			var bw bitstream.Writer
			bw.StartDiscard()
			e.writeVP9FrameTile(&bw, miRows, miCols,
				vp9EncoderTileBounds(tileRow, tileCol, miRows, miCols, tileInfo),
				partitionProbs, seg, baseMi, txMode, kind, key, inter)
			_, _ = bw.Stop()
		}
	}
	return e.finishVP9CountTokenCollection() == nil
}

// vp9ChooseSegmentMapCodingMethod mirrors libvpx's segment-map coding
// selection: count the emitted map, then choose temporal prediction only when
// its segment-id and prediction-flag cost beats coding the map directly.
func (e *VP9Encoder) vp9ChooseSegmentMapCodingMethod(seg *vp9dec.SegmentationParams,
	miRows, miCols int, tileInfo vp9dec.TileInfo, intraOnly bool,
) {
	if e == nil || seg == nil || !seg.Enabled || !seg.UpdateMap ||
		miRows <= 0 || miCols <= 0 || len(e.miGrid) < miRows*miCols {
		return
	}
	var noPredCounts [vp9dec.MaxSegments]uint32
	var tUnpredCounts [vp9dec.MaxSegments]uint32
	var temporalCounts [vp9dec.PredictionProbs][2]uint32
	tileCols := 1 << uint(tileInfo.Log2TileCols)
	for tileCol := range tileCols {
		tile := vp9EncoderTileBounds(0, tileCol, miRows, miCols, tileInfo)
		for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
			for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
				e.countVP9SegmentMapSB(miRows, miCols, tile, miRow,
					miCol, common.Block64x64, !intraOnly, &noPredCounts,
					&temporalCounts, &tUnpredCounts)
			}
		}
	}

	var noPredTree, tPredTree [vp9dec.SegTreeProbs]uint8
	vp9CalcSegTreeProbs(noPredCounts, &noPredTree)
	noPredCost := vp9CostSegMap(noPredCounts, noPredTree)
	tPredCost := int(^uint(0) >> 1)
	var predProbs [vp9dec.PredictionProbs]uint8
	if !intraOnly {
		vp9CalcSegTreeProbs(tUnpredCounts, &tPredTree)
		tPredCost = vp9CostSegMap(tUnpredCounts, tPredTree)
		for i := range predProbs {
			count0 := temporalCounts[i][0]
			count1 := temporalCounts[i][1]
			predProbs[i] = encoder.GetBinaryProb(count0, count1)
			tPredCost += int(count0)*encoder.VP9CostZero(predProbs[i]) +
				int(count1)*encoder.VP9CostOne(predProbs[i])
		}
	}
	if tPredCost < noPredCost {
		seg.TemporalUpdate = true
		seg.TreeProbs = tPredTree
		seg.PredProbs = predProbs
		return
	}
	seg.TemporalUpdate = false
	seg.TreeProbs = noPredTree
	for i := range seg.PredProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
}

func (e *VP9Encoder) countVP9SegmentMapSB(miRows, miCols int,
	tile vp9dec.TileBounds, miRow, miCol int, bsize common.BlockSize,
	allowTemporal bool, noPredCounts *[vp9dec.MaxSegments]uint32,
	temporalCounts *[vp9dec.PredictionProbs][2]uint32,
	tUnpredCounts *[vp9dec.MaxSegments]uint32,
) {
	if miRow >= miRows || miCol >= miCols || miCol >= tile.MiColEnd {
		return
	}
	mi := e.vp9MiAt(miRows, miCols, miRow, miCol)
	if mi == nil {
		return
	}
	bs := int(common.Num8x8BlocksWideLookup[bsize])
	hbs := bs >> 1
	bw := int(common.Num8x8BlocksWideLookup[mi.SbType])
	bh := int(common.Num8x8BlocksHighLookup[mi.SbType])
	if bw == bs && bh == bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	if bw == bs && bh < bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow+hbs, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	if bw < bs && bh == bs {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol+hbs,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	subsize := common.SubsizeLookup[common.PartitionSplit][bsize]
	if subsize >= common.BlockSizes || hbs <= 0 {
		e.countVP9SegmentMapBlock(miRows, miCols, tile, miRow, miCol,
			allowTemporal, noPredCounts, temporalCounts, tUnpredCounts)
		return
	}
	for dr := 0; dr <= hbs; dr += hbs {
		for dc := 0; dc <= hbs; dc += hbs {
			e.countVP9SegmentMapSB(miRows, miCols, tile, miRow+dr,
				miCol+dc, subsize, allowTemporal, noPredCounts,
				temporalCounts, tUnpredCounts)
		}
	}
}

func (e *VP9Encoder) countVP9SegmentMapBlock(miRows, miCols int,
	tile vp9dec.TileBounds, miRow, miCol int, allowTemporal bool,
	noPredCounts *[vp9dec.MaxSegments]uint32,
	temporalCounts *[vp9dec.PredictionProbs][2]uint32,
	tUnpredCounts *[vp9dec.MaxSegments]uint32,
) {
	if miRow >= miRows || miCol >= miCols || miCol >= tile.MiColEnd {
		return
	}
	mi := e.vp9MiAt(miRows, miCols, miRow, miCol)
	if mi == nil || mi.SegmentID >= vp9dec.MaxSegments {
		return
	}
	segID := mi.SegmentID
	noPredCounts[segID]++
	if !allowTemporal {
		return
	}
	left := e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	if miCol <= tile.MiColStart {
		left = nil
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	ctx := vp9dec.GetPredContextSegId(above, left)
	if ctx < 0 || ctx >= vp9dec.PredictionProbs {
		ctx = 0
	}
	predicted := uint8(0)
	if e.vp9EncoderPreviousSegmentID(miRows, miCols, miRow, miCol,
		mi.SbType) == segID {
		predicted = 1
	}
	temporalCounts[ctx][predicted]++
	if predicted == 0 {
		tUnpredCounts[segID]++
	}
	e.setVP9SegmentMapPredicted(miRows, miCols, miRow, miCol, mi.SbType,
		predicted)
}

func (e *VP9Encoder) setVP9SegmentMapPredicted(miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, predicted uint8,
) {
	if e == nil || bsize < common.Block4x4 || bsize >= common.BlockSizes ||
		miRow < 0 || miCol < 0 || miRow >= miRows || miCol >= miCols {
		return
	}
	bw := min(miCols-miCol, int(common.Num8x8BlocksWideLookup[bsize]))
	bh := min(miRows-miRow, int(common.Num8x8BlocksHighLookup[bsize]))
	if bw <= 0 || bh <= 0 {
		return
	}
	for y := range bh {
		row := (miRow + y) * miCols
		for x := range bw {
			idx := row + miCol + x
			if idx >= 0 && idx < len(e.miGrid) {
				e.miGrid[idx].SegIDPredicted = predicted
			}
		}
	}
}

func vp9CalcSegTreeProbs(segCounts [vp9dec.MaxSegments]uint32,
	probs *[vp9dec.SegTreeProbs]uint8,
) {
	c01 := segCounts[0] + segCounts[1]
	c23 := segCounts[2] + segCounts[3]
	c45 := segCounts[4] + segCounts[5]
	c67 := segCounts[6] + segCounts[7]
	probs[0] = encoder.GetBinaryProb(c01+c23, c45+c67)
	probs[1] = encoder.GetBinaryProb(c01, c23)
	probs[2] = encoder.GetBinaryProb(c45, c67)
	probs[3] = encoder.GetBinaryProb(segCounts[0], segCounts[1])
	probs[4] = encoder.GetBinaryProb(segCounts[2], segCounts[3])
	probs[5] = encoder.GetBinaryProb(segCounts[4], segCounts[5])
	probs[6] = encoder.GetBinaryProb(segCounts[6], segCounts[7])
}

func vp9CostSegMap(segCounts [vp9dec.MaxSegments]uint32,
	probs [vp9dec.SegTreeProbs]uint8,
) int {
	c01 := segCounts[0] + segCounts[1]
	c23 := segCounts[2] + segCounts[3]
	c45 := segCounts[4] + segCounts[5]
	c67 := segCounts[6] + segCounts[7]
	c0123 := c01 + c23
	c4567 := c45 + c67
	cost := int(c0123)*encoder.VP9CostZero(probs[0]) +
		int(c4567)*encoder.VP9CostOne(probs[0])
	if c0123 > 0 {
		cost += int(c01)*encoder.VP9CostZero(probs[1]) +
			int(c23)*encoder.VP9CostOne(probs[1])
		if c01 > 0 {
			cost += int(segCounts[0])*encoder.VP9CostZero(probs[3]) +
				int(segCounts[1])*encoder.VP9CostOne(probs[3])
		}
		if c23 > 0 {
			cost += int(segCounts[2])*encoder.VP9CostZero(probs[4]) +
				int(segCounts[3])*encoder.VP9CostOne(probs[4])
		}
	}
	if c4567 > 0 {
		cost += int(c45)*encoder.VP9CostZero(probs[2]) +
			int(c67)*encoder.VP9CostOne(probs[2])
		if c45 > 0 {
			cost += int(segCounts[4])*encoder.VP9CostZero(probs[5]) +
				int(segCounts[5])*encoder.VP9CostOne(probs[5])
		}
		if c67 > 0 {
			cost += int(segCounts[6])*encoder.VP9CostZero(probs[6]) +
				int(segCounts[7])*encoder.VP9CostOne(probs[6])
		}
	}
	return cost
}
