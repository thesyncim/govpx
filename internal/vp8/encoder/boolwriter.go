package encoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/boolhuff.c and
// vp8/encoder/boolhuff.h.

var ErrBufferTooSmall = errors.New("govpx: VP8 encoder buffer too small")

type BoolWriter struct {
	err   error
	buf   []byte
	count int
	pos   int
	low   uint32
	rng   uint32
}

func (w *BoolWriter) Init(dst []byte) {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.pos = 0
	w.buf = dst
	w.err = nil
}

// WriteBit encodes a single bit at the fixed probability of 128.
// It is exactly equivalent to WriteBool(bit, 128).
func (w *BoolWriter) WriteBit(bit uint8) {
	if w.err != nil {
		return
	}

	split := (w.rng + 1) >> 1
	// Branchless conditional select keyed on bit: mask is all-ones when
	// bit is set, zero otherwise, so the add and the rng-split fold-in
	// vanish on the bit==0 path without a jump.
	mask := -uint32(bit & 1)
	low := w.low + (split & mask)
	rng := split + ((w.rng - 2*split) & mask)

	shift := int(tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		w.writeBoolFlush(low, rng, count, shift)
		return
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

// WriteBool encodes a single bit with the given (8-bit) probability.
// This is the encoder hot path: it is invoked tens of times per
// macroblock.
func (w *BoolWriter) WriteBool(bit uint8, probability uint8) {
	if w.err != nil {
		return
	}

	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))
	// Branchless conditional select keyed on bit: mask is all-ones when
	// the bit is set, zero otherwise. The `bit==0` arm keeps rng=split
	// and low unchanged; the `bit==1` arm folds in (w.rng - split) -
	// split = w.rng - 2*split.
	mask := -uint32(bit & 1)
	low := w.low + (split & mask)
	rng := split + ((w.rng - 2*split) & mask)

	shift := int(tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		w.writeBoolFlush(low, rng, count, shift)
		return
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func (w *BoolWriter) writeBoolFlush(low uint32, rng uint32, count int, shift int) {
	offset := shift - count
	if ((low << uint(offset-1)) & 0x80000000) != 0 {
		w.propagateCarry()
		if w.err != nil {
			return
		}
	}
	if w.pos >= len(w.buf) {
		w.err = ErrBufferTooSmall
		return
	}

	w.buf[w.pos] = byte((low >> uint(24-offset)) & 0xff)
	w.pos++
	shift = count
	low = uint32((uint64(low) << uint(offset)) & 0xffffff)
	count -= 8
	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

// WriteLiteral encodes the lower 'bits' bits of value, MSB first, each
// at probability 128. It is the literal-bit equivalent of
// vp8_encode_value in libvpx.
//
// The per-bit body is inlined here (rather than looping over WriteBit)
// so the encoder state stays in registers across the per-bit
// iterations and the buffer-error sentinel is checked only once per
// call. The carry / byte-emit case is identical to WriteBit but reuses
// the in-register accumulator instead of round-tripping through the
// BoolWriter struct on each iteration.
func (w *BoolWriter) WriteLiteral(value uint32, bits int) {
	if bits <= 0 || w.err != nil {
		return
	}

	low := w.low
	rng := w.rng
	count := w.count
	pos := w.pos
	buf := w.buf

	for bit := bits - 1; bit >= 0; bit-- {
		split := (rng + 1) >> 1
		// Branchless interval selection keyed on the literal bit.
		mask := -((value >> uint(bit)) & 1)
		low += split & mask
		rng = split + ((rng - 2*split) & mask)

		shift := int(tables.BoolNorm[byte(rng)])
		rng <<= uint(shift)
		count += shift

		if count < 0 {
			low <<= uint(shift)
			continue
		}

		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			// Spill the byte cursor back so propagateCarry sees the
			// up-to-date pos, then reload.
			w.pos = pos
			w.propagateCarry()
			if w.err != nil {
				return
			}
		}
		if pos >= len(buf) {
			w.err = ErrBufferTooSmall
			w.pos = pos
			return
		}
		buf[pos] = byte((low >> uint(24-offset)) & 0xff)
		pos++
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8

		low <<= uint(shift)
	}

	w.low = low
	w.rng = rng
	w.count = count
	w.pos = pos
}

// Finish flushes the trailing bits of the bool coder so that the last
// byte is fully written out.
//
// Equivalent to vp8_stop_encode (32 zero bits encoded at p=128).
func (w *BoolWriter) Finish() {
	w.WriteLiteral(0, 32)
}

func (w *BoolWriter) BytesWritten() int {
	return w.pos
}

func (w *BoolWriter) Bytes() []byte {
	return w.buf[:w.pos]
}

func (w *BoolWriter) Err() error {
	return w.err
}

func (w *BoolWriter) propagateCarry() {
	x := w.pos - 1
	for x >= 0 && w.buf[x] == 0xff {
		w.buf[x] = 0
		x--
	}
	if x < 0 {
		w.err = ErrBufferTooSmall
		return
	}
	w.buf[x]++
}
