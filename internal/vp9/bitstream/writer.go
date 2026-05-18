package bitstream

import "github.com/thesyncim/govpx/internal/vp9/tables"

// Ported from libvpx v1.16.0:
//   - vpx_dsp/bitwriter.h (vpx_writer, vpx_write, vpx_write_bit, vpx_write_literal)
//   - vpx_dsp/bitwriter.c (vpx_start_encode, vpx_stop_encode)

// Writer is the VP9 boolean range coder writer. It mirrors vpx_writer
// byte-for-byte: same field layout, same split formula, same carry-fixup
// rule on the previously-emitted bytes. The output is appended into a
// caller-owned slice so the writer is allocation-free in steady state.
type Writer struct {
	lowValue uint32
	rng      uint32
	count    int32

	buf []byte
	pos uint32

	err bool
}

// Start seeds the writer with a caller-owned destination buffer. The
// buffer is written into directly; if a Write would advance past the end
// it is left unchanged and the writer goes into the error state. Start
// emits the marker bit (0) that the reader checks during Init.
func (w *Writer) Start(dst []byte) {
	w.lowValue = 0
	w.rng = 255
	w.count = -24
	w.err = false
	w.pos = 0
	w.buf = dst
	w.WriteBit(0)
}

// Write encodes one bit against the probability prob (out of 256). Body
// matches vpx_write line-for-line including the carry-propagation loop
// over previously-emitted 0xff bytes when a fresh byte would carry into
// them.
func (w *Writer) Write(bit, prob uint32) {
	rng := w.rng
	lowValue := w.lowValue
	count := w.count

	split := 1 + (((rng - 1) * prob) >> 8)
	rng = split
	if bit != 0 {
		lowValue += split
		rng = w.rng - split
	}

	shift := int32(tables.VpxNorm[byte(rng)])
	rng <<= uint(shift)
	count += shift

	if count >= 0 {
		offset := shift - count

		if !w.err {
			if (lowValue<<uint(offset-1))&0x80000000 != 0 {
				x := int(w.pos) - 1
				for x >= 0 && w.buf[x] == 0xff {
					w.buf[x] = 0
					x--
				}
				w.buf[x]++
			}

			if w.pos < uint32(len(w.buf)) {
				w.buf[w.pos] = byte((lowValue >> uint(24-offset)) & 0xff)
				w.pos++
			} else {
				w.err = true
			}
		}

		lowValue <<= uint(offset)
		shift = count
		lowValue &= 0xffffff
		count -= 8
	}

	lowValue <<= uint(shift)
	w.count = count
	w.lowValue = lowValue
	w.rng = rng
}

// WriteBit encodes one equally-likely bit. Equivalent to Write(bit, 128).
func (w *Writer) WriteBit(bit uint32) { w.Write(bit, 128) }

// WriteLiteral writes bits equally-likely bits of data, MSB first.
func (w *Writer) WriteLiteral(data, bits uint32) {
	for b := int(bits) - 1; b >= 0; b-- {
		w.WriteBit((data >> uint(b)) & 1)
	}
}

// Stop flushes the remaining 32 trailing bits and inserts a fix-up zero
// byte to avoid collisions with libvpx's superframe-index marker
// (0b110xxxxx). Returns the number of bytes written into the destination
// buffer and an error if the buffer overflowed at any point.
func (w *Writer) Stop() (int, error) {
	for range 32 {
		w.WriteBit(0)
	}

	if !w.err && w.pos > 0 && (w.buf[w.pos-1]&0xe0) == 0xc0 {
		if w.pos < uint32(len(w.buf)) {
			w.buf[w.pos] = 0
			w.pos++
		} else {
			w.err = true
		}
	}

	if w.err {
		return int(w.pos), ErrBufferOverflow
	}
	return int(w.pos), nil
}

// Pos returns the number of fully-emitted bytes so far. This is an
// approximate progress marker only — the boolean coder buffers bits in
// (lowValue, count) that have not been emitted yet. Intended for
// diagnostic probes that want to attribute output growth to a specific
// writer call site (e.g. CompressedHeaderProbe in the encoder package).
func (w *Writer) Pos() int { return int(w.pos) }

// ErrBufferOverflow is returned by Stop when the destination buffer
// passed to Start was too small to hold the encoded payload.
var ErrBufferOverflow = errBufferOverflow{}

type errBufferOverflow struct{}

func (errBufferOverflow) Error() string {
	return "govpx: VP9 bitstream writer overflowed destination buffer"
}
