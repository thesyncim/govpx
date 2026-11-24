package encoder

import "testing"

func TestFillResidual4x4SliceMatchesLibvpxEdgeExtension(t *testing.T) {
	const (
		width  = 6
		height = 5
		stride = 8
	)
	plane := make([]byte, stride*height)
	for row := range height {
		for col := range width {
			plane[row*stride+col] = byte(128 + row*11 + col)
		}
	}

	for _, tc := range []struct {
		name string
		x    int
		y    int
	}{
		{name: "interior", x: 1, y: 1},
		{name: "right_edge", x: 4, y: 1},
		{name: "bottom_edge", x: 1, y: 3},
		{name: "bottom_right_edge", x: 4, y: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got [16]int16
			var want [16]int16
			fillResidual4x4Slice(plane, stride, width, height, tc.x, tc.y, got[:])
			for row := range 4 {
				sampleY := min(tc.y+row, height-1)
				for col := range 4 {
					sampleX := min(tc.x+col, width-1)
					want[row*4+col] = int16(int(plane[sampleY*stride+sampleX]) - 128)
				}
			}
			if got != want {
				t.Fatalf("residuals = %v, want libvpx edge-extension residuals %v", got, want)
			}
		})
	}
}
