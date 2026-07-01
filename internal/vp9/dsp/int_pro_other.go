//go:build !arm64 || purego

package dsp

// Scalar-only builds: the batched integer-projection helpers fall
// through to the portable loops in int_pro.go.

func intProRowStripsAsm(hbuf []int16, ref []uint8, refOff, refStride, height, strips int) bool {
	return false
}

func intProColsAsm(vbuf []int16, ref []uint8, refOff, refStride, width, rows, normFactor int) bool {
	return false
}
