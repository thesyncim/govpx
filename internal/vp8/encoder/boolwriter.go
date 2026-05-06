package encoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/boolhuff.c and
// vp8/encoder/boolhuff.h.

var ErrBufferTooSmall = errors.New("libgopx: VP8 encoder buffer too small")

type BoolWriter struct {
	low   uint32
	rng   uint32
	count int
	pos   int
	buf   []byte
	err   error
}

func (w *BoolWriter) Init(dst []byte) {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.pos = 0
	w.buf = dst
	w.err = nil
}

func (w *BoolWriter) WriteBit(bit uint8) {
	if w.err != nil {
		return
	}

	split := (w.rng + 1) >> 1
	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
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
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func (w *BoolWriter) WriteBool(bit uint8, probability uint8) {
	if w.err != nil {
		return
	}

	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))
	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
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
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func (w *BoolWriter) WriteLiteral(value uint32, bits int) {
	if bits <= 0 {
		return
	}
	for bit := bits - 1; bit >= 0; bit-- {
		w.WriteBit(uint8((value >> uint(bit)) & 1))
	}
}

func (w *BoolWriter) Finish() {
	for i := 0; i < 32; i++ {
		w.WriteBit(0)
	}
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
