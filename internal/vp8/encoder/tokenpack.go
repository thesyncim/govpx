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
	if records == nil || uint(row) >= uint(len(records.RowStarts)-1) {
		return
	}
	records.RowStarts[row] = len(records.Records)
}

func MarkInterCoefficientTokenRecordRowEnd(records *InterCoefficientTokenRecords, row int) {
	if records == nil || uint(row) >= uint(len(records.RowStarts)-1) {
		return
	}
	records.RowStarts[row+1] = len(records.Records)
}

// appendTokenUnchecked is the hot-path entry that packs+appends a
// coefficient token without re-validating the inputs. Callers (e.g.
// countBlockCoefficientTokensAndRecords) validate at function entry,
// so the per-coefficient cost of re-validating showed up as ~10% extra
// time in the entropy walk under pprof.
func (records *InterCoefficientTokenRecords) appendTokenUnchecked(blockType int, band int, ctx int, token int, magnitude int, sign uint8, skipEOBNode bool) {
	if records == nil {
		return
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
	records.Records = append(records.Records, CoefficientTokenRecord(value))
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

	// Records are validated at the producer (countBlockCoefficientTokensAndRecords)
	// before being packed by appendTokenUnchecked. By the time we reach this
	// writer, every field is guaranteed to be in range, so the per-record
	// OOR check below is dead weight in the hot loop.
	for _, record := range records {
		raw := uint32(record)
		token := int(raw & coefficientTokenRecordTokenMask)
		blockType := int((raw >> coefficientTokenRecordBlockTypeShift) & coefficientTokenRecordTwoBitMask)
		band := int((raw >> coefficientTokenRecordBandShift) & coefficientTokenRecordBandMask)
		ctx := int((raw >> coefficientTokenRecordContextShift) & coefficientTokenRecordTwoBitMask)

		p := &(*probs)[blockType][band][ctx]
		// Take a pointer instead of copying the 15-byte path struct on each
		// record; the table never moves so the indirection has no aliasing
		// concern in this hot per-token loop.
		path := &coefficientTokenBranchPaths[token]
		// Branchless skipEOBNode flag → loop start index. The shift+mask
		// extracts the bit directly as 0/1, replacing the per-record
		// if-then on the same bit.
		start := uint8((raw >> coefficientTokenRecordSkipEOBNodeShift) & 1)
		// path.len is bounded by len(coefficientTokenBranchPath.bits) = 7
		// at build time; clamp here so the per-iter bounds check on
		// pathBits[i]/pathNodes[i] folds away.
		pathLen := min(path.len, 7)
		pathBits := &path.bits
		pathNodes := &path.nodes
		for i := start; i < pathLen; i++ {
			bit := pathBits[i]
			probability := p[pathNodes[i]]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			// Branchless interval selection: mask is all-ones when bit
			// is set, zero otherwise. The bit==0 arm keeps rng = split
			// and low unchanged; the bit==1 arm folds in
			// (old_rng - split) - split = rng - 2*split.
			mask := -uint32(bit & 1)
			low += split & mask
			rng = split + ((rng - 2*split) & mask)

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

		// magnitude is guaranteed in (0, DCTMaxValue] by the producer
		// (countBlockCoefficientTokensAndRecords); the offset
		// (= mag - baseVal) is non-negative for the same reason.
		mag := int((raw >> coefficientTokenRecordMagnitudeShift) & coefficientTokenRecordMagMask)
		extra := coefficientExtraBitEncodings[token]
		extraLen := int(extra.len)
		offset := mag - int(extra.baseVal)
		for i := range extraLen {
			shiftIndex := extraLen - 1 - i
			bit := uint8((offset >> uint(shiftIndex)) & 1)
			probability := extra.probs[i]
			split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
			// Branchless interval selection: mask is all-ones when bit
			// is set, zero otherwise. The bit==0 arm keeps rng = split
			// and low unchanged; the bit==1 arm folds in
			// (old_rng - split) - split = rng - 2*split.
			mask := -uint32(bit & 1)
			low += split & mask
			rng = split + ((rng - 2*split) & mask)

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

		// Branchless sign-bit interval selection mirroring the inner
		// tree-edge loop: mask is all-ones when the sign bit is set so
		// the bit==1 arm folds in (rng - split) - split = rng - 2*split.
		split := (rng + 1) >> 1
		mask := -uint32((raw >> coefficientTokenRecordSignShift) & 1)
		low += split & mask
		rng = split + ((rng - 2*split) & mask)
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
		// DefaultZigZag1D returns a permutation of 0..15. Mask the uint8
		// lookup so the compiler can elide the bounds check on
		// qcoeff[zigZagPos] (qcoeff is *[16]int16).
		zigZagPos := tables.DefaultZigZag1D[coeffPos] & 0xF
		coeff := int(qcoeff[zigZagPos])
		// Inline coefficient classification: abs + LUT load instead of
		// a range-comparison switch. mag carries the absolute magnitude;
		// sign is derived directly from the signed coeff. Index 0 of the
		// LUT is tables.ZeroToken so the zero-coefficient branch falls
		// through with no special case. Out-of-range magnitudes are
		// rejected once with ErrInvalidPacketConfig.
		// Branchless |coeff| split. signMask sign-extends so the abs and
		// the sign-nibble both come from the same shift, dropping the
		// per-coefficient negative-branch from the inner pack loop.
		signMask := coeff >> intSignShift
		mag := (coeff ^ signMask) - signMask
		sign := uint8(signMask & 1)
		// Uint range check folds the (mag < 0) guard the int comparison
		// could not eliminate (signed-overflow possibility) and yields a
		// proven [0, DCTMaxValue] for the LUT load below.
		if uint(mag) > uint(tables.DCTMaxValue) {
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
			// Branchless interval selection: mask is all-ones when bit
			// is set, zero otherwise. The bit==0 arm keeps rng = split
			// and low unchanged; the bit==1 arm folds in
			// (old_rng - split) - split = rng - 2*split.
			mask := -uint32(bit & 1)
			low += split & mask
			rng = split + ((rng - 2*split) & mask)

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
			for i := range extraLen {
				shiftIndex := extraLen - 1 - i
				bit := uint8((offset >> uint(shiftIndex)) & 1)
				probability := extra.probs[i]
				split := uint32(1 + (((rng - 1) * uint32(probability)) >> 8))
				// Branchless interval selection.
				mask := -uint32(bit & 1)
				low += split & mask
				rng = split + ((rng - 2*split) & mask)

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
			// Branchless sign-bit encoding. The sign bit is uniformly
			// distributed, so the branch would frequently mispredict;
			// folding it into mask arithmetic costs the same on either path.
			signMask := -uint32(sign & 1)
			low += split & signMask
			rng = split + ((rng - 2*split) & signMask)
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
			// Branchless interval selection: mask is all-ones when bit
			// is set, zero otherwise. The bit==0 arm keeps rng = split
			// and low unchanged; the bit==1 arm folds in
			// (old_rng - split) - split = rng - 2*split.
			mask := -uint32(bit & 1)
			low += split & mask
			rng = split + ((rng - 2*split) & mask)

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
