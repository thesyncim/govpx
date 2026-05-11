//go:build !govpx_oracle_trace

package encoder

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c coefficient token
// selection and vp8/encoder/bitstream.c coefficient token packing.

type MacroblockCoefficients struct {
	QCoeff [25][16]int16

	// EOB is the authoritative per-block end-of-block count, matching
	// libvpx's xd->eobs side channel. Token writers do not rescan QCoeff.
	EOB [25]uint8
}
