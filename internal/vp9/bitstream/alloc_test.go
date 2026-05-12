package bitstream

import "testing"

// TestReaderReadZeroAlloc asserts the steady-state Read path is
// allocation-free after Init. The fill path is reached repeatedly inside
// the loop so we cover both the inline normalize branch and the
// big-endian 64-bit refill.
func TestReaderReadZeroAlloc(t *testing.T) {
	buf := make([]byte, 4096)
	var w Writer
	w.Start(buf)
	for i := 0; i < 8192; i++ {
		w.Write(uint32(i&1), uint32(100+(i%150)))
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r Reader
	allocs := testing.AllocsPerRun(50, func() {
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		for i := 0; i < 8192; i++ {
			_ = r.Read(uint32(100 + (i % 150)))
		}
	})
	if allocs != 0 {
		t.Fatalf("Reader.Read steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestWriterWriteZeroAlloc asserts the steady-state Write path is
// allocation-free after Start.
func TestWriterWriteZeroAlloc(t *testing.T) {
	dst := make([]byte, 4096)
	var w Writer
	allocs := testing.AllocsPerRun(50, func() {
		w.Start(dst)
		for i := 0; i < 8192; i++ {
			w.Write(uint32(i&1), uint32(100+(i%150)))
		}
		if _, err := w.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Writer.Write steady state: got %v allocs/op, want 0", allocs)
	}
}
