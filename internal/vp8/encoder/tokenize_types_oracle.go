//go:build govpx_oracle_trace

package encoder

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c coefficient token
// selection and vp8/encoder/bitstream.c coefficient token packing.

type MacroblockCoefficients struct {
	QCoeff [25][16]int16

	// EOB is the authoritative per-block end-of-block count, matching
	// libvpx's xd->eobs side channel. Token writers do not rescan QCoeff.
	EOB [25]uint8

	// OracleY1DCEOB1 tracks, per Y block 0..15, whether libvpx would leave
	// eob=1 from quantizing the original Y1 DC before the Y2 path consumes it.
	OracleY1DCEOB1 [16]uint8

	// OracleStaleY2* carries the trace-only stale Y2 block visible in libvpx
	// oracle rows for SPLITMV/B_PRED macroblocks.
	OracleStaleY2EOB    uint8
	OracleStaleY2QCoeff [16]int16
	OracleStaleY2Set    bool
}
