package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
)

func TestBoolWriterRoundTripsWithBoolDecoder(t *testing.T) {
	cases := []struct {
		bit  uint8
		prob uint8
	}{
		{bit: 0, prob: 128},
		{bit: 1, prob: 128},
		{bit: 1, prob: 200},
		{bit: 0, prob: 73},
		{bit: 1, prob: 1},
		{bit: 0, prob: 255},
	}
	var w BoolWriter
	buf := make([]byte, 32)
	w.Init(buf)
	for _, tc := range cases {
		w.WriteBool(tc.bit, tc.prob)
	}
	w.WriteLiteral(0xa5, 8)
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	for _, tc := range cases {
		if got := br.ReadBool(tc.prob); got != tc.bit {
			t.Fatalf("ReadBool(%d) = %d, want %d", tc.prob, got, tc.bit)
		}
	}
	if got := br.ReadLiteral(8); got != 0xa5 {
		t.Fatalf("ReadLiteral = 0x%x, want 0xa5", got)
	}
	if err := br.Err(); err != nil {
		t.Fatalf("Decoder error = %v, want nil", err)
	}
}

func TestBoolWriterReportsSmallBuffer(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 1))
	for i := 0; i < 16; i++ {
		w.WriteLiteral(0xff, 8)
	}
	w.Finish()
	if !errors.Is(w.Err(), ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", w.Err())
	}
	if w.BytesWritten() > 1 {
		t.Fatalf("bytes written = %d, want at most 1", w.BytesWritten())
	}
}

func TestBoolWriterAllocatesZero(t *testing.T) {
	var w BoolWriter
	buf := make([]byte, 64)
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		for i := 0; i < 16; i++ {
			w.WriteBool(uint8(i&1), uint8(17+i*13))
		}
		w.WriteLiteral(0x5a, 8)
		w.Finish()
		_ = w.Err()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkBoolWriterWriteBool(b *testing.B) {
	var w BoolWriter
	buf := make([]byte, 4096)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		for bit := 0; bit < 1024; bit++ {
			w.WriteBool(uint8(bit&1), uint8(1+(bit&254)))
		}
		w.Finish()
	}
}

func BenchmarkBoolWriterWriteLiteral(b *testing.B) {
	var w BoolWriter
	buf := make([]byte, 4096)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		for n := 0; n < 128; n++ {
			w.WriteLiteral(uint32(n), 8)
		}
		w.Finish()
	}
}
