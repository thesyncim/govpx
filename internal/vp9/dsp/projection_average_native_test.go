package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

func TestProjectionColumnsExactWindows(t *testing.T) {
	for _, w := range []int{16, 32, 64, 128} {
		for _, rows := range []int{0, 1, 2, 3, 4, 5, 7, 16, 128} {
			for _, ss := range []int{0, w - 1, w, w + 7} {
				for _, align := range []int{0, 1, 7} {
					for _, pattern := range []int{0, 1, 2} {
						n := 0
						if rows > 0 {
							n = (rows-1)*ss + w
						}
						src := make([]byte, align+n)
						for i := range src {
							switch pattern {
							case 0:
								src[i] = 255
							case 1:
								src[i] = byte(i*37 + rows)
							case 2:
								src[i] = byte((i % 2) * 255)
							}
						}
						saved := bytes.Clone(src)
						for _, norm := range []int{0, 3, 5, 14, 15, 63, -1} {
							out := make([]int16, rows+2)
							out[0], out[len(out)-1] = -1234, 2345
							IntProCols(out[1:len(out)-1], src, align, ss, w, rows, norm)
							for y := range rows {
								var sum int16
								for x := range w {
									sum += int16(src[align+y*ss+x])
								}
								want := sum >> uint(norm)
								if out[y+1] != want {
									t.Fatalf("w%d rows%d stride%d align%d norm%d row%d got%d want%d", w, rows, ss, align, norm, y, out[y+1], want)
								}
							}
							if out[0] != -1234 || out[len(out)-1] != 2345 || !bytes.Equal(src, saved) {
								t.Fatal("projection changed guard or input")
							}
						}
					}
				}
			}
		}
	}
}

func TestAverageQuadExactWindows(t *testing.T) {
	for _, ss := range []int{0, 1, 8, 15, 16, 17, 1280} {
		for _, align := range []int{0, 1, 7} {
			for trial := range 5 {
				src := make([]byte, align+15*ss+16)
				for i := range src {
					switch trial {
					case 0:
						src[i] = 0
					case 1:
						src[i] = 255
					case 2:
						src[i] = byte((i % 2) * 255)
					default:
						src[i] = byte(i*37 + trial*13)
					}
				}
				saved := bytes.Clone(src)
				var got [4]int32
				Avg8x8Quad(src, align, ss, &got)
				for quad := range 4 {
					sum := 0
					for y := range 8 {
						for x := range 8 {
							sum += int(src[align+(y+8*(quad/2))*ss+x+8*(quad%2)])
						}
					}
					want := int32((sum + 32) >> 6)
					if got[quad] != want {
						t.Fatalf("stride%d align%d trial%d quad%d got%d want%d", ss, align, trial, quad, got[quad], want)
					}
				}
				if !bytes.Equal(src, saved) {
					t.Fatal("average changed input")
				}
			}
		}
	}
}

var projectionNativeResult int16

func BenchmarkProjectionColumnsNative(b *testing.B) {
	for _, w := range []int{16, 32, 64} {
		for _, rows := range []int{1, 7, 128} {
			b.Run(fmt.Sprintf("%dx%d", w, rows), func(b *testing.B) {
				ss := w + 7
				src := make([]byte, (rows-1)*ss+w)
				out := make([]int16, rows)
				for i := range src {
					src[i] = byte(i*37 + 11)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					IntProCols(out, src, 0, ss, w, rows, 3+(w>>5))
				}
				projectionNativeResult = out[rows-1]
			})
		}
	}
}
