package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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

func TestComputeSkinMap16x16MatchesReference(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "aligned", width: 384, height: 288},
		{name: "partial edge", width: 385, height: 289},
	} {
		t.Run(size.name, func(t *testing.T) {
			src := skinMapTestSource(size.width, size.height)
			rows := (size.height + 15) >> 4
			cols := (size.width + 15) >> 4
			count := rows * cols
			consec := make([]uint8, count)
			for i := range consec {
				switch {
				case i%11 == 0:
					consec[i] = 66
				case i%7 == 0:
					consec[i] = 31
				default:
					consec[i] = uint8((i * 5) & 15)
				}
			}
			got := make([]uint8, count)
			want := make([]uint8, count)
			computeSkinMap(src, rows, cols, consec, got)
			computeSkinMapReference(src, rows, cols, consec, want)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("skinMap[%d] = %d, want %d", i, got[i], want[i])
				}
			}
		})
	}
}

func BenchmarkComputeSkinMap16x16LargeFrame(b *testing.B) {
	benchmarkComputeSkinMap16x16LargeFrame(b, computeSkinMap)
}

func BenchmarkComputeSkinMap16x16ReferenceLargeFrame(b *testing.B) {
	benchmarkComputeSkinMap16x16LargeFrame(b, computeSkinMapReference)
}

func benchmarkComputeSkinMap16x16LargeFrame(b *testing.B, fn func(vp8enc.SourceImage, int, int, []uint8, []uint8)) {
	const (
		width  = 1280
		height = 720
	)
	src := skinMapTestSource(width, height)
	rows := (height + 15) >> 4
	cols := (width + 15) >> 4
	count := rows * cols
	consec := make([]uint8, count)
	for i := range consec {
		if i%5 == 0 {
			consec[i] = 70
		} else {
			consec[i] = uint8(i & 31)
		}
	}
	skinMap := make([]uint8, count)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fn(src, rows, cols, consec, skinMap)
	}
}

func skinMapTestSource(width int, height int) vp8enc.SourceImage {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	src := vp8enc.SourceImage{
		Y:        make([]byte, width*height),
		U:        make([]byte, uvWidth*uvHeight),
		V:        make([]byte, uvWidth*uvHeight),
		Width:    width,
		Height:   height,
		UVWidth:  uvWidth,
		UVHeight: uvHeight,
		YStride:  width,
		UStride:  uvWidth,
		VStride:  uvWidth,
	}
	for row := range height {
		for col := range width {
			src.Y[row*src.YStride+col] = byte(24 + ((row*13 + col*7) & 223))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			src.U[row*src.UStride+col] = byte(84 + ((row*3 + col*5) & 95))
			src.V[row*src.VStride+col] = byte(112 + ((row*7 + col*11) & 111))
		}
	}
	return src
}

func computeSkinMapReference(src vp8enc.SourceImage, rows int, cols int, consecZeroLast []uint8, skinMap []uint8) {
	count := rows * cols
	if count <= 0 || len(skinMap) < count || min(src.Width, src.Height) <= 0 {
		return
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	useSkin8x8 := src.Width*src.Height <= skinDetectionMaxSmallFrame
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			consecutive := 0
			if len(consecZeroLast) > index {
				consecutive = int(consecZeroLast[index])
			}
			var skin bool
			if useSkin8x8 {
				skin = computeSkin8x8Block(src, uvWidth, uvHeight, row, col, consecutive)
			} else {
				y := average2x2Clamped(src.Y, src.YStride, src.Width, src.Height, row*16+7, col*16+7)
				u := average2x2Clamped(src.U, src.UStride, uvWidth, uvHeight, row*8+3, col*8+3)
				v := average2x2Clamped(src.V, src.VStride, uvWidth, uvHeight, row*8+3, col*8+3)
				if consecutive <= 60 {
					motion := 1
					if consecutive > 25 {
						motion = 0
					}
					skin = skinPixel(y, u, v, motion)
				}
			}
			if skin {
				skinMap[index] = 1
			} else {
				skinMap[index] = 0
			}
		}
	}
	smoothSkinMap(rows, cols, skinMap[:count])
}
