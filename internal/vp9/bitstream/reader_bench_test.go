package bitstream

import "testing"

func benchmarkReaderFixture(b *testing.B, bits int, prob uint32) ([]byte, []uint32) {
	b.Helper()
	buf := make([]byte, bits/2)
	wants := make([]uint32, bits)
	var w Writer
	w.Start(buf)
	for i := range wants {
		bit := uint32((i ^ (i >> 2) ^ (i >> 5)) & 1)
		wants[i] = bit
		w.Write(bit, prob)
	}
	size, err := w.Stop()
	if err != nil {
		b.Fatalf("Stop: %v", err)
	}
	return buf[:size], wants
}

func BenchmarkReaderReadProb128(b *testing.B) {
	src, wants := benchmarkReaderFixture(b, 8192, 128)
	var r Reader
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Init(src); err != nil {
			b.Fatalf("Init: %v", err)
		}
		for _, want := range wants {
			if got := r.Read(128); got != want {
				b.Fatalf("got %d, want %d", got, want)
			}
		}
	}
}

func BenchmarkReaderReadBit(b *testing.B) {
	src, wants := benchmarkReaderFixture(b, 8192, 128)
	var r Reader
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Init(src); err != nil {
			b.Fatalf("Init: %v", err)
		}
		for _, want := range wants {
			if got := r.ReadBit(); got != want {
				b.Fatalf("got %d, want %d", got, want)
			}
		}
	}
}
