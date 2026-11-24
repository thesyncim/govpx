package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestClearVP9DecodedDQCoeffsUsesLibvpxEOBTiers(t *testing.T) {
	tests := []struct {
		name   string
		txSize common.TxSize
		txType common.TxType
		eob    int
		clearN int
	}{
		{name: "empty", txSize: common.Tx32x32, txType: common.DctDct, eob: 0, clearN: 0},
		{name: "dc only", txSize: common.Tx32x32, txType: common.DctDct, eob: 1, clearN: 1},
		{name: "dct 4x4 sparse", txSize: common.Tx4x4, txType: common.DctDct, eob: 10, clearN: 16},
		{name: "dct 8x8 sparse", txSize: common.Tx8x8, txType: common.DctDct, eob: 10, clearN: 32},
		{name: "dct 16x16 sparse", txSize: common.Tx16x16, txType: common.DctDct, eob: 10, clearN: 64},
		{name: "dct 32x32 sparse", txSize: common.Tx32x32, txType: common.DctDct, eob: 34, clearN: 256},
		{name: "hybrid 8x8 full", txSize: common.Tx8x8, txType: common.AdstDct, eob: 2, clearN: 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coeffs := make([]int16, 1024)
			for i := range coeffs {
				coeffs[i] = int16(i + 1)
			}

			clearVP9DecodedDQCoeffs(coeffs[:16<<(tt.txSize<<1)], tt.txSize,
				tt.txType, tt.eob)

			for i, got := range coeffs {
				want := int16(i + 1)
				if i < tt.clearN {
					want = 0
				}
				if got != want {
					t.Fatalf("coeffs[%d] = %d, want %d", i, got, want)
				}
			}
		})
	}
}
