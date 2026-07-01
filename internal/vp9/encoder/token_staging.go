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
		return
	}
	b.Tokens = buffers.EnsureLen(b.Tokens, TokenAllocForMI(miRows, miCols))
	b.Lists = buffers.EnsureLenZeroed(b.Lists, TokenListAllocForMI(miRows))
	b.Used = 0
	b.miRows = miRows
	b.miCols = miCols
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
