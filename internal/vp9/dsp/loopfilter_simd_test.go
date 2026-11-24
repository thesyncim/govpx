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

func TestVP9LoopFilter8ScalarMatchesReference(t *testing.T) {
	type singleFn func([]uint8, int, int, uint8, uint8, uint8)
	singleCases := []struct {
		name      string
		special   singleFn
		reference singleFn
		cursor    int
	}{
		{"Horizontal8", vpxLpfHorizontal8Scalar, vpxLpfHorizontal8Reference, vp9LfHorizontalCursor},
		{"Vertical8", vpxLpfVertical8Scalar, vpxLpfVertical8Reference, vp9LfVerticalCursor},
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

	rng := rand.New(rand.NewPCG(0x565039384c46, 0x524546))
	for _, tc := range singleCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range params {
				for trial := range 64 {
					base := randomVP9LfPlane(rng, trial)
					got := append([]uint8(nil), base...)
					want := append([]uint8(nil), base...)
					tc.special(got, tc.cursor, vp9LfPitch, p.blimit, p.limit, p.thresh)
					tc.reference(want, tc.cursor, vp9LfPitch, p.blimit, p.limit, p.thresh)
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

func vpxLpfHorizontal8Reference(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4*pitch]
		p2 := plane[s-3*pitch]
		p1 := plane[s-2*pitch]
		p0 := plane[s-pitch]
		q0 := plane[s+0]
		q1 := plane[s+1*pitch]
		q2 := plane[s+2*pitch]
		q3 := plane[s+3*pitch]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			flat := flatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3)
			filter8(mask, thresh, flat, plane,
				s-4*pitch, s-3*pitch, s-2*pitch, s-pitch,
				s, s+pitch, s+2*pitch, s+3*pitch)
		}
		s++
	}
}

func vpxLpfVertical8Reference(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4]
		p2 := plane[s-3]
		p1 := plane[s-2]
		p0 := plane[s-1]
		q0 := plane[s+0]
		q1 := plane[s+1]
		q2 := plane[s+2]
		q3 := plane[s+3]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			flat := flatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3)
			filter8(mask, thresh, flat, plane,
				s-4, s-3, s-2, s-1, s, s+1, s+2, s+3)
		}
		s += pitch
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

func BenchmarkVP9LoopFilterHorizontal8DualNonFlat(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfHorizontal8Dual(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterHorizontal8DualNonFlatScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfHorizontal8DualScalar(plane, vp9LfHorizontalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical8DualNonFlat(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VpxLpfVertical8Dual(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
	}
}

func BenchmarkVP9LoopFilterVertical8DualNonFlatScalar(b *testing.B) {
	plane := texturedVP9LfPlane()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vpxLpfVertical8DualScalar(plane, vp9LfVerticalCursor, vp9LfPitch, 64, 32, 8, 64, 32, 8)
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

func TestVP9LoopFilter16DispatchMatchesScalar(t *testing.T) {
	type singleFn func([]uint8, int, int, uint8, uint8, uint8)
	cases := []struct {
		name     string
		dispatch singleFn
		scalar   singleFn
		cursor   int
	}{
		{"Horizontal16", VpxLpfHorizontal16,
			func(p []uint8, s, pitch int, b, l, th uint8) {
				mbLpfHorizontalEdgeW(p, s, pitch, b, l, th, 1)
			}, vp9LfHorizontalCursor},
		{"Horizontal16Dual", VpxLpfHorizontal16Dual,
			func(p []uint8, s, pitch int, b, l, th uint8) {
				mbLpfHorizontalEdgeW(p, s, pitch, b, l, th, 2)
			}, vp9LfHorizontalCursor},
		{"Vertical16", VpxLpfVertical16,
			func(p []uint8, s, pitch int, b, l, th uint8) {
				mbLpfVerticalEdgeW(p, s, pitch, b, l, th, 8)
			}, vp9LfVerticalCursor},
		{"Vertical16Dual", VpxLpfVertical16Dual,
			func(p []uint8, s, pitch int, b, l, th uint8) {
				mbLpfVerticalEdgeW(p, s, pitch, b, l, th, 16)
			}, vp9LfVerticalCursor},
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
		{193, 63, 15},
		{255, 63, 7},
	}

	rng := rand.New(rand.NewPCG(0x4c503136, 0x53494d44))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range params {
				for trial := range 48 {
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
