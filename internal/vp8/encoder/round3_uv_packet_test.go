package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"reflect"
	"testing"
)

func round3UVPacketCapture(fn func()) (pan string) {
	defer func() {
		if p := recover(); p != nil {
			pan = fmt.Sprint(p)
		}
	}()
	fn()
	return
}

func round3UVPacketCoefficients(pattern string) MacroblockCoefficients {
	var c MacroblockCoefficients
	for block := range 25 {
		for j := range 16 {
			switch pattern {
			case "zero":
			case "lumaOnly":
				if block < 16 && j == 1 {
					c.QCoeff[block][j] = int16(block%3 + 1)
				}
			case "denseSmall":
				c.QCoeff[block][j] = int16((block+j)%3 - 1)
			case "denseMixed":
				c.QCoeff[block][j] = int16((block*17+j*13)%2049 - 1024)
			}
		}
	}
	setAllMacroblockEOBs(&c, false)
	return c
}

func round3UVPacketFrozenBlock(counts *coefficientTokenCounts, records *InterCoefficientTokenRecords, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	// Records-nil callers (e.g. probability-only update passes) re-enter
	// the cheaper count-only path; the per-coefficient nil check that
	// previously sat inside appendTokenUnchecked is hoisted once here.
	if records == nil {
		return countBlockCoefficientTokens(counts, blockType, ctx, skipDC, qcoeff, eob)
	}
	// Uint range checks collapse the (x < 0 || x >= max) dual-bound
	// pairs into one branch each. Hot per-block validation.
	if counts == nil || qcoeff == nil ||
		uint(blockType) >= uint(tables.BlockTypes) ||
		uint(ctx) >= uint(tables.PrevCoefContexts) ||
		uint(skipDC) > 1 {
		return ErrInvalidPacketConfig
	}
	if eob <= skipDC {
		(*counts)[blockType][skipDC][ctx][tables.DCTEOBToken]++
		records.appendTokenUnchecked(blockType, skipDC, ctx, tables.DCTEOBToken, 0, 0, false)
		return nil
	}
	if eob > 16 {
		return ErrInvalidPacketConfig
	}

	band := skipDC
	tokenCtx := ctx
	skipEOBNode := false
	for pos := skipDC; pos < eob; pos++ {
		// DefaultZigZag1D returns a permutation of 0..15. Mask the uint8
		// lookup result so the compiler can elide the bounds check on
		// qcoeff[rc] (qcoeff is *[16]int16).
		rc := tables.DefaultZigZag1D[pos] & 0xF
		coeff := int(qcoeff[rc])
		// Branchless |coeff| split into magnitude and sign nibble: signMask
		// is -1 when coeff is negative, 0 otherwise.
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
		// blockType and band were validated to [0,3] and the LUT yields
		// 0..7 for band. AND-mask both so BlockTypes(=4) and
		// CoefBands(=8) bounds checks elide (both are powers of two).
		(*counts)[blockType&3][band&7][tokenCtx][token]++
		// records guaranteed non-nil by the entry branch above, so the
		// per-coefficient nil check inside appendTokenUnchecked has been
		// dropped (it is the hot-path unchecked variant by contract).
		records.appendTokenUnchecked(blockType, band, tokenCtx, token, mag, sign, skipEOBNode)
		if pos+1 < 16 {
			band = int(tables.CoefBandsTable[pos+1])
			tokenCtx = int(tables.PrevTokenClass[token])
			skipEOBNode = tokenCtx == 0
		}
	}
	if eob < 16 {
		(*counts)[blockType&3][band&7][tokenCtx][tables.DCTEOBToken]++
		records.appendTokenUnchecked(blockType, band, tokenCtx, tables.DCTEOBToken, 0, 0, false)
	}
	return nil
}

func round3UVPacketFrozenMacro(is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords) error {
	if records == nil {
		return countCoefficientMacroblockTokens(is4x4, above, left, coeffs, counts)
	}
	if above == nil || left == nil || coeffs == nil || counts == nil {
		return ErrInvalidPacketConfig
	}
	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		ctx := int(above.Y2 + left.Y2)
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if eob <= 0 {
			// Macroblock entry and context checks already prove these arguments.
			(*counts)[1][0][ctx][tables.DCTEOBToken]++
			records.appendTokenUnchecked(1, 0, ctx, tables.DCTEOBToken, 0, 0, false)
		} else if err := round3UVPacketFrozenBlock(counts, records, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		above.Y2 = hasCoeffs
		left.Y2 = hasCoeffs

		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	if !zeroY1TokenRecords(above, left, coeffs, counts, records, blockType, skipDC) {
		for block := range 16 {
			eob := coeffs.BlockEOB(block, skipDC)
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(above.Y1[a] + left.Y1[l])
			if ctx >= tables.PrevCoefContexts {
				return ErrInvalidPacketConfig
			}
			if eob <= skipDC {
				// Macroblock entry and context checks already prove these arguments.
				(*counts)[blockType][skipDC][ctx][tables.DCTEOBToken]++
				records.appendTokenUnchecked(blockType, skipDC, ctx, tables.DCTEOBToken, 0, 0, false)
			} else if err := round3UVPacketFrozenBlock(counts, records, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
				return err
			}
			hasCoeffs := uint8(0)
			if eob > skipDC {
				hasCoeffs = 1
			}
			above.Y1[a] = hasCoeffs
			left.Y1[l] = hasCoeffs
		}

	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := common.UVTokenContextIndex(block)
		ctx := int(getTokenUVContext(above, a) + getTokenUVContext(left, l))
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if eob <= 0 {
			// Macroblock entry and context checks already prove these arguments.
			(*counts)[2][0][ctx][tables.DCTEOBToken]++
			records.appendTokenUnchecked(2, 0, ctx, tables.DCTEOBToken, 0, 0, false)
		} else if err := round3UVPacketFrozenBlock(counts, records, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		setTokenUVContext(above, a, hasCoeffs)
		setTokenUVContext(left, l, hasCoeffs)
	}
	return nil
}

func TestRound3UVPacket(t *testing.T) {
	n := 0
	for _, pattern := range []string{"zero", "lumaOnly", "denseSmall", "denseMixed"} {
		for _, is4 := range []bool{false, true} {
			for _, capacity := range []int{0, 1, 7, 8, 15, 64, 1024} {
				for _, ctx := range []byte{0, 1, 255} {
					a, b := round3UVPacketCoefficients(pattern), round3UVPacketCoefficients(pattern)
					if is4 {
						setAllMacroblockEOBs(&a, true)
						b = a
					}
					var ac, bc coefficientTokenCounts
					aa, al := TokenContextPlanes{Y2: ctx}, TokenContextPlanes{}
					ba, bl := aa, al
					ar := InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, capacity)}
					br := InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, capacity)}
					var ae, be error
					ap := round3UVPacketCapture(func() { ae = AccumulateInterMacroblockTokenCountsAndRecords(&ac, &ar, is4, &aa, &al, &a) })
					bp := round3UVPacketCapture(func() { be = round3UVPacketFrozenMacro(is4, &ba, &bl, &b, &bc, &br) })
					if ap != bp || ae != be || ac != bc || aa != ba || al != bl || a != b || !reflect.DeepEqual(ar.Records, br.Records) {
						t.Fatalf("%s is4%v cap%d ctx%d panic%q/%q err%v/%v", pattern, is4, capacity, ctx, ap, bp, ae, be)
					}
					n++
				}
			}
		}
	}
	t.Logf("%d full macro state/error/partial-panic cases", n)
}

var round3UVPacketSink uint64

func BenchmarkRound3UVPacket(b *testing.B) {
	for _, pattern := range []string{"zero", "lumaOnly", "denseSmall", "denseMixed"} {
		b.Run(pattern, func(b *testing.B) {
			c := round3UVPacketCoefficients(pattern)
			var ac, wc coefficientTokenCounts
			var a, l, wa, wl TokenContextPlanes
			ar := InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, 1024)}
			wr := InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, 1024)}
			if round3UVPacketFrozenMacro(false, &wa, &wl, &c, &wc, &wr) != nil {
				b.Fatal("oracle")
			}
			b.ReportAllocs()
			for b.Loop() {
				a, l = TokenContextPlanes{}, TokenContextPlanes{}
				ar.Records = ar.Records[:0]
				if AccumulateInterMacroblockTokenCountsAndRecords(&ac, &ar, false, &a, &l, &c) != nil {
					b.Fatal("count")
				}
			}
			if a != wa || l != wl || !reflect.DeepEqual(ar.Records, wr.Records) {
				b.Fatal("state")
			}
			for i := range ac {
				for j := range ac[i] {
					for k := range ac[i][j] {
						for m := range ac[i][j][k] {
							if ac[i][j][k][m] != wc[i][j][k][m]*b.N {
								b.Fatal("counts")
							}
						}
					}
				}
			}
			round3UVPacketSink += uint64(len(ar.Records))
		})
	}
}
