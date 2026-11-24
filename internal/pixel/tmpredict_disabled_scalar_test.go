package pixel

import (
	"bytes"
	"testing"
)

// Frozen pre-optimization pixel scalar semantics: left[y] is reread per pixel.
func tmDisabledOriginalScalar(dst []byte, ds int, above, left []byte, top byte, w int) {
	for y := range w {
		for x := range w {
			dst[y*ds+x] = byte(min(max(int(above[x])+int(left[y])-int(top), 0), 255))
		}
	}
}

func tmCheckDisabledScalarContract(t *testing.T) {
	t.Helper()
	if Enabled {
		t.Fatal("test requires actual disabled pixel dispatch")
	}
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{1, 3, 8, 16} {
		for _, ds := range []int{-1, 0, 1, w, w + 2, 37, maxInt} {
			span := 64
			if ds >= 0 && ds <= 37 {
				span = (w-1)*ds + w
			}
			for mode := 0; mode < 5; mode++ {
				for _, dlen := range []int{0, 1, span - 1, span} {
					for _, n := range []int{0, w - 1, w} {
						for _, nl := range []int{0, w - 1, w} {
							for _, top := range []byte{0, 128, 255} {
								off, ao, lo := 32, 32+span+17, 32+span+w+23
								switch mode {
								case 1:
									lo = off
								case 2:
									ao = off
								case 3:
									ao, lo = off, off
								case 4:
									lo = off + 1
								}
								original := make([]byte, off+span+2*w+47)
								for i := range original {
									original[i] = byte(i*37 + 11)
								}
								want := bytes.Clone(original)
								wp := tmBytesCapture(func() {
									tmDisabledOriginalScalar(want[off:off+dlen:off+dlen], ds, want[ao:ao+n:ao+n], want[lo:lo+nl:lo+nl], top, w)
								})
								for which, call := range []func([]byte, int, []byte, []byte, byte, int){TMPredict, TMPredictNative} {
									got := bytes.Clone(original)
									gp := tmBytesCapture(func() { call(got[off:off+dlen:off+dlen], ds, got[ao:ao+n:ao+n], got[lo:lo+nl:lo+nl], top, w) })
									if gp != wp || !bytes.Equal(got, want) {
										t.Fatalf("entry%d w%d ds%d mode%d dst%d refs%d/%d top%d panic%q/%q", which, w, ds, mode, dlen, n, nl, top, gp, wp)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
