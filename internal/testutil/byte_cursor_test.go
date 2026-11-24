package testutil

import "testing"

func TestByteCursorWrapsShortInputs(t *testing.T) {
	c := NewByteCursor([]byte{1, 2})
	if got := []byte{c.Next(), c.Next(), c.Next(), c.Next()}; string(got) != string([]byte{1, 2, 1, 2}) {
		t.Fatalf("wrapped bytes = %v, want [1 2 1 2]", got)
	}
	if c.Remaining() != -2 {
		t.Fatalf("Remaining = %d, want -2 after wrapped reads", c.Remaining())
	}
}

func TestByteCursorEmptyInputIsZero(t *testing.T) {
	c := NewByteCursor(nil)
	if c.Next() != 0 || c.U16LE() != 0 || c.Pick(8) != 0 {
		t.Fatalf("empty cursor produced non-zero values")
	}
}

func TestByteCursorU16LEAndPick(t *testing.T) {
	c := NewByteCursor([]byte{0x34, 0x12, 0xff})
	if got := c.U16LE(); got != 0x1234 {
		t.Fatalf("U16LE = %#x, want 0x1234", got)
	}
	if got := c.Pick(10); got != 5 {
		t.Fatalf("Pick = %d, want 5", got)
	}
	if got := c.Pick(1); got != 0 {
		t.Fatalf("Pick(1) = %d, want 0", got)
	}
}
