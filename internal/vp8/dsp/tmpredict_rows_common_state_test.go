package dsp

import (
	"bytes"
	"testing"
)

// This common reference test uses only complete windows, disjoint input/output
// planes and nonoverlapping output rows. Historical assembly and current Go
// kernels have different malformed-window/alias contracts; those remain covered
// by the current-only frozen-state test. This file imports no pixel internals.
func TestTMPredictRowsNativeState(t *testing.T) {
	for _, w := range []int{4, 8, 16} {
		for _, ds := range []int{w, 37, 1312} {
			for _, pattern := range []string{"flat", "mixed", "clipped"} {
				for _, align := range []int{0, 1, 7, 15} {
					for _, top := range []byte{0, 128, 255} {
						span := (w-1)*ds + w
						off, ao, lo := 7+align, 7+align, 11+15-align
						storage := bytes.Repeat([]byte{0x6b}, off+span+31)
						above, left := make([]byte, w+47), make([]byte, w+47)
						for i := range above {
							above[i], left[i] = byte(i*37+11), byte(i*29+251)
						}
						for i := 0; i < w; i++ {
							switch pattern {
							case "flat":
								above[ao+i], left[lo+i] = 128, 128
							case "clipped":
								above[ao+i], left[lo+i] = byte((i&1)*255), byte((i&1)*255)
							}
						}
						a, l := above[ao:ao+w:ao+w], left[lo:lo+w:lo+w]
						abefore, lbefore := bytes.Clone(above), bytes.Clone(left)
						expected := bytes.Clone(storage)
						for y := 0; y < w; y++ {
							for x := 0; x < w; x++ {
								v := int(a[x]) + int(l[y]) - int(top)
								if v < 0 {
									v = 0
								}
								if v > 255 {
									v = 255
								}
								expected[off+y*ds+x] = byte(v)
							}
						}
						dst := storage[off : off+span : off+span]
						if w == 4 {
							Intra4x4TMPredict(dst, ds, a, l, top)
						} else if w == 8 {
							IntraTMPredict8x8(dst, ds, a, l, top)
						} else {
							IntraTMPredict16x16(dst, ds, a, l, top)
						}
						if !bytes.Equal(storage, expected) || !bytes.Equal(above, abefore) || !bytes.Equal(left, lbefore) {
							t.Fatalf("w%d ds%d pattern%s align%d top%d full state mismatch", w, ds, pattern, align, top)
						}
					}
				}
			}
		}
	}
}
