package boolcoder

import (
	"encoding/binary"
	"errors"
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/dboolhuff.c
// - vp8/decoder/dboolhuff.h

var (
	ErrInvalidInput = errors.New("govpx: invalid VP8 boolcoder input")
	ErrTruncated    = errors.New("govpx: truncated VP8 boolcoder input")
)

const (
	valueSize  = 64
	lotsOfBits = 0x40000000
)

type Decoder struct {
	buf []byte
	pos int

	value uint64
	count int
	rng   uint32
}

func (d *Decoder) Init(src []byte) error {
	if src == nil {
		src = []byte{}
	}
	d.buf = src
	d.pos = 0
	d.value = 0
	d.count = -8
	d.rng = 255
	d.fill()
	return nil
}

// fillBoolSlow refills the 64-bit boolean decoder register when count < 0.
// Kept //go:noinline so hot coefficient loops do not inline the cold refill
// path at every readBool site (which bloats code size and spills registers).
//
//go:noinline
func fillBoolSlow(buf []byte, pos int, value uint64, count int) (uint64, int, int) {
	shift := valueSize - 8 - (count + 8)
	bytesLeft := len(buf) - pos
	if bytesLeft > 8 {
		big := binary.BigEndian.Uint64(buf[pos:])
		consumed := (shift >> 3) + 1
		value |= (big >> (uint(56-shift) & 63)) &^ (uint64(1)<<uint(shift&7) - 1)
		count += consumed << 3
		pos += consumed
		return value, count, pos
	}
	bitsLeft := bytesLeft * 8
	x := shift + 8 - bitsLeft
	loopEnd := 0
	if x >= 0 {
		count += lotsOfBits
		loopEnd = x
	}
	if x < 0 || bitsLeft != 0 {
		for shift >= loopEnd {
			count += 8
			value |= uint64(buf[pos]) << (uint(shift) & 63)
			pos++
			shift -= 8
		}
	}
	return value, count, pos
}

func (d *Decoder) ReadBool(prob uint8) uint8 {
	rng0 := d.rng
	split := uint32(1 + (((rng0 - 1) * uint32(prob)) >> 8))
	count := d.count
	if count < 0 {
		d.fill()
		count = d.count
	}

	bigsplit := uint64(split) << (valueSize - 8)
	value := d.value
	rng := split
	bit := uint8(0)
	if value >= bigsplit {
		rng = rng0 - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(bits.LeadingZeros8(uint8(rng))) & 7
	d.rng = rng << shift
	d.value = value << shift
	d.count = count - int(shift)
	return bit
}

func (d *Decoder) ReadBit() uint8 {
	rng0 := d.rng
	split := (rng0 + 1) >> 1
	count := d.count
	if count < 0 {
		d.fill()
		count = d.count
	}

	bigsplit := uint64(split) << (valueSize - 8)
	value := d.value
	rng := split
	bit := uint8(0)
	if value >= bigsplit {
		rng = rng0 - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(bits.LeadingZeros8(uint8(rng))) & 7
	d.rng = rng << shift
	d.value = value << shift
	d.count = count - int(shift)
	return bit
}

func (d *Decoder) ReadLiteral(bits int) uint32 {
	var v uint32
	for bit := bits - 1; bit >= 0; bit-- {
		v |= uint32(d.ReadBit()) << uint(bit)
	}
	return v
}

// ReadVP8BlockCoeffs decodes one VP8 coefficient block while keeping the
// entropy reader registers local for the block's full token walk.
func (d *Decoder) ReadVP8BlockCoeffs(probs *tables.CoefficientProbs, blockType int, ctx int, n int, out *[16]int16) int {
	value := d.value
	count := d.count
	rng := d.rng
	pos := d.pos
	buf := d.buf

	// fill mirrors libvpx v1.16.0 vp8/decoder/dboolhuff.c
	// vp8dx_bool_decoder_fill. When more than 8 bytes remain the byte loop
	// is replaced by a single big-endian 8-byte load that consumes the
	// identical (shift>>3)+1 bytes and produces bit-identical value/count
	// evolution; the strictly-greater guard keeps the end-of-buffer
	// VP8_LOTS_OF_BITS accounting on the byte loop.
	fill := func() {
		shift := valueSize - 8 - (count + 8)
		bytesLeft := len(buf) - pos
		if bytesLeft > 8 {
			big := binary.BigEndian.Uint64(buf[pos:])
			consumed := (shift >> 3) + 1
			value |= (big >> (uint(56-shift) & 63)) &^ (uint64(1)<<uint(shift&7) - 1)
			count += consumed << 3
			pos += consumed
			return
		}
		bitsLeft := bytesLeft * 8
		x := shift + 8 - bitsLeft
		loopEnd := 0

		if x >= 0 {
			count += lotsOfBits
			loopEnd = x
		}

		if x < 0 || bitsLeft != 0 {
			for shift >= loopEnd {
				count += 8
				value |= uint64(buf[pos]) << (uint(shift) & 63)
				pos++
				shift -= 8
			}
		}
	}

	// Keep p as a pointer into the probability table: copying the 11-byte
	// entry by value forces a stack spill whose byte-wide reloads stall on
	// store-to-load forwarding in this hottest decoder loop.
	bt := &(*probs)[blockType]
	p := &bt[n][ctx]
	rng0 := rng
	split := uint32(1 + (((rng0 - 1) * uint32(p[0])) >> 8))
	if count < 0 {
		fill()
	}

	bigsplit := uint64(split) << (valueSize - 8)
	nextRange := split
	if value >= bigsplit {
		nextRange = rng0 - split
		value -= bigsplit
	} else {
		shift := tables.BoolNorm[byte(nextRange)]
		rng = nextRange << (shift & 31)
		value <<= shift & 63
		count -= int(shift)

		d.value = value
		d.count = count
		d.rng = rng
		d.pos = pos
		return 0
	}

	shift := tables.BoolNorm[byte(nextRange)]
	rng = nextRange << (shift & 31)
	value <<= shift & 63
	count -= int(shift)
	readBool := func(prob uint8) uint8 {
		rng0 := rng
		split := uint32(1 + (((rng0 - 1) * uint32(prob)) >> 8))
		if count < 0 {
			fill()
		}

		bigsplit := uint64(split) << (valueSize - 8)
		nextRange := split
		bit := uint8(0)
		if value >= bigsplit {
			nextRange = rng0 - split
			value -= bigsplit
			bit = 1
		}

		shift := tables.BoolNorm[byte(nextRange)]
		rng = nextRange << (shift & 31)
		value <<= shift & 63
		count -= int(shift)
		return bit
	}
	// readSignedCoeff mirrors libvpx v1.16.0 vp8/decoder/detokenize.c GetSigned
	// (lines 58-79). The sign bit is encoded with probability 0.5 exactly, so
	// libvpx hard-codes the range-normalization to a single left shift rather
	// than dispatching through vp8_norm[range]. The two paths only diverge
	// when the post-decision range is 128 (input range == 255 with sign bit
	// 0): vp8_norm[128] = 0 leaves range/value/count unchanged whereas
	// GetSigned unconditionally doubles range/value and decrements count.
	// Mismatched state would cascade into later bool reads.
	readSignedCoeff := func(coeff int) int16 {
		rng0 := rng
		split := (rng0 + 1) >> 1
		if count < 0 {
			fill()
		}
		bigsplit := uint64(split) << (valueSize - 8)
		nextRange := split
		var mask int16
		if value >= bigsplit {
			nextRange = rng0 - split
			value -= bigsplit
			mask = -1
		}
		// Fixed single-bit normalization (matches libvpx GetSigned): the
		// post-decision range is always in [64, 128] for a 0.5-probability
		// split, so a one-bit shift is the correct normalization.
		rng = nextRange << 1
		value <<= 1
		count--
		v := int16(coeff)
		return (v ^ mask) - mask
	}
	readTokenCategory := func(cat int) int {
		v := 0
		switch cat {
		case 0:
			for _, prob := range tables.Cat3Prob {
				v += v + int(readBool(prob))
			}
		case 1:
			for _, prob := range tables.Cat4Prob {
				v += v + int(readBool(prob))
			}
		case 2:
			for _, prob := range tables.Cat5Prob {
				v += v + int(readBool(prob))
			}
		default:
			for _, prob := range tables.Cat6Prob {
				v += v + int(readBool(prob))
			}
		}
		return v + 3 + (8 << cat)
	}

	for {
		n++
		if readBool(p[1]) == 0 {
			if n == 16 {
				d.value = value
				d.count = count
				d.rng = rng
				d.pos = pos
				return 16
			}
			// n is 1..15 here and CoefBandsTable values are 0..7; the
			// masks are numerically no-ops that elide bounds checks.
			p = &bt[tables.CoefBandsTable[n&15]&7][0]
		} else {
			v := 0
			tokenClass := uint8(2)
			if readBool(p[2]) == 0 {
				tokenClass = 1
				v = 1
			} else {
				if readBool(p[3]) == 0 {
					if readBool(p[4]) == 0 {
						v = 2
					} else {
						v = 3 + int(readBool(p[5]))
					}
				} else {
					if readBool(p[6]) == 0 {
						if readBool(p[7]) == 0 {
							v = 5 + int(readBool(159))
						} else {
							v = 7 + 2*int(readBool(165))
							v += int(readBool(145))
						}
					} else {
						bit1 := int(readBool(p[8]))
						bit0 := int(readBool(p[9+bit1]))
						cat := 2*bit1 + bit0
						v = readTokenCategory(cat)
					}
				}
			}

			j := tables.DefaultZigZag1D[(n-1)&15]
			out[j&15] = readSignedCoeff(v)
			if n == 16 {
				d.value = value
				d.count = count
				d.rng = rng
				d.pos = pos
				return 16
			}
			p = &bt[tables.CoefBandsTable[n&15]&7][tokenClass]
			if readBool(p[0]) == 0 {
				d.value = value
				d.count = count
				d.rng = rng
				d.pos = pos
				return n
			}
			continue
		}
	}
}

// ReadVP8MacroblockCoeffs decodes every coefficient block of one macroblock
// in a single call, mirroring libvpx v1.16.0 vp8/decoder/detokenize.c
// vp8_decode_mb_tokens: the Y2 block first for non-4x4 macroblocks (with the
// luma DC token skipped), then the 16 luma blocks, then the 8 chroma blocks.
// Keeping the whole walk in one function keeps the entropy reader state
// (value/count/range/position) in registers across all block walks instead
// of reloading and spilling it around a per-block call.
//
// aboveY1..leftY2 are the entropy context planes (updated in place exactly
// as the per-block sequence would). qcoeff and eob receive the decoded
// coefficients and per-block end-of-block counts for the blocks that decode
// coefficients; skipped-by-EOB blocks only write eob and contexts, matching
// DecodeMacroblockTokens' contract over cleared token storage. The returned
// dirtyMask has bit b set when block b decoded a nonzero coefficient.
// vp8BlockFastMargin is the headroom (in bytes) required before the end of
// the token partition buffer to decode a complete 4x4 block with unconditional
// 8-byte big-endian refills. One 4x4 block has at most 16 coefficients, each
// taking at most 18 boolean reads of <=7 bits plus 1 sign bit (<= 254 bytes),
// plus 8 bytes for the final 64-bit load.
const vp8BlockFastMargin = 288

func fillBoolFast(buf []byte, pos int, value uint64, count int) (uint64, int, int) {
	shift := valueSize - 8 - (count + 8)
	big := binary.BigEndian.Uint64(buf[pos:])
	n := (shift &^ 7) + 8
	value |= (big >> uint(64-n)) << uint(shift&7)
	return value, count + n, pos + (n >> 3)
}

func vp8ReadBoolNoFill(prob uint8, value uint64, rng uint32, count int) (uint8, uint64, uint32, int) {
	split := uint32(1 + (((rng - 1) * uint32(prob)) >> 8))
	bigsplit := uint64(split) << (valueSize - 8)
	nextRange := split
	bit := uint8(0)
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		bit = 1
	}
	shift := uint32(bits.LeadingZeros8(uint8(nextRange)))
	return bit, value << shift, nextRange << shift, count - int(shift)
}

func vp8ReadSignedCoeffNoFill(coeff int, value uint64, rng uint32, count int) (int16, uint64, uint32, int) {
	split := (rng + 1) >> 1
	bigsplit := uint64(split) << (valueSize - 8)
	nextRange := split
	var m16 int16
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		m16 = -1
	}
	v := int16(coeff)
	return (v ^ m16) - m16, value << 1, nextRange << 1, count - 1
}

func decodeVP8NonZeroBlockFast(
	buf []byte, pos int, value uint64, rng uint32, count int,
	bt *[tables.CoefBands][tables.PrevCoefContexts][tables.EntropyNodes]uint8,
	p *[tables.EntropyNodes]uint8,
	nn int,
	out *[16]int16,
) (int, uint64, uint32, int, int) {
	var bit uint8
	for {
		nn++
		bit, value, rng, count = vp8ReadBoolNoFill(p[1], value, rng, count)
		if bit == 0 {
			if nn == 16 {
				return 16, value, rng, count, pos
			}
			p = &bt[tables.CoefBandsTable[nn&15]&7][0]
			if count < 16 {
				value, count, pos = fillBoolFast(buf, pos, value, count)
			}
			continue
		}
		v := 0
		tokenClass := uint8(2)
		bit, value, rng, count = vp8ReadBoolNoFill(p[2], value, rng, count)
		if bit == 0 {
			tokenClass = 1
			v = 1
		} else {
			if count < 32 {
				value, count, pos = fillBoolFast(buf, pos, value, count)
			}
			bit, value, rng, count = vp8ReadBoolNoFill(p[3], value, rng, count)
			if bit == 0 {
				bit, value, rng, count = vp8ReadBoolNoFill(p[4], value, rng, count)
				if bit == 0 {
					v = 2
				} else {
					bit, value, rng, count = vp8ReadBoolNoFill(p[5], value, rng, count)
					v = 3 + int(bit)
				}
			} else {
				if count < 32 {
					value, count, pos = fillBoolFast(buf, pos, value, count)
				}
				bit, value, rng, count = vp8ReadBoolNoFill(p[6], value, rng, count)
				if bit == 0 {
					bit, value, rng, count = vp8ReadBoolNoFill(p[7], value, rng, count)
					if bit == 0 {
						bit, value, rng, count = vp8ReadBoolNoFill(159, value, rng, count)
						v = 5 + int(bit)
					} else {
						bit, value, rng, count = vp8ReadBoolNoFill(165, value, rng, count)
						v = 7 + 2*int(bit)
						bit, value, rng, count = vp8ReadBoolNoFill(145, value, rng, count)
						v += int(bit)
					}
				} else {
					var b1, b0 uint8
					b1, value, rng, count = vp8ReadBoolNoFill(p[8], value, rng, count)
					b0, value, rng, count = vp8ReadBoolNoFill(p[9+b1], value, rng, count)
					cat := 2*int(b1) + int(b0)
					switch cat {
					case 0:
						for _, prob := range tables.Cat3Prob {
							if count < 0 {
								value, count, pos = fillBoolFast(buf, pos, value, count)
							}
							bit, value, rng, count = vp8ReadBoolNoFill(prob, value, rng, count)
							v += v + int(bit)
						}
					case 1:
						for _, prob := range tables.Cat4Prob {
							if count < 0 {
								value, count, pos = fillBoolFast(buf, pos, value, count)
							}
							bit, value, rng, count = vp8ReadBoolNoFill(prob, value, rng, count)
							v += v + int(bit)
						}
					case 2:
						for _, prob := range tables.Cat5Prob {
							if count < 0 {
								value, count, pos = fillBoolFast(buf, pos, value, count)
							}
							bit, value, rng, count = vp8ReadBoolNoFill(prob, value, rng, count)
							v += v + int(bit)
						}
					default:
						for _, prob := range tables.Cat6Prob {
							if count < 0 {
								value, count, pos = fillBoolFast(buf, pos, value, count)
							}
							bit, value, rng, count = vp8ReadBoolNoFill(prob, value, rng, count)
							v += v + int(bit)
						}
					}
					v += 3 + (8 << cat)
					if count < 0 {
						value, count, pos = fillBoolFast(buf, pos, value, count)
					}
				}
			}
		}

		j := tables.DefaultZigZag1D[(nn-1)&15]
		out[j&15], value, rng, count = vp8ReadSignedCoeffNoFill(v, value, rng, count)
		if nn == 16 {
			return 16, value, rng, count, pos
		}
		p = &bt[tables.CoefBandsTable[nn&15]&7][tokenClass]
		if count < 24 {
			value, count, pos = fillBoolFast(buf, pos, value, count)
		}
		bit, value, rng, count = vp8ReadBoolNoFill(p[0], value, rng, count)
		if bit == 0 {
			return nn, value, rng, count, pos
		}
	}
}

func (d *Decoder) ReadVP8MacroblockCoeffs(
	probs *tables.CoefficientProbs, is4x4 bool,
	aboveY1 *[4]uint8, leftY1 *[4]uint8,
	aboveU *[2]uint8, leftU *[2]uint8,
	aboveV *[2]uint8, leftV *[2]uint8,
	aboveY2 *uint8, leftY2 *uint8,
	qcoeff *[25][16]int16, eob *[25]uint8,
) (eobTotal int, dirtyMask uint32) {
	value := d.value
	count := d.count
	rng := d.rng
	pos := d.pos
	buf := d.buf
	fastLimit := len(buf) - vp8BlockFastMargin

	skipDC := 0
	lumaType := 3
	var bit uint8

	if !is4x4 {
		skipDC = 1
		lumaType = 0
		if pos > fastLimit {
			d.value, d.count, d.rng, d.pos = value, count, rng, pos
			return d.readVP8MacroblockCoeffsSlow(
				probs, is4x4, -1, eobTotal, dirtyMask,
				aboveY1, leftY1, aboveU, leftU, aboveV, leftV,
				aboveY2, leftY2, qcoeff, eob,
			)
		}
		bt := &(*probs)[1]
		p := &bt[0][int(*aboveY2+*leftY2)]
		if count < 24 {
			value, count, pos = fillBoolFast(buf, pos, value, count)
		}
		bit, value, rng, count = vp8ReadBoolNoFill(p[0], value, rng, count)
		if bit == 0 {
			*aboveY2 = 0
			*leftY2 = 0
			eob[24] = 0
			eobTotal = -16
		} else {
			var nz int
			nz, value, rng, count, pos = decodeVP8NonZeroBlockFast(buf, pos, value, rng, count, bt, p, 0, &qcoeff[24])
			has := uint8(0)
			if nz > 0 {
				has = 1
				dirtyMask |= 1 << 24
			}
			*aboveY2 = has
			*leftY2 = has
			eob[24] = uint8(nz)
			eobTotal = nz - 16
		}
	}

	btY := &(*probs)[lumaType]
	bandY := &btY[skipDC]
	for b := range 16 {
		if pos > fastLimit {
			d.value, d.count, d.rng, d.pos = value, count, rng, pos
			return d.readVP8MacroblockCoeffsSlow(
				probs, is4x4, b, eobTotal, dirtyMask,
				aboveY1, leftY1, aboveU, leftU, aboveV, leftV,
				aboveY2, leftY2, qcoeff, eob,
			)
		}
		aP := &aboveY1[b&3]
		lP := &leftY1[b>>2]
		p := &bandY[int(*aP+*lP)]
		if count < 24 {
			value, count, pos = fillBoolFast(buf, pos, value, count)
		}
		bit, value, rng, count = vp8ReadBoolNoFill(p[0], value, rng, count)
		if bit == 0 {
			*aP = 0
			*lP = 0
			eob[b] = uint8(skipDC)
			eobTotal += skipDC
			continue
		}
		var nz int
		nz, value, rng, count, pos = decodeVP8NonZeroBlockFast(buf, pos, value, rng, count, btY, p, skipDC, &qcoeff[b])
		has := uint8(0)
		if nz > 0 {
			has = 1
			dirtyMask |= 1 << uint(b)
		}
		*aP = has
		*lP = has
		eob[b] = uint8(nz + skipDC)
		eobTotal += nz + skipDC
	}

	btUV := &(*probs)[2]
	bandUV := &btUV[0]
	for i := range 4 {
		b := 16 + i
		if pos > fastLimit {
			d.value, d.count, d.rng, d.pos = value, count, rng, pos
			return d.readVP8MacroblockCoeffsSlow(
				probs, is4x4, b, eobTotal, dirtyMask,
				aboveY1, leftY1, aboveU, leftU, aboveV, leftV,
				aboveY2, leftY2, qcoeff, eob,
			)
		}
		aP := &aboveU[i&1]
		lP := &leftU[i>>1]
		p := &bandUV[int(*aP+*lP)]
		if count < 24 {
			value, count, pos = fillBoolFast(buf, pos, value, count)
		}
		bit, value, rng, count = vp8ReadBoolNoFill(p[0], value, rng, count)
		if bit == 0 {
			*aP = 0
			*lP = 0
			eob[b] = 0
			continue
		}
		var nz int
		nz, value, rng, count, pos = decodeVP8NonZeroBlockFast(buf, pos, value, rng, count, btUV, p, 0, &qcoeff[b])
		has := uint8(0)
		if nz > 0 {
			has = 1
			dirtyMask |= 1 << uint(b)
		}
		*aP = has
		*lP = has
		eob[b] = uint8(nz)
		eobTotal += nz
	}
	for i := range 4 {
		b := 20 + i
		if pos > fastLimit {
			d.value, d.count, d.rng, d.pos = value, count, rng, pos
			return d.readVP8MacroblockCoeffsSlow(
				probs, is4x4, b, eobTotal, dirtyMask,
				aboveY1, leftY1, aboveU, leftU, aboveV, leftV,
				aboveY2, leftY2, qcoeff, eob,
			)
		}
		aP := &aboveV[i&1]
		lP := &leftV[i>>1]
		p := &bandUV[int(*aP+*lP)]
		if count < 24 {
			value, count, pos = fillBoolFast(buf, pos, value, count)
		}
		bit, value, rng, count = vp8ReadBoolNoFill(p[0], value, rng, count)
		if bit == 0 {
			*aP = 0
			*lP = 0
			eob[b] = 0
			continue
		}
		var nz int
		nz, value, rng, count, pos = decodeVP8NonZeroBlockFast(buf, pos, value, rng, count, btUV, p, 0, &qcoeff[b])
		has := uint8(0)
		if nz > 0 {
			has = 1
			dirtyMask |= 1 << uint(b)
		}
		*aP = has
		*lP = has
		eob[b] = uint8(nz)
		eobTotal += nz
	}

	d.value = value
	d.count = count
	d.rng = rng
	d.pos = pos
	return eobTotal, dirtyMask
}

//go:noinline
func (d *Decoder) readVP8MacroblockCoeffsSlow(
	probs *tables.CoefficientProbs, is4x4 bool,
	startIdx, eobTotal int, dirtyMask uint32,
	aboveY1 *[4]uint8, leftY1 *[4]uint8,
	aboveU *[2]uint8, leftU *[2]uint8,
	aboveV *[2]uint8, leftV *[2]uint8,
	aboveY2 *uint8, leftY2 *uint8,
	qcoeff *[25][16]int16, eob *[25]uint8,
) (int, uint32) {
	value := d.value
	count := d.count
	rng := d.rng
	pos := d.pos
	buf := d.buf

	readBool := func(prob uint8) uint8 {
		rng0 := rng
		split := uint32(1 + (((rng0 - 1) * uint32(prob)) >> 8))
		if count < 0 {
			value, count, pos = fillBoolSlow(buf, pos, value, count)
		}

		bigsplit := uint64(split) << (valueSize - 8)
		nextRange := split
		bit := uint8(0)
		if value >= bigsplit {
			nextRange = rng0 - split
			value -= bigsplit
			bit = 1
		}

		shift := uint32(bits.LeadingZeros8(uint8(nextRange))) & 7
		rng = nextRange << shift
		value <<= shift
		count -= int(shift)
		return bit
	}
	readSignedCoeff := func(coeff int) int16 {
		rng0 := rng
		split := (rng0 + 1) >> 1
		if count < 0 {
			value, count, pos = fillBoolSlow(buf, pos, value, count)
		}
		bigsplit := uint64(split) << (valueSize - 8)
		nextRange := split
		var m16 int16
		if value >= bigsplit {
			nextRange = rng0 - split
			value -= bigsplit
			m16 = -1
		}
		rng = nextRange << 1
		value <<= 1
		count--
		v := int16(coeff)
		return (v ^ m16) - m16
	}
	readTokenCategory := func(cat int) int {
		v := 0
		switch cat {
		case 0:
			for _, prob := range tables.Cat3Prob {
				v += v + int(readBool(prob))
			}
		case 1:
			for _, prob := range tables.Cat4Prob {
				v += v + int(readBool(prob))
			}
		case 2:
			for _, prob := range tables.Cat5Prob {
				v += v + int(readBool(prob))
			}
		default:
			for _, prob := range tables.Cat6Prob {
				v += v + int(readBool(prob))
			}
		}
		return v + 3 + (8 << cat)
	}

	skipDC := 0
	lumaType := 3
	if !is4x4 {
		skipDC = 1
		lumaType = 0
	}

	for idx := startIdx; idx < 24; idx++ {
		var b int
		var bt *[tables.CoefBands][tables.PrevCoefContexts][tables.EntropyNodes]uint8
		var aP, lP *uint8
		var n int
		switch {
		case idx < 0:
			b = 24
			bt = &(*probs)[1]
			aP, lP = aboveY2, leftY2
		case idx < 16:
			b = idx
			bt = &(*probs)[lumaType]
			aP = &aboveY1[idx&3]
			lP = &leftY1[(idx&0x0c)>>2]
			n = skipDC
		default:
			b = idx
			bt = &(*probs)[2]
			if idx < 20 {
				aP = &aboveU[idx&1]
				lP = &leftU[(idx&3)>>1]
			} else {
				aP = &aboveV[idx&1]
				lP = &leftV[(idx&3)>>1]
			}
		}

		ctx := int(*aP + *lP)
		p := &bt[n&1][ctx]
		if readBool(p[0]) == 0 {
			*aP = 0
			*lP = 0
			if idx < 0 {
				eob[24] = 0
				eobTotal -= 16
			} else if idx < 16 {
				eob[b] = uint8(skipDC)
				eobTotal += skipDC
			} else {
				eob[b] = 0
			}
			continue
		}
		out := &qcoeff[b]
		nonzeros := 0
		nn := n
	walk:
		for {
			nn++
			if readBool(p[1]) == 0 {
				if nn == 16 {
					nonzeros = 16
					break walk
				}
				p = &bt[tables.CoefBandsTable[nn&15]&7][0]
				continue
			}
			v := 0
			tokenClass := uint8(2)
			if readBool(p[2]) == 0 {
				tokenClass = 1
				v = 1
			} else {
				if readBool(p[3]) == 0 {
					if readBool(p[4]) == 0 {
						v = 2
					} else {
						v = 3 + int(readBool(p[5]))
					}
				} else {
					if readBool(p[6]) == 0 {
						if readBool(p[7]) == 0 {
							v = 5 + int(readBool(159))
						} else {
							v = 7 + 2*int(readBool(165))
							v += int(readBool(145))
						}
					} else {
						bit1 := int(readBool(p[8]))
						bit0 := int(readBool(p[9+bit1]))
						cat := 2*bit1 + bit0
						v = readTokenCategory(cat)
					}
				}
			}

			j := tables.DefaultZigZag1D[(nn-1)&15]
			out[j&15] = readSignedCoeff(v)
			if nn == 16 {
				nonzeros = 16
				break walk
			}
			p = &bt[tables.CoefBandsTable[nn&15]&7][tokenClass]
			if readBool(p[0]) == 0 {
				nonzeros = nn
				break walk
			}
		}

		hasCoeffs := uint8(0)
		if nonzeros > 0 {
			hasCoeffs = 1
			dirtyMask |= 1 << uint(b)
		}
		*aP = hasCoeffs
		*lP = hasCoeffs
		switch {
		case idx < 0:
			eob[24] = uint8(nonzeros)
			eobTotal += nonzeros - 16
		case idx < 16:
			nz := nonzeros + skipDC
			eob[b] = uint8(nz)
			eobTotal += nz
		default:
			eob[b] = uint8(nonzeros)
			eobTotal += nonzeros
		}
	}

	d.value = value
	d.count = count
	d.rng = rng
	d.pos = pos
	return eobTotal, dirtyMask
}

// ReadCoefUpdateProbsInto scans the full VP8 coefficient probability update
// header in a single call. For every entry of updateProbs it reads one
// "should update" bool; on a 1, it reads an 8-bit literal and writes the new
// value into the matching probs slot. updateProbs and probs are flat views of
// the [BlockTypes][CoefBands][PrevCoefContexts][EntropyNodes] tables.
//
// nonDefaultPartitionCtx is set to true when any update lands on a ctx > 0
// slot, matching the IndependentPartitions = false signal in the parent header.
// updateCount returns the number of updates applied.
//
// The decoder state is kept in registers across the entire scan, avoiding the
// per-call pointer dereferences that the standalone ReadBool method incurs.
// updateProbs must be exactly BlockTypes*CoefBands*PrevCoefContexts*EntropyNodes
// (= 4*8*3*11 = 1056) long; probs, when non-nil, has the same length.
// nodesPerCtx is EntropyNodes (=11) and ctxsPerBand is PrevCoefContexts (=3).
func (d *Decoder) ReadCoefUpdateProbsInto(updateProbs []uint8, probs []uint8, nodesPerCtx, ctxsPerBand int) (updateCount int, nonDefaultPartitionCtx bool) {
	value := d.value
	count := d.count
	rng := d.rng
	pos := d.pos
	buf := d.buf

	for i, prob := range updateProbs {
		// Inline ReadBool for the update flag.
		rng0 := rng
		split := rng0 - 1
		if prob != 255 {
			split = uint32(1 + (((rng0 - 1) * uint32(prob)) >> 8))
		}
		if count < 0 {
			// Inline fill().
			shift := valueSize - 8 - (count + 8)
			bytesLeft := len(buf) - pos
			bitsLeft := bytesLeft * 8
			x := shift + 8 - bitsLeft
			loopEnd := 0
			if x >= 0 {
				count += lotsOfBits
				loopEnd = x
			}
			if x < 0 || bitsLeft != 0 {
				for shift >= loopEnd {
					count += 8
					value |= uint64(buf[pos]) << (uint(shift) & 63)
					pos++
					shift -= 8
				}
			}
		}

		bigsplit := uint64(split) << (valueSize - 8)
		nextRange := split
		var bit uint8
		if value >= bigsplit {
			nextRange = rng0 - split
			value -= bigsplit
			bit = 1
		}
		s := tables.BoolNorm[byte(nextRange)]
		rng = nextRange << (s & 31)
		value <<= s & 63
		count -= int(s)

		if bit == 0 {
			continue
		}

		// Read an 8-bit literal using inline ReadBit calls.
		var lit uint32
		for k := 7; k >= 0; k-- {
			rng0 = rng
			split = (rng0 + 1) >> 1
			if count < 0 {
				shift := valueSize - 8 - (count + 8)
				bytesLeft := len(buf) - pos
				bitsLeft := bytesLeft * 8
				x := shift + 8 - bitsLeft
				loopEnd := 0
				if x >= 0 {
					count += lotsOfBits
					loopEnd = x
				}
				if x < 0 || bitsLeft != 0 {
					for shift >= loopEnd {
						count += 8
						value |= uint64(buf[pos]) << (uint(shift) & 63)
						pos++
						shift -= 8
					}
				}
			}
			bigsplit = uint64(split) << (valueSize - 8)
			nextRange = split
			var lbit uint32
			if value >= bigsplit {
				nextRange = rng0 - split
				value -= bigsplit
				lbit = 1
			}
			s = tables.BoolNorm[byte(nextRange)]
			rng = nextRange << (s & 31)
			value <<= s & 63
			count -= int(s)
			lit |= lbit << uint(k)
		}

		if probs != nil {
			probs[i] = uint8(lit)
		}
		updateCount++
		// node = i % nodesPerCtx; ctx = (i / nodesPerCtx) % ctxsPerBand.
		if (i/nodesPerCtx)%ctxsPerBand != 0 {
			nonDefaultPartitionCtx = true
		}
	}

	d.value = value
	d.count = count
	d.rng = rng
	d.pos = pos
	return updateCount, nonDefaultPartitionCtx
}

func (d *Decoder) Err() error {
	if d.count > valueSize && d.count < lotsOfBits {
		return ErrTruncated
	}
	return nil
}

func (d *Decoder) Corrupted() bool {
	return d.Err() != nil
}

func (d *Decoder) fill() {
	shift := valueSize - 8 - (d.count + 8)
	bytesLeft := len(d.buf) - d.pos
	if bytesLeft > 8 {
		// Batched form of the byte loop below (see ReadVP8BlockCoeffs):
		// bit-identical when more than 8 bytes remain.
		big := binary.BigEndian.Uint64(d.buf[d.pos:])
		consumed := (shift >> 3) + 1
		d.value |= (big >> (uint(56-shift) & 63)) &^ (uint64(1)<<uint(shift&7) - 1)
		d.count += consumed << 3
		d.pos += consumed
		return
	}
	bitsLeft := bytesLeft * 8
	x := shift + 8 - bitsLeft
	loopEnd := 0

	if x >= 0 {
		d.count += lotsOfBits
		loopEnd = x
	}

	if x < 0 || bitsLeft != 0 {
		for shift >= loopEnd {
			d.count += 8
			d.value |= uint64(d.buf[d.pos]) << (uint(shift) & 63)
			d.pos++
			shift -= 8
		}
	}
}
