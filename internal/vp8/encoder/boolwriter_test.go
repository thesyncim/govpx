package encoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
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
	for range 16 {
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

func TestBoolWriterWriteBitMatchesWriteBool128(t *testing.T) {
	pattern := []uint8{0, 1, 1, 0, 1, 0, 0, 1, 1, 1, 0}
	bufBit := make([]byte, 64)
	bufBool := make([]byte, 64)
	var bitWriter, boolWriter BoolWriter
	bitWriter.Init(bufBit)
	boolWriter.Init(bufBool)

	for _, bit := range pattern {
		bitWriter.WriteBit(bit)
		boolWriter.WriteBool(bit, 128)
	}
	bitWriter.WriteLiteral(0xa5, 8)
	for i := 7; i >= 0; i-- {
		boolWriter.WriteBool(uint8((0xa5>>uint(i))&1), 128)
	}
	bitWriter.Finish()
	for range 32 {
		boolWriter.WriteBool(0, 128)
	}

	if bitWriter.Err() != nil || boolWriter.Err() != nil {
		t.Fatalf("errors = %v/%v, want nil", bitWriter.Err(), boolWriter.Err())
	}
	if !bytes.Equal(bitWriter.Bytes(), boolWriter.Bytes()) {
		t.Fatalf("WriteBit bytes = %x, WriteBool bytes = %x", bitWriter.Bytes(), boolWriter.Bytes())
	}
}

func TestBoolWriterAllocatesZero(t *testing.T) {
	var w BoolWriter
	buf := make([]byte, 64)
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		for i := range 16 {
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
		for bit := range 1024 {
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
		for n := range 128 {
			w.WriteLiteral(uint32(n), 8)
		}
		w.Finish()
	}
}
