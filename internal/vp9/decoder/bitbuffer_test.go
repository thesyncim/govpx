package decoder

import "testing"

func TestBitReaderReadBit(t *testing.T) {
	// 0b10110001
	var r BitReader
	r.Init([]byte{0xB1})
	want := []uint32{1, 0, 1, 1, 0, 0, 0, 1}
	for i, w := range want {
		if got := r.ReadBit(); got != w {
			t.Errorf("bit %d: got %d, want %d", i, got, w)
		}
	}
	if r.HasError() {
		t.Fatal("unexpected error before EOF")
	}
	// One more bit -> EOF.
	r.ReadBit()
	if !r.HasError() {
		t.Fatal("expected error past EOF")
	}
}

func TestBitReaderReadLiteral(t *testing.T) {
	// 0xDEADBEEF in big-endian.
	var r BitReader
	r.Init([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	if got := r.ReadLiteral(32); got != 0xDEADBEEF {
		t.Errorf("got %x, want 0xDEADBEEF", got)
	}
	if r.BytesRead() != 4 {
		t.Errorf("BytesRead = %d, want 4", r.BytesRead())
	}
}

func TestBitReaderReadSignedLiteral(t *testing.T) {
	// Layout for 4-bit signed literal of 5, sign=0: 0b01010 (5, then 0)
	// then 4-bit signed literal of -5: 0b01011 (5, then 1).
	// Combined nibbles: 0101 0010 1100 ?000 padded -> 0x52, 0xC0.
	var r BitReader
	r.Init([]byte{0x52, 0xC0})
	if got := r.ReadSignedLiteral(4); got != 5 {
		t.Errorf("first signed literal: got %d, want 5", got)
	}
	if got := r.ReadSignedLiteral(4); got != -5 {
		t.Errorf("second signed literal: got %d, want -5", got)
	}
}

func TestBitReaderAlloc(t *testing.T) {
	src := []byte{0xAA, 0x55, 0xCC, 0x33, 0xF0, 0x0F, 0x12, 0x34}
	var r BitReader
	allocs := testing.AllocsPerRun(200, func() {
		r.Init(src)
		_ = r.ReadLiteral(8)
		_ = r.ReadBit()
		_ = r.ReadSignedLiteral(4)
		_ = r.ReadLiteral(12)
	})
	if allocs != 0 {
		t.Fatalf("BitReader steady state: got %v allocs/op, want 0", allocs)
	}
}
