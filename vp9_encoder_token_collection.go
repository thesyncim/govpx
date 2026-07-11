package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9TokenCollectState struct {
	active        bool
	tileRow       int
	tileCol       int
	listSBRowBase int
	err           error
}

type vp9TokenReplayState struct {
	active          bool
	tileRow         int
	tileCol         int
	frame           *encoder.TokenFrameBuffer
	tokens          []encoder.TokenExtra
	cursor          int
	leafModes       []uint8
	leafCursor      int
	partitions      []uint8
	partitionCursor int
	err             error
}

func vp9ModeTreeCollectsTokens(kind vp9ModeTreeKind) bool {
	return kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource
}

func (e *VP9Encoder) vp9CountTokenCollectionEligible(tileRows, tileCols int,
	kind vp9ModeTreeKind,
) bool {
	return e != nil && vp9ModeTreeCollectsTokens(kind) &&
		!e.svc.UseSvc &&
		e.sf.TxSizeSearchMethod != UseFullRD &&
		tileRows > 0 && tileRows <= encoder.TokenStageMaxTileRows &&
		tileCols > 0 && tileCols <= encoder.TokenStageMaxTileCols
}

func (e *VP9Encoder) beginVP9CountTokenCollection(miRows, miCols, tileRows, tileCols int,
	kind vp9ModeTreeKind,
) bool {
	if e != nil {
		e.vp9ThreadedTokenReplayReady = false
	}
	if !e.vp9CountTokenCollectionEligible(tileRows, tileCols, kind) {
		if e != nil {
			e.vp9TokenCollect = vp9TokenCollectState{}
			e.vp9TokenFrame.Reset()
		}
		return false
	}
	e.vp9TokenFrame.Ensure(miRows, miCols)
	if len(e.vp9TokenFrame.Tokens) == 0 || len(e.vp9TokenFrame.Lists) == 0 {
		e.vp9TokenCollect = vp9TokenCollectState{}
		return false
	}
	e.vp9TokenCollect = vp9TokenCollectState{active: true}
	return true
}

func (e *VP9Encoder) beginVP9ThreadedCountTokenCollection(pool *vp9TileWorkerPool,
	miRows, miCols, tileRows, tileCols int, kind vp9ModeTreeKind,
) bool {
	if e != nil {
		e.vp9ThreadedTokenReplayReady = false
	}
	if !e.vp9CountTokenCollectionEligible(tileRows, tileCols, kind) ||
		pool == nil || tileCols <= 1 || len(pool.countTokens) < tileCols {
		if e != nil {
			e.vp9TokenCollect = vp9TokenCollectState{}
			e.vp9TokenReplay = vp9TokenReplayState{}
			e.vp9TokenFrame.Reset()
		}
		return false
	}
	e.vp9TokenFrame.Reset()
	e.vp9TokenCollect = vp9TokenCollectState{}
	e.vp9TokenReplay = vp9TokenReplayState{}
	return true
}

func (e *VP9Encoder) finishVP9ThreadedCountTokenCollection(pool *vp9TileWorkerPool,
	miRows, tileCols int,
) bool {
	if e == nil || pool == nil || tileCols <= 0 || len(pool.countTokens) < tileCols {
		return false
	}
	for tileCol := range tileCols {
		worker := &pool.workers[tileCol]
		if err := worker.finishVP9CountTokenCollection(); err != nil {
			e.vp9ThreadedTokenReplayReady = false
			return false
		}
		pool.countTokens[tileCol] = worker.vp9TokenFrame
		worker.vp9TokenCollect = vp9TokenCollectState{}
	}
	if !e.vp9ThreadedCountTokenFramesReady(pool, miRows, tileCols) {
		e.vp9ThreadedTokenReplayReady = false
		return false
	}
	e.vp9ThreadedTokenReplayReady = true
	return true
}

func (e *VP9Encoder) vp9ThreadedCountTokenFramesReady(pool *vp9TileWorkerPool,
	miRows, tileCols int,
) bool {
	if e == nil || pool == nil || tileCols <= 0 || len(pool.countTokens) < tileCols {
		return false
	}
	sbRows := common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	if sbRows <= 0 {
		return false
	}
	for tileCol := range tileCols {
		src := &pool.countTokens[tileCol]
		if src.Used <= 0 {
			return false
		}
		for tileSBRow := range sbRows {
			srcIdx, ok := src.TokenListIndex(0, tileCol, tileSBRow)
			if !ok {
				return false
			}
			list := src.Lists[srcIdx]
			tokens, ok := src.TokensForList(list)
			leafModes, leafOK := src.LeafModesForList(src.LeafLists[srcIdx])
			partitions, partitionOK := src.PartitionsForList(src.PartitionLists[srcIdx])
			if !ok || len(tokens) == 0 || !leafOK || len(leafModes) == 0 ||
				len(leafModes) != vp9TokenListEOSBCount(tokens) ||
				!partitionOK || len(partitions) == 0 {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) finishVP9CountTokenCollection() error {
	if e == nil {
		return nil
	}
	err := e.vp9TokenCollect.err
	e.vp9TokenCollect.active = false
	if err != nil {
		e.vp9TokenFrame.Reset()
	}
	return err
}

func (e *VP9Encoder) beginVP9TokenReplay(tileRows, tileCols int,
	kind vp9ModeTreeKind,
) bool {
	if e == nil || !vp9ModeTreeCollectsTokens(kind) ||
		e.sf.TxSizeSearchMethod == UseFullRD ||
		tileRows <= 0 || tileRows > encoder.TokenStageMaxTileRows ||
		tileCols <= 0 || tileCols > encoder.TokenStageMaxTileCols ||
		!e.vp9HasCountTokensForReplay() {
		if e != nil {
			e.vp9TokenReplay = vp9TokenReplayState{}
		}
		return false
	}
	e.vp9TokenReplay = vp9TokenReplayState{active: true}
	return true
}

func (e *VP9Encoder) vp9HasCountTokensForReplay() bool {
	return e != nil &&
		(e.vp9ThreadedTokenReplayReady ||
			(e.vp9TokenFrame.Used > 0 && e.vp9TokenFrame.LeafUsed > 0 &&
				e.vp9TokenFrame.PartitionUsed > 0 &&
				len(e.vp9TokenFrame.Lists) > 0))
}

func (e *VP9Encoder) finishVP9TokenReplay() error {
	if e == nil {
		return nil
	}
	err := e.vp9TokenReplay.err
	e.vp9TokenReplay = vp9TokenReplayState{}
	return err
}

func (e *VP9Encoder) startVP9CountTokenList(tile vp9dec.TileBounds, miRow int) int {
	if e == nil {
		return -1
	}
	tileSBRow := common.AlignToSB(miRow-tile.MiRowStart) >> common.MiBlockSizeLog2
	if e.vp9TokenReplay.active {
		if e.vp9TokenReplay.err != nil {
			return -1
		}
		frame := &e.vp9TokenFrame
		if e.vp9TokenReplay.frame != nil {
			frame = e.vp9TokenReplay.frame
		}
		idx, ok := frame.TokenListIndex(e.vp9TokenReplay.tileRow,
			e.vp9TokenReplay.tileCol, tileSBRow)
		if !ok {
			e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
			return -1
		}
		tokens, ok := frame.TokensForList(frame.Lists[idx])
		leafModes, leafOK := frame.LeafModesForList(frame.LeafLists[idx])
		partitions, partitionOK := frame.PartitionsForList(frame.PartitionLists[idx])
		if !ok || len(tokens) == 0 || !leafOK || len(leafModes) == 0 ||
			!partitionOK || len(partitions) == 0 {
			e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
			return -1
		}
		e.vp9TokenReplay.tokens = tokens
		e.vp9TokenReplay.cursor = 0
		e.vp9TokenReplay.leafModes = leafModes
		e.vp9TokenReplay.leafCursor = 0
		e.vp9TokenReplay.partitions = partitions
		e.vp9TokenReplay.partitionCursor = 0
		return idx
	}
	if !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return -1
	}
	tileSBRow -= e.vp9TokenCollect.listSBRowBase
	idx, ok := e.vp9TokenFrame.StartTokenList(e.vp9TokenCollect.tileRow,
		e.vp9TokenCollect.tileCol, tileSBRow)
	if !ok {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
		return -1
	}
	return idx
}

func (e *VP9Encoder) finishVP9CountTokenList(idx int) {
	if e == nil || idx < 0 {
		return
	}
	if e.vp9TokenReplay.active {
		if e.vp9TokenReplay.err == nil &&
			(e.vp9TokenReplay.cursor != len(e.vp9TokenReplay.tokens) ||
				e.vp9TokenReplay.leafCursor != len(e.vp9TokenReplay.leafModes) ||
				e.vp9TokenReplay.partitionCursor != len(e.vp9TokenReplay.partitions)) {
			e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
		}
		e.vp9TokenReplay.tokens = nil
		e.vp9TokenReplay.cursor = 0
		e.vp9TokenReplay.leafModes = nil
		e.vp9TokenReplay.leafCursor = 0
		e.vp9TokenReplay.partitions = nil
		e.vp9TokenReplay.partitionCursor = 0
		return
	}
	if !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return
	}
	if !e.vp9TokenFrame.FinishTokenList(idx) {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
}

func (e *VP9Encoder) collectVP9CoefTokensArgs(args *encoder.WriteCoefSbArgs) bool {
	if e == nil || args == nil || !e.vp9TokenCollect.active ||
		e.vp9TokenCollect.err != nil {
		return false
	}
	args.TokenDst = e.vp9TokenFrame.Tokens
	args.TokenIndex = &e.vp9TokenFrame.Used
	args.TokenOnly = true
	return true
}

func (e *VP9Encoder) stageVP9PackedLeafMode(mode common.PredictionMode) {
	if e == nil || !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return
	}
	if int(mode) >= common.IntraModes ||
		!e.vp9TokenFrame.AppendLeafMode(uint8(mode)) {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
}

func (e *VP9Encoder) peekVP9PackedLeafMode() (common.PredictionMode, bool) {
	if e == nil || !e.vp9TokenReplay.active || e.vp9TokenReplay.err != nil ||
		e.vp9TokenReplay.leafCursor < 0 ||
		e.vp9TokenReplay.leafCursor >= len(e.vp9TokenReplay.leafModes) {
		return 0, false
	}
	mode := common.PredictionMode(e.vp9TokenReplay.leafModes[e.vp9TokenReplay.leafCursor])
	return mode, int(mode) < common.IntraModes
}

func (e *VP9Encoder) consumeVP9PackedLeafMode() {
	if e == nil || !e.vp9TokenReplay.active || e.vp9TokenReplay.err != nil {
		return
	}
	if _, ok := e.peekVP9PackedLeafMode(); !ok {
		e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
		return
	}
	e.vp9TokenReplay.leafCursor++
}

func (e *VP9Encoder) stageVP9PackedPartition(partition common.PartitionType) {
	if e == nil || !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return
	}
	if partition >= common.PartitionTypes ||
		!e.vp9TokenFrame.AppendPartition(uint8(partition)) {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
}

func (e *VP9Encoder) nextVP9PackedPartition() (common.PartitionType, bool) {
	if e == nil || !e.vp9TokenReplay.active || e.vp9TokenReplay.err != nil ||
		e.vp9TokenReplay.partitionCursor < 0 ||
		e.vp9TokenReplay.partitionCursor >= len(e.vp9TokenReplay.partitions) {
		return common.PartitionInvalid, false
	}
	partition := common.PartitionType(
		e.vp9TokenReplay.partitions[e.vp9TokenReplay.partitionCursor])
	if partition >= common.PartitionTypes {
		e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
		return common.PartitionInvalid, false
	}
	e.vp9TokenReplay.partitionCursor++
	return partition, true
}

func (e *VP9Encoder) finishVP9CoefTokenLeaf() {
	if e == nil {
		return
	}
	if e.vp9TokenReplay.active {
		e.consumeVP9ReplayEOSBLeaf()
		e.consumeVP9PackedLeafMode()
		return
	}
	if !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return
	}
	if !e.vp9TokenFrame.AppendToken(encoder.TokenExtra{Token: encoder.EOSBToken}) {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
}

func (e *VP9Encoder) packVP9ReplayCoefTokenLeaf(bw *bitstream.Writer) bool {
	return e.packVP9ReplayCoefTokenLeafWithContexts(bw, nil)
}

func (e *VP9Encoder) packVP9ReplayCoefTokenLeafWithContexts(bw *bitstream.Writer,
	coefArgs *encoder.WriteCoefSbArgs,
) bool {
	if e == nil || bw == nil || !e.vp9TokenReplay.active {
		return false
	}
	if e.vp9TokenReplay.err != nil {
		return true
	}
	tokens := e.vp9TokenReplay.tokens[e.vp9TokenReplay.cursor:]
	if coefArgs != nil {
		n, err := encoder.PackTokensAndCommitCoefSbContexts(bw, tokens,
			&e.fc.CoefProbs, *coefArgs)
		if err != nil {
			e.vp9TokenReplay.err = err
			return true
		}
		e.vp9TokenReplay.cursor += n
		e.consumeVP9PackedLeafMode()
		return true
	}
	n := encoder.PackTokens(bw, tokens, &e.fc.CoefProbs)
	if n <= 0 || n > len(tokens) ||
		tokens[n-1].Token != encoder.EOSBToken {
		e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
		return true
	}
	e.vp9TokenReplay.cursor += n
	e.consumeVP9PackedLeafMode()
	return true
}

func (e *VP9Encoder) consumeVP9ReplayEOSBLeaf() {
	if e == nil || !e.vp9TokenReplay.active || e.vp9TokenReplay.err != nil {
		return
	}
	if e.vp9TokenReplay.cursor >= len(e.vp9TokenReplay.tokens) ||
		e.vp9TokenReplay.tokens[e.vp9TokenReplay.cursor].Token != encoder.EOSBToken {
		e.vp9TokenReplay.err = encoder.ErrTokenBufferFull
		return
	}
	e.vp9TokenReplay.cursor++
}

func (e *VP9Encoder) vp9CountTokenListForTileSBRow(tileRow, tileCol, tileSBRow int) (
	encoder.TokenList, bool,
) {
	if e == nil {
		return encoder.TokenList{}, false
	}
	idx, ok := e.vp9TokenFrame.TokenListIndex(tileRow, tileCol, tileSBRow)
	if !ok || idx >= len(e.vp9TokenFrame.Lists) {
		return encoder.TokenList{}, false
	}
	list := e.vp9TokenFrame.Lists[idx]
	if list.Count == 0 {
		return encoder.TokenList{}, false
	}
	return list, true
}

func vp9TokenListEOSBCount(tokens []encoder.TokenExtra) int {
	count := 0
	for _, tok := range tokens {
		if tok.Token == encoder.EOSBToken {
			count++
		}
	}
	return count
}

func vp9TokenListHasOnlyEOSBTerminatedLeaves(tokens []encoder.TokenExtra) bool {
	if len(tokens) == 0 {
		return false
	}
	return tokens[len(tokens)-1].Token == encoder.EOSBToken
}

func vp9TokenCollectionPlaneDequant(dq *vp9dec.DequantTables, segID int) [vp9dec.MaxMbPlane][2]int16 {
	return [vp9dec.MaxMbPlane][2]int16{
		dq.Y[segID],
		dq.Uv[segID],
		dq.Uv[segID],
	}
}
