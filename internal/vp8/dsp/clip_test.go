package dsp

import "testing"

func TestClipPixel(t *testing.T) {
	tests := []struct {
		in   int
		want uint8
	}{
		{in: -300, want: 0},
		{in: -1, want: 0},
		{in: 0, want: 0},
		{in: 128, want: 128},
		{in: 255, want: 255},
		{in: 256, want: 255},
		{in: 999, want: 255},
	}
	for _, tt := range tests {
		if got := ClipPixel(tt.in); got != tt.want {
			t.Fatalf("ClipPixel(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestClamp(t *testing.T) {
	if Clamp(-2, 4, 9) != 4 || Clamp(6, 4, 9) != 6 || Clamp(12, 4, 9) != 9 {
		t.Fatalf("Clamp returned unexpected values")
	}
}

func TestClipAllocatesZero(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		_ = ClipPixel(-1)
		_ = ClipPixelAdd(250, 20)
		_ = Clamp(10, 0, 4)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkClipPixel(b *testing.B) {
	sum := uint8(0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sum ^= ClipPixel(i - 128)
	}
	_ = sum
}
