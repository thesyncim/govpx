package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

type vp9TokenCollectState struct {
	active  bool
	tileRow int
	tileCol int
	err     error
}

func vp9ModeTreeCollectsTokens(kind vp9ModeTreeKind) bool {
	return kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource
}

func (e *VP9Encoder) beginVP9CountTokenCollection(miRows, miCols, tileRows, tileCols int,
	kind vp9ModeTreeKind,
) bool {
	if e == nil || !vp9ModeTreeCollectsTokens(kind) ||
		tileRows <= 0 || tileRows > encoder.TokenStageMaxTileRows ||
		tileCols <= 0 || tileCols > encoder.TokenStageMaxTileCols {
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

func (e *VP9Encoder) finishVP9CountTokenCollection() error {
	if e == nil {
		return nil
	}
	err := e.vp9TokenCollect.err
	e.vp9TokenCollect.active = false
	return err
}

func (e *VP9Encoder) startVP9CountTokenList(tile vp9dec.TileBounds, miRow int) int {
	if e == nil || !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return -1
	}
	tileSBRow := common.AlignToSB(miRow-tile.MiRowStart) >> common.MiBlockSizeLog2
	idx, ok := e.vp9TokenFrame.StartTokenList(e.vp9TokenCollect.tileRow,
		e.vp9TokenCollect.tileCol, tileSBRow)
	if !ok {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
		return -1
	}
	return idx
}

func (e *VP9Encoder) finishVP9CountTokenList(idx int) {
	if e == nil || idx < 0 || !e.vp9TokenCollect.active ||
		e.vp9TokenCollect.err != nil {
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

func (e *VP9Encoder) finishVP9CoefTokenLeaf() {
	if e == nil || !e.vp9TokenCollect.active || e.vp9TokenCollect.err != nil {
		return
	}
	if !e.vp9TokenFrame.AppendToken(encoder.TokenExtra{Token: encoder.EOSBToken}) {
		e.vp9TokenCollect.err = encoder.ErrTokenBufferFull
	}
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
