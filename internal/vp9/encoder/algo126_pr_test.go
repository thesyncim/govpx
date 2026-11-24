package encoder

import (
	"fmt"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func algo126Round3Model(seed int) (m [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8) {
	for b := range m {
		for c := range m[b] {
			for n := range m[b][c] {
				m[b][c][n] = uint8(1 + (seed+b*37+c*13+n*79)%255)
			}
		}
	}
	return
}

func TestAlgo126Round3CostTable(t *testing.T) {
	for seed := 0; seed < 256; seed++ {
		m := algo126Round3Model(seed)
		if seed%7 == 0 {
			m[seed%6][(seed/6)%6][2] = 0
		}
		var got CoeffTreeTokenCostTable
		if !FillCoeffTreeTokenCostTable(&m, &got) {
			t.Fatal("false")
		}
		for b := range got {
			for c := range got[b] {
				for skip := range 2 {
					for tok := range EntropyTokens {
						want := uint16(CoeffTreeTokenCost(m[b][c][:], skip != 0, tok))
						if got[b][c][skip][tok] != want {
							t.Fatalf("seed%d %d/%d/%d/%d", seed, b, c, skip, tok)
						}
					}
				}
			}
		}
	}
	if FillCoeffTreeTokenCostTable(nil, new(CoeffTreeTokenCostTable)) {
		t.Fatal("nil")
	}
}

var algo126Round3Costs CoeffTreeTokenCostTable

func BenchmarkAlgo126Round3CostTable(b *testing.B) {
	for _, seed := range []int{1, 127, 253} {
		b.Run(fmt.Sprint(seed), func(b *testing.B) {
			m := algo126Round3Model(seed)
			b.ReportAllocs()
			for b.Loop() {
				FillCoeffTreeTokenCostTable(&m, &algo126Round3Costs)
			}
		})
	}
}

func algo126Round3CostPanic(fn func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	fn()
	return
}

func TestAlgo126Round3CostTableInvalidState(t *testing.T) {
	for _, p := range [][3]byte{{0, 127, 127}, {127, 0, 127}, {0, 0, 127}, {0, 0, 0}} {
		for _, at := range [][2]int{{0, 0}, {3, 4}} {
			m := algo126Round3Model(79)
			m[at[0]][at[1]] = p
			var a, b CoeffTreeTokenCostTable
			for i := range a {
				for j := range a[i] {
					for k := range a[i][j] {
						for l := range a[i][j][k] {
							a[i][j][k][l] = 571
						}
					}
				}
			}
			b = a
			pa := algo126Round3CostPanic(func() { FillCoeffTreeTokenCostTable(&m, &a) })
			pb := algo126Round3CostPanic(func() { algo126Round3FrozenFillCost(&m, &b) })
			if pa != pb || a != b {
				t.Fatalf("prob%v slot%v panic%v/%v or partial state", p, at, pa, pb)
			}
		}
	}
}

// Frozen from production870699ce; candidate source never generates expectations.

func algo126Round3FrozenFillCost(
	model *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	out *CoeffTreeTokenCostTable,
) bool {
	if model == nil || out == nil {
		return false
	}
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			for token := range EntropyTokens {
				(*out)[band][ctx][0][token] = uint16(CoeffTreeTokenCost(
					(*model)[band][ctx][:], false, token))
				(*out)[band][ctx][1][token] = uint16(CoeffTreeTokenCost(
					(*model)[band][ctx][:], true, token))
			}
		}
	}
	return true
}

// Frozen from production870699ce; candidate source never generates expectations.
