package govpx

import "testing"

func TestAverage2x2Clamped(t *testing.T) {
	plane := []byte{
		10, 20, 30,
		40, 50, 60,
		70, 80, 90,
	}
	tests := []struct {
		name string
		y    int
		x    int
		want int
	}{
		{name: "interior", y: 1, x: 1, want: 70},
		{name: "top left clamped", y: -1, x: -1, want: 30},
		{name: "bottom right clamped", y: 2, x: 2, want: 90},
		{name: "right edge", y: 1, x: 2, want: 75},
		{name: "bottom edge", y: 2, x: 1, want: 85},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := average2x2Clamped(plane, 3, 3, 3, tt.y, tt.x); got != tt.want {
				t.Fatalf("average2x2Clamped(..., %d, %d) = %d, want %d", tt.y, tt.x, got, tt.want)
			}
		})
	}
}

func BenchmarkAverage2x2ClampedInterior(b *testing.B) {
	const (
		width  = 1920
		height = 1080
		stride = width
	)
	plane := make([]byte, width*height)
	for i := range plane {
		plane[i] = byte(i)
	}
	b.ReportAllocs()
	sum := 0
	for i := 0; i < b.N; i++ {
		row := (i * 16) % (height - 1)
		col := (i * 16) % (width - 1)
		sum += average2x2Clamped(plane, stride, width, height, row, col)
	}
	if sum == 0 {
		b.Fatal(sum)
	}
}
