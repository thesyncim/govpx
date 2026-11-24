package vp8test

import "testing"

func TestVP8TestKeyFramePacketHeader(t *testing.T) {
	packet := KeyFramePacket(320, 240, 17, 2, true)
	if len(packet) != 10 {
		t.Fatalf("len(packet) = %d, want 10", len(packet))
	}
	tag := uint32(packet[0]) | uint32(packet[1])<<8 | uint32(packet[2])<<16
	if tag&1 != 0 {
		t.Fatalf("frame type bit = %d, want keyframe", tag&1)
	}
	if got := int((tag >> 1) & 7); got != 2 {
		t.Fatalf("profile = %d, want 2", got)
	}
	if tag&(1<<4) == 0 {
		t.Fatal("show_frame bit is not set")
	}
	if got := int(tag >> 5); got != 17 {
		t.Fatalf("first partition size = %d, want 17", got)
	}
	if got := packet[3:6]; string(got) != string([]byte{0x9d, 0x01, 0x2a}) {
		t.Fatalf("sync code = % x, want 9d 01 2a", got)
	}
	width := int(packet[6]) | int(packet[7])<<8
	height := int(packet[8]) | int(packet[9])<<8
	if width != 320 || height != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", width, height)
	}
}

func TestVP8TestInterFramePacketHeader(t *testing.T) {
	packet := InterFramePacket(31, 1, false)
	if len(packet) != 3 {
		t.Fatalf("len(packet) = %d, want 3", len(packet))
	}
	tag := uint32(packet[0]) | uint32(packet[1])<<8 | uint32(packet[2])<<16
	if tag&1 == 0 {
		t.Fatal("frame type bit = 0, want interframe")
	}
	if got := int((tag >> 1) & 7); got != 1 {
		t.Fatalf("profile = %d, want 1", got)
	}
	if tag&(1<<4) != 0 {
		t.Fatal("show_frame bit is set")
	}
	if got := int(tag >> 5); got != 31 {
		t.Fatalf("first partition size = %d, want 31", got)
	}
}

func TestVP8TestPacketPayloadHelpers(t *testing.T) {
	first := FirstPartitionWithBaseQIndex(36)
	key := KeyFramePacketWithFirstPartition(16, 16, first)
	if len(key) < 10+len(first) {
		t.Fatalf("key packet length = %d, want at least %d", len(key), 10+len(first))
	}
	if got := string(key[10 : 10+len(first)]); got != string(first) {
		t.Fatal("key first partition was not copied")
	}

	tokens := []byte{0xaa, 0xbb}
	inter := InterFramePacketWithTokenPartitions(first, 10, tokens)
	tokenSizeOffset := 3 + len(first)
	if got := int(inter[tokenSizeOffset]) |
		int(inter[tokenSizeOffset+1])<<8 |
		int(inter[tokenSizeOffset+2])<<16; got != 10 {
		t.Fatalf("first token partition size = %d, want 10", got)
	}
	if got := string(inter[len(inter)-len(tokens):]); got != string(tokens) {
		t.Fatal("inter token payload was not copied")
	}
}
