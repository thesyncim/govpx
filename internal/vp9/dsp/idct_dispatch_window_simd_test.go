//go:build (amd64 || arm64) && !purego

package dsp

import "testing"

func TestIDCTDispatchWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	buf := make([]uint8, 64)
	tests := []struct {
		name         string
		stride, w, h int
		want         bool
	}{
		{name: "fits", stride: 8, w: 8, h: 8, want: true},
		{name: "stride-less-than-width", stride: 7, w: 8, h: 1, want: false},
		{name: "past-end", stride: 8, w: 8, h: 9, want: false},
		{name: "row-span-overflow", stride: huge/2 + 1, w: 1, h: 3, want: false},
		{name: "width-overflow", stride: huge, w: huge, h: 1, want: false},
	}
	for _, tt := range tests {
		got := dcWindowOK(buf, tt.stride, tt.w, tt.h)
		if got != tt.want {
			t.Fatalf("%s: dcWindowOK(stride=%d w=%d h=%d) = %v, want %v",
				tt.name, tt.stride, tt.w, tt.h, got, tt.want)
		}
	}
}
