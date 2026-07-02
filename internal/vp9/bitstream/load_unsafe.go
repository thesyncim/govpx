//go:build amd64 || arm64

package bitstream

import (
	"math/bits"
	"unsafe"
)

// load64BE reads 8 bytes big-endian from b at offset i. The unaligned
// raw load plus byte-reverse intrinsic keeps the inline cost low enough
// that FillBits — and therefore every boolean-decoder refill — stays
// call-free inside hot loops. amd64 and arm64 both support unaligned
// 64-bit loads.
func load64BE(b []byte, i int) uint64 {
	_ = b[i+7] // bounds check for the full 8-byte window
	return bits.ReverseBytes64(*(*uint64)(unsafe.Pointer(&b[i])))
}
