package common

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestScanIsPermutation checks that every forward scan in DefaultScanOrders
// is a permutation of [0, N) — i.e. each transform-block position appears
// exactly once. A bad scan table would corrupt every residual block.
func TestScanIsPermutation(t *testing.T) {
	for tx := range TxSizes {
		so := DefaultScanOrders[tx]
		seen := make([]bool, len(so.Scan))
		for i, pos := range so.Scan {
			if int(pos) >= len(so.Scan) {
				t.Errorf("tx=%d scan[%d]=%d out of range", tx, i, pos)
				continue
			}
			if seen[pos] {
				t.Errorf("tx=%d scan[%d]=%d already seen", tx, i, pos)
			}
			seen[pos] = true
		}
	}
}

// TestIScanInvertsScan verifies that IScan is the inverse permutation of
// Scan, shifted by +1: libvpx stores iscan as the EOB position (1-based)
// rather than the raster index (0-based), so iscan[scan[i]] == i + 1.
// This is the contract every detokenize / coefficient writer relies on.
func TestIScanInvertsScan(t *testing.T) {
	cases := []struct {
		tx   TxSize
		scan []int16
		inv  []int16
	}{
		{Tx4x4, tables.DefaultScan4x4[:], tables.DefaultIScan4x4[:]},
		{Tx8x8, tables.DefaultScan8x8[:], tables.DefaultIScan8x8[:]},
		{Tx16x16, tables.DefaultScan16x16[:], tables.DefaultIScan16x16[:]},
		{Tx32x32, tables.DefaultScan32x32[:], tables.DefaultIScan32x32[:]},
		{Tx4x4, tables.RowScan4x4[:], tables.RowIScan4x4[:]},
		{Tx4x4, tables.ColScan4x4[:], tables.ColIScan4x4[:]},
		{Tx8x8, tables.RowScan8x8[:], tables.RowIScan8x8[:]},
		{Tx8x8, tables.ColScan8x8[:], tables.ColIScan8x8[:]},
		{Tx16x16, tables.RowScan16x16[:], tables.RowIScan16x16[:]},
		{Tx16x16, tables.ColScan16x16[:], tables.ColIScan16x16[:]},
	}
	for _, tc := range cases {
		for i, pos := range tc.scan {
			if int(tc.inv[pos]) != i+1 {
				t.Errorf("tx=%d iscan[scan[%d]=%d]=%d, want %d", tc.tx, i, pos, tc.inv[pos], i+1)
			}
		}
	}
}

// TestNeighborTableSize checks neighbor tables are 2*(N+1) int16s — one
// pair per coefficient plus the initial DC dummy pair (matching libvpx's
// [N * MAX_NEIGHBORS + 2] declared size that effectively allocates one
// extra pair).
func TestNeighborTableSize(t *testing.T) {
	cases := []struct {
		size int
		n    []int16
	}{
		{16, tables.DefaultScan4x4Neighbors[:]},
		{64, tables.DefaultScan8x8Neighbors[:]},
		{256, tables.DefaultScan16x16Neighbors[:]},
		{1024, tables.DefaultScan32x32Neighbors[:]},
	}
	for _, tc := range cases {
		want := 2 * (tc.size + 1)
		if len(tc.n) != want {
			t.Errorf("neighbors size=%d, want %d (2*(%d+1))", len(tc.n), want, tc.size)
		}
	}
}
