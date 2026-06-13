package encoder

import "testing"

func TestForwardWHT4x4WindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	tests := []struct {
		name                  string
		inputLen, stride, out int
		want                  bool
	}{
		{name: "fits-packed", inputLen: 16, stride: 4, out: 16, want: true},
		{name: "fits-strided", inputLen: 28, stride: 8, out: 16, want: true},
		{name: "short-input", inputLen: 15, stride: 4, out: 16, want: false},
		{name: "short-output", inputLen: 16, stride: 4, out: 15, want: false},
		{name: "stride-too-small", inputLen: 16, stride: 3, out: 16, want: false},
		{name: "negative-stride", inputLen: 16, stride: -1, out: 16, want: false},
		{name: "stride-overflow", inputLen: 64, stride: huge/3 + 2, out: 16, want: false},
	}
	for _, tt := range tests {
		input := make([]int16, tt.inputLen)
		output := make([]int16, tt.out)
		got := forward4x4WindowOK(input, tt.stride, output)
		if got != tt.want {
			t.Fatalf("%s: forward4x4WindowOK(input=%d stride=%d output=%d) = %v, want %v",
				tt.name, tt.inputLen, tt.stride, tt.out, got, tt.want)
		}
	}
}
