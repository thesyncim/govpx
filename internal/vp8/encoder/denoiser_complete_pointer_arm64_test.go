//go:build goexperiment.simd && arm64 && !purego

package encoder

import (
	"bytes"
	"fmt"
	"testing"
)

func TestDenoiserCompletePointerWindowsScalar(t *testing.T) {
	requireCompleteDenoiser(t)
	layouts := []struct {
		ms, as, ss int
		mo, ao, so int
		shared     bool
	}{
		{16, 17, 23, 1, 3, 5, false},
		{17, 23, 31, 3, 5, 7, false},
		{31, 16, 19, 15, 1, 9, false},
		{23, 31, 17, 5, 15, 3, false},
		{17, 19, 23, 1, 3, 9, true},
		{23, 31, 17, 19, 5, 1, true},
		{19, 17, 31, 3, 7, 3, true},
	}
	clone := func(data [][]byte) [][]byte {
		copy := make([][]byte, len(data))
		for i := range data {
			copy[i] = bytes.Clone(data[i])
		}
		return copy
	}
	seen := map[int]bool{}
	for li, l := range layouts {
		t.Run(fmt.Sprint(li), func(t *testing.T) {
			mn, an, sn := 15*l.ms+16, 15*l.as+16, 15*l.ss+16
			si, mcSize := 1, l.mo+mn+13
			if l.shared {
				si, mcSize = 0, max(l.mo+mn, l.so+sn)+13
			}
			views := func(data [][]byte) (mc, avg, sig []byte) {
				// Exact visible spans retain spare backing capacity and canaries.
				return data[0][l.mo : l.mo+mn], data[2][l.ao : l.ao+an], data[si][l.so : l.so+sn]
			}
			for mode := range 3 {
				original := [][]byte{
					bytes.Repeat([]byte{0xa5}, mcSize),
					bytes.Repeat([]byte{0xd3}, l.so+sn+13),
					bytes.Repeat([]byte{0x79}, l.ao+an+13),
				}
				mc, _, sig := views(original)
				for y := range 16 {
					for x := range 16 {
						mc[y*l.ms+x] = byte(120 + (x+y)%3)
						if mode == 1 {
							mc[y*l.ms+x] = byte(x*17 + y*31)
						} else if mode == 2 {
							mc[y*l.ms+x] = 255
						}
					}
				}
				for y := range 16 {
					for x := range 16 {
						sig[y*l.ss+x] = byte(119 + (x+y)%3)
						if mode == 1 {
							sig[y*l.ss+x] = byte(x*29 + y*7)
						} else if mode == 2 {
							sig[y*l.ss+x] = 0
						}
					}
				}
				for _, motion := range []uint32{0, 25} {
					for _, increase := range []bool{false, true} {
						gotData, wantData := clone(original), clone(original)
						gm, ga, gs := views(gotData)
						wm, wa, ws := views(wantData)
						want := DenoiserFilterYScalar(wm, l.ms, wa, l.as, ws, l.ss, motion, increase)
						got, handled := denoiserFilterYCompleteSIMD(gm, l.ms, ga, l.as, gs, l.ss, motion, increase)
						if !handled || got != want {
							t.Fatalf("mode%d motion%d increase%v decision%d/%d handled%v", mode, motion, increase, got, want, handled)
						}
						for i := range gotData {
							if !bytes.Equal(gotData[i], wantData[i]) {
								t.Fatalf("mode%d motion%d increase%v backing%d differs, including padding/canaries", mode, motion, increase, i)
							}
						}
						seen[got] = true
					}
				}
				if mode == 0 {
					for which := range 3 {
						data := clone(original)
						m, a, s := views(data)
						args := [3][]byte{m, a, s}
						args[which] = args[which][:len(args[which])-1]
						if _, handled := denoiserFilterYCompleteSIMD(args[0], l.ms, args[1], l.as, args[2], l.ss, 0, false); handled {
							t.Fatalf("one-byte-short window%d accepted", which)
						}
						for i := range data {
							if !bytes.Equal(data[i], original[i]) {
								t.Fatalf("short window%d modified backing%d", which, i)
							}
						}
					}
				}
			}
		})
	}
	if !seen[DenoiserCopyBlock] || !seen[DenoiserFilterBlock] {
		t.Fatal("both complete decisions must be covered")
	}
}
