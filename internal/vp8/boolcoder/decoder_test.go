package boolcoder

import (
	"errors"
	"testing"
)

func TestReadLiteralDeterministic(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := []uint32{
		d.ReadLiteral(1),
		d.ReadLiteral(3),
		d.ReadLiteral(8),
		d.ReadLiteral(5),
		d.ReadLiteral(12),
	}
	want := []uint32{0, 6, 172, 6, 3635}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("literal[%d] = %d, want %d; got sequence %v", i, got[i], want[i], got)
		}
	}
	if err := d.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func TestReadBoolDeterministic(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0x9d, 0x01, 0x2a, 0xff, 0x00, 0x80, 0x42, 0x24}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	probs := []uint8{128, 1, 255, 17, 64, 192, 99, 128, 128, 220, 7, 33}
	got := make([]uint8, len(probs))
	for i, p := range probs {
		got[i] = d.ReadBool(p)
	}
	want := []uint8{1, 1, 0, 1, 0, 0, 1, 1, 1, 0, 1, 1}
	for i, p := range probs {
		if got[i] != want[i] {
			t.Fatalf("bit[%d] with prob %d = %d, want %d; got sequence %v", i, p, got[i], want[i], got)
		}
	}
	if err := d.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func TestTruncatedInputReportsStableError(t *testing.T) {
	var d Decoder
	if err := d.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	_ = d.ReadBit()
	if !errors.Is(d.Err(), ErrTruncated) {
		t.Fatalf("Err = %v, want ErrTruncated", d.Err())
	}
	if !d.Corrupted() {
		t.Fatalf("Corrupted = false, want true")
	}
}

func TestDecoderReuse(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	first := d.ReadLiteral(4)
	if err := d.Init([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}
	second := d.ReadLiteral(4)
	if first == second {
		t.Fatalf("reuse did not reset state: first=%d second=%d", first, second)
	}
}

func TestDecoderAllocatesZeroAfterInit(t *testing.T) {
	var d Decoder
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	if err := d.Init(src); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = d.ReadBool(128)
		_ = d.ReadLiteral(3)
		_ = d.Err()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkReadBool(b *testing.B) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var d Decoder
	_ = d.Init(src)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if d.Err() != nil {
			_ = d.Init(src)
		}
		_ = d.ReadBool(128)
	}
}

func BenchmarkReadLiteral8(b *testing.B) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var d Decoder
	_ = d.Init(src)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if d.Err() != nil {
			_ = d.Init(src)
		}
		_ = d.ReadLiteral(8)
	}
}
