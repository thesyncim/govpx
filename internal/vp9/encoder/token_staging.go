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
// The C struct stores context_tree, token and extra. Go stores the coefficient
// probability row indices so replay can look up the current FrameCoefProbs after
// compressed-header probability updates.
type TokenExtra struct {
	Token int16
	Extra int16

	TxSize    common.TxSize
	PlaneType uint8
	RefType   uint8
	Band      uint8
	Context   uint8
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
	Tokens []TokenExtra
	Lists  []TokenList
	Used   int

	miRows int
	miCols int
	sbRows int
}

func (b *TokenFrameBuffer) Ensure(miRows, miCols int) {
	if b == nil {
		return
	}
	if miRows <= 0 || miCols <= 0 {
		b.Reset()
		b.Tokens = b.Tokens[:0]
		b.Lists = b.Lists[:0]
		b.miRows = 0
		b.miCols = 0
		b.sbRows = 0
		return
	}
	b.Tokens = buffers.EnsureLen(b.Tokens, TokenAllocForMI(miRows, miCols))
	b.Lists = buffers.EnsureLenZeroed(b.Lists, TokenListAllocForMI(miRows))
	b.Used = 0
	b.miRows = miRows
	b.miCols = miCols
	b.sbRows = common.AlignToSB(miRows) >> common.MiBlockSizeLog2
}

func (b *TokenFrameBuffer) Reset() {
	if b == nil {
		return
	}
	b.Used = 0
	clear(b.Lists)
}

func (b *TokenFrameBuffer) Release() {
	if b == nil {
		return
	}
	b.Tokens = nil
	b.Lists = nil
	b.Used = 0
	b.miRows = 0
	b.miCols = 0
	b.sbRows = 0
}

func (b *TokenFrameBuffer) AppendToken(tok TokenExtra) bool {
	if b == nil || b.Used < 0 || b.Used >= len(b.Tokens) {
		return false
	}
	b.Tokens[b.Used] = tok
	b.Used++
	return true
}

func (b *TokenFrameBuffer) TokenListIndex(tileRow, tileCol, tileSBRow int) (int, bool) {
	if b == nil || b.sbRows <= 0 ||
		tileRow < 0 || tileRow >= TokenStageMaxTileRows ||
		tileCol < 0 || tileCol >= TokenStageMaxTileCols ||
		tileSBRow < 0 || tileSBRow >= b.sbRows {
		return 0, false
	}
	idx := (tileRow*TokenStageMaxTileCols+tileCol)*b.sbRows + tileSBRow
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
	return true
}

func (b *TokenFrameBuffer) TokensForList(list TokenList) ([]TokenExtra, bool) {
	if b == nil || list.Start < 0 || list.Stop < list.Start ||
		list.Stop > b.Used {
		return nil, false
	}
	return b.Tokens[list.Start:list.Stop], true
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
	if miRows <= 0 {
		return 0
	}
	sbRows := common.AlignToSB(miRows) >> common.MiBlockSizeLog2
	return sbRows * TokenStageMaxTileRows * TokenStageMaxTileCols
}

func alignPowerOfTwo(v, align int) int {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}
