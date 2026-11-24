//go:build !govpx_oracle_trace

package encoder

import (
	"fmt"
	"reflect"
	"testing"
)

func BenchmarkTokenZeroY1Native(b *testing.B) {
	for _, is4 := range []bool{false, true} {
		for _, pattern := range []string{"zero", "sparse", "dense", "lumaDC"} {
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
					if pattern == "lumaDC" {
						coeff = MacroblockCoefficients{}
						active := 24
						if is4 {
							active = 16
						}
						coeff.QCoeff[active][0] = 1
						coeff.EOB[active] = 1
					}
					initialAbove := TokenContextPlanes{}
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

func TestTokenZeroY1FrozenState(t *testing.T) {
	checks := 0
	for seed := range 128 {
		for _, is4 := range []bool{false, true} {
			for mode := range 6 {
				for _, capacity := range []int{0, 1, 24, 384} {
					for _, alias := range []bool{false, true} {
						coeff := tokenCountMacroInput(seed)
						for block := 0; block < 16; block++ {
							coeff.EOB[block] = 0
							if !is4 && seed%2 == 0 {
								coeff.EOB[block] = 1
							}
						}
						ca, cb := coeff, coeff
						var aa, ab, la, lb TokenContextPlanes
						for j := range 4 {
							aa.Y1[j] = 0
							la.Y1[j] = 0
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
