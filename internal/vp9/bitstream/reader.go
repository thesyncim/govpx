package bitstream

import (
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

	// tail mirrors the final min(8, len(buf)) bytes of buf followed by
	// zeros, and tailBase is the buf position tail[0] corresponds to
	// (max(0, len(buf)-8)). Together they let the refill path issue one
	// unconditional 8-byte big-endian load for any pos: reads beyond the
	// logical end see zero bits, exactly like vpx_reader_fill's
	// byte-at-a-time tail loop.
	tail     [16]byte
	tailBase int
}

// ReaderState is a hot-loop-local view of Reader's arithmetic-coder state.
// libvpx's VP9 detokenizer keeps value, range, and count in locals through
// decode_coefs and writes them back once; this type gives the same shape to Go
// callers that need to batch many Read calls without exposing Reader fields.
type ReaderState struct {
	r     *Reader
	Value uint64
	Range uint32
	Count int32
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
	r.tailBase = max(0, len(src)-8)
	r.tail = [16]byte{}
	copy(r.tail[:], src[r.tailBase:])
	r.fill()
	if r.ReadBit() != 0 { // marker bit
		return ErrInvalidInput
	}
	return nil
}

// LocalState returns a local arithmetic-coder state snapshot. Call Commit
// before the underlying Reader is used again.
func (r *Reader) LocalState() ReaderState {
	return ReaderState{
		r:     r,
		Value: r.value,
		Range: r.rng,
		Count: r.count,
	}
}

// Read decodes one bit against the probability prob (out of 256) and
// returns 0 or 1. The body matches vpx_read line-for-line so the cache
// layout, normalization shift, and end-of-stream count update remain
// byte-identical to libvpx.
func (r *Reader) Read(prob uint32) uint32 {
	baseRange := r.rng
	split := (baseRange*prob + (256 - prob)) >> 8

	if r.count < 0 {
		r.fill()
	}

	value := r.value
	count := r.count
	bigsplit := uint64(split) << (valueBits - 8)

	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = baseRange - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	nextRange <<= shift
	value <<= shift
	count -= int32(shift)

	r.value = value
	r.count = count
	r.rng = nextRange
	return bit
}

// ReadBit decodes one equally-likely bit. Equivalent to Read(128).
func (r *Reader) ReadBit() uint32 {
	rng := r.rng
	split := (rng + 1) >> 1

	if r.count < 0 {
		r.fill()
	}

	value := r.value
	count := r.count
	bigsplit := uint64(split) << (valueBits - 8)

	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	nextRange <<= shift
	value <<= shift
	count -= int32(shift)

	r.value = value
	r.count = count
	r.rng = nextRange
	return bit
}

// Read decodes one bit against prob using local arithmetic-coder state.
func (s *ReaderState) Read(prob uint32) uint32 {
	baseRange := s.Range
	split := (baseRange*prob + (256 - prob)) >> 8

	if s.Count < 0 {
		s.fill()
	}

	value := s.Value
	count := s.Count
	bigsplit := uint64(split) << (valueBits - 8)

	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = baseRange - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	nextRange <<= shift
	value <<= shift
	count -= int32(shift)

	s.Value = value
	s.Count = count
	s.Range = nextRange
	return bit
}

// ReadBit decodes one equally-likely bit using local arithmetic-coder state.
func (s *ReaderState) ReadBit() uint32 {
	rng := s.Range
	split := (rng + 1) >> 1

	if s.Count < 0 {
		s.fill()
	}

	value := s.Value
	count := s.Count
	bigsplit := uint64(split) << (valueBits - 8)

	nextRange := split
	var bit uint32
	if value >= bigsplit {
		nextRange = rng - split
		value -= bigsplit
		bit = 1
	}

	shift := uint32(tables.VpxNorm[byte(nextRange)])
	nextRange <<= shift
	value <<= shift
	count -= int32(shift)

	s.Value = value
	s.Count = count
	s.Range = nextRange
	return bit
}

// Commit writes local arithmetic-coder state back to the underlying Reader.
func (s *ReaderState) Commit() {
	if s == nil || s.r == nil {
		return
	}
	s.r.value = s.Value
	s.r.rng = s.Range
	s.r.count = s.Count
}

// Fill refreshes local value/count variables from the underlying byte stream.
func (s *ReaderState) Fill(value *uint64, count *int32) {
	s.Value = *value
	s.Count = *count
	s.fill()
	*value = s.Value
	*count = s.Count
}

// ReadLiteral decodes bits equally-likely bits, MSB first.
func (r *Reader) ReadLiteral(bits int) uint32 {
	var literal uint32
	for b := bits - 1; b >= 0; b-- {
		literal |= r.ReadBit() << uint(b)
	}
	return literal
}

// ReadTree decodes a value from a binary token tree. `tree` is the
// VP9 token-tree array (int8 per libvpx's vpx_tree_index): positive
// entries are next-node indices, non-positive entries are leaf labels
// stored as the negation of the decoded value. `probs` carries one
// probability per internal-node index pair. Bit-identical to libvpx's
// vpx_read_tree in vpx_dsp/bitreader.h.
func (r *Reader) ReadTree(tree []int8, probs []uint8) int {
	i := int8(0)
	for {
		next := tree[int(i)+int(r.Read(uint32(probs[i>>1])))]
		if next <= 0 {
			return -int(next)
		}
		i = next
	}
}

// HasError mirrors vpx_reader_has_error: 1 iff a bit was requested after
// the end of stream was reached. Returns a bool here to match Go idiom.
func (r *Reader) HasError() bool {
	return r.count > valueBits && r.count < lotsOfBits
}

// fill refreshes the value register from the underlying buffer,
// mirroring vpx_reader_fill in vpx_dsp/bitreader.c (see FillBits).
func (r *Reader) fill() {
	r.value, r.count, r.pos = FillBits(r.buf, &r.tail, r.tailBase, r.pos,
		r.value, r.count)
}

func (s *ReaderState) fill() {
	s.Value, s.Count, s.r.pos = FillBits(s.r.buf, &s.r.tail, s.r.tailBase,
		s.r.pos, s.Value, s.Count)
}

// BitView exposes the refill window of the underlying Reader so external
// hot loops can keep the entire boolean-decoder state — including the
// buffer position — in local variables and refill via FillBits without
// any function call. Callers must write the advanced position back with
// CommitPos before the Reader is used again.
func (s *ReaderState) BitView() (buf []byte, tail *[16]byte, tailBase int, pos int) {
	return s.r.buf, &s.r.tail, s.r.tailBase, s.r.pos
}

// CommitPos writes a locally advanced buffer position back to the
// underlying Reader. Pairs with BitView.
func (s *ReaderState) CommitPos(pos int) {
	s.r.pos = pos
}

// FillFast is FillBits restricted to refills that happen more than 8
// bytes before the end of buf: no zero-padding redirect and no
// end-of-stream sentinel is possible there, so the whole refill is one
// unconditional big-endian load plus shift arithmetic. It is small
// enough to inline, which keeps guarded hot loops (see the coefficient
// detokenizer) completely call-free. Callers must guarantee
// len(buf)-pos >= 8 and hand the last few stream bytes to the general
// FillBits path.
func FillFast(buf []byte, pos int, value uint64, count int32) (uint64, int32, int) {
	shift := int32(valueBits-8) - (count + 8)
	be := load64BE(buf, pos)
	bits := (shift &^ 7) + 8
	value |= (be >> uint(64-bits)) << uint(shift&7)
	return value, count + bits, pos + int(bits>>3)
}

// FillBits is vpx_reader_fill on caller-held state. It must only be
// called when count < 0, matching libvpx's `if (r->count < 0)
// vpx_reader_fill(r)` discipline; the returned value/count/pos triple is
// bit- and error-exact with the upstream byte-loop implementation.
//
// tail/tailBase carry the Reader's zero-padded final-8-byte window: once
// fewer than 9 bytes remain, the 8-byte load is redirected there so the
// extra lanes read zeros (vpx_reader_fill shifts in nothing past the
// end) and the lotsOfBits end-of-stream sentinel is stamped into count
// exactly when the logical buffer runs out. The function is sized to
// stay within the inliner budget: keeping refills call-free lets Go hold
// hot-loop decoder state in registers, which is what libvpx gets from C
// callee-saved registers around its cold fill call.
func FillBits(buf []byte, tail *[16]byte, tailBase int, pos int,
	value uint64, count int32,
) (uint64, int32, int) {
	shift := int32(valueBits-8) - (count + 8)
	src := buf
	idx := pos
	if len(buf)-pos <= 8 {
		src = tail[:]
		idx = pos - tailBase
	}
	be := load64BE(src, idx)
	bits := (shift &^ 7) + 8
	value |= (be >> uint(64-bits)) << uint(shift&7)
	bitsLeft := int32(len(buf)-pos) * 8
	if shift+8 >= bitsLeft {
		count += bitsLeft + lotsOfBits
		pos = len(buf)
	} else {
		count += bits
		pos += int(bits >> 3)
	}
	return value, count, pos
}
