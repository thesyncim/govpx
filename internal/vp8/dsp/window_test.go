package dsp

import "testing"

func TestDSPWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	buf := make([]byte, 64)
	tests := []struct {
		name         string
		stride, w, h int
		want         bool
	}{
		{name: "fits-8x8", stride: 8, w: 8, h: 8, want: true},
		{name: "fits-16x4", stride: 16, w: 16, h: 4, want: true},
		{name: "past-end", stride: 8, w: 8, h: 9, want: false},
		{name: "negative-stride", stride: -1, w: 8, h: 8, want: false},
		{name: "zero-stride-fits", stride: 0, w: 8, h: 8, want: true},
		{name: "zero-width", stride: 8, w: 0, h: 8, want: false},
		{name: "zero-height", stride: 8, w: 8, h: 0, want: false},
		{name: "row-span-overflow", stride: huge/2 + 1, w: 1, h: 3, want: false},
		{name: "width-overflow", stride: huge, w: huge, h: 2, want: false},
	}
	for _, tt := range tests {
		got := dspWindowOK(buf, tt.stride, tt.w, tt.h)
		if got != tt.want {
			t.Fatalf("%s: dspWindowOK(stride=%d w=%d h=%d) = %v, want %v",
				tt.name, tt.stride, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestDSPSIMDPredictWindowOK(t *testing.T) {
	const (
		srcStride    = 32
		srcLoadWidth = 21
		srcRows      = 13
		dstStride    = 16
		dstWidth     = 8
		dstRows      = 8
	)
	src := make([]byte, (srcRows-1)*srcStride+srcLoadWidth)
	dst := make([]byte, (dstRows-1)*dstStride+dstWidth)
	if !dspSIMDPredictWindowOK(src, srcStride, srcLoadWidth, srcRows, dst, dstStride, dstWidth, dstRows) {
		t.Fatal("valid SIMD prediction window was rejected")
	}
	if dspSIMDPredictWindowOK(src[:len(src)-1], srcStride, srcLoadWidth, srcRows, dst, dstStride, dstWidth, dstRows) {
		t.Fatal("short source SIMD prediction window was accepted")
	}
	if dspSIMDPredictWindowOK(src, srcStride, srcLoadWidth, srcRows, dst[:len(dst)-1], dstStride, dstWidth, dstRows) {
		t.Fatal("short destination SIMD prediction window was accepted")
	}
	if dspSIMDPredictWindowOK(src, 0, srcLoadWidth, srcRows, dst, dstStride, dstWidth, dstRows) {
		t.Fatal("zero source stride was accepted")
	}
	if dspSIMDPredictWindowOK(src, srcStride, srcLoadWidth, srcRows, dst, 0, dstWidth, dstRows) {
		t.Fatal("zero destination stride was accepted")
	}
	if dspSIMDPredictWindowOK(src, -srcStride, srcLoadWidth, srcRows, dst, dstStride, dstWidth, dstRows) {
		t.Fatal("negative source stride was accepted")
	}
	if dspSIMDPredictWindowOK(src, srcStride, srcLoadWidth, srcRows, dst, -dstStride, dstWidth, dstRows) {
		t.Fatal("negative destination stride was accepted")
	}

	maxInt := int(^uint(0) >> 1)
	if dspSIMDPredictWindowOK(nil, maxInt/2+1, 1, 3, dst, dstStride, dstWidth, dstRows) {
		t.Fatal("overflowing source window was accepted")
	}
	if dspSIMDPredictWindowOK(src, srcStride, srcLoadWidth, srcRows, nil, maxInt/2+1, 1, 3) {
		t.Fatal("overflowing destination window was accepted")
	}
}
