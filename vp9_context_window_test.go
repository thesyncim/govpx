package govpx

import "testing"

func TestVP9ContextWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	tests := []struct {
		name      string
		off, n, l int
		want      bool
	}{
		{name: "fits", off: 2, n: 2, l: 8, want: true},
		{name: "fits-at-end", off: 6, n: 2, l: 8, want: true},
		{name: "zero-width-at-end", off: 8, n: 0, l: 8, want: true},
		{name: "negative-offset", off: -1, n: 2, l: 8, want: false},
		{name: "negative-length", off: 0, n: -1, l: 8, want: false},
		{name: "too-wide", off: 0, n: 9, l: 8, want: false},
		{name: "past-end", off: 7, n: 2, l: 8, want: false},
		{name: "huge-offset", off: huge, n: 2, l: 8, want: false},
	}
	for _, tt := range tests {
		if got := vp9ContextWindowOK(tt.off, tt.n, tt.l); got != tt.want {
			t.Fatalf("%s: vp9ContextWindowOK(%d, %d, %d) = %v, want %v",
				tt.name, tt.off, tt.n, tt.l, got, tt.want)
		}
	}
}
