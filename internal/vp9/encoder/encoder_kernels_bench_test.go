package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"math/rand"
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
	for seed := range 256 {
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

func algo127Round3Nmv(seed int) vp9dec.NmvContext {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	for a := range fc.Nmvc.Comps {
		c := &fc.Nmvc.Comps[a]
		c.Sign = uint8(1 + (seed+77*a)%255)
		c.Hp = uint8(1 + (seed+19*a)%255)
		c.Class0Hp = uint8(1 + (seed+31*a)%255)
		for i := range c.Bits {
			c.Bits[i] = uint8(1 + (seed+11*i+43*a)%255)
		}
		for i := range c.Classes {
			c.Classes[i] = uint8(1 + (seed+37*i+9*a)%255)
		}
		for i := range c.Fp {
			c.Fp[i] = uint8(1 + (seed+53*i+17*a)%255)
		}
	}
	return fc.Nmvc
}

func TestAlgo127Round3Nmv(t *testing.T) {
	for _, seed := range []int{0, 1, 17, 128, 254} {
		for _, hp := range []bool{false, true} {
			ctx := algo127Round3Nmv(seed)
			for a := range ctx.Comps {
				var got, want [nmvCostTableVals]int
				buildNmvComponentCostTable(got[:], &ctx.Comps[a], hp)
				algo127Round3FrozenNmv(want[:], &ctx.Comps[a], hp)
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("seed%d hp%v axis%d value%d got%d want%d", seed, hp, a, i, got[i], want[i])
					}
				}
			}
		}
	}
}

var algo127Round3NmvTable NmvCostTable

func BenchmarkAlgo127Round3Nmv(b *testing.B) {
	for _, hp := range []bool{false, true} {
		b.Run(fmt.Sprint(hp), func(b *testing.B) {
			ctx := algo127Round3Nmv(73)
			b.ReportAllocs()
			for b.Loop() {
				algo127Round3NmvTable.Build(&ctx, hp)
			}
		})
	}
}

func algo127Round3FrozenNmv(mvcost []int, c *vp9dec.NmvComponent, useHP bool) {
	if len(mvcost) < nmvCostTableVals || c == nil {
		return
	}
	offset := nmvCostTableMax
	signCost := [2]int{VP9CostBit(c.Sign, 0), VP9CostBit(c.Sign, 1)}
	var classCost [vp9dec.MvClasses]int
	VP9CostTokens(classCost[:], c.Classes[:], tables.MvClassTree[:])
	var class0Cost [vp9dec.Class0Size]int
	VP9CostTokens(class0Cost[:], c.Class0[:], tables.MvClass0Tree[:])
	var bitsCost [vp9dec.MvOffsetBits][2]int
	for i := range bitsCost {
		bitsCost[i][0] = VP9CostBit(c.Bits[i], 0)
		bitsCost[i][1] = VP9CostBit(c.Bits[i], 1)
	}
	var class0FPCost [vp9dec.Class0Size][vp9dec.MvFpSize]int
	for i := range class0FPCost {
		VP9CostTokens(class0FPCost[i][:], c.Class0Fp[i][:], tables.MvFpTree[:])
	}
	var fpCost [vp9dec.MvFpSize]int
	VP9CostTokens(fpCost[:], c.Fp[:], tables.MvFpTree[:])
	class0HPCost := [2]int{VP9CostBit(c.Class0Hp, 0), VP9CostBit(c.Class0Hp, 1)}
	hpCost := [2]int{VP9CostBit(c.Hp, 0), VP9CostBit(c.Hp, 1)}

	mvcost[offset] = 0
	for o := range vp9dec.Class0Size << 3 {
		d := o >> 3
		f := (o >> 1) & 3
		cost := classCost[tables.MvClass0] + class0Cost[d] + class0FPCost[d][f]
		if useHP {
			cost += class0HPCost[o&1]
		}
		v := o + 1
		mvcost[offset+v] = cost + signCost[0]
		mvcost[offset-v] = cost + signCost[1]
	}
	for cls := tables.MvClass1; cls < vp9dec.MvClasses; cls++ {
		for d := 0; d < (1 << uint(cls)); d++ {
			wholeCost := classCost[cls]
			n := cls + vp9dec.Class0Bits - 1
			for i := range n {
				wholeCost += bitsCost[i][(d>>uint(i))&1]
			}
			for f := range vp9dec.MvFpSize {
				cost := wholeCost + fpCost[f]
				v := (vp9dec.Class0Size << uint(cls+2)) + d*8 + f*2 + 1
				if useHP {
					mvcost[offset+v] = cost + hpCost[0] + signCost[0]
					mvcost[offset-v] = cost + hpCost[0] + signCost[1]
					if v+1 > nmvCostTableMax {
						break
					}
					mvcost[offset+v+1] = cost + hpCost[1] + signCost[0]
					mvcost[offset-v-1] = cost + hpCost[1] + signCost[1]
					continue
				}
				mvcost[offset+v] = cost + signCost[0]
				mvcost[offset-v] = cost + signCost[1]
				if v+1 > nmvCostTableMax {
					break
				}
				mvcost[offset+v+1] = cost + signCost[0]
				mvcost[offset-v-1] = cost + signCost[1]
			}
		}
	}
}

// Frozen from production870699ce; candidate source never generates expectations.

func algo128Round3CoefInputs(pattern int) (p vp9dec.CoefProbsModel, c CoefBranchStatsPerTx) {
	for i := range p {
		for j := range p[i] {
			for k := range p[i][j] {
				for l := range p[i][j][k] {
					for n := range p[i][j][k][l] {
						p[i][j][k][l][n] = uint8(1 + (i*73+j*17+k*13+l*23+n*41)%255)
					}
				}
			}
		}
	}
	if pattern != 0 {
		for i := range c {
			for j := range c[i] {
				for k := range c[i][j] {
					for l := range c[i][j][k] {
						for n := range c[i][j][k][l] {
							if pattern == 1 && (i+j+k+l+n)%7 != 0 {
								continue
							}
							c[i][j][k][l][n] = [2]uint32{uint32(30 + (i*19+j*77+k*103+l*89+n*47)%1700), uint32(10 + (i*107+j*79+k*71+l*53+n*67)%1100)}
						}
					}
				}
			}
		}
	}
	return
}

func TestAlgo128Round3CoefUpdates(t *testing.T) {
	for pattern := range 3 {
		for _, step := range []int{1, 2, 4} {
			for _, mode := range []CoefUpdateMode{CoefUpdateTwoLoop, CoefUpdateOneLoopReduced} {
				p, c := algo128Round3CoefInputs(pattern)
				q := p
				var x, y [8192]byte
				var a, b bitstream.Writer
				a.Start(x[:])
				b.Start(y[:])
				updateCoefProbsTxSize(&a, &p, &c, step, mode)
				algo128Round3FrozenCoefUpdate(&b, &q, &c, step, mode)
				nx, ex := a.Stop()
				ny, ey := b.Stop()
				if (ex == nil) != (ey == nil) || nx != ny || !bytes.Equal(x[:nx], y[:ny]) || p != q {
					t.Fatalf("pattern%d step%d mode%d bytes/state differ", pattern, step, mode)
				}
			}
		}
	}
}

var algo128Round3CoefProbs vp9dec.CoefProbsModel

var algo128Round3Bytes [8192]byte

var algo128Round3Int int

func BenchmarkAlgo128Round3CoefUpdates(b *testing.B) {
	for pattern := range 3 {
		for _, mode := range []CoefUpdateMode{CoefUpdateTwoLoop, CoefUpdateOneLoopReduced} {
			b.Run(fmt.Sprintf("p%d/m%d", pattern, mode), func(b *testing.B) {
				p, c := algo128Round3CoefInputs(pattern)
				var w bitstream.Writer
				b.ReportAllocs()
				for b.Loop() {
					algo128Round3CoefProbs = p
					w.Start(algo128Round3Bytes[:])
					updateCoefProbsTxSize(&w, &algo128Round3CoefProbs, &c, 4, mode)
					algo128Round3Int, _ = w.Stop()
				}
			})
		}
	}
}

func algo128Round3FrozenCoefUpdate(bw *bitstream.Writer,
	probs *vp9dec.CoefProbsModel, counts *CoefBranchStatsPerTx,
	stepsize int, mode CoefUpdateMode,
) {
	if mode == CoefUpdateOneLoopReduced {
		updateCoefProbsTxSizeOneLoopReduced(bw, probs, counts, stepsize)
		return
	}

	// Dry run.
	var totalSavings int64
	updateCount := 0
	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, _ := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						if s > 0 {
							totalSavings += s - int64(VP9CostZero(DiffUpdateProb))
							updateCount++
						} else {
							totalSavings -= int64(VP9CostZero(DiffUpdateProb))
						}
					}
				}
			}
		}
	}

	if updateCount == 0 || totalSavings < 0 {
		bw.WriteBit(0)
		return
	}
	bw.WriteBit(1)

	// Emit pass.
	for i := range vp9dec.CoefPlaneTypes {
		for j := range vp9dec.CoefRefTypes {
			for k := range vp9dec.CoefBands {
				bcc := vp9dec.BandCoefContexts(k)
				for l := range bcc {
					for t := range UnconstrainedNodes {
						s, newp := coefSlotSavings(probs, counts, i, j, k, l, t, stepsize)
						oldp := probs[i][j][k][l][t]
						if s > 0 && newp != oldp {
							bw.Write(1, DiffUpdateProb)
							WriteProbDiffUpdate(bw, newp, oldp)
							probs[i][j][k][l][t] = newp
						} else {
							bw.Write(0, DiffUpdateProb)
						}
					}
				}
			}
		}
	}
}

// Frozen from production870699ce; candidate source never generates expectations.

func TestAlgo129Round3EOBCount(t *testing.T) {
	for n := range 300 {
		tokens := make([]TokenExtra, n)
		for i := range tokens {
			tokens[i] = TokenExtra{Token: EobToken, ProbOff: uint16((i * 107 % (coefProbsFlatLen / UnconstrainedNodes)) * UnconstrainedNodes)}
		}
		if n%5 == 0 && n > 0 {
			tokens[n/2].ProbOff = 65535
		}
		if n%7 == 0 && n > 0 {
			tokens[n/2].Token = OneToken
		}
		var a, b FrameCoefBranchStats
		a[0][0][0][0][0][0][0] = ^uint32(0)
		b = a
		ga := CountCoefEOBTokens(tokens, &a)
		gb := algo129Round3FrozenEOBCount(tokens, &b)
		if ga != gb || a != b {
			t.Fatalf("n%d", n)
		}
	}
	if CountCoefEOBTokens(nil, nil) {
		t.Fatal("nil")
	}
}

var algo129Round3Stats FrameCoefBranchStats

func BenchmarkAlgo129Round3EOBCount(b *testing.B) {
	for _, n := range []int{1, 16, 256, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			tokens := make([]TokenExtra, n)
			for i := range tokens {
				tokens[i] = TokenExtra{Token: EobToken, ProbOff: uint16((i * 107 % (coefProbsFlatLen / UnconstrainedNodes)) * UnconstrainedNodes)}
			}
			b.ReportAllocs()
			for b.Loop() {
				CountCoefEOBTokens(tokens, &algo129Round3Stats)
			}
		})
	}
}

func algo129Round3FrozenEOBCount(tokens []TokenExtra, stats *FrameCoefBranchStats) bool {
	if stats == nil {
		return false
	}
	for _, tok := range tokens {
		probOff := int(tok.ProbOff)
		if tok.Token != EobToken || probOff%UnconstrainedNodes != 0 ||
			probOff < 0 || probOff >= coefProbsFlatLen {
			return false
		}
		row := probOff / UnconstrainedNodes
		ctx := row % vp9dec.CoefContexts
		row /= vp9dec.CoefContexts
		band := row % vp9dec.CoefBands
		row /= vp9dec.CoefBands
		ref := row % vp9dec.CoefRefTypes
		row /= vp9dec.CoefRefTypes
		plane := row % vp9dec.CoefPlaneTypes
		row /= vp9dec.CoefPlaneTypes
		if row < 0 || row >= int(common.TxSizes) {
			return false
		}
		recordCoefBranch00(&stats[row][plane][ref][band][ctx])
	}
	return true
}

// Frozen from production870699ce; candidate source never generates expectations.

func TestAlgo134Round3Variance4(t *testing.T) {
	var a, b [4096]byte
	for i := range a {
		a[i] = byte(i*97 + 17)
		b[i] = byte(i*53 + 71)
	}
	for _, key := range []bool{false, true} {
		for _, stride := range []int{8, 13, 64} {
			for _, wh := range [][2]int{{8, 8}, {7, 7}, {13, 9}, {64, 64}} {
				if wh[0] > stride {
					continue
				}
				for _, xy := range [][2]int{{0, 0}, {1, 1}, {5, 3}, {10, 7}, {63, 63}} {
					var x, y V8x8
					for i := range x.Split {
						x.Split[i].PartVariances.None.Variance = 97
					}
					y = x
					fillVariance4x4Avg(a[:], stride, b[:], stride, xy[0], xy[1], &x, wh[0], wh[1], key)
					algo134Round3FrozenVariance4(a[:], stride, b[:], stride, xy[0], xy[1], &y, wh[0], wh[1], key)
					if x != y {
						t.Fatalf("key%v stride%d wh%v xy%v", key, stride, wh, xy)
					}
				}
			}
		}
	}
}

var algo134Round3Tree V8x8

func BenchmarkAlgo134Round3Variance4(b *testing.B) {
	var a, p [4096]byte
	for i := range a {
		a[i] = byte(i*97 + 17)
		p[i] = byte(i*53 + 71)
	}
	for _, key := range []bool{false, true} {
		for _, xy := range [][2]int{{0, 0}, {7, 11}, {60, 60}} {
			b.Run(fmt.Sprintf("key%v/x%d", key, xy[0]), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					fillVariance4x4Avg(a[:], 64, p[:], 64, xy[0], xy[1], &algo134Round3Tree, 64, 64, key)
				}
			})
		}
	}
}

func algo134Round3FrozenVariance4(src []uint8, srcStride int, dst []uint8, dstStride int,
	x8Idx, y8Idx int, vst *V8x8, pixelsWide, pixelsHigh int, isKeyFrame bool,
) {
	for k := range 4 {
		x4Idx := x8Idx + ((k & 1) << 2)
		y4Idx := y8Idx + ((k >> 1) << 2)
		var sse uint32
		var sum int32
		if x4Idx < pixelsWide && y4Idx < pixelsHigh {
			sAvg := avg4x4Clamped(src, srcStride, x4Idx, y4Idx,
				pixelsWide, pixelsHigh)
			dAvg := 128
			if !isKeyFrame {
				dAvg = avg4x4Clamped(dst, dstStride, x4Idx, y4Idx,
					pixelsWide, pixelsHigh)
			}
			diff := sAvg - dAvg
			sum = int32(diff)
			sse = uint32(diff * diff)
		}
		fillVariance(sse, sum, 0, &vst.Split[k].PartVariances.None)
	}
}

// Frozen from production870699ce; candidate source never generates expectations.

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
	for trial := range 1200 {
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

func algo136R3sSearchCounts(seed int) (ct [EntropyNodes][2]uint32) {
	for i := range ct {
		ct[i] = [2]uint32{uint32((seed*107 + i*73) % 4096), uint32((seed*53 + i*37) % 2048)}
	}
	return
}

func TestAlgo136Round3SearchSeed(t *testing.T) {
	for seed := 1; seed <= 255; seed++ {
		ct := algo136R3sSearchCounts(seed)
		for _, best := range []uint8{1, uint8(seed), 128, 255} {
			for _, step := range []int{1, 2, 4} {
				a, b := best, best
				x := ProbDiffUpdateSavingsSearchModel(&ct, uint8(seed), &a, 252, step)
				y := algo136R3sFrozenModel(&ct, uint8(seed), &b, 252, step)
				if x != y || a != b {
					t.Fatalf("model seed%d best%d step%d", seed, best, step)
				}
				a, b = best, best
				x = ProbDiffUpdateSavingsSearch(ct[2], uint8(seed), &a, 252)
				y = algo136R3sFrozenBinary(ct[2], uint8(seed), &b, 252)
				if x != y || a != b {
					t.Fatalf("binary seed%d best%d", seed, best)
				}
			}
		}
	}
}

var algo136R3sSavings int64

var algo136R3sBest uint8

func BenchmarkAlgo136Round3SearchSeed(b *testing.B) {
	for _, model := range []bool{false, true} {
		for _, aligned := range []bool{false, true} {
			b.Run(fmt.Sprintf("model%v/aligned%v", model, aligned), func(b *testing.B) {
				ct := algo136R3sSearchCounts(17)
				seed := uint8(73)
				if !aligned {
					seed = 192
				}
				b.ReportAllocs()
				for b.Loop() {
					algo136R3sBest = seed
					if model {
						algo136R3sSavings = ProbDiffUpdateSavingsSearchModel(&ct, 73, &algo136R3sBest, 252, 4)
					} else {
						algo136R3sSavings = ProbDiffUpdateSavingsSearch(ct[2], 73, &algo136R3sBest, 252)
					}
				}
			})
		}
	}
}

func algo136R3sFrozenBinary(ct [2]uint32, oldp uint8, bestp *uint8, upd uint8) int64 {
	oldB := int64(CostBranch256(ct, oldp))
	bestSavings := int64(0)
	bestNewP := oldp
	step := int(1)
	if int(*bestp) > int(oldp) {
		step = -1
	}
	updCost := VP9CostOne(upd) - VP9CostZero(upd)

	if oldB > int64(updCost+(MinDelpBits<<VP9ProbCostShift)) {
		for newp := int(*bestp); newp != int(oldp); newp += step {
			newB := int64(CostBranch256(ct, uint8(newp)))
			updateB := int64(ProbDiffUpdateCost(uint8(newp), oldp) + updCost)
			savings := oldB - newB - updateB
			if savings > bestSavings {
				bestSavings = savings
				bestNewP = uint8(newp)
			}
		}
	}
	*bestp = bestNewP
	return bestSavings
}

// Frozen870699ce implementation.

func algo136R3sFrozenModel(ct *[EntropyNodes][2]uint32,
	oldp uint8, bestp *uint8, upd uint8, stepsize int,
) int64 {
	stepSign := int64(1)
	if int(*bestp) > int(oldp) {
		stepSign = -1
	}
	step := int64(stepsize) * stepSign
	updCost := int64(VP9CostOne(upd) - VP9CostZero(upd))

	oldPareto := tables.Pareto8Full[oldp-1]
	oldB := int64(CostBranch256(ct[PivotNode], oldp))
	for i := UnconstrainedNodes; i < EntropyNodes; i++ {
		oldB += int64(CostBranch256(ct[i], oldPareto[i-UnconstrainedNodes]))
	}

	bestSavings := int64(0)
	bestNewP := oldp

	if oldB > updCost+(MinDelpBits<<VP9ProbCostShift) {
		for newp := int64(*bestp); (newp-int64(oldp))*stepSign < 0; newp += step {
			if newp < 1 || newp > 255 {
				continue
			}
			newPareto := tables.Pareto8Full[newp-1]
			newB := int64(CostBranch256(ct[PivotNode], uint8(newp)))
			for i := UnconstrainedNodes; i < EntropyNodes; i++ {
				newB += int64(CostBranch256(ct[i], newPareto[i-UnconstrainedNodes]))
			}
			updateB := int64(ProbDiffUpdateCost(uint8(newp), oldp)) + updCost
			savings := oldB - newB - updateB
			if savings > bestSavings {
				bestSavings = savings
				bestNewP = uint8(newp)
			}
		}
	}
	*bestp = bestNewP
	return bestSavings
}

// Frozen870699ce implementation.

func algo137R3sCostOutcome(fn func() int) (v int, p bool) {
	defer func() { p = recover() != nil }()
	v = fn()
	return
}

func TestAlgo137Round3ProbCost(t *testing.T) {
	for a := 0; a <= 255; a++ {
		for b := 0; b <= 255; b++ {
			x, xp := algo137R3sCostOutcome(func() int { return ProbDiffUpdateCost(uint8(a), uint8(b)) })
			y, yp := algo137R3sCostOutcome(func() int { return algo137R3sFrozenProbCost(uint8(a), uint8(b)) })
			if x != y || xp != yp {
				t.Fatalf("%d/%d got%d/%v want%d/%v", a, b, x, xp, y, yp)
			}
		}
	}
}

var algo137R3sCost int

func BenchmarkAlgo137Round3ProbCost(b *testing.B) {
	for _, pair := range [][2]byte{{128, 127}, {30, 230}, {220, 30}, {73, 192}} {
		b.Run(fmt.Sprint(pair), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				algo137R3sCost = ProbDiffUpdateCost(pair[0], pair[1])
			}
		})
	}
}

func algo137R3sFrozenProbCost(newp, oldp uint8) int {
	delp := remapProb(int(newp), int(oldp))
	return int(updateBits[delp]) << VP9ProbCostShift
}

// Frozen870699ce implementation.

var algo138R3sCost int

func TestAlgo138Round3ExtraCost(t *testing.T) {
	for a := -1; a <= 65536; a++ {
		for _, sign := range []int{0, 1, 3} {
			x, y := CoeffTokenExtraCost(a, sign)
			u, v := algo138R3sFrozenExtraCost(a, sign)
			if x != u || y != v {
				t.Fatalf("a%d sign%d got%d/%d want%d/%d", a, sign, x, y, u, v)
			}
		}
	}
}

var algo138R3sToken int

func BenchmarkAlgo138Round3ExtraCost(b *testing.B) {
	for _, a := range []int{0, 1, 17, 4095, 4096, 8192, 32767, 32768, 32769} {
		b.Run(fmt.Sprint(a), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				algo138R3sToken, algo138R3sCost = CoeffTokenExtraCost(a, 1)
			}
		})
	}
}

func algo138R3sFrozenExtraCost(absVal, sign int) (token int, cost int) {
	if uint(absVal) < coeffTokenExtraCostTableSize {
		entry := coeffTokenExtraCostTable[absVal]
		return int(entry.token), int(entry.cost)
	}
	return coeffTokenExtraCostSlow(absVal, sign)
}

// Frozen870699ce implementation.

func algo140R3sFirstInput(pattern string) (a, p [16384]byte) {
	for i := range a {
		if pattern == "flat" {
			a[i] = 127
			p[i] = 127
		} else {
			a[i] = byte((i*73 + i/128*19) ^ ((i / 128) * 37))
			p[i] = byte(i*47 + i/128*71)
		}
	}
	if pattern == "same" {
		p = a
	}
	if pattern == "shift" {
		copy(p[129:], a[:len(a)-129])
	}
	return
}

func TestAlgo140Round3FirstPass(t *testing.T) {
	for _, pattern := range []string{"flat", "same", "shift", "dense"} {
		a, p := algo140R3sFirstInput(pattern)
		for _, xy := range [][2]int{{0, 0}, {7, 13}, {48, 57}, {112, 112}} {
			for _, size := range [][2]int{{4, 4}, {8, 8}, {16, 16}, {11, 7}} {
				x, r, c := FirstPassMotionSearch(a[:], 128, p[:], 128, xy[0], xy[1], size[0], size[1], 128, 128)
				y, s, d := algo140R3sFrozenFirstPass(a[:], 128, p[:], 128, xy[0], xy[1], size[0], size[1], 128, 128)
				if x != y || r != s || c != d {
					t.Fatalf("%s xy%v size%v got%d/%d/%d want%d/%d/%d", pattern, xy, size, x, r, c, y, s, d)
				}
			}
		}
	}
}

var algo140R3sError uint64

var algo140R3sRow, r3sCol int16

func BenchmarkAlgo140Round3FirstPass(b *testing.B) {
	for _, pattern := range []string{"flat", "same", "shift", "dense"} {
		a, p := algo140R3sFirstInput(pattern)
		for _, size := range []int{8, 16} {
			b.Run(fmt.Sprintf("%s/%d", pattern, size), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					algo140R3sError, algo140R3sRow, r3sCol = FirstPassMotionSearch(a[:], 128, p[:], 128, 48, 48, size, size, 128, 128)
				}
			})
		}
	}
}

func algo140R3sFrozenFirstPass(src []byte, srcStride int, ref []byte, refStride int,
	x int, y int, w int, h int, width int, height int,
) (best uint64, bestRowQ3 int16, bestColQ3 int16) {
	best = ^uint64(0)
	for dy := -FirstPassSearchRange; dy <= FirstPassSearchRange; dy++ {
		refY := y + dy
		if refY < 0 || refY+h > height {
			continue
		}
		for dx := -FirstPassSearchRange; dx <= FirstPassSearchRange; dx++ {
			refX := x + dx
			if refX < 0 || refX+w > width {
				continue
			}
			err := BlockSSE(src, srcStride, ref, refStride, x, y, refX, refY, w, h)
			if err < best {
				best = err
				bestRowQ3 = int16(dy << 3)
				bestColQ3 = int16(dx << 3)
			}
		}
	}
	return best, bestRowQ3, bestColQ3
}
