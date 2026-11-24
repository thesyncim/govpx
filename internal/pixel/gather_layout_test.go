package pixel

import "testing"

var gatherLayoutSink int16

func BenchmarkGatherBlockLayout(b *testing.B) {
	for _, c := range []struct {
		name string
		w    int
	}{{"16x16", 16}, {"8x8", 8}} {
		b.Run(c.name, func(b *testing.B) {
			const stride = 32
			src, pred := make([]byte, (c.w-1)*stride+c.w), make([]byte, (c.w-1)*stride+c.w)
			out := make([]int16, c.w*c.w)
			for i := range src {
				src[i] = byte(i*37 + 71)
				pred[i] = byte(i*13 + 199)
			}
			b.ReportAllocs()
			for b.Loop() {
				Gather(src, stride, pred, stride, out, c.w)
			}
			gatherLayoutSink = out[len(out)-1]
		})
	}
}

// Read-only rows may overlap or repeat. Pairing must preserve source windows
// and every 4x4 coefficient position even when successive rows share bytes.
func TestGatherRepeatedAndOverlappingRows(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, strides := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 3}, {w - 1, w + 1}} {
			for align := 0; align < 8; align++ {
				ss, ps := strides[0], strides[1]
				src, pred := make([]byte, align+(w-1)*ss+w), make([]byte, align+(w-1)*ps+w)
				for i := range src {
					src[i] = byte(i*73 + 197)
				}
				for i := range pred {
					pred[i] = byte(i*131 + 7)
				}
				out := make([]int16, w*w+2)
				out[0] = 12345
				out[len(out)-1] = -12345
				Gather(src[align:], ss, pred[align:], ps, out[1:len(out)-1], w)
				for y := range w {
					for x := range w {
						index := ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
						want := int16(src[align+y*ss+x]) - int16(pred[align+y*ps+x])
						if out[1+index] != want {
							t.Fatalf("w%d strides%v align%d xy%d,%d got%d want%d", w, strides, align, x, y, out[1+index], want)
						}
					}
				}
				if out[0] != 12345 || out[len(out)-1] != -12345 {
					t.Fatal("gather changed output guards")
				}
			}
		}
	}
}
