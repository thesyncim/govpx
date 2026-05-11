package encoder

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Coefficient token packing follows libvpx v1.16.0 vp8_pack_tokens: encode the
// token tree, optional category bits, then the sign bit while keeping bool-coder
// state in locals across the tree walk.

type coefficientExtraBitEncoding struct {
	len     uint8
	baseVal int16
	probs   [11]uint8
}

var coefficientExtraBitEncodings = buildCoefficientExtraBitEncodings()

func buildCoefficientExtraBitEncodings() [tables.MaxEntropyTokens]coefficientExtraBitEncoding {
	var encodings [tables.MaxEntropyTokens]coefficientExtraBitEncoding
	for token := range tables.MaxEntropyTokens {
		extra := tables.ExtraBitsTable[token]
		encoding := &encodings[token]
		encoding.len = uint8(extra.Len)
		encoding.baseVal = extra.BaseVal
		copy(encoding.probs[:], extra.Prob)
	}
	return encodings
}

func writeBlockTokensEOB(w *BoolWriter, probs *tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	if w.err != nil {
		return w.err
	}
	if eob <= skipDC {
		w.WriteBool(0, (*probs)[blockType][skipDC][ctx][0])
		return w.Err()
	}
	if eob > 16 {
		return ErrInvalidPacketConfig
	}

	low := w.low
	rng := w.rng
	count := w.count
	pos := w.pos
	buf := w.buf

	tokenCtx := ctx
	band := skipDC
	skipEOBNode := false
	for coeffPos := skipDC; coeffPos < eob; coeffPos++ {
		zigZagPos := int(tables.DefaultZigZag1D[coeffPos])
		coeff := int(qcoeff[zigZagPos])
		// Inline of coeffToken: abs + LUT load vs the previous switch
		// + function call (gcflags -m=2 reports coeffToken as too
		// complex to inline). mag carries the absolute magnitude; sign
		// is derived directly from the signed coeff. Index 0 of the
		// LUT is tables.ZeroToken so the zero-coefficient branch falls
		// through with no special case. Out-of-range magnitudes are
		// rejected once with ErrInvalidPacketConfig.
		mag := coeff
		sign := uint8(0)
		if coeff < 0 {
			mag = -coeff
			sign = 1
		}
		if mag > tables.DCTMaxValue {
			return ErrInvalidPacketConfig
		}
		token := int(coeffAbsTokenLUT[mag])

		p := &(*probs)[blockType][band][tokenCtx]
		path := coefficientTokenBranchPaths[token]
		start := uint8(0)
		if skipEOBNode {
			start = 1
		}
		for i := start; i < path.len; i++ {
			bit := path.bits[i]
			probability := p[path.nodes[i]]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			if bit != 0 {
				low += split
				rng -= split
			} else {
				rng = split
			}

			shift := int(tables.BoolNorm[byte(rng)])
			rng <<= uint(shift)
			count += shift
			if count >= 0 {
				offset := shift - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != nil {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = ErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				buf[pos] = byte((low >> uint(24-offset)) & 0xff)
				pos++
				shift = count
				low = uint32((uint64(low) << uint(offset)) & 0xffffff)
				count -= 8
			}
			low <<= uint(shift)
		}

		if token != tables.ZeroToken {
			extra := coefficientExtraBitEncodings[token]
			extraLen := int(extra.len)
			offset := mag - int(extra.baseVal)
			for i := 0; i < extraLen; i++ {
				shiftIndex := extraLen - 1 - i
				bit := uint8((offset >> uint(shiftIndex)) & 1)
				probability := extra.probs[i]
				split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
				if bit != 0 {
					low += split
					rng -= split
				} else {
					rng = split
				}

				shift := int(tables.BoolNorm[byte(rng)])
				rng <<= uint(shift)
				count += shift
				if count >= 0 {
					offset := shift - count
					if ((low << uint(offset-1)) & 0x80000000) != 0 {
						w.pos = pos
						w.propagateCarry()
						if w.err != nil {
							return storeBlockTokenPack(w, low, rng, count, pos)
						}
					}
					if pos >= len(buf) {
						w.err = ErrBufferTooSmall
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
					buf[pos] = byte((low >> uint(24-offset)) & 0xff)
					pos++
					shift = count
					low = uint32((uint64(low) << uint(offset)) & 0xffffff)
					count -= 8
				}
				low <<= uint(shift)
			}

			split := (rng + 1) >> 1
			if sign != 0 {
				low += split
				rng -= split
			} else {
				rng = split
			}
			rng <<= 1
			if (low & 0x80000000) != 0 {
				w.pos = pos
				w.propagateCarry()
				if w.err != nil {
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
			}
			low <<= 1
			count++
			if count == 0 {
				count = -8
				if pos >= len(buf) {
					w.err = ErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				buf[pos] = byte(low >> 24)
				pos++
				low &= 0xffffff
			}
		}

		if coeffPos == 15 {
			return storeBlockTokenPack(w, low, rng, count, pos)
		}
		band = int(tables.CoefBandsTable[coeffPos+1])
		tokenCtx = int(tables.PrevTokenClass[token])
		skipEOBNode = tokenCtx == 0
	}

	if eob < 16 {
		p := &(*probs)[blockType][band][tokenCtx]
		path := coefficientTokenBranchPaths[tables.DCTEOBToken]
		for i := uint8(0); i < path.len; i++ {
			bit := path.bits[i]
			probability := p[path.nodes[i]]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			if bit != 0 {
				low += split
				rng -= split
			} else {
				rng = split
			}

			shift := int(tables.BoolNorm[byte(rng)])
			rng <<= uint(shift)
			count += shift
			if count >= 0 {
				offset := shift - count
				if ((low << uint(offset-1)) & 0x80000000) != 0 {
					w.pos = pos
					w.propagateCarry()
					if w.err != nil {
						return storeBlockTokenPack(w, low, rng, count, pos)
					}
				}
				if pos >= len(buf) {
					w.err = ErrBufferTooSmall
					return storeBlockTokenPack(w, low, rng, count, pos)
				}
				buf[pos] = byte((low >> uint(24-offset)) & 0xff)
				pos++
				shift = count
				low = uint32((uint64(low) << uint(offset)) & 0xffffff)
				count -= 8
			}
			low <<= uint(shift)
		}
	}

	return storeBlockTokenPack(w, low, rng, count, pos)
}

func storeBlockTokenPack(w *BoolWriter, low uint32, rng uint32, count int, pos int) error {
	w.low = low
	w.rng = rng
	w.count = count
	w.pos = pos
	return w.err
}
