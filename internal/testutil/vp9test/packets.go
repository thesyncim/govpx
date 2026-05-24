package vp9test

import (
	"testing"

	vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// BitPacker writes MSB-first VP9 uncompressed-header bits for tests that need
// compact synthetic packets without depending on the root package.
type BitPacker struct {
	buf []byte
	pos int
}

func (p *BitPacker) WriteBit(b uint32) {
	if p.pos == 0 {
		p.buf = append(p.buf, 0)
	}
	if b != 0 {
		p.buf[len(p.buf)-1] |= 1 << (7 - p.pos)
	}
	p.pos = (p.pos + 1) & 7
}

func (p *BitPacker) WriteLiteral(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		p.WriteBit((v >> uint(i)) & 1)
	}
}

func (p *BitPacker) FlushByte() {
	if p.pos != 0 {
		p.pos = 0
	}
}

func (p *BitPacker) Bytes() []byte {
	p.FlushByte()
	out := make([]byte, len(p.buf))
	copy(out, p.buf)
	return out
}

func ShowExistingFramePacket(slot uint8) []byte {
	var pk BitPacker
	pk.WriteLiteral(2, 2)              // frame_marker = 0b10
	pk.WriteLiteral(0, 2)              // profile = 0
	pk.WriteBit(1)                     // show_existing_frame
	pk.WriteLiteral(uint32(slot&7), 3) // frame_to_show_map_idx
	return pk.Bytes()
}

func SuperframePacket(t testing.TB, frames ...[]byte) []byte {
	t.Helper()
	need, err := vp9bits.SuperframeSize(frames...)
	if err != nil {
		t.Fatalf("SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := vp9bits.PackSuperframeInto(packet, frames...)
	if err != nil {
		t.Fatalf("PackSuperframeInto: %v", err)
	}
	return packet[:n]
}
