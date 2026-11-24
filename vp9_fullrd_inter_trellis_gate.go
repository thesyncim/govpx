package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// vp9DoTrellisOptInterY mirrors libvpx do_trellis_opt (vp9/encoder/vp9_encoder.h
// :1317-1366) for the inter Y/UV planes on the full-RD mode-selection path: it
// decides whether block_rd_txfm / super_block_(u)vrd runs vp9_optimize_b (the
// coefficient trellis, vp9_rdopt.c:797-802) for a transform block.
//
// The decision is driven by cpi->sf.trellis_opt_tx_rd.method:
//
//   - DISABLE_TRELLIS_OPT  → false (no trellis). REALTIME speed >= 1 (e.g. cpu4)
//     sets this (vp9_speed_features.c:485-488), so the RAW quantizer eob/coeffs
//     are kept verbatim. This is the case the {0,1,1,0,1} VAR_BASED cpu4 seed
//     hits — running the trellis there (as the producers previously did
//     unconditionally) wrongly zeroed AC coefficients libvpx keeps, inflating
//     dist and shrinking rate, which flipped per-leaf mode decisions deep in the
//     superblock (e.g. mi(2,3) NEARMV vs NEWMV).
//   - ENABLE_TRELLIS_OPT   → true (always trellis). REALTIME speed 0 (cpu0) keeps
//     the default from vp9_set_speed_features_framesize_independent
//     (vp9_speed_features.c:975-976), so the {0,2,0,0,2} cpu0 producer pins are
//     unchanged.
//
// The two threshold-gated methods (ENABLE_TRELLIS_OPT_TX_RD_SRC_VAR /
// _RESIDUAL_MSE) are used only by the GOOD-quality (VOD) path, which never
// reaches these realtime-only full-RD inter producers; they are treated as
// "enabled" (libvpx's default return 1 when the per-block threshold data is
// absent, vp9_encoder.h:1331-1333,1350-1352) so behaviour stays defined if a
// future caller wires them in.
func (e *VP9Encoder) vp9DoTrellisOptInterY(txSize common.TxSize) bool {
	return e.sf.TrellisOptTxRd.Method != DisableTrellisOpt
}
