package buffers

import (
	"unsafe"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

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

// ByteSliceAligned reports whether buf starts on an align-byte boundary.
func ByteSliceAligned(buf []byte, align int) bool {
	if align <= 1 || len(buf) == 0 {
		return true
	}
	return uintptr(unsafe.Pointer(&buf[0]))%uintptr(align) == 0
}

// AlignmentPadding returns the prefix needed to align buf to align bytes.
func AlignmentPadding(buf []byte, align int) int {
	if align <= 1 || len(buf) == 0 {
		return 0
	}
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	rem := ptr % uintptr(align)
	if rem == 0 {
		return 0
	}
	return int(uintptr(align) - rem)
}

// AlignOffsetForSlice returns off rounded up so &buf[off] has align-byte
// alignment.
func AlignOffsetForSlice(buf []byte, off, align int) int {
	if align <= 1 || len(buf) == 0 {
		return off
	}
	ptr := uintptr(unsafe.Pointer(&buf[0])) + uintptr(off)
	rem := ptr % uintptr(align)
	if rem == 0 {
		return off
	}
	return off + int(uintptr(align)-rem)
}

// Align rounds v up to an align-byte boundary. Callers pass power-of-two
// alignments, matching the VPx frame-buffer layout contracts.
func Align(v, align int) int {
	if align <= 1 {
		return v
	}
	return (v + align - 1) &^ (align - 1)
}

// RoundUp rounds v up to the next multiple of align.
func RoundUp(v, align int) int {
	if align <= 1 {
		return v
	}
	r := v % align
	if r == 0 {
		return v
	}
	return v + align - r
}

// I420EncodeBufferSize returns a conservative output buffer size for codecs
// that encode one I420 frame into a caller-provided byte slice. The estimate
// is max(minSize, headerSlack+4*rawI420Bytes) and reports
// [vpxerrors.ErrInvalidConfig] on invalid dimensions or int overflow.
func I420EncodeBufferSize(width, height, headerSlack, minSize int) (int, error) {
	if width <= 0 || height <= 0 || headerSlack < 0 || minSize < 0 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height {
		return 0, vpxerrors.ErrInvalidConfig
	}
	y := width * height
	uvWidth := (width + 1) / 2
	uvHeight := (height + 1) / 2
	if uvWidth > maxInt/uvHeight {
		return 0, vpxerrors.ErrInvalidConfig
	}
	uv := uvWidth * uvHeight
	if uv > (maxInt-y)/2 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	raw420 := y + 2*uv
	if raw420 > (maxInt-headerSlack)/4 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	size := headerSlack + raw420*4
	if size < minSize {
		size = minSize
	}
	return size, nil
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
