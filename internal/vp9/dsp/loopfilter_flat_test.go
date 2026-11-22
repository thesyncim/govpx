package dsp

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// flatVP9LfPlane builds a plane whose neighbouring samples differ by at most
// one, with a few outliers and a level jump on half of the rows, so that the
// flat and flat2 masks are true on some lanes and false on others within one
// packet.
func flatVP9LfPlane(rng *rand.Rand, trial int) []uint8 {
	plane := make([]uint8, vp9LfPitch*vp9LfHeight)
	for y := range vp9LfHeight {
		level := 40 + rng.IntN(160)
		for x := range vp9LfPitch {
			plane[y*vp9LfPitch+x] = uint8(level + rng.IntN(2))
		}
	}
	for range 8 + trial%16 {
		i := rng.IntN(len(plane))
		plane[i] = uint8(int(plane[i]) + rng.IntN(7) - 3)
	}
	if trial%4 == 3 {
		for y := range vp9LfHeight {
			for x := vp9LfPitch / 2; x < vp9LfPitch; x++ {
				plane[y*vp9LfPitch+x]++
			}
		}
	}
	return plane
}

// TestVP9LoopFilter16FlatMatchesScalar drives the 16-wide filters over
// packets that are flat on every row except a few, for every dispatch shape.
func TestVP9LoopFilter16FlatMatchesScalar(t *testing.T) {
	for which := range 4 {
		dispatch, scalar, cursor := loopFilter16Shape(which)
		for trial := range 200 {
			rng := rand.New(rand.NewPCG(uint64(trial), 0x666c6174))
			base := flatVP9LfPlane(rng, trial)
			for _, p := range [][3]uint8{{255, 63, 7}, {64, 32, 16}, {12, 8, 2}} {
				got := append([]uint8(nil), base...)
				want := append([]uint8(nil), base...)
				dispatch(got, cursor, vp9LfPitch, p[0], p[1], p[2])
				scalar(want, cursor, vp9LfPitch, p[0], p[1], p[2])
				if !bytes.Equal(got, want) {
					t.Fatalf("shape %d params %v trial %d: mismatch at byte %d", which, p, trial, firstVP9LfDiff(got, want))
				}
			}
		}
	}
}

func loopFilter16Shape(which int) (dispatch, scalar func([]uint8, int, int, uint8, uint8, uint8), cursor int) {
	switch which {
	case 0:
		return VpxLpfHorizontal16, func(p []uint8, s, pitch int, b, l, th uint8) { mbLpfHorizontalEdgeW(p, s, pitch, b, l, th, 1) }, vp9LfHorizontalCursor
	case 1:
		return VpxLpfHorizontal16Dual, func(p []uint8, s, pitch int, b, l, th uint8) { mbLpfHorizontalEdgeW(p, s, pitch, b, l, th, 2) }, vp9LfHorizontalCursor
	case 2:
		return VpxLpfVertical16, func(p []uint8, s, pitch int, b, l, th uint8) { mbLpfVerticalEdgeW(p, s, pitch, b, l, th, 8) }, vp9LfVerticalCursor
	}
	return VpxLpfVertical16Dual, func(p []uint8, s, pitch int, b, l, th uint8) { mbLpfVerticalEdgeW(p, s, pitch, b, l, th, 16) }, vp9LfVerticalCursor
}

func FuzzVP9LoopFilter16MatchesScalar(f *testing.F) {
	f.Add([]byte{1, 2, 3}, uint8(32), uint8(16), uint8(8), uint8(0))
	f.Add([]byte{128, 128, 129, 7}, uint8(64), uint8(32), uint8(4), uint8(1))
	f.Add([]byte{0, 255}, uint8(255), uint8(63), uint8(7), uint8(2))
	f.Add([]byte{100}, uint8(12), uint8(8), uint8(2), uint8(3))
	f.Fuzz(func(t *testing.T, seed []byte, blimit, limit, thresh, which uint8) {
		dispatch, scalar, cursor := loopFilter16Shape(int(which) % 4)
		var s1, s2 uint64
		for i, b := range seed {
			if i%2 == 0 {
				s1 = s1<<8 | uint64(b)
			} else {
				s2 = s2<<8 | uint64(b)
			}
		}
		rng := rand.New(rand.NewPCG(s1, s2))
		base := randomVP9LfPlane(rng, len(seed))
		got := append([]uint8(nil), base...)
		want := append([]uint8(nil), base...)
		dispatch(got, cursor, vp9LfPitch, blimit, limit, thresh)
		scalar(want, cursor, vp9LfPitch, blimit, limit, thresh)
		if !bytes.Equal(got, want) {
			t.Fatalf("which=%d blimit=%d limit=%d thresh=%d: mismatch at byte %d", which, blimit, limit, thresh, firstVP9LfDiff(got, want))
		}
	})
}

// BenchmarkVP9LoopFilter16Flat times the 16-wide edges on a flat packet whose
// every lane takes the flat2 branch, the case that dominates smooth content.
func BenchmarkVP9LoopFilter16Flat(b *testing.B) {
	names := [4]string{"Horizontal16", "Horizontal16Dual", "Vertical16", "Vertical16Dual"}
	for which, name := range names {
		dispatch, scalar, cursor := loopFilter16Shape(which)
		plane := make([]uint8, vp9LfPitch*vp9LfHeight)
		for i := range plane {
			plane[i] = 128 + uint8(i&1)
		}
		for _, impl := range []struct {
			name string
			fn   func([]uint8, int, int, uint8, uint8, uint8)
		}{{"dispatch", dispatch}, {"scalar", scalar}} {
			b.Run(name+"/"+impl.name, func(b *testing.B) {
				for b.Loop() {
					impl.fn(plane, cursor, vp9LfPitch, 64, 32, 8)
				}
			})
		}
	}
}
