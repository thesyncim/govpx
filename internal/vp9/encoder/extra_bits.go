package encoder

import "github.com/thesyncim/govpx/internal/vp9/tables"

// VP9 token-extra-bit metadata. Ported from libvpx v1.16.0
// vp9/encoder/vp9_tokenize.{h,c} — vp9_extra_bit and the
// vp9_extra_bits[ENTROPY_TOKENS] table for the 8-bit profile.
//
// pack_mb_tokens consults this table for every non-EOB coefficient
// it emits: the magnitude class index picks the (prob, len, base)
// triple, which determines how many extra bits to write and what
// their per-bit probability row is.
//
// The libvpx C struct also carries a `cost` pointer (16-bit array
// pre-computing the bit cost per leaf magnitude). That cost-side
// metadata is encoder-RD-only; this port focuses on the wire
// fragment first, with cost tables added when the encoder rate
// pipeline calls for them.

// EntropyTokens mirrors libvpx's ENTROPY_TOKENS — the count of token
// classes the coefficient stream emits.
const EntropyTokens = 12

// Token class indices, mirroring the C #defines.
const (
	ZeroToken     = 0
	OneToken      = 1
	TwoToken      = 2
	ThreeToken    = 3
	FourToken     = 4
	Category1Tok  = 5
	Category2Tok  = 6
	Category3Tok  = 7
	Category4Tok  = 8
	Category5Tok  = 9
	Category6Tok  = 10
	EobToken      = 11
	EobModelToken = 3
)

// VP9ExtraBit mirrors libvpx's vp9_extra_bit struct. Prob is the
// per-bit probability slice for the magnitude bits (nil for tokens
// that don't carry extra bits). Len is the extra-bit count (0 for
// the ZERO / ONE..FOUR / EOB tokens; 1..5 for CAT1..CAT5; 14 for
// CAT6 in the 8-bit profile). BaseVal is the starting absolute
// magnitude.
type VP9ExtraBit struct {
	Prob    []uint8
	Len     int
	BaseVal int
}

// VP9ExtraBits mirrors vp9_extra_bits[ENTROPY_TOKENS] for the 8-bit
// profile. The per-token (prob, len, base) triple flows into
// pack_mb_tokens — caller picks the class index, then writes Len
// extra bits against Prob followed by a 1-bit sign and the (base +
// extra) magnitude.
var VP9ExtraBits = [EntropyTokens]VP9ExtraBit{
	{nil, 0, 0},                  // ZeroToken
	{nil, 0, 1},                  // OneToken
	{nil, 0, 2},                  // TwoToken
	{nil, 0, 3},                  // ThreeToken
	{nil, 0, 4},                  // FourToken
	{tables.Cat1Prob[:], 1, 5},   // Category1Tok (CAT1_MIN_VAL=5)
	{tables.Cat2Prob[:], 2, 7},   // Category2Tok (CAT2_MIN_VAL=7)
	{tables.Cat3Prob[:], 3, 11},  // Category3Tok (CAT3_MIN_VAL=11)
	{tables.Cat4Prob[:], 4, 19},  // Category4Tok (CAT4_MIN_VAL=19)
	{tables.Cat5Prob[:], 5, 35},  // Category5Tok (CAT5_MIN_VAL=35)
	{tables.Cat6Prob[:], 14, 67}, // Category6Tok (CAT6_MIN_VAL=67)
	{nil, 0, 0},                  // EobToken
}

// TokenForAbsCoeff mirrors libvpx's token-class picker: given an
// absolute coefficient value, returns the (token class index,
// extra-bits value) pair pack_mb_tokens emits. ZERO / ONE..FOUR are
// represented verbatim; CAT1..CAT6 carry the value above their
// BaseVal as `Len` extra bits.
func TokenForAbsCoeff(abs int) (token int, extra int) {
	switch {
	case abs == 0:
		return ZeroToken, 0
	case abs == 1:
		return OneToken, 0
	case abs == 2:
		return TwoToken, 0
	case abs == 3:
		return ThreeToken, 0
	case abs == 4:
		return FourToken, 0
	case abs <= 6:
		return Category1Tok, abs - VP9ExtraBits[Category1Tok].BaseVal
	case abs <= 10:
		return Category2Tok, abs - VP9ExtraBits[Category2Tok].BaseVal
	case abs <= 18:
		return Category3Tok, abs - VP9ExtraBits[Category3Tok].BaseVal
	case abs <= 34:
		return Category4Tok, abs - VP9ExtraBits[Category4Tok].BaseVal
	case abs <= 66:
		return Category5Tok, abs - VP9ExtraBits[Category5Tok].BaseVal
	default:
		return Category6Tok, abs - VP9ExtraBits[Category6Tok].BaseVal
	}
}
