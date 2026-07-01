package buffers

import (
	"io"
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

// EnsureCapacity returns buf sliced to size, growing it with make when needed.
func EnsureCapacity(buf []byte, size int) []byte {
	if cap(buf) < size {
		return make([]byte, size)
	}
	return buf[:size]
}

// EnsureLen returns buf sliced to size, growing it with make when needed.
// Existing elements are not cleared when storage is reused.
func EnsureLen[S ~[]E, E any](buf S, size int) S {
	if cap(buf) < size {
		return make(S, size)
	}
	return buf[:size]
}

// EnsureLenZeroed returns buf sliced to size and cleared. It grows with make
// when needed; reused storage is zeroed before it is returned.
func EnsureLenZeroed[S ~[]E, E any](buf S, size int) S {
	if cap(buf) < size {
		return make(S, size)
	}
	buf = buf[:size]
	clear(buf)
	return buf
}

// EnsureLenZeroTail returns buf sliced to size, clearing only newly exposed
// elements when the backing storage is reused. Existing elements below the old
// length are left untouched.
func EnsureLenZeroTail[S ~[]E, E any](buf S, size int) S {
	if cap(buf) < size {
		return make(S, size)
	}
	oldLen := len(buf)
	buf = buf[:size]
	if oldLen < size {
		clear(buf[oldLen:size])
	}
	return buf
}

// EnsureAlignedCapacity returns buf sliced to size with an aligned first byte,
// allocating aligned storage when the current backing array is too small or
// starts at the wrong boundary.
func EnsureAlignedCapacity(buf []byte, size int, align int) []byte {
	if cap(buf) < size {
		return NewAligned(size, align)
	}
	buf = buf[:size]
	if !ByteSliceAligned(buf, align) {
		return NewAligned(size, align)
	}
	return buf
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

// PlaneLen returns the backing bytes needed for rows of a visible-width plane
// stored in a larger stride. It counts only the visible bytes on the last row.
func PlaneLen(stride, rows, visibleWidth int) int {
	if rows <= 0 {
		return 0
	}
	return stride*(rows-1) + visibleWidth
}

// Chroma420Dimensions returns the 4:2:0 chroma plane dimensions for a luma
// rectangle of width by height pixels.
func Chroma420Dimensions(width, height int) (int, int) {
	uvWidth := width/2 + width%2
	uvHeight := height/2 + height%2
	return uvWidth, uvHeight
}

// I420FrameSize returns the raw bytes needed for one packed I420 frame.
// The boolean is false for invalid dimensions or int overflow.
func I420FrameSize(width, height int) (int, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height {
		return 0, false
	}
	y := width * height
	uvWidth, uvHeight := Chroma420Dimensions(width, height)
	if uvWidth > maxInt/uvHeight {
		return 0, false
	}
	uv := uvWidth * uvHeight
	if uv > (maxInt-y)/2 {
		return 0, false
	}
	return y + 2*uv, true
}

// AppendPlane appends the visible rows of a strided plane to dst.
func AppendPlane(dst []byte, plane []byte, stride, width, height int) []byte {
	for row := range height {
		off := row * stride
		dst = append(dst, plane[off:off+width]...)
	}
	return dst
}

// WritePlane writes the visible rows of a strided plane.
func WritePlane(w io.Writer, plane []byte, stride, width, height int) error {
	for row := range height {
		off := row * stride
		if _, err := w.Write(plane[off : off+width]); err != nil {
			return err
		}
	}
	return nil
}

// Fill writes value to every byte in buf.
func Fill(buf []byte, value byte) {
	if len(buf) < 64 {
		for i := range buf {
			buf[i] = value
		}
		return
	}
	buf[0] = value
	for filled := 1; filled < len(buf); {
		filled += copy(buf[filled:], buf[:filled])
	}
}

// AppendI420Planes appends one packed I420 frame in Y, U, V order.
func AppendI420Planes(dst []byte, width, height int,
	y []byte, yStride int, u []byte, uStride int, v []byte, vStride int,
) []byte {
	dst = AppendPlane(dst, y, yStride, width, height)
	uvWidth, uvHeight := Chroma420Dimensions(width, height)
	dst = AppendPlane(dst, u, uStride, uvWidth, uvHeight)
	return AppendPlane(dst, v, vStride, uvWidth, uvHeight)
}

// WriteI420Planes writes one packed I420 frame in Y, U, V order.
func WriteI420Planes(w io.Writer, width, height int,
	y []byte, yStride int, u []byte, uStride int, v []byte, vStride int,
) error {
	if err := WritePlane(w, y, yStride, width, height); err != nil {
		return err
	}
	uvWidth, uvHeight := Chroma420Dimensions(width, height)
	if err := WritePlane(w, u, uStride, uvWidth, uvHeight); err != nil {
		return err
	}
	return WritePlane(w, v, vStride, uvWidth, uvHeight)
}

// I420EncodeBufferSize returns a conservative output buffer size for codecs
// that encode one I420 frame into a caller-provided byte slice. The estimate
// is max(minSize, headerSlack+4*rawI420Bytes) and reports
// [vpxerrors.ErrInvalidConfig] on invalid dimensions or int overflow.
func I420EncodeBufferSize(width, height, headerSlack, minSize int) (int, error) {
	raw420, ok := I420FrameSize(width, height)
	if !ok || headerSlack < 0 || minSize < 0 {
		return 0, vpxerrors.ErrInvalidConfig
	}
	maxInt := int(^uint(0) >> 1)
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
