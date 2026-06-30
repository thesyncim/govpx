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
