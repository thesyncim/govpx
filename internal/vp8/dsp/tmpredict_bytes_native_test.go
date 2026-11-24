package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var tmBytesNativeSink uint64

// Scalar arithmetic oracle; the benchmark's reference windows are disjoint.
func tmBytesNativeOracle(dst []byte, ds, w int, a, l []byte, top byte) {
	for y := 0; y < w; y++ {
		for x := 0; x < w; x++ {
			v := int(a[x]) + int(l[y]) - int(top)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			dst[y*ds+x] = byte(v)
		}
	}
}

func BenchmarkTMPredictRowsNative(b *testing.B) {
	for _, w := range []int{4, 8, 16} {
		for _, ds := range []int{w, 37, 1312} {
			for _, pattern := range []string{"flat", "mixed", "clipped"} {
				b.Run(fmt.Sprintf("%dx%d/pitch%d/%s", w, w, ds, pattern), func(b *testing.B) {
					span := (w-1)*ds + w
					storage := bytes.Repeat([]byte{0x6b}, span+31)
					above, left := make([]byte, w+31), make([]byte, w+31)
					top := byte(128)
					for i := range above {
						switch pattern {
						case "flat":
							above[i], left[i] = 128, 128
						case "mixed":
							above[i], left[i] = byte(i*37+11), byte(i*29+251)
							top = 137
						case "clipped":
							above[i], left[i] = byte((i&1)*255), byte((i&1)*255)
						}
					}
					a, l := above[7:7+w:7+w], left[11:11+w:11+w]
					abefore, lbefore := bytes.Clone(above), bytes.Clone(left)
					expected := bytes.Clone(storage)
					dst := storage[7 : 7+span : 7+span]
					tmBytesNativeOracle(expected[7:7+span:7+span], ds, w, a, l, top)
					// Preflight and post-loop comparisons include every output byte and canary.
					if w == 4 {
						Intra4x4TMPredict(dst, ds, a, l, top)
					} else if w == 8 {
						IntraTMPredict8x8(dst, ds, a, l, top)
					} else {
						IntraTMPredict16x16(dst, ds, a, l, top)
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
						b.Fatal("preflight full state mismatch")
					}
					b.ReportAllocs()
					for b.Loop() {
						if w == 4 {
							Intra4x4TMPredict(dst, ds, a, l, top)
						} else if w == 8 {
							IntraTMPredict8x8(dst, ds, a, l, top)
						} else {
							IntraTMPredict16x16(dst, ds, a, l, top)
						}
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
						b.Fatal("full output/input/canary mismatch")
					}
					var checksum uint64
					for y := 0; y < w; y++ {
						for x := 0; x < w; x++ {
							checksum += uint64(dst[y*ds+x])
						}
					}
					tmBytesNativeSink = checksum
				})
			}
		}
	}
}
