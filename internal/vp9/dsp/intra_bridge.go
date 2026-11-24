package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// The SIMD bridge only accepts complete rows. Invalid windows retain the
// original predictor's partial-write/panic behavior.
func intraBridgeWindow(dst []byte, stride, bs int) bool {
	return pixel.Enabled && stride >= bs && len(dst) >= bs && stride <= (len(dst)-bs)/(bs-1)
}
func intraBridgeDisjoint(dst []byte, stride, bs int, edge []byte, count int) bool {
	return intraBridgeWindow(dst, stride, bs) && len(edge) >= count
}
