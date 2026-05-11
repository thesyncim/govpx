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

// MaxCoefficientTokenRecordsPerMacroblock is the maximum number of coefficient
// token records a non-skipped VP8 macroblock can emit. It is 384 for both
// whole-block and 4x4-token modes:
//   - whole-block: Y2 16 + Y1 16*15 + UV 8*16
//   - 4x4-token:  Y1 16*16 + UV 8*16
const MaxCoefficientTokenRecordsPerMacroblock = 384

// CoefficientTokenRecord is a compact, probability-independent coefficient
// token prepared during accepted-MB reconstruction and later emitted after
// coefficient probability updates are finalized.
type CoefficientTokenRecord uint32

const (
	coefficientTokenRecordTokenShift              = 0
	coefficientTokenRecordBlockTypeShift          = 4
	coefficientTokenRecordBandShift               = 6
	coefficientTokenRecordContextShift            = 9
	coefficientTokenRecordMagnitudeShift          = 11
	coefficientTokenRecordSignShift               = 23
	coefficientTokenRecordSkipEOBNodeShift        = 24
	coefficientTokenRecordTokenMask        uint32 = 0x0f
	coefficientTokenRecordTwoBitMask       uint32 = 0x03
	coefficientTokenRecordBandMask         uint32 = 0x07
	coefficientTokenRecordMagMask          uint32 = 0x0fff
)

type InterCoefficientTokenRecords struct {
	Records   []CoefficientTokenRecord
	RowStarts []int
	Rows      int
}

func ResetInterCoefficientTokenRecords(records *InterCoefficientTokenRecords, rows int, macroblocks int) {
	if records == nil {
		return
	}
	if rows < 0 {
		rows = 0
	}
	if macroblocks < 0 {
		macroblocks = 0
	}
	records.Rows = rows
	if cap(records.RowStarts) < rows+1 {
		records.RowStarts = make([]int, rows+1)
	} else {
		records.RowStarts = records.RowStarts[:rows+1]
		clear(records.RowStarts)
	}
	capacity := macroblocks * MaxCoefficientTokenRecordsPerMacroblock
	if cap(records.Records) < capacity {
		records.Records = make([]CoefficientTokenRecord, 0, capacity)
	} else {
		records.Records = records.Records[:0]
	}
}

func MarkInterCoefficientTokenRecordRowStart(records *InterCoefficientTokenRecords, row int) {
	if records == nil || row < 0 || row >= len(records.RowStarts)-1 {
		return
	}
	records.RowStarts[row] = len(records.Records)
}

func MarkInterCoefficientTokenRecordRowEnd(records *InterCoefficientTokenRecords, row int) {
	if records == nil || row < 0 || row >= len(records.RowStarts)-1 {
		return
	}
	records.RowStarts[row+1] = len(records.Records)
}

func (records *InterCoefficientTokenRecords) appendToken(blockType int, band int, ctx int, token int, magnitude int, sign uint8, skipEOBNode bool) error {
	if records == nil {
		return nil
	}
	record, ok := packCoefficientTokenRecord(blockType, band, ctx, token, magnitude, sign, skipEOBNode)
	if !ok {
		return ErrInvalidPacketConfig
	}
	records.Records = append(records.Records, record)
	return nil
}

func packCoefficientTokenRecord(blockType int, band int, ctx int, token int, magnitude int, sign uint8, skipEOBNode bool) (CoefficientTokenRecord, bool) {
	if blockType < 0 || blockType >= tables.BlockTypes ||
		band < 0 || band >= tables.CoefBands ||
		ctx < 0 || ctx >= tables.PrevCoefContexts ||
		token < 0 || token >= tables.MaxEntropyTokens ||
		magnitude < 0 || magnitude > tables.DCTMaxValue ||
		sign > 1 {
		return 0, false
	}
	value := uint32(token) << coefficientTokenRecordTokenShift
	value |= uint32(blockType) << coefficientTokenRecordBlockTypeShift
	value |= uint32(band) << coefficientTokenRecordBandShift
	value |= uint32(ctx) << coefficientTokenRecordContextShift
	value |= uint32(magnitude) << coefficientTokenRecordMagnitudeShift
	value |= uint32(sign) << coefficientTokenRecordSignShift
	if skipEOBNode {
		value |= 1 << coefficientTokenRecordSkipEOBNodeShift
	}
	return CoefficientTokenRecord(value), true
}

func (r CoefficientTokenRecord) token() int {
	return int((uint32(r) >> coefficientTokenRecordTokenShift) & coefficientTokenRecordTokenMask)
}

func (r CoefficientTokenRecord) blockType() int {
	return int((uint32(r) >> coefficientTokenRecordBlockTypeShift) & coefficientTokenRecordTwoBitMask)
}

func (r CoefficientTokenRecord) band() int {
	return int((uint32(r) >> coefficientTokenRecordBandShift) & coefficientTokenRecordBandMask)
}

func (r CoefficientTokenRecord) ctx() int {
	return int((uint32(r) >> coefficientTokenRecordContextShift) & coefficientTokenRecordTwoBitMask)
}

func (r CoefficientTokenRecord) magnitude() int {
	return int((uint32(r) >> coefficientTokenRecordMagnitudeShift) & coefficientTokenRecordMagMask)
}

func (r CoefficientTokenRecord) sign() uint8 {
	return uint8((uint32(r) >> coefficientTokenRecordSignShift) & 1)
}

func (r CoefficientTokenRecord) skipEOBNode() bool {
	return ((uint32(r) >> coefficientTokenRecordSkipEOBNodeShift) & 1) != 0
}

func validPreparedCoefficientTokenRows(records *InterCoefficientTokenRecords, rows int) bool {
	if records == nil || rows < 0 || records.Rows != rows || len(records.RowStarts) < rows+1 {
		return false
	}
	if records.RowStarts[0] != 0 || records.RowStarts[rows] != len(records.Records) {
		return false
	}
	for row := range rows {
		start, end := records.RowStarts[row], records.RowStarts[row+1]
		if start < 0 || start > end || end > len(records.Records) {
			return false
		}
	}
	return true
}

func writePreparedInterCoefficientTokenGrid(w *BoolWriter, rows int, records *InterCoefficientTokenRecords, probs *tables.CoefficientProbs) error {
	if w == nil || probs == nil {
		return ErrInvalidPacketConfig
	}
	if !validPreparedCoefficientTokenRows(records, rows) {
		return ErrModeBufferTooSmall
	}
	for row := range rows {
		start, end := records.RowStarts[row], records.RowStarts[row+1]
		if err := writePreparedCoefficientTokenRecords(w, probs, records.Records[start:end]); err != nil {
			return err
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func writePreparedInterCoefficientTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, records *InterCoefficientTokenRecords, probs *tables.CoefficientProbs) error {
	if writers == nil || probs == nil || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}
	if !validPreparedCoefficientTokenRows(records, rows) {
		return ErrModeBufferTooSmall
	}
	for row := range rows {
		w := &writers[row&(partitions-1)]
		start, end := records.RowStarts[row], records.RowStarts[row+1]
		if err := writePreparedCoefficientTokenRecords(w, probs, records.Records[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func writePreparedCoefficientTokenRecords(w *BoolWriter, probs *tables.CoefficientProbs, records []CoefficientTokenRecord) error {
	if w == nil || probs == nil {
		return ErrInvalidPacketConfig
	}
	if w.err != nil {
		return w.err
	}

	low := w.low
	rng := w.rng
	count := w.count
	pos := w.pos
	buf := w.buf

	// Records are validated when produced by packCoefficientTokenRecord:
	// it rejects out-of-range token/blockType/band/ctx/magnitude/sign
	// before appending, and appendToken propagates the error. By the time
	// we reach this writer, every field is guaranteed to be in range, so
	// the per-record OOR check below is dead weight in the hot loop.
	// (writeBlockTokensEOB's matching emit path likewise relies on the
	// caller validating eob/qcoeff once at MB granularity.)
	for _, record := range records {
		raw := uint32(record)
		token := int(raw & coefficientTokenRecordTokenMask)
		blockType := int((raw >> coefficientTokenRecordBlockTypeShift) & coefficientTokenRecordTwoBitMask)
		band := int((raw >> coefficientTokenRecordBandShift) & coefficientTokenRecordBandMask)
		ctx := int((raw >> coefficientTokenRecordContextShift) & coefficientTokenRecordTwoBitMask)

		p := &(*probs)[blockType][band][ctx]
		path := coefficientTokenBranchPaths[token]
		start := uint8(0)
		if (raw>>coefficientTokenRecordSkipEOBNodeShift)&1 != 0 {
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

		if token == tables.ZeroToken || token == tables.DCTEOBToken {
			continue
		}

		// magnitude is guaranteed in (0, DCTMaxValue] by the producer (see
		// countBlockCoefficientTokensAndRecords): non-zero non-EOB tokens
		// only appear for non-zero coeffs, and packCoefficientTokenRecord
		// already rejected out-of-range magnitudes when the record was
		// appended. The offset (= mag - baseVal) is non-negative for the
		// same reason.
		mag := int((raw >> coefficientTokenRecordMagnitudeShift) & coefficientTokenRecordMagMask)
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
		if (raw>>coefficientTokenRecordSignShift)&1 != 0 {
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

	return storeBlockTokenPack(w, low, rng, count, pos)
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
