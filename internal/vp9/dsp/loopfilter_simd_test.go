package dsp

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func TestVP9LoopFilterDispatchMatchesScalar(t *testing.T) {
	type singleFn func([]uint8, int, int, uint8, uint8, uint8)
	singleCases := []struct {
		name     string
		dispatch singleFn
		scalar   singleFn
		cursor   int
	}{
		{"Horizontal4", VpxLpfHorizontal4, vpxLpfHorizontal4Scalar, vp9LfHorizontalCursor},
		{"Vertical4", VpxLpfVertical4, vpxLpfVertical4Scalar, vp9LfVerticalCursor},
		{"Horizontal8", VpxLpfHorizontal8, vpxLpfHorizontal8Scalar, vp9LfHorizontalCursor},
		{"Vertical8", VpxLpfVertical8, vpxLpfVertical8Scalar, vp9LfVerticalCursor},
	}

	params := []struct {
		blimit uint8
		limit  uint8
		thresh uint8
	}{
		{4, 2, 0},
		{12, 8, 2},
		{32, 16, 8},
		{64, 32, 16},
		{128, 48, 4},
		{255, 63, 7},
	}

	rng := rand.New(rand.NewPCG(0x5650394c46, 0x44495350))
	for _, tc := range singleCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range params {
				for trial := range 32 {
					base := randomVP9LfPlane(rng, trial)
					got := append([]uint8(nil), base...)
					want := append([]uint8(nil), base...)
					tc.dispatch(got, tc.cursor, vp9LfPitch, p.blimit, p.limit, p.thresh)
					tc.scalar(want, tc.cursor, vp9LfPitch, p.blimit, p.limit, p.thresh)
					if !bytes.Equal(got, want) {
						t.Fatalf("%s blimit=%d limit=%d thresh=%d trial=%d mismatch at byte %d",
							tc.name, p.blimit, p.limit, p.thresh, trial, firstVP9LfDiff(got, want))
					}
				}
			}
		})
	}
}

func TestVP9LoopFilterDualDispatchMatchesScalar(t *testing.T) {
	type dualFn func([]uint8, int, int, uint8, uint8, uint8, uint8, uint8, uint8)
	dualCases := []struct {
		name     string
		dispatch dualFn
		scalar   dualFn
		cursor   int
	}{
		{"Horizontal4Dual", VpxLpfHorizontal4Dual, vpxLpfHorizontal4DualScalar, vp9LfHorizontalCursor},
		{"Vertical4Dual", VpxLpfVertical4Dual, vpxLpfVertical4DualScalar, vp9LfVerticalCursor},
		{"Horizontal8Dual", VpxLpfHorizontal8Dual, vpxLpfHorizontal8DualScalar, vp9LfHorizontalCursor},
		{"Vertical8Dual", VpxLpfVertical8Dual, vpxLpfVertical8DualScalar, vp9LfVerticalCursor},
	}

	params := []struct {
		blimit0 uint8
		limit0  uint8
		thresh0 uint8
		blimit1 uint8
		limit1  uint8
		thresh1 uint8
	}{
		{12, 8, 2, 12, 8, 2},
		{32, 16, 8, 32, 16, 8},
		{64, 32, 16, 48, 24, 8},
		{255, 63, 7, 128, 48, 4},
	}

	rng := rand.New(rand.NewPCG(0x4455414c, 0x5650394c46))
	for _, tc := range dualCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range params {
				for trial := range 32 {
					base := randomVP9LfPlane(rng, trial)
					got := append([]uint8(nil), base...)
					want := append([]uint8(nil), base...)
					tc.dispatch(got, tc.cursor, vp9LfPitch,
						p.blimit0, p.limit0, p.thresh0, p.blimit1, p.limit1, p.thresh1)
					tc.scalar(want, tc.cursor, vp9LfPitch,
						p.blimit0, p.limit0, p.thresh0, p.blimit1, p.limit1, p.thresh1)
					if !bytes.Equal(got, want) {
						t.Fatalf("%s params=%+v trial=%d mismatch at byte %d",
							tc.name, p, trial, firstVP9LfDiff(got, want))
					}
				}
			}
		})
	}
}

func BenchmarkVP9LoopFilterHorizontal4(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfHorizontal4(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal4Scalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfHorizontal4Scalar(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical4(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfVertical4(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical4Scalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfVertical4Scalar(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal4Dual(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfHorizontal4Dual(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal4DualScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfHorizontal4DualScalar(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical4Dual(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfVertical4Dual(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical4DualScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfVertical4DualScalar(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal8NonFlat(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfHorizontal8(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal8NonFlatScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfHorizontal8Scalar(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical8NonFlat(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfVertical8(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical8NonFlatScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfVertical8Scalar(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8)
	}
}

const (
	vp9LfPitch            = 64
	vp9LfHeight           = 40
	vp9LfHorizontalCursor = 16*vp9LfPitch + 16
	vp9LfVerticalCursor   = 8*vp9LfPitch + 16
)

func randomVP9LfPlane(rng *rand.Rand, trial int) []uint8 {
	plane := make([]uint8, vp9LfPitch*vp9LfHeight)
	for i := range plane {
		plane[i] = uint8(rng.IntN(256))
	}
	if trial%3 == 1 {
		for y := range vp9LfHeight {
			for x := range vp9LfPitch {
				plane[y*vp9LfPitch+x] = uint8(96 + (x+y+trial)&7)
			}
		}
	}
	return plane
}

func texturedVP9LfPlane() []uint8 {
	plane := make([]uint8, vp9LfPitch*vp9LfHeight)
	for y := range vp9LfHeight {
		for x := range vp9LfPitch {
			plane[y*vp9LfPitch+x] = uint8((x*19 + y*37 + (x^y)*5) & 0xff)
		}
	}
	return plane
}

func firstVP9LfDiff(got, want []uint8) int {
	for i := range got {
		if got[i] != want[i] {
			return i
		}
	}
	return -1
}
