package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

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
	for pattern := 0; pattern < 3; pattern++ {
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
	for pattern := 0; pattern < 3; pattern++ {
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
