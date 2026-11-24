package dsp

import "testing"

func TestVP9IntraDCPredictors(t *testing.T) {
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
		want uint8
	}{
		{name: "dc4", bs: 4, fn: VpxDcPredictor4x4, want: 19},
		{name: "dc8", bs: 8, fn: VpxDcPredictor8x8, want: 24},
		{name: "dc16", bs: 16, fn: VpxDcPredictor16x16, want: 34},
		{name: "dc32", bs: 32, fn: VpxDcPredictor32x32, want: 54},
		{name: "dc_left4", bs: 4, fn: VpxDcLeftPredictor4x4, want: 26},
		{name: "dc_left8", bs: 8, fn: VpxDcLeftPredictor8x8, want: 32},
		{name: "dc_left16", bs: 16, fn: VpxDcLeftPredictor16x16, want: 44},
		{name: "dc_left32", bs: 32, fn: VpxDcLeftPredictor32x32, want: 68},
		{name: "dc_top4", bs: 4, fn: VpxDcTopPredictor4x4, want: 12},
		{name: "dc_top8", bs: 8, fn: VpxDcTopPredictor8x8, want: 16},
		{name: "dc_top16", bs: 16, fn: VpxDcTopPredictor16x16, want: 24},
		{name: "dc_top32", bs: 32, fn: VpxDcTopPredictor32x32, want: 40},
		{name: "dc128_4", bs: 4, fn: VpxDc128Predictor4x4, want: 128},
		{name: "dc128_8", bs: 8, fn: VpxDc128Predictor8x8, want: 128},
		{name: "dc128_16", bs: 16, fn: VpxDc128Predictor16x16, want: 128},
		{name: "dc128_32", bs: 32, fn: VpxDc128Predictor32x32, want: 128},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			above, left := intraBenchEdges()
			tc.fn(dst, stride, above, left)
			for r := 0; r < tc.bs; r++ {
				row := dst[r*stride : r*stride+tc.bs]
				for c, got := range row {
					if got != tc.want {
						t.Fatalf("(%d,%d) = %d, want %d", r, c, got, tc.want)
					}
				}
			}
		})
	}
}

func TestVP9IntraDirectionalPredictors(t *testing.T) {
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
		want func(r, c int, above, left []uint8) uint8
	}{
		{name: "v4", bs: 4, fn: VpxVPredictor4x4, want: func(_, c int, above, _ []uint8) uint8 { return above[1+c] }},
		{name: "v8", bs: 8, fn: VpxVPredictor8x8, want: func(_, c int, above, _ []uint8) uint8 { return above[1+c] }},
		{name: "v16", bs: 16, fn: VpxVPredictor16x16, want: func(_, c int, above, _ []uint8) uint8 { return above[1+c] }},
		{name: "v32", bs: 32, fn: VpxVPredictor32x32, want: func(_, c int, above, _ []uint8) uint8 { return above[1+c] }},
		{name: "h4", bs: 4, fn: VpxHPredictor4x4, want: func(r, _ int, _, left []uint8) uint8 { return left[r] }},
		{name: "h8", bs: 8, fn: VpxHPredictor8x8, want: func(r, _ int, _, left []uint8) uint8 { return left[r] }},
		{name: "h16", bs: 16, fn: VpxHPredictor16x16, want: func(r, _ int, _, left []uint8) uint8 { return left[r] }},
		{name: "h32", bs: 32, fn: VpxHPredictor32x32, want: func(r, _ int, _, left []uint8) uint8 { return left[r] }},
		{name: "tm4", bs: 4, fn: VpxTmPredictor4x4, want: tmPredictorTestValue},
		{name: "tm8", bs: 8, fn: VpxTmPredictor8x8, want: tmPredictorTestValue},
		{name: "tm16", bs: 16, fn: VpxTmPredictor16x16, want: tmPredictorTestValue},
		{name: "tm32", bs: 32, fn: VpxTmPredictor32x32, want: tmPredictorTestValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			above, left := intraBenchEdges()
			tc.fn(dst, stride, above, left)
			for r := 0; r < tc.bs; r++ {
				row := dst[r*stride : r*stride+tc.bs]
				for c, got := range row {
					want := tc.want(r, c, above, left)
					if got != want {
						t.Fatalf("(%d,%d) = %d, want %d", r, c, got, want)
					}
				}
			}
		})
	}
}

func TestVP9TmPredictorClips(t *testing.T) {
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
	}{
		{name: "tm4", bs: 4, fn: VpxTmPredictor4x4},
		{name: "tm8", bs: 8, fn: VpxTmPredictor8x8},
		{name: "tm16", bs: 16, fn: VpxTmPredictor16x16},
		{name: "tm32", bs: 32, fn: VpxTmPredictor32x32},
	}
	above, left := intraClipEdges()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			tc.fn(dst, stride, above, left)
			for r := 0; r < tc.bs; r++ {
				row := dst[r*stride : r*stride+tc.bs]
				for c, got := range row {
					want := tmPredictorTestValue(r, c, above, left)
					if got != want {
						t.Fatalf("(%d,%d) = %d, want %d", r, c, got, want)
					}
				}
			}
		})
	}
}

func BenchmarkVP9IntraDCPredictors(b *testing.B) {
	above, left := intraBenchEdges()
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
	}{
		{name: "dc4", bs: 4, fn: VpxDcPredictor4x4},
		{name: "dc8", bs: 8, fn: VpxDcPredictor8x8},
		{name: "dc16", bs: 16, fn: VpxDcPredictor16x16},
		{name: "dc32", bs: 32, fn: VpxDcPredictor32x32},
		{name: "dc_left4", bs: 4, fn: VpxDcLeftPredictor4x4},
		{name: "dc_left8", bs: 8, fn: VpxDcLeftPredictor8x8},
		{name: "dc_left16", bs: 16, fn: VpxDcLeftPredictor16x16},
		{name: "dc_left32", bs: 32, fn: VpxDcLeftPredictor32x32},
		{name: "dc_top4", bs: 4, fn: VpxDcTopPredictor4x4},
		{name: "dc_top8", bs: 8, fn: VpxDcTopPredictor8x8},
		{name: "dc_top16", bs: 16, fn: VpxDcTopPredictor16x16},
		{name: "dc_top32", bs: 32, fn: VpxDcTopPredictor32x32},
		{name: "dc128_4", bs: 4, fn: VpxDc128Predictor4x4},
		{name: "dc128_8", bs: 8, fn: VpxDc128Predictor8x8},
		{name: "dc128_16", bs: 16, fn: VpxDc128Predictor16x16},
		{name: "dc128_32", bs: 32, fn: VpxDc128Predictor32x32},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.fn(dst, stride, above, left)
			}
		})
	}
}

func BenchmarkVP9IntraDirectionalPredictors(b *testing.B) {
	above, left := intraBenchEdges()
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
	}{
		{name: "v4", bs: 4, fn: VpxVPredictor4x4},
		{name: "v8", bs: 8, fn: VpxVPredictor8x8},
		{name: "v16", bs: 16, fn: VpxVPredictor16x16},
		{name: "v32", bs: 32, fn: VpxVPredictor32x32},
		{name: "h4", bs: 4, fn: VpxHPredictor4x4},
		{name: "h8", bs: 8, fn: VpxHPredictor8x8},
		{name: "h16", bs: 16, fn: VpxHPredictor16x16},
		{name: "h32", bs: 32, fn: VpxHPredictor32x32},
		{name: "tm4", bs: 4, fn: VpxTmPredictor4x4},
		{name: "tm8", bs: 8, fn: VpxTmPredictor8x8},
		{name: "tm16", bs: 16, fn: VpxTmPredictor16x16},
		{name: "tm32", bs: 32, fn: VpxTmPredictor32x32},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.fn(dst, stride, above, left)
			}
		})
	}
}

func BenchmarkVP9TmPredictorClipHeavy(b *testing.B) {
	above, left := intraClipEdges()
	tests := []struct {
		name string
		bs   int
		fn   func(dst []uint8, stride int, above, left []uint8)
	}{
		{name: "tm4", bs: 4, fn: VpxTmPredictor4x4},
		{name: "tm8", bs: 8, fn: VpxTmPredictor8x8},
		{name: "tm16", bs: 16, fn: VpxTmPredictor16x16},
		{name: "tm32", bs: 32, fn: VpxTmPredictor32x32},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			stride := tc.bs + 7
			dst := make([]uint8, stride*tc.bs)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.fn(dst, stride, above, left)
			}
		})
	}
}

func intraBenchEdges() (above, left []uint8) {
	above = make([]uint8, 65)
	left = make([]uint8, 32)
	above[0] = 13
	for i := 1; i < len(above); i++ {
		above[i] = uint8(2*i + 7)
	}
	for i := range left {
		left[i] = uint8(3*i + 21)
	}
	return above, left
}

func intraClipEdges() (above, left []uint8) {
	above = make([]uint8, 65)
	left = make([]uint8, 32)
	above[0] = 200
	for i := 1; i < len(above); i++ {
		if i&1 == 0 {
			above[i] = 255
		} else {
			above[i] = 0
		}
	}
	for i := range left {
		if i&1 == 0 {
			left[i] = 255
		} else {
			left[i] = 0
		}
	}
	return above, left
}

func tmPredictorTestValue(r, c int, above, left []uint8) uint8 {
	v := int(left[r]) + int(above[1+c]) - int(above[0])
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
