package encoder

import "testing"

func TestMacroblockMeanLumaSSEUsesClampedVisibleEdges(t *testing.T) {
	src := SourceImage{
		Width:   17,
		Height:  17,
		YStride: 17,
		Y:       make([]byte, 17*17),
	}
	for y := range src.Height {
		for x := range src.Width {
			src.Y[y*src.YStride+x] = byte(y*src.Width + x)
		}
	}
	got := MacroblockMeanLumaSSE(src, 1, 1)

	sum := 0
	sse := 0
	for row := range 16 {
		y := min(16+row, src.Height-1)
		for col := range 16 {
			x := min(16+col, src.Width-1)
			v := int(src.Y[y*src.YStride+x])
			sum += v
			sse += v * v
		}
	}
	want := max(sse-int((int64(sum)*int64(sum)+128)>>8), 0)
	if got != want {
		t.Fatalf("MacroblockMeanLumaSSE edge MB = %d, want %d", got, want)
	}
}
