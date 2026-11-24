//go:build !govpx_oracle_trace

package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"reflect"
	"testing"
)

func tokenCountMacroCapture(fn func() error) (err, pan string) {
	defer func() {
		if p := recover(); p != nil {
			pan = fmt.Sprintf("%T:%v", p, p)
		}
	}()
	e := fn()
	if e != nil {
		err = e.Error()
	}
	return
}
func tokenCountMacroInput(seed int) MacroblockCoefficients {
	var c MacroblockCoefficients
	for b := range 25 {
		for k := range 16 {
			switch seed % 4 {
			case 0:
				c.QCoeff[b][k] = 0
			case 1:
				c.QCoeff[b][k] = int16((b*37+k*17+seed)%4097 - 2048)
			case 2:
				c.QCoeff[b][k] = int16((b*12347 + k*31009 + seed*31) & 65535)
			case 3:
				if k == 0 {
					c.QCoeff[b][k] = int16(b + 1)
				}
			}
		}
		choices := [8]uint8{0, 0, 0, 1, 2, 16, 17, 255}
		c.EOB[b] = choices[(seed/4+b)%8]
		if seed%8 == 0 {
			c.EOB[b] = 0
		}
	}
	return c
}
func TestTokenCountMacroFrozenState(t *testing.T) {
	checks := 0
	for seed := range 128 {
		for _, is4 := range []bool{false, true} {
			for mode := range 6 {
				for _, capacity := range []int{0, 1, 24, 384} {
					for _, alias := range []bool{false, true} {
						coeff := tokenCountMacroInput(seed)
						ca, cb := coeff, coeff
						var aa, ab, la, lb TokenContextPlanes
						for j := range 4 {
							aa.Y1[j] = uint8((seed + j) % 2)
							la.Y1[j] = uint8((seed/2 + j) % 2)
						}
						aa.Y2 = uint8(seed % 2)
						la.Y2 = uint8((seed / 2) % 2)
						aa.U = [2]uint8{0, 1}
						aa.V = [2]uint8{1, 0}
						la.U = [2]uint8{1, 0}
						la.V = [2]uint8{0, 1}
						if seed%7 == 0 {
							aa.Y1[3] = 255
							la.Y1[0] = 1
							aa.Y2 = 255
							la.Y2 = 1
						}
						ab, lb = aa, la
						var ac, bc coefficientTokenCounts
						ac[0][0][0][0] = seed * 31
						bc = ac
						ba, bb := make([]CoefficientTokenRecord, capacity+4), make([]CoefficientTokenRecord, capacity+4)
						for j := range ba {
							ba[j] = CoefficientTokenRecord(j*17 + seed)
						}
						copy(bb, ba)
						ra, rb := InterCoefficientTokenRecords{Records: ba[2 : 2 : 2+capacity], Rows: 7}, InterCoefficientTokenRecords{Records: bb[2 : 2 : 2+capacity], Rows: 7}
						ra.rowStartsInline[17], rb.rowStartsInline[17] = 123, 123
						ap, bp, lp, mp, cp, dp, xp, yp, rp, sp := &aa, &ab, &la, &lb, &ca, &cb, &ac, &bc, &ra, &rb
						if alias {
							lp, mp = ap, bp
						}
						switch mode {
						case 1:
							ap, bp = nil, nil
						case 2:
							lp, mp = nil, nil
						case 3:
							cp, dp = nil, nil
						case 4:
							xp, yp = nil, nil
						case 5:
							rp, sp = nil, nil
						}
						ae, ax := tokenCountMacroCapture(func() error { return tokenCountFrozenMacro(is4, ap, lp, cp, xp, rp) })
						be, bx := tokenCountMacroCapture(func() error { return countCoefficientMacroblockTokensAndRecords(is4, bp, mp, dp, yp, sp) })
						checks++
						if ae != be || ax != bx || aa != ab || la != lb || ca != cb || ac != bc || !reflect.DeepEqual(ra, rb) || !reflect.DeepEqual(ba, bb) {
							t.Fatalf("seed%d is4%v mode%d cap%d alias%v err%q/%q panic%q/%q", seed, is4, mode, capacity, alias, ae, be, ax, bx)
						}
					}
				}
			}
		}
	}
	t.Logf("full-state/error/panic/backing cases=%d", checks)
}

var tokenCountMacroSink int

func BenchmarkTokenCountMacroNative(b *testing.B) {
	for _, is4 := range []bool{false, true} {
		for _, pattern := range []string{"zero", "sparse", "dense"} {
			for _, withRecords := range []bool{false, true} {
				b.Run(fmt.Sprintf("is4%v/%s/records%v", is4, pattern, withRecords), func(b *testing.B) {
					var coeff MacroblockCoefficients
					if pattern != "zero" {
						for j := range 25 {
							coeff.QCoeff[j][0] = int16(j + 1)
							coeff.EOB[j] = 1
							if pattern == "dense" {
								coeff.EOB[j] = 16
								for k := range 16 {
									coeff.QCoeff[j][k] = int16((j*31+k*13)%63 - 31)
								}
							}
						}
					}
					initialAbove := TokenContextPlanes{Y1: [4]uint8{1, 0, 1, 0}, U: [2]uint8{1, 0}, V: [2]uint8{0, 1}, Y2: 1}
					initialLeft := TokenContextPlanes{}
					var wc, gc coefficientTokenCounts
					wa, wl, ga, gl := initialAbove, initialLeft, initialAbove, initialLeft
					wr, gr := InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, 384)}, InterCoefficientTokenRecords{Records: make([]CoefficientTokenRecord, 0, 384)}
					wp, gp := &wr, &gr
					if !withRecords {
						wp, gp = nil, nil
					}
					if err := tokenCountFrozenMacro(is4, &wa, &wl, &coeff, &wc, wp); err != nil {
						b.Fatal(err)
					}
					if err := AccumulateInterMacroblockTokenCountsAndRecords(&gc, gp, is4, &ga, &gl, &coeff); err != nil {
						b.Fatal(err)
					}
					check := func() {
						if wa != ga || wl != gl || !reflect.DeepEqual(wr.Records, gr.Records) {
							b.Fatal("context/prepared records differ from frozen parent")
						}
					}
					check()
					if wc != gc {
						b.Fatal("count oracle")
					}
					gc = coefficientTokenCounts{}
					gr.Records = gr.Records[:0]
					b.ReportAllocs()
					for b.Loop() {
						ga, gl = initialAbove, initialLeft
						gr.Records = gr.Records[:0]
						if err := AccumulateInterMacroblockTokenCountsAndRecords(&gc, gp, is4, &ga, &gl, &coeff); err != nil {
							b.Fatal(err)
						}
					}
					for bt := range wc {
						for band := range wc[bt] {
							for ctx := range wc[bt][band] {
								for tok, x := range wc[bt][band][ctx] {
									if gc[bt][band][ctx][tok] != x*b.N {
										b.Fatal("accumulated count oracle")
									}
								}
							}
						}
					}
					check()
					tokenCountMacroSink = gc[0][0][0][0] + len(gr.Records)
				})
			}
		}
	}
}

func tokenCountFrozenMacro(is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords) error {
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
		if err := countBlockCoefficientTokensAndRecords(counts, records, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
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

	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(above.Y1[a] + left.Y1[l])
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := countBlockCoefficientTokensAndRecords(counts, records, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		above.Y1[a] = hasCoeffs
		left.Y1[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := common.UVTokenContextIndex(block)
		ctx := int(getTokenUVContext(above, a) + getTokenUVContext(left, l))
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := countBlockCoefficientTokensAndRecords(counts, records, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
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
