package bitstream

import (
	"encoding/binary"
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// Ported from libvpx v1.16.0:
//   - vpx_dsp/bitreader.h (vpx_reader, vpx_read, vpx_read_bit, vpx_read_literal, vpx_read_tree)
//   - vpx_dsp/bitreader.c (vpx_reader_init, vpx_reader_fill, vpx_reader_find_end)

// valueBits is the bit width of the value buffer used by the VP9 boolean
// range coder. libvpx uses size_t; on the supported 64-bit targets that is
// 64 bits and the wire format depends on it being exactly 64 — the fill
// path reads big-endian 64-bit words straight out of the bitstream.
const valueBits = 64

// lotsOfBits is the sentinel libvpx adds to count when the coded buffer is
// exhausted. The exact value doesn't matter beyond "large positive, easy to
// load as an immediate"; we keep libvpx's choice byte-for-byte so error
// detection lines up with the upstream behavior.
const lotsOfBits = 0x40000000

// ErrInvalidInput indicates that vpx_reader_init found a nil buffer with a
// non-zero size, or the marker bit at the start of the compressed header
// was not zero.
var ErrInvalidInput = errors.New("govpx: invalid VP9 bitstream input")

// Reader is the VP9 boolean range coder reader. It is allocation-free in
// steady state; the only allocation happens (if any) inside the caller's
// owned input slice. Fields are ordered the way libvpx orders them in
// vpx_reader to keep cache layout similar.
type Reader struct {
	value uint64
	rng   uint32
	count int32

	buf []byte
	pos int
}

// Init seeds the reader from src, mirroring vpx_reader_init. It returns
// ErrInvalidInput if the first bit read after the initial fill is not
// zero (libvpx rejects the frame in that case).
func (r *Reader) Init(src []byte) error {
	r.buf = src
	r.pos = 0
	r.value = 0
	r.count = -8
	r.rng = 255
	r.fill()
	if r.Read(128) != 0 { // marker bit
		return ErrInvalidInput
	}
	return nil
}

// Read decodes one bit against the probability prob (out of 256) and
// returns 0 or 1. The body matches vpx_read line-for-line so the cache
// layout, normalization shift, and end-of-stream count update remain
// byte-identical to libvpx.
func (r *Reader) Read(prob uint32) uint32 {
	split := (r.rng*prob + (256 - prob)) >> 8

	if r.count < 0 {
		r.fill()
	}

	value := r.value
	count := r.count
	bigsplit := uint64(split) << (valueBits - 8)

	rng := split
	var bit uint32
	if value >= bigsplit {
		rng = r.rng - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(rng)])
	rng <<= shift
	value <<= shift
	count -= int32(shift)

	r.value = value
	r.count = count
	r.rng = rng
	return bit
}

// ReadBit decodes one equally-likely bit. Equivalent to Read(128).
func (r *Reader) ReadBit() uint32 { return r.Read(128) }

// ReadLiteral decodes bits equally-likely bits, MSB first.
func (r *Reader) ReadLiteral(bits int) uint32 {
	var literal uint32
	for b := bits - 1; b >= 0; b-- {
		literal |= r.Read(128) << uint(b)
	}
	return literal
}

// HasError mirrors vpx_reader_has_error: 1 iff a bit was requested after
// the end of stream was reached. Returns a bool here to match Go idiom.
func (r *Reader) HasError() bool {
	return r.count > valueBits && r.count < lotsOfBits
}

// fill refreshes the value register from the underlying buffer. It tries
// to satisfy the entire 64-bit window with a single big-endian 64-bit
// load when enough bytes remain; otherwise it falls back to the
// byte-at-a-time loop and stamps lotsOfBits into count when the buffer
// runs out. Layout follows vpx_reader_fill in vpx_dsp/bitreader.c.
//
//go:noinline
func (r *Reader) fill() {
	count := r.count
	value := r.value
	bytesLeft := len(r.buf) - r.pos
	bitsLeft := bytesLeft * 8
	shift := int(valueBits-8) - int(count+8)

	if bitsLeft > valueBits {
		bits := (shift &^ 7) + 8
		be := binary.BigEndian.Uint64(r.buf[r.pos:])
		nv := be >> uint(valueBits-bits)
		count += int32(bits)
		r.pos += bits >> 3
		value |= nv << uint(shift&7)
	} else {
		bitsOver := shift + 8 - bitsLeft
		loopEnd := 0
		if bitsOver >= 0 {
			count += lotsOfBits
			loopEnd = bitsOver
		}
		if bitsOver < 0 || bitsLeft > 0 {
			for shift >= loopEnd {
				count += 8
				value |= uint64(r.buf[r.pos]) << uint(shift)
				r.pos++
				shift -= 8
			}
		}
	}

	r.value = value
	r.count = count
}

// FindEnd rewinds the buffer pointer back to the byte after the last bit
// actually consumed, matching vpx_reader_find_end. Used by the decoder
// when walking from header into tile data.
func (r *Reader) FindEnd() int {
	for r.count > 8 && r.count < valueBits {
		r.count -= 8
		r.pos--
	}
	return r.pos
}
