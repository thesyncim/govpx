package pixel

import (
	"bytes"
	"runtime"
	"testing"
)

func TestTMPredictBytesDeclinesBeforeWrites(t *testing.T) {
	selected := Enabled && runtime.GOARCH == "arm64"
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{8, 16} {
		for _, ds := range []int{-1, 0, 1, w - 1, w, 37, (1 << 30) - 1, 1 << 30, maxInt, -maxInt - 1} {
			span := 512
			if ds >= 0 && ds <= 37 {
				span = (w-1)*ds + w
			}
			for _, dlen := range []int{0, 1, span - 1, span} {
				for _, alen := range []int{0, w - 1, w} {
					for _, llen := range []int{0, w - 1, w} {
						for _, clip := range []bool{false, true} {
							got := bytes.Repeat([]byte{91}, span+17)
							want := bytes.Clone(got)
							a, l := make([]byte, w), make([]byte, w)
							for i := range a {
								a[i] = byte(i*37 + 11)
								l[i] = byte(i*23 + 251)
							}
							capD, capA, capL := span, w, w
							if clip {
								capD, capA, capL = dlen, alen, llen
							}
							eligible := selected && uint(ds)>>30 == 0 && (w-1)*ds+w <= dlen && alen >= w && llen >= w
							if eligible {
								tmBytesPrior(want[:dlen:capD], ds, a[:alen:capA], l[:llen:capL], 128, w)
							}
							ok := TMPredictBytes(got[:dlen:capD], ds, a[:alen:capA], l[:llen:capL], 128, w)
							if ok != eligible || !bytes.Equal(got, want) {
								t.Fatalf("w%d ds%d dst%d refs%d/%d clip%v handled%v want%v", w, ds, dlen, alen, llen, clip, ok, eligible)
							}
						}
					}
				}
			}
		}
	}
}
