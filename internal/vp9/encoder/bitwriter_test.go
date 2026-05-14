package encoder

import "testing"

// TestBitWriterPacksBitsMSBFirst checks the byte-layout invariant
// against a hand-built expected stream: the boolean 1, 0, 1, 1, 0,
// 1, 1, 1 in that order packs into 0xb7 (binary 10110111).
func TestBitWriterPacksBitsMSBFirst(t *testing.T) {
	buf := make([]byte, 1)
	w := NewBitWriter(buf)
	for _, b := range []uint32{1, 0, 1, 1, 0, 1, 1, 1} {
		w.WriteBit(b)
	}
	if buf[0] != 0xb7 {
		t.Errorf("buf[0] = %#x, want 0xb7", buf[0])
	}
	if got := w.BytesWritten(); got != 1 {
		t.Errorf("BytesWritten = %d, want 1", got)
	}
}

// TestBitWriterLiteralRoundsToFullByte: WriteLiteral writes the low
// `bits` bits in MSB-first order regardless of byte boundaries.
func TestBitWriterLiteralRoundsToFullByte(t *testing.T) {
	buf := make([]byte, 2)
	w := NewBitWriter(buf)
	w.WriteLiteral(0x123, 12) // 0001 0010 0011
	// First byte: 0001 0010 = 0x12. Second byte: 0011 ???? = 0x30.
	if buf[0] != 0x12 || buf[1] != 0x30 {
		t.Errorf("buf = %#x, %#x; want 0x12, 0x30", buf[0], buf[1])
	}
	if got := w.BytesWritten(); got != 2 {
		t.Errorf("BytesWritten = %d, want 2", got)
	}
	if got := w.BitsWritten(); got != 12 {
		t.Errorf("BitsWritten = %d, want 12", got)
	}
}

func TestBitWriterClearsZeroBitsOnReuse(t *testing.T) {
	buf := []byte{0xff}
	w := NewBitWriter(buf)
	w.WriteLiteral(0, 8)
	if buf[0] != 0 {
		t.Fatalf("buf[0] after zero rewrite = %#x, want 0", buf[0])
	}
}

// TestBitWriterSignedLiteral: writes magnitude then sign bit, matching
// libvpx's vpx_wb_write_signed_literal.
func TestBitWriterSignedLiteral(t *testing.T) {
	buf := make([]byte, 1)
	w := NewBitWriter(buf)
	// -5 with 3 magnitude bits: 101 then sign 1 → 1011 0000 = 0xb0.
	w.WriteSignedLiteral(-5, 3)
	if buf[0] != 0xb0 {
		t.Errorf("buf[0] = %#x, want 0xb0", buf[0])
	}
	if got := w.BitsWritten(); got != 4 {
		t.Errorf("BitsWritten = %d, want 4", got)
	}
}
