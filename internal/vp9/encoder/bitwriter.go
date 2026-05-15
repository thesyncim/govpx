package encoder

// BitWriter is the encoder-side complement of internal/vp9/decoder.BitReader.
// Packs bits MSB-first into a caller-provided buffer. Ported from libvpx
// v1.16.0 vpx_dsp/bitwriter_buffer.{h,c} — vpx_wb_write_bit,
// vpx_wb_write_literal, and vpx_wb_bytes_written.
//
// The buffer is owned by the caller; BitWriter only tracks the current
// bit position. Zero-alloc on the hot path; Bytes / BytesWritten are
// constant-time.
type BitWriter struct {
	buf    []byte
	bitPos int // total bits written from the start of buf
}

// NewBitWriter returns a BitWriter that packs into `buf`. The caller
// is responsible for sizing `buf` large enough for the expected
// header payload; libvpx's uncompressed header tops out at ~32 bytes.
func NewBitWriter(buf []byte) *BitWriter {
	return &BitWriter{buf: buf}
}

// Init resets w to pack into buf. It is the stack-owned form of
// NewBitWriter for hot paths that reuse caller-owned writers.
func (w *BitWriter) Init(buf []byte) {
	w.buf = buf
	w.bitPos = 0
}

// WriteBit writes a single bit, value 0 or 1.
func (w *BitWriter) WriteBit(v uint32) {
	off := w.bitPos
	w.bitPos++
	idx := off >> 3
	if off&7 == 0 {
		w.buf[idx] = 0
	}
	mask := byte(1 << (7 - uint(off&7)))
	if int(v&1) != 0 {
		w.buf[idx] |= mask
	} else {
		w.buf[idx] &^= mask
	}
}

// WriteLiteral writes the low `bits` bits of `value`, MSB-first.
// Mirrors vpx_wb_write_literal exactly.
func (w *BitWriter) WriteLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.WriteBit((value >> uint(bit)) & 1)
	}
}

// WriteSignedLiteral writes a sign-magnitude signed value: `bits`
// magnitude bits MSB-first, then a 1-bit sign. Mirrors
// vpx_wb_write_signed_literal in libvpx.
func (w *BitWriter) WriteSignedLiteral(value int32, bits int) {
	mag := value
	sign := uint32(0)
	if mag < 0 {
		mag = -mag
		sign = 1
	}
	w.WriteLiteral(uint32(mag), bits)
	w.WriteBit(sign)
}

// BytesWritten returns the byte count rounded up to cover every bit
// emitted so far. Mirrors vpx_wb_bytes_written.
func (w *BitWriter) BytesWritten() int {
	return (w.bitPos + 7) >> 3
}

// BitsWritten returns the exact bit count emitted.
func (w *BitWriter) BitsWritten() int { return w.bitPos }
