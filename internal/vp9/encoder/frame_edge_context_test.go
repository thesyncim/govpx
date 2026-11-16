package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// A 64x64 leaf at the edge of a 56x56 frame has transforms crossing both
// boundaries, including a partial 16x16 chroma transform. Nonzero coefficients
// must set the in-frame contexts and clear the rest, as vp9_set_contexts does.
func TestCoefficientContextsAtPartialFrameEdges(t *testing.T) {
	for _, size := range []int{7, 8} {
		for _, path := range []string{"direct", "commit", "tokens", "pack"} {
			t.Run(path+map[int]string{7: "/clipped", 8: "/aligned"}[size], func(t *testing.T) {
				var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
				vp9dec.SetupBlockPlanes(&planes, 1, 1)
				reset := func() {
					for p := range planes {
						planes[p].AboveContext = bytes.Repeat([]byte{9}, 16)
						planes[p].LeftContext = bytes.Repeat([]byte{9}, 16)
					}
				}
				reset()
				fc := vp9dec.FrameCoefProbs{}
				fc[common.Tx4x4] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs4x4)
				fc[common.Tx8x8] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs8x8)
				fc[common.Tx16x16] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs16x16)
				fc[common.Tx32x32] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs32x32)
				var coefficients [1024]int16
				coefficients[0] = 1
				args := WriteCoefSbArgs{BSize: common.Block64x64, MiTxSize: common.Tx32x32, IsInter: 1,
					MiRows: size, MiCols: size, Planes: &planes, Fc: &fc,
					PlaneDequant: [vp9dec.MaxMbPlane][2]int16{{16, 16}, {16, 16}, {16, 16}},
					GetQCoeffs:   func(_, _, _ int, tx common.TxSize) []int16 { return coefficients[:vp9dec.MaxEobForTxSize(tx)] },
					GetEOB:       func(_, _, _ int, _ common.TxSize) (int, bool) { return 1, true },
				}
				var bw bitstream.Writer
				bw.Start(make([]byte, 8192))
				var err error
				switch path {
				case "direct":
					err = WriteCoefSbFromArgs(&bw, &args)
				case "commit":
					err = CommitCoefSbContexts(args)
				default:
					tokens := make([]TokenExtra, 8192)
					used := 0
					args.TokenDst = tokens
					args.TokenIndex = &used
					args.TokenOnly = true
					err = WriteCoefSbFromArgs(&bw, &args)
					if err != nil {
						t.Fatal(err)
					}
					tokens[used] = TokenExtra{Token: EOSBToken}
					tokens = tokens[:used+1]
					reset()
					if path == "tokens" {
						err = CommitCoefSbContextsFromTokens(args, tokens)
					} else {
						_, err = PackTokensAndCommitCoefSbContexts(&bw, tokens, &fc, args)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				for p := range planes {
					span, visible := 16, size*2
					if p > 0 {
						span, visible = 8, size
					}
					for _, ctx := range [][]byte{planes[p].AboveContext, planes[p].LeftContext} {
						for i, value := range ctx {
							want := byte(9)
							if i < span {
								want = 0
								if i < visible {
									want = 1
								}
							}
							if value != want {
								t.Fatalf("plane %d context[%d]=%d, want %d (context %v)", p, i, value, want, ctx)
							}
						}
					}
				}
			})
		}
	}
}
