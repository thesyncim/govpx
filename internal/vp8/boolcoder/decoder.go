package boolcoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/dboolhuff.c
// - vp8/decoder/dboolhuff.h

var (
	ErrInvalidInput = errors.New("libgopx: invalid VP8 boolcoder input")
	ErrTruncated    = errors.New("libgopx: truncated VP8 boolcoder input")
)

const (
	valueSize  = 64
	lotsOfBits = 0x40000000
)

type Decoder struct {
	buf []byte
	pos int

	value uint64
	count int
	rng   uint32
}

func (d *Decoder) Init(src []byte) error {
	if src == nil {
		src = []byte{}
	}
	d.buf = src
	d.pos = 0
	d.value = 0
	d.count = -8
	d.rng = 255
	d.fill()
	return nil
}

func (d *Decoder) ReadBool(prob uint8) uint8 {
	if d.count < 0 {
		d.fill()
	}

	split := uint32(1 + (((d.rng - 1) * uint32(prob)) >> 8))
	bigsplit := uint64(split) << (valueSize - 8)

	value := d.value
	count := d.count
	rng := split
	bit := uint8(0)

	if value >= bigsplit {
		rng = d.rng - split
		value -= bigsplit
		bit = 1
	}

	shift := tables.BoolNorm[byte(rng)]
	rng <<= shift
	value <<= shift
	count -= int(shift)

	d.value = value
	d.count = count
	d.rng = rng
	return bit
}

func (d *Decoder) ReadBit() uint8 {
	return d.ReadBool(128)
}

func (d *Decoder) ReadLiteral(bits int) uint32 {
	var v uint32
	for bit := bits - 1; bit >= 0; bit-- {
		v |= uint32(d.ReadBit()) << uint(bit)
	}
	return v
}

func (d *Decoder) Err() error {
	if d.count > valueSize && d.count < lotsOfBits {
		return ErrTruncated
	}
	return nil
}

func (d *Decoder) Corrupted() bool {
	return d.Err() != nil
}

func (d *Decoder) Range() uint32 {
	return d.rng
}

func (d *Decoder) Count() int {
	return d.count
}

func (d *Decoder) Pos() int {
	return d.pos
}

func (d *Decoder) fill() {
	shift := valueSize - 8 - (d.count + 8)
	bytesLeft := len(d.buf) - d.pos
	bitsLeft := bytesLeft * 8
	x := shift + 8 - bitsLeft
	loopEnd := 0

	if x >= 0 {
		d.count += lotsOfBits
		loopEnd = x
	}

	if x < 0 || bitsLeft != 0 {
		for shift >= loopEnd {
			d.count += 8
			d.value |= uint64(d.buf[d.pos]) << uint(shift)
			d.pos++
			shift -= 8
		}
	}
}
