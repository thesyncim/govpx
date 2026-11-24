package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

// Exercise costs after changing frame contexts, including a transition back
// from non-RD inter mode selection to a frame that needs cached token costs.
// The independent scalar cost path supplies the oracle for every token model.
func TestVP9NonrdCoeffCostFrameTransitions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, CpuUsed: 8})
	if err != nil {
		t.Fatal(err)
	}
	_, gatePresent := any(e).(interface {
		refreshVP9CoeffTokenCostsForFrame(*vp9dec.UncompressedHeader)
	})
	seq := []struct {
		name            string
		frame           common.FrameType
		intra           bool
		nonrd, compound int
		wantTable       bool
	}{
		{"nonrd", common.InterFrame, false, 1, 0, false},
		{"intra", common.InterFrame, true, 1, 0, true},
		{"key", common.KeyFrame, false, 1, 0, true},
		{"fullrd", common.InterFrame, false, 0, 0, true},
		{"compound", common.InterFrame, false, 1, 1, true},
		{"nonrd-again", common.InterFrame, false, 1, 0, false},
	}
	for step, c := range seq {
		t.Run(c.name, func(t *testing.T) {
			for tx := range common.TxSizes {
				for plane := range vp9dec.CoefPlaneTypes {
					for ref := range vp9dec.CoefRefTypes {
						for band := range vp9dec.CoefBands {
							for ctx := range vp9dec.CoefContexts {
								for node := range vp9dec.UnconstrainedNodes {
									e.frameContexts[0].CoefProbs[tx][plane][ref][band][ctx][node] = uint8(1 + (step*37+int(tx)*31+plane*19+ref*17+band*13+ctx*7+node)%255)
								}
							}
						}
					}
				}
			}
			e.sf.UseNonrdPickMode = c.nonrd
			e.sf.UseCompoundNonrdPickmode = c.compound
			e.prepareVP9EncoderFrameContext(&vp9dec.UncompressedHeader{FrameType: c.frame, IntraOnly: c.intra})
			wantTable := c.wantTable || !gatePresent
			if e.vp9CoeffTokenCostsValid != wantTable {
				t.Fatalf("cost table valid=%t, want %t (frame gate=%t)", e.vp9CoeffTokenCostsValid, wantTable, gatePresent)
			}
			for tx := common.Tx4x4; tx < common.TxSizes; tx++ {
				n := vp9dec.MaxEobForTxSize(tx)
				scan := common.DefaultScanOrders[tx]
				q := make([]int16, n)
				dq := make([]int16, n)
				for i := range q {
					q[i] = int16((i*7+step*3)%11 - 5)
					dq[i] = q[i] * 90
				}
				for fast := 0; fast <= 1; fast++ {
					e.sf.UseFastCoefCosting = fast
					for plane := 0; plane < 2; plane++ {
						for ref := 0; ref < 2; ref++ {
							for ctx := 0; ctx < 3; ctx++ {
								var scratch [1024]byte
								want := encoder.CoeffBlockRateCost(encoder.CoeffBlockRateCostInput{TxSize: tx, CoefModel: &e.fc.CoefProbs[tx][plane][ref], ScanOrder: scan, Dequant: [2]int16{90, 90}, Coeffs: dq, QCoeffs: q, InitCtx: ctx, Fast: fast != 0, TokenCache: &scratch})
								var got int
								if ref == 0 {
									got = e.vp9KeyframeCoeffBlockRateCostPlaneQ(tx, plane, scan, [2]int16{90, 90}, dq, q, ctx, 0, false)
								} else {
									got = e.vp9InterCoeffBlockRateCostQEOB(tx, plane, [2]int16{90, 90}, dq, q, ctx, 0, false)
								}
								if got != want {
									t.Fatalf("tx=%d fast=%d p=%d ref=%d ctx=%d got=%d want=%d", tx, fast, plane, ref, ctx, got, want)
								}
							}
						}
					}
				}
			}
		})
	}
}

var vp9CoeffCostPrepareSink int

func BenchmarkVP9NonrdCoeffCostPrepare(b *testing.B) {
	for _, kind := range []string{"nonrd-inter", "key", "fullrd-inter", "compound-inter"} {
		b.Run(kind, func(b *testing.B) {
			e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, CpuUsed: 8})
			if err != nil {
				b.Fatal(err)
			}
			hdr := vp9dec.UncompressedHeader{FrameType: common.InterFrame}
			e.sf.UseNonrdPickMode = 1
			e.sf.UseCompoundNonrdPickmode = 0
			switch kind {
			case "key":
				hdr.FrameType = common.KeyFrame
			case "fullrd-inter":
				e.sf.UseNonrdPickMode = 0
			case "compound-inter":
				e.sf.UseCompoundNonrdPickmode = 1
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vp9CoeffCostPrepareSink = e.prepareVP9EncoderFrameContext(&hdr)
			}
		})
	}
}
