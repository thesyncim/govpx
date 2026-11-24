package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// libvpx v1.16.0 default tx_probs (vp9/common/vp9_entropymode.c:281-285):
//
//	struct tx_probs default_tx_probs = {
//	  /* p32x32[ctx][3] */ { { 3, 136, 37 }, { 5, 52, 13 } },
//	  /* p16x16[ctx][2] */ { { 20, 152 }, { 15, 101 } },
//	  /* p8x8  [ctx][1] */ { { 100 }, { 66 } },
//	};
var (
	defaultTxProbsP8x8   = [2][]uint8{{100}, {66}}
	defaultTxProbsP16x16 = [2][]uint8{{20, 152}, {15, 101}}
	defaultTxProbsP32x32 = [2][]uint8{{3, 136, 37}, {5, 52, 13}}
)

// TestFullRDTxSizeCostRowMatchesLibvpx pins cpi->tx_size_cost values
// against ground truth emitted by the verbatim libvpx fill loop
// (vp9/encoder/vp9_rd.c:116-132) compiled against the real
// vp9_prob_cost[256] table. The expected ints below were produced by a
// standalone C harness (txcost_oracle.c) that includes
// vp9/encoder/vp9_cost.c and replays the fill loop over default_tx_probs;
// every entry is `cpi->tx_size_cost[max_tx-1][ctx][tx_size]`.
func TestFullRDTxSizeCostRowMatchesLibvpx(t *testing.T) {
	cases := []struct {
		name  string
		probs []uint8
		maxTx common.TxSize
		want  [common.TxSizes]int // indexed by tx_size; 0 == not signalled
	}{
		// max=TX_8X8.
		{"max8x8/ctx0", defaultTxProbsP8x8[0], common.Tx8x8,
			[common.TxSizes]int{694, 366, 0, 0}},
		{"max8x8/ctx1", defaultTxProbsP8x8[1], common.Tx8x8,
			[common.TxSizes]int{1001, 220, 0, 0}},
		// max=TX_16X16.
		{"max16x16/ctx0", defaultTxProbsP16x16[0], common.Tx16x16,
			[common.TxSizes]int{1883, 445, 725, 0}},
		{"max16x16/ctx1", defaultTxProbsP16x16[1], common.Tx16x16,
			[common.TxSizes]int{2096, 732, 416, 0}},
		// max=TX_32X32.
		{"max32x32/ctx0", defaultTxProbsP32x32[0], common.Tx32x32,
			[common.TxSizes]int{3284, 476, 1998, 684}},
		{"max32x32/ctx1", defaultTxProbsP32x32[1], common.Tx32x32,
			[common.TxSizes]int{2907, 1192, 2384, 221}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FullRDTxSizeCostRow(c.probs, c.maxTx)
			if got != c.want {
				t.Fatalf("FullRDTxSizeCostRow(%s) = %v, want %v",
					c.name, got, c.want)
			}
		})
	}
}

// TestTxSizeRateCostMatchesLibvpx pins the per-tx-size signalling cost
// that choose_tx_size_from_rd reads at vp9_rdopt.c:958 against the same
// libvpx ground-truth values, exercised through the TxSizeRateCost helper
// the live keyframe/inter pickers call.
func TestTxSizeRateCostMatchesLibvpx(t *testing.T) {
	cases := []struct {
		name   string
		probs  []uint8
		txSize common.TxSize
		maxTx  common.TxSize
		want   int
	}{
		{"max32/ctx0/tx4x4", defaultTxProbsP32x32[0], common.Tx4x4, common.Tx32x32, 3284},
		{"max32/ctx0/tx8x8", defaultTxProbsP32x32[0], common.Tx8x8, common.Tx32x32, 476},
		{"max32/ctx0/tx16x16", defaultTxProbsP32x32[0], common.Tx16x16, common.Tx32x32, 1998},
		{"max32/ctx0/tx32x32", defaultTxProbsP32x32[0], common.Tx32x32, common.Tx32x32, 684},
		{"max16/ctx1/tx4x4", defaultTxProbsP16x16[1], common.Tx4x4, common.Tx16x16, 2096},
		{"max16/ctx1/tx8x8", defaultTxProbsP16x16[1], common.Tx8x8, common.Tx16x16, 732},
		{"max16/ctx1/tx16x16", defaultTxProbsP16x16[1], common.Tx16x16, common.Tx16x16, 416},
		{"max8/ctx1/tx4x4", defaultTxProbsP8x8[1], common.Tx4x4, common.Tx8x8, 1001},
		{"max8/ctx1/tx8x8", defaultTxProbsP8x8[1], common.Tx8x8, common.Tx8x8, 220},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TxSizeRateCost(c.probs, c.txSize, c.maxTx)
			if got != c.want {
				t.Fatalf("TxSizeRateCost(%s) = %d, want %d", c.name, got, c.want)
			}
		})
	}
}

// TestFullRDChooseTxSizeMatchesLibvpx pins the choose_tx_size_from_rd
// tx_size selection decision (vp9_rdopt.c:946-1009) against ground truth
// from the standalone C harness txrd_oracle.c, which replays the exact
// libvpx selection loop with explicit per-tx-size (rate,dist,skip,sse)
// inputs and the RDCOST macro compiled against vp9_prob_cost[256].
func TestFullRDChooseTxSizeMatchesLibvpx(t *testing.T) {
	// Scenario A: intra, TX_MODE_SELECT, max_tx=TX_32X32, ctx=0,
	// breakout off, depth=2, bs>BLOCK_32X32 so end_tx=TX_16X16.
	// Only TX_16X16/TX_32X32 produced a valid txfm result.
	// libvpx txrd_oracle: best_tx=TX_16X16 rate=2898 dist=5000 skip=0
	// sse=6000 bestrd=640352.
	t.Run("intra_max32_select", func(t *testing.T) {
		row := FullRDTxSizeCostRow(defaultTxProbsP32x32[0], common.Tx32x32)
		// Sanity: row must equal the libvpx tx_size_cost values.
		if row != ([common.TxSizes]int{3284, 476, 1998, 684}) {
			t.Fatalf("cost row = %v", row)
		}
		var cand [common.TxSizes]FullRDTxCandidate
		cand[common.Tx16x16] = FullRDTxCandidate{Valid: true, Rate: 900, Dist: 5000, Skip: false, SSE: 6000}
		cand[common.Tx32x32] = FullRDTxCandidate{Valid: true, Rate: 300, Dist: 9000, Skip: false, SSE: 11000}
		s0 := VP9CostBit(192, 0)
		s1 := VP9CostBit(192, 1)
		res := FullRDChooseTxSize(cand, row, common.Tx32x32,
			int(common.Tx32x32), int(common.Tx16x16),
			58, RDDivBits, s0, s1,
			false, false, false, true, rdCostMax)
		if res.TxSize != common.Tx16x16 || res.Rate != 2898 ||
			res.Dist != 5000 || res.Skip || res.SSE != 6000 ||
			res.BestRDCost != 640352 {
			t.Fatalf("got %+v, want {TX_16X16 2898 5000 false 6000 640352}", res)
		}
	})

	// Scenario B: inter, TX_MODE_SELECT, max_tx=TX_16X16, ctx=1,
	// breakout ON, depth=2 so end_tx=TX_4X4. All three valid.
	// libvpx txrd_oracle: best_tx=TX_4X4 rate=3296 dist=3000 skip=0
	// sse=3500 bestrd=384686.
	t.Run("inter_max16_breakout", func(t *testing.T) {
		row := FullRDTxSizeCostRow(defaultTxProbsP16x16[1], common.Tx16x16)
		if row != ([common.TxSizes]int{2096, 732, 416, 0}) {
			t.Fatalf("cost row = %v", row)
		}
		var cand [common.TxSizes]FullRDTxCandidate
		cand[common.Tx4x4] = FullRDTxCandidate{Valid: true, Rate: 1200, Dist: 3000, Skip: false, SSE: 3500}
		cand[common.Tx8x8] = FullRDTxCandidate{Valid: true, Rate: 400, Dist: 4500, Skip: false, SSE: 5000}
		cand[common.Tx16x16] = FullRDTxCandidate{Valid: true, Rate: 150, Dist: 8000, Skip: false, SSE: 9000}
		s0 := VP9CostBit(100, 0)
		s1 := VP9CostBit(100, 1)
		res := FullRDChooseTxSize(cand, row, common.Tx16x16,
			int(common.Tx16x16), int(common.Tx4x4),
			88, RDDivBits, s0, s1,
			true, false, true, true, rdCostMax)
		if res.TxSize != common.Tx4x4 || res.Rate != 3296 ||
			res.Dist != 3000 || res.Skip || res.SSE != 3500 ||
			res.BestRDCost != 384686 {
			t.Fatalf("got %+v, want {TX_4X4 3296 3000 false 3500 384686}", res)
		}
	})
}
