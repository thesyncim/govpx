package common

import "github.com/thesyncim/govpx/internal/vp9/tables"

// Ported from libvpx v1.16.0 vp9/common/vp9_scan.{c,h}.
//
// ScanOrder bundles the three lookup tables that describe how a
// transform block's coefficients are linearized for entropy coding: the
// forward scan (block position -> raster position), the inverse scan
// (raster position -> block position), and the neighbor-pair table used
// to compute coefficient context bins.

// MaxNeighbors is libvpx's MAX_NEIGHBORS — two neighbours per coefficient.
const MaxNeighbors = 2

// ScanOrder is the trio of tables that drive coefficient entropy coding
// for a particular (TxSize, TxType) pair. All three slices alias into
// internal/vp9/tables storage; ScanOrder values themselves carry no
// allocation cost at use time.
type ScanOrder struct {
	Scan      []int16
	IScan     []int16
	Neighbors []int16
}

// DefaultScanOrders is libvpx's vp9_default_scan_orders, indexed by
// TxSize. It is the dispatch table used when the block does not have a
// custom intra-prediction-driven scan.
var DefaultScanOrders = [TxSizes]ScanOrder{
	{Scan: tables.DefaultScan4x4[:], IScan: tables.DefaultIScan4x4[:], Neighbors: tables.DefaultScan4x4Neighbors[:]},
	{Scan: tables.DefaultScan8x8[:], IScan: tables.DefaultIScan8x8[:], Neighbors: tables.DefaultScan8x8Neighbors[:]},
	{Scan: tables.DefaultScan16x16[:], IScan: tables.DefaultIScan16x16[:], Neighbors: tables.DefaultScan16x16Neighbors[:]},
	{Scan: tables.DefaultScan32x32[:], IScan: tables.DefaultIScan32x32[:], Neighbors: tables.DefaultScan32x32Neighbors[:]},
}

// ScanOrders is libvpx's vp9_scan_orders, indexed by [TxSize][TxType].
// 32x32 transforms only run DCT_DCT, so all four TxType entries point at
// the same DCT_DCT scan — matching libvpx's table layout.
var ScanOrders = [TxSizes][TxTypes]ScanOrder{
	{ // Tx4x4
		{Scan: tables.DefaultScan4x4[:], IScan: tables.DefaultIScan4x4[:], Neighbors: tables.DefaultScan4x4Neighbors[:]},
		{Scan: tables.RowScan4x4[:], IScan: tables.RowIScan4x4[:], Neighbors: tables.RowScan4x4Neighbors[:]},
		{Scan: tables.ColScan4x4[:], IScan: tables.ColIScan4x4[:], Neighbors: tables.ColScan4x4Neighbors[:]},
		{Scan: tables.DefaultScan4x4[:], IScan: tables.DefaultIScan4x4[:], Neighbors: tables.DefaultScan4x4Neighbors[:]},
	},
	{ // Tx8x8
		{Scan: tables.DefaultScan8x8[:], IScan: tables.DefaultIScan8x8[:], Neighbors: tables.DefaultScan8x8Neighbors[:]},
		{Scan: tables.RowScan8x8[:], IScan: tables.RowIScan8x8[:], Neighbors: tables.RowScan8x8Neighbors[:]},
		{Scan: tables.ColScan8x8[:], IScan: tables.ColIScan8x8[:], Neighbors: tables.ColScan8x8Neighbors[:]},
		{Scan: tables.DefaultScan8x8[:], IScan: tables.DefaultIScan8x8[:], Neighbors: tables.DefaultScan8x8Neighbors[:]},
	},
	{ // Tx16x16
		{Scan: tables.DefaultScan16x16[:], IScan: tables.DefaultIScan16x16[:], Neighbors: tables.DefaultScan16x16Neighbors[:]},
		{Scan: tables.RowScan16x16[:], IScan: tables.RowIScan16x16[:], Neighbors: tables.RowScan16x16Neighbors[:]},
		{Scan: tables.ColScan16x16[:], IScan: tables.ColIScan16x16[:], Neighbors: tables.ColScan16x16Neighbors[:]},
		{Scan: tables.DefaultScan16x16[:], IScan: tables.DefaultIScan16x16[:], Neighbors: tables.DefaultScan16x16Neighbors[:]},
	},
	{ // Tx32x32 — DCT_DCT only; the row/col rows of the table mirror the default.
		{Scan: tables.DefaultScan32x32[:], IScan: tables.DefaultIScan32x32[:], Neighbors: tables.DefaultScan32x32Neighbors[:]},
		{Scan: tables.DefaultScan32x32[:], IScan: tables.DefaultIScan32x32[:], Neighbors: tables.DefaultScan32x32Neighbors[:]},
		{Scan: tables.DefaultScan32x32[:], IScan: tables.DefaultIScan32x32[:], Neighbors: tables.DefaultScan32x32Neighbors[:]},
		{Scan: tables.DefaultScan32x32[:], IScan: tables.DefaultIScan32x32[:], Neighbors: tables.DefaultScan32x32Neighbors[:]},
	},
}

// GetCoefContext maps a coefficient index c to a 0..2 context bin using
// the (above, left) entries of token_cache addressed by neighbors. This
// is the body of libvpx's get_coef_context, expressed as a function so
// the inlining decision is the Go compiler's. Returns int to match
// libvpx's return type even though only values 0..2 actually appear.
func GetCoefContext(neighbors, tokenCache []int16, c int) int {
	above := tokenCache[neighbors[MaxNeighbors*c+0]]
	left := tokenCache[neighbors[MaxNeighbors*c+1]]
	return int((1 + above + left) >> 1)
}
