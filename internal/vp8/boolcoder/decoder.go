package boolcoder

import (
	"errors"

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

	shift := tables.BoolNorm[byte(rng)]
	rng <<= shift
	value <<= shift
	count -= int(shift)

	d.value = value
	d.count = count
	d.rng = rng
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

	shift := tables.BoolNorm[byte(rng)]
	rng <<= shift
	value <<= shift
	count -= int(shift)

	d.value = value
	d.count = count
	d.rng = rng
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

	p := (*probs)[blockType][n][ctx]
	rng0 := rng
	split := uint32(1 + (((rng0 - 1) * uint32(p[0])) >> 8))
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
				value |= uint64(buf[pos]) << uint(shift)
				pos++
				shift -= 8
			}
		}
	}

	bigsplit := uint64(split) << (valueSize - 8)
	nextRange := split
	if value >= bigsplit {
		nextRange = rng0 - split
		value -= bigsplit
	} else {
		shift := tables.BoolNorm[byte(nextRange)]
		rng = nextRange << shift
		value <<= shift
		count -= int(shift)

		d.value = value
		d.count = count
		d.rng = rng
		d.pos = pos
		return 0
	}

	shift := tables.BoolNorm[byte(nextRange)]
	rng = nextRange << shift
	value <<= shift
	count -= int(shift)

	fill := func() {
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
				value |= uint64(buf[pos]) << uint(shift)
				pos++
				shift -= 8
			}
		}
	}
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
		rng = nextRange << shift
		value <<= shift
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
			p = (*probs)[blockType][tables.CoefBandsTable[n]][0]
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

			j := tables.DefaultZigZag1D[n-1]
			out[j] = readSignedCoeff(v)
			if n == 16 {
				d.value = value
				d.count = count
				d.rng = rng
				d.pos = pos
				return 16
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][tokenClass]
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
					value |= uint64(buf[pos]) << uint(shift)
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
		rng = nextRange << s
		value <<= s
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
						value |= uint64(buf[pos]) << uint(shift)
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
			rng = nextRange << s
			value <<= s
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
			d.value |= uint64(d.buf[d.pos]) << uint(shift)
			d.pos++
			shift -= 8
		}
	}
}
