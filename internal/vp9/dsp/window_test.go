package dsp

import "testing"

func TestDSPReadWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	buf := make([]uint8, 32)
	tests := []struct {
		name              string
		off, stride, w, h int
		want              bool
	}{
		{name: "fits", off: 2, stride: 8, w: 4, h: 3, want: true},
		{name: "fits-at-end", off: 0, stride: 8, w: 8, h: 4, want: true},
		{name: "negative-offset", off: -1, stride: 8, w: 4, h: 3, want: false},
		{name: "negative-stride", off: 0, stride: -1, w: 4, h: 3, want: false},
		{name: "zero-width", off: 0, stride: 8, w: 0, h: 3, want: false},
		{name: "past-end", off: 1, stride: 8, w: 8, h: 4, want: false},
		{name: "row-span-overflow", off: 0, stride: huge/2 + 1, w: 1, h: 3, want: false},
		{name: "offset-row-overflow", off: huge, stride: 1, w: 1, h: 2, want: false},
		{name: "width-overflow", off: huge - 1, stride: 1, w: 2, h: 1, want: false},
	}
	for _, tt := range tests {
		got := dspReadWindowOK(buf, tt.off, tt.stride, tt.w, tt.h)
		if got != tt.want {
			t.Fatalf("%s: dspReadWindowOK(off=%d stride=%d w=%d h=%d) = %v, want %v",
				tt.name, tt.off, tt.stride, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestDSPSubpelReadWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	buf := make([]uint8, 30)
	if !dspSubpelReadWindowOK(buf, 0, 10, 9, 2) {
		t.Fatal("dspSubpelReadWindowOK rejected exact (w+1)x(h+1) fit")
	}
	if dspSubpelReadWindowOK(buf, 1, 10, 9, 2) {
		t.Fatal("dspSubpelReadWindowOK accepted past-end subpel window")
	}
	if dspSubpelReadWindowOK(buf, 0, huge, 1, huge) {
		t.Fatal("dspSubpelReadWindowOK accepted overflowing height expansion")
	}
	if dspSubpelReadWindowOK(buf, 0, 1, huge, 1) {
		t.Fatal("dspSubpelReadWindowOK accepted overflowing width expansion")
	}
}
