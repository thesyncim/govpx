package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

const (
	// Mirrors libvpx VP9 MAX_TILE_ROWS / MAX_TILE_COLS token-list backing.
	TokenStageMaxTileRows = 4
	TokenStageMaxTileCols = 1 << 6

	// EOSBToken mirrors EOSB_TOKEN. It is not signalled; it only terminates
	// a staged SB stream while replaying tokens.
	EOSBToken = 127
)

// TokenExtra mirrors libvpx TOKENEXTRA with Go-friendly probability addressing.
// The C struct stores context_tree, token and extra. Go stores the flat byte
// offset of the token's [UnconstrainedNodes]uint8 probability row inside
// FrameCoefProbs — precomputed from (tx_size, plane_type, ref_type, band, ctx)
// at stage time — so replay can look up the current FrameCoefProbs after
// compressed-header probability updates without re-walking the 5-level table.
type TokenExtra struct {
	Token int16
	Extra int16

	// ProbOff = ((tx*CoefPlaneTypes+pt)*CoefRefTypes+ref)*CoefBands*
	// CoefContexts*UnconstrainedNodes + (band*CoefContexts+ctx)*
	// UnconstrainedNodes. Always a multiple of UnconstrainedNodes and
	// < len(FrameCoefProbs) flattened.
	ProbOff uint16
}

// TokenList mirrors libvpx TOKENLIST. Start/Stop are indices into
// TokenFrameBuffer.Tokens rather than C pointers.
type TokenList struct {
	Start int
	Stop  int
	Count uint32
}

// TokenFrameBuffer owns the per-frame token arena and SB-row token lists used
// by the staged VP9 bitstream path.
type TokenFrameBuffer struct {
	Tokens    []TokenExtra
	Lists     []TokenList
	LeafModes []uint8
	LeafLists []TokenList
	Used      int
	LeafUsed  int

	miRows          int
	miCols          int
	sbRows          int
	listTileRowBase int
	listTileColBase int
	listTileRows    int
	listTileCols    int
}

func (b *TokenFrameBuffer) Ensure(miRows, miCols int) {
	b.ensure(miRows, miCols, 0, 0, TokenStageMaxTileRows, TokenStageMaxTileCols)
}

func (b *TokenFrameBuffer) EnsureForTile(miRows, miCols, tileRow, tileCol int) {
	b.ensure(miRows, miCols, tileRow, tileCol, 1, 1)
}

func (b *TokenFrameBuffer) ensure(miRows, miCols, tileRowBase, tileColBase, tileRows, tileCols int) {
	if b == nil {
		return
	}
	if miRows <= 0 || miCols <= 0 ||
		tileRows <= 0 || tileCols <= 0 ||
		tileRowBase < 0 || tileColBase < 0 ||
		tileRowBase+tileRows > TokenStageMaxTileRows ||
		tileColBase+tileCols > TokenStageMaxTileCols {
		b.Reset()
		b.Tokens = b.Tokens[:0]
		b.Lists = b.Lists[:0]
		b.LeafModes = b.LeafModes[:0]
		b.LeafLists = b.LeafLists[:0]
		b.miRows = 0
		b.miCols = 0
		b.sbRows = 0
		b.listTileRowBase = 0
		b.listTileColBase = 0
		b.listTileRows = 0
		b.listTileCols = 0
		return
	}
	b.Tokens = buffers.EnsureLen(b.Tokens, TokenAllocForMI(miRows, miCols))
	b.Lists = buffers.EnsureLenZeroed(b.Lists,
		TokenListAllocForTileGrid(miRows, tileRows, tileCols))
	b.LeafModes = buffers.EnsureLen(b.LeafModes, miRows*miCols)
	b.LeafLists = buffers.EnsureLenZeroed(b.LeafLists, len(b.Lists))
	b.Used = 0
	b.LeafUsed = 0
	b.miRows = miRows
	b.miCols = miCols
	b.sbRows = common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	b.listTileRowBase = tileRowBase
	b.listTileColBase = tileColBase
	b.listTileRows = tileRows
	b.listTileCols = tileCols
}

func (b *TokenFrameBuffer) Reset() {
	if b == nil {
		return
	}
	b.Used = 0
	b.LeafUsed = 0
	clear(b.Lists)
	clear(b.LeafLists)
}

func (b *TokenFrameBuffer) Release() {
	if b == nil {
		return
	}
	b.Tokens = nil
	b.Lists = nil
	b.LeafModes = nil
	b.LeafLists = nil
	b.Used = 0
	b.LeafUsed = 0
	b.miRows = 0
	b.miCols = 0
	b.sbRows = 0
	b.listTileRowBase = 0
	b.listTileColBase = 0
	b.listTileRows = 0
	b.listTileCols = 0
}

func (b *TokenFrameBuffer) AppendToken(tok TokenExtra) bool {
	if b == nil || b.Used < 0 || b.Used >= len(b.Tokens) {
		return false
	}
	b.Tokens[b.Used] = tok
	b.Used++
	return true
}

func (b *TokenFrameBuffer) AppendLeafMode(mode uint8) bool {
	if b == nil || b.LeafUsed < 0 || b.LeafUsed >= len(b.LeafModes) {
		return false
	}
	b.LeafModes[b.LeafUsed] = mode
	b.LeafUsed++
	return true
}

func (b *TokenFrameBuffer) TokenListIndex(tileRow, tileCol, tileSBRow int) (int, bool) {
	if b == nil || b.sbRows <= 0 ||
		b.listTileRows <= 0 || b.listTileCols <= 0 ||
		tileRow < b.listTileRowBase ||
		tileRow >= b.listTileRowBase+b.listTileRows ||
		tileCol < b.listTileColBase ||
		tileCol >= b.listTileColBase+b.listTileCols ||
		tileSBRow < 0 || tileSBRow >= b.sbRows {
		return 0, false
	}
	localTileRow := tileRow - b.listTileRowBase
	localTileCol := tileCol - b.listTileColBase
	idx := (localTileRow*b.listTileCols+localTileCol)*b.sbRows + tileSBRow
	if idx < 0 || idx >= len(b.Lists) {
		return 0, false
	}
	return idx, true
}

func (b *TokenFrameBuffer) StartTokenList(tileRow, tileCol, tileSBRow int) (int, bool) {
	idx, ok := b.TokenListIndex(tileRow, tileCol, tileSBRow)
	if !ok {
		return 0, false
	}
	b.Lists[idx] = TokenList{Start: b.Used}
	b.LeafLists[idx] = TokenList{Start: b.LeafUsed}
	return idx, true
}

func (b *TokenFrameBuffer) FinishTokenList(idx int) bool {
	if b == nil || idx < 0 || idx >= len(b.Lists) {
		return false
	}
	l := &b.Lists[idx]
	if l.Start < 0 || l.Start > b.Used {
		return false
	}
	l.Stop = b.Used
	l.Count = uint32(l.Stop - l.Start)
	leaf := &b.LeafLists[idx]
	if leaf.Start < 0 || leaf.Start > b.LeafUsed {
		return false
	}
	leaf.Stop = b.LeafUsed
	leaf.Count = uint32(leaf.Stop - leaf.Start)
	return true
}

func (b *TokenFrameBuffer) TokensForList(list TokenList) ([]TokenExtra, bool) {
	if b == nil || list.Start < 0 || list.Stop < list.Start ||
		list.Stop > b.Used {
		return nil, false
	}
	return b.Tokens[list.Start:list.Stop], true
}

func (b *TokenFrameBuffer) LeafModesForList(list TokenList) ([]uint8, bool) {
	if b == nil || list.Start < 0 || list.Stop < list.Start ||
		list.Stop > b.LeafUsed {
		return nil, false
	}
	return b.LeafModes[list.Start:list.Stop], true
}

// TokenAllocForMI mirrors libvpx get_token_alloc. miRows/miCols are VP9 8x8
// mode-info units; libvpx's mb_rows/mb_cols are 16x16 units.
func TokenAllocForMI(miRows, miCols int) int {
	if miRows <= 0 || miCols <= 0 {
		return 0
	}
	mbRows := (miRows + 1) >> 1
	mbCols := (miCols + 1) >> 1
	align := 1 << (common.MiBlockSizeLog2 - 1)
	alignedRows := alignPowerOfTwo(mbRows, align)
	alignedCols := alignPowerOfTwo(mbCols, align)
	return alignedRows * alignedCols * (16*16*3 + 4)
}

// TokenListAllocForMI mirrors libvpx's tplist backing allocation:
// sb_rows * MAX_TILE_ROWS * MAX_TILE_COLS.
func TokenListAllocForMI(miRows int) int {
	return TokenListAllocForTileGrid(miRows, TokenStageMaxTileRows, TokenStageMaxTileCols)
}

func TokenListAllocForTileGrid(miRows, tileRows, tileCols int) int {
	if miRows <= 0 {
		return 0
	}
	sbRows := common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	if tileRows <= 0 || tileCols <= 0 {
		return 0
	}
	return sbRows * tileRows * tileCols
}

func alignPowerOfTwo(v, align int) int {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}
