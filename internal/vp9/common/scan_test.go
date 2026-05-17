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
// Cited by #113 as a quantize-audit drift source: any silent scan/iscan
// edit must trip this invariant. Every (scan, iscan) pair listed in
// libvpx vp9/common/vp9_scan.c (default/row/col across 4x4/8x8/16x16
// plus default 32x32) appears below.
func TestIScanInvertsScan(t *testing.T) {
	cases := []struct {
		tx   TxSize
		name string
		scan []int16
		inv  []int16
	}{
		{Tx4x4, "default_4x4", tables.DefaultScan4x4[:], tables.DefaultIScan4x4[:]},
		{Tx8x8, "default_8x8", tables.DefaultScan8x8[:], tables.DefaultIScan8x8[:]},
		{Tx16x16, "default_16x16", tables.DefaultScan16x16[:], tables.DefaultIScan16x16[:]},
		{Tx32x32, "default_32x32", tables.DefaultScan32x32[:], tables.DefaultIScan32x32[:]},
		{Tx4x4, "row_4x4", tables.RowScan4x4[:], tables.RowIScan4x4[:]},
		{Tx4x4, "col_4x4", tables.ColScan4x4[:], tables.ColIScan4x4[:]},
		{Tx8x8, "row_8x8", tables.RowScan8x8[:], tables.RowIScan8x8[:]},
		{Tx8x8, "col_8x8", tables.ColScan8x8[:], tables.ColIScan8x8[:]},
		{Tx16x16, "row_16x16", tables.RowScan16x16[:], tables.RowIScan16x16[:]},
		{Tx16x16, "col_16x16", tables.ColScan16x16[:], tables.ColIScan16x16[:]},
	}
	for _, tc := range cases {
		if len(tc.scan) != len(tc.inv) {
			t.Errorf("%s: scan len=%d iscan len=%d", tc.name, len(tc.scan), len(tc.inv))
			continue
		}
		for i, pos := range tc.scan {
			if int(pos) >= len(tc.inv) {
				t.Errorf("%s scan[%d]=%d out of range for iscan len=%d",
					tc.name, i, pos, len(tc.inv))
				continue
			}
			if int(tc.inv[pos]) != i+1 {
				t.Errorf("%s iscan[scan[%d]=%d]=%d, want %d",
					tc.name, i, pos, tc.inv[pos], i+1)
			}
		}
	}
}

// TestScanOrders32x32IsDefaultOnly mirrors libvpx vp9/common/vp9_scan.c
// lines 716-723: ScanOrders[Tx32x32][*] all alias to the DCT_DCT default
// scan/iscan/neighbors. Sub-8x8 paths can't produce 32x32 transforms, so
// the row/col entries are never reached, but the table layout has to
// match libvpx exactly so the (TxSize, TxType) dispatch is byte-for-byte
// identical.
func TestScanOrders32x32IsDefaultOnly(t *testing.T) {
	def := DefaultScanOrders[Tx32x32]
	for tt := range TxTypes {
		so := ScanOrders[Tx32x32][tt]
		if &so.Scan[0] != &def.Scan[0] {
			t.Errorf("tx32 type=%d Scan not aliased to default", tt)
		}
		if &so.IScan[0] != &def.IScan[0] {
			t.Errorf("tx32 type=%d IScan not aliased to default", tt)
		}
		if &so.Neighbors[0] != &def.Neighbors[0] {
			t.Errorf("tx32 type=%d Neighbors not aliased to default", tt)
		}
	}
}

// TestNeighborTableSize checks neighbor tables are 2*(N+1) int16s — one
// pair per coefficient plus the initial DC dummy pair (matching libvpx's
// [N * MAX_NEIGHBORS + 2] declared size that effectively allocates one
// extra pair). Every neighbor table libvpx ships gets checked.
func TestNeighborTableSize(t *testing.T) {
	cases := []struct {
		name string
		size int
		n    []int16
	}{
		{"default_4x4", 16, tables.DefaultScan4x4Neighbors[:]},
		{"default_8x8", 64, tables.DefaultScan8x8Neighbors[:]},
		{"default_16x16", 256, tables.DefaultScan16x16Neighbors[:]},
		{"default_32x32", 1024, tables.DefaultScan32x32Neighbors[:]},
		{"row_4x4", 16, tables.RowScan4x4Neighbors[:]},
		{"col_4x4", 16, tables.ColScan4x4Neighbors[:]},
		{"row_8x8", 64, tables.RowScan8x8Neighbors[:]},
		{"col_8x8", 64, tables.ColScan8x8Neighbors[:]},
		{"row_16x16", 256, tables.RowScan16x16Neighbors[:]},
		{"col_16x16", 256, tables.ColScan16x16Neighbors[:]},
	}
	for _, tc := range cases {
		want := 2 * (tc.size + 1)
		if len(tc.n) != want {
			t.Errorf("%s neighbors size=%d, want %d (2*(%d+1))",
				tc.name, len(tc.n), want, tc.size)
		}
	}
}
