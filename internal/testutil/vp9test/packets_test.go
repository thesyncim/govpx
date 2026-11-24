package vp9test

import (
	"bytes"
	"testing"

	vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"
)

func TestBitPackerWritesMSBFirst(t *testing.T) {
	var pk BitPacker
	pk.WriteLiteral(2, 2)
	pk.WriteBit(1)
	pk.WriteLiteral(0, 3)
	pk.WriteLiteral(3, 2)

	if got, want := pk.Bytes(), []byte{0xa3}; !bytes.Equal(got, want) {
		t.Fatalf("packed bits = %08b, want %08b", got, want)
	}
}

func TestShowExistingFramePacket(t *testing.T) {
	if got, want := ShowExistingFramePacket(5), []byte{0x8d}; !bytes.Equal(got, want) {
		t.Fatalf("show-existing packet = %08b, want %08b", got, want)
	}
}

func TestSuperframePacket(t *testing.T) {
	frames := [][]byte{{0x82}, {0x83, 0x00}}
	packet := SuperframePacket(t, frames...)
	sf, err := vp9bits.ParseSuperframe(packet)
	if err != nil {
		t.Fatalf("ParseSuperframe: %v", err)
	}
	if sf.Count != len(frames) {
		t.Fatalf("superframe count = %d, want %d", sf.Count, len(frames))
	}
	for i := range frames {
		if !bytes.Equal(sf.Frames[i], frames[i]) {
			t.Fatalf("frame %d = %x, want %x", i, sf.Frames[i], frames[i])
		}
	}
}
