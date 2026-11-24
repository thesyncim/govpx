package decoder

// BitReader is the byte-aligned bit reader used by the VP9 uncompressed
// header — distinct from the boolean range coder used by the compressed
// header and tile payload. Ported from libvpx v1.16.0
// vpx_dsp/bitreader_buffer.{h,c}: reads MSB-first from a caller-owned
// buffer with a running bit offset.
//
// All methods on BitReader are allocation-free; the caller owns the
// source bytes.
type BitReader struct {
	buf       []byte
	bitOffset uint64
	err       bool
}

// Init seeds the reader with src. Resets any previously-accumulated
// error state so the BitReader can be reused across frames.
func (r *BitReader) Init(src []byte) {
	r.buf = src
	r.bitOffset = 0
	r.err = false
}

// BytesRead returns the number of whole bytes consumed so far,
// matching vpx_rb_bytes_read — rounded up so a partially-consumed byte
// still counts.
func (r *BitReader) BytesRead() int {
	return int((r.bitOffset + 7) >> 3)
}

// HasError reports whether any read since Init went past the end of
// the source buffer.
func (r *BitReader) HasError() bool { return r.err }

// ReadBit pulls a single MSB-first bit from the buffer. Bit-identical
// to vpx_rb_read_bit; returns 0 and sets the error flag on EOF.
func (r *BitReader) ReadBit() uint32 {
	off := r.bitOffset
	p := off >> 3
	q := 7 - uint(off&0x7)
	if int(p) >= len(r.buf) {
		r.err = true
		return 0
	}
	bit := uint32(r.buf[p]>>q) & 1
	r.bitOffset = off + 1
	return bit
}

// ReadLiteral pulls `bits` bits as a big-endian integer (MSB first).
// Bit-identical to vpx_rb_read_literal.
func (r *BitReader) ReadLiteral(bits int) uint32 {
	var v uint32
	for b := bits - 1; b >= 0; b-- {
		v |= r.ReadBit() << uint(b)
	}
	return v
}

// ReadSignedLiteral mirrors vpx_rb_read_signed_literal: the magnitude
// is read as an unsigned literal, then a trailing sign bit selects
// negation. Note the sign convention: 1 means negative.
func (r *BitReader) ReadSignedLiteral(bits int) int32 {
	v := int32(r.ReadLiteral(bits))
	if r.ReadBit() != 0 {
		return -v
	}
	return v
}
