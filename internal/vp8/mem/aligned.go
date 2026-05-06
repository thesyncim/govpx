package mem

import "unsafe"

// Adapted from libvpx v1.16.0 vpx_mem/vpx_mem.c aligned allocation behavior
// for Go-owned byte slices.

// NewAligned returns a byte slice whose first element is aligned to align.
// It allocates backing memory and is intended for initialization-time buffers.
func NewAligned(size int, align int) []byte {
	if size <= 0 {
		return nil
	}
	if align <= 1 {
		return make([]byte, size)
	}
	if align&(align-1) != 0 {
		align = nextPowerOfTwo(align)
	}

	buf := make([]byte, size+align-1)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	off := int((uintptr(align) - ptr&uintptr(align-1)) & uintptr(align-1))
	return buf[off : off+size]
}

func nextPowerOfTwo(v int) int {
	if v <= 1 {
		return 1
	}
	v--
	for shift := 1; shift < intSizeBits; shift <<= 1 {
		v |= v >> shift
	}
	return v + 1
}

const intSizeBits = 32 << (^uint(0) >> 63)
