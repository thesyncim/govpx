//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// flatReference is the eight-position path that LoopFilter used for the VP9
// flat filters before the sixteen-lane kernel existed.
func flatReference(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) {
	for base := 0; base < n; base += 8 {
		if !loopFilterNonFlat(p, q0+base*outer, step, outer, blimit, limit, thresh) {
			loopFilter8(p, q0+base*outer, step, outer, blimit, limit, thresh, kind)
		}
	}
}

func checkFlat16(t testing.TB, data []byte, q0, step, n, blimit, limit, thresh, kind int) {
	t.Helper()
	got, want := bytes.Clone(data), bytes.Clone(data)
	loopFilterFlatWide(got, q0, step, 1, n, blimit, limit, thresh, kind)
	flatReference(want, q0, step, 1, n, blimit, limit, thresh, kind)
	if !bytes.Equal(got, want) {
		t.Fatalf("kind%d pitch%d n%d q0=%d blimit%d limit%d thresh%d: wide path differs from 8-lane path", kind, step, n, q0, blimit, limit, thresh)
	}
}

func TestLoopFilterFlat16Eligibility(t *testing.T) {
	if !loopFilterFlat16Eligible(32, 1, 16, 8) || !loopFilterFlat16Eligible(16, 1, 32, 16) {
		t.Fatal("horizontal 16-position flat edges must take the wide kernel")
	}
	for _, c := range [][4]int{{1, 32, 16, 8}, {32, 1, 8, 8}, {32, 1, 24, 16}, {32, 1, 16, 4}, {8, 1, 16, 8}} {
		if loopFilterFlat16Eligible(c[0], c[1], c[2], c[3]) {
			t.Fatalf("step%d outer%d n%d kind%d must not take the wide kernel", c[0], c[1], c[2], c[3])
		}
	}
}

func TestLoopFilterFlat16MatchesEightLane(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	rng := rand.New(rand.NewPCG(0x16f1a7, 0x4a3b))
	checked := 0
	for _, kind := range []int{8, 16} {
		for _, pitch := range []int{16, 17, 24, 32, 64} {
			for _, n := range []int{16, 32} {
				for trial := range 192 {
					data := make([]byte, 16*pitch+64+n)
					base := byte(rng.UintN(256))
					spread := []uint{1, 2, 3, 17, 256}[trial%5]
					for i := range data {
						data[i] = base + byte(rng.UintN(spread))
					}
					// Make some lanes exactly flat so the flat7/flat15
					// masks mix within one vector.
					if trial%3 == 0 {
						for i := 0; i < len(data); i += 5 {
							data[i] = base
						}
					}
					q0 := 8*pitch + trial%3
					blimit := []int{0, 8, 40, 128, 254}[trial%5]
					limit := []int{0, 1, 6, 63, 255}[(trial/5)%5]
					thresh := []int{0, 1, 8, 255}[(trial/25)%4]
					checkFlat16(t, data, q0, pitch, n, blimit, limit, thresh, kind)
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no case ran")
	}
}

func FuzzLoopFilterFlat16(f *testing.F) {
	f.Add([]byte{128, 128, 129, 130}, uint8(40), uint8(6), uint8(1), false)
	f.Add(bytes.Repeat([]byte{100, 101, 99}, 400), uint8(255), uint8(255), uint8(0), true)
	f.Fuzz(func(t *testing.T, seed []byte, blimit, limit, thresh uint8, wideKind bool) {
		if !Enabled || len(seed) == 0 {
			return
		}
		const pitch = 32
		data := make([]byte, 40*pitch)
		for i := range data {
			data[i] = seed[i%len(seed)] + byte(i/len(seed))
		}
		kind := 8
		if wideKind {
			kind = 16
		}
		checkFlat16(t, data, 8*pitch, pitch, 16, int(blimit), int(limit), int(thresh), kind)
	})
}
