package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"math/rand"
	"testing"
)

func algo135Round3Model(seed int) (m [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8) {
	for b := range m {
		for c := range m[b] {
			for n := range m[b][c] {
				m[b][c][n] = uint8(1 + (seed+b*37+c*13+n*79)%255)
			}
		}
	}
	return
}

var algo135Round3Int int

func TestAlgo135Round3KnownCost(t *testing.T) {
	r := rand.New(rand.NewSource(126135))
	m := algo135Round3Model(77)
	var tab CoeffTreeTokenCostTable
	FillCoeffTreeTokenCostTable(&m, &tab)
	for trial := 0; trial < 1200; trial++ {
		tx := common.TxSize(trial % 4)
		scan := common.ScanOrders[tx][trial%4].Scan
		n := 16 << uint(tx*2)
		q := make([]int16, n)
		for i := range q {
			if r.Intn(3) == 0 {
				q[i] = int16(r.Uint32())
			}
		}
		eob := r.Intn(n+4) - 2
		ctx := trial % 3
		a := CoeffBlockRateCostFastKnownQCoeffTable(tx, &tab, scan, q, ctx, eob)
		b := algo135Round3FrozenKnownCost(tx, &tab, scan, q, ctx, eob)
		if a != b {
			t.Fatalf("trial%d tx%d eob%d got%d want%d", trial, tx, eob, a, b)
		}
	}
}

func BenchmarkAlgo135Round3KnownCost(b *testing.B) {
	m := algo135Round3Model(77)
	var tab CoeffTreeTokenCostTable
	FillCoeffTreeTokenCostTable(&m, &tab)
	for _, tx := range []common.TxSize{common.Tx4x4, common.Tx8x8, common.Tx16x16, common.Tx32x32} {
		for _, pattern := range []string{"zero", "sparse", "dense"} {
			b.Run(fmt.Sprintf("tx%d/%s", tx, pattern), func(b *testing.B) {
				n := 16 << uint(tx*2)
				q := make([]int16, n)
				scan := common.DefaultScanOrders[tx].Scan
				eob := n
				for i := range q {
					if pattern == "dense" || i%7 == 0 {
						q[i] = int16((i*17)%89 - 43)
					}
				}
				if pattern == "zero" {
					clear(q)
					eob = 0
				}
				if pattern == "sparse" {
					eob = min(n, 23)
				}
				b.ReportAllocs()
				for b.Loop() {
					algo135Round3Int = CoeffBlockRateCostFastKnownQCoeffTable(tx, &tab, scan, q, 1, eob)
				}
			})
		}
	}
}

func algo135Round3FrozenKnownCost(txSize common.TxSize,
	costs *CoeffTreeTokenCostTable, scan []int16, qcoeffs []int16,
	initCtx int, eob int,
) int {
	if txSize >= common.TxSizes || costs == nil || initCtx < 0 || initCtx > 2 {
		return 0
	}
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if len(scan) < maxEob || len(qcoeffs) < maxEob {
		return 0
	}
	if eob <= 0 {
		return int((*costs)[0][initCtx][0][EobToken])
	}
	if eob > maxEob {
		eob = maxEob
	}

	rate := 0
	prevToken, extraCost := coeffTokenExtraCostQCoeff(qcoeffs[0])
	rate += extraCost
	rate += int((*costs)[0][initCtx][0][prevToken])

	bandIdx := 1
	bandLeft := coeffCostBandCounts[txSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		token, extra := coeffTokenExtraCostQCoeff(qcoeffs[raster])
		ctx := 0
		skipIdx := 0
		if prevToken == ZeroToken {
			ctx = 1
			skipIdx = 1
		}
		rate += extra
		rate += int((*costs)[bandIdx][ctx][skipIdx][token])
		prevToken = token
		bandLeft--
		if bandLeft == 0 {
			bandIdx++
			if bandIdx >= len(coeffCostBandCounts[txSize]) {
				break
			}
			bandLeft = coeffCostBandCounts[txSize][bandIdx]
		}
	}
	if bandLeft != 0 {
		ctx := 0
		if prevToken == ZeroToken {
			ctx = 1
		}
		rate += int((*costs)[bandIdx][ctx][0][EobToken])
	}
	return rate
}
