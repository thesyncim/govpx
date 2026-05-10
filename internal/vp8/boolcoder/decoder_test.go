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

func TestReadBoolMaxMatchesReadBool255(t *testing.T) {
	probs := []uint8{255, 128, 7, 255, 220, 33, 255, 99, 1, 255, 192}
	for seed := range 16 {
		src := []byte{
			byte(0x9d + seed*3),
			byte(0x01 ^ seed*11),
			byte(0x2a + seed*5),
			byte(0xff - seed),
			byte(0x00 + seed*7),
			byte(0x80 ^ seed*13),
			byte(0x42 + seed*17),
			byte(0x24 ^ seed*19),
		}
		var generic, specialized Decoder
		if err := generic.Init(src); err != nil {
			t.Fatalf("generic Init seed %d returned error: %v", seed, err)
		}
		if err := specialized.Init(src); err != nil {
			t.Fatalf("specialized Init seed %d returned error: %v", seed, err)
		}

		for step, prob := range probs {
			var got uint8
			if prob == 255 {
				got = specialized.ReadBoolMax()
			} else {
				got = specialized.ReadBool(prob)
			}
			want := generic.ReadBool(prob)
			if got != want ||
				specialized.value != generic.value ||
				specialized.count != generic.count ||
				specialized.rng != generic.rng ||
				specialized.pos != generic.pos {
				t.Fatalf("seed %d step %d prob %d: bit/state = %d/%x/%d/%d/%d, want %d/%x/%d/%d/%d",
					seed, step, prob,
					got, specialized.value, specialized.count, specialized.rng, specialized.pos,
					want, generic.value, generic.count, generic.rng, generic.pos)
			}
		}
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
