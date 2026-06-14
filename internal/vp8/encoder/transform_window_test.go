package encoder

import "testing"

func TestTransform4x4WindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	tests := []struct {
		name             string
		inputLen, stride int
		want             bool
	}{
		{name: "fits-packed", inputLen: 16, stride: 4, want: true},
		{name: "fits-strided", inputLen: 28, stride: 8, want: true},
		{name: "short-input", inputLen: 15, stride: 4, want: false},
		{name: "stride-too-small", inputLen: 16, stride: 3, want: false},
		{name: "negative-stride", inputLen: 16, stride: -1, want: false},
		{name: "stride-overflow", inputLen: 64, stride: huge/3 + 2, want: false},
	}
	for _, tt := range tests {
		input := make([]int16, tt.inputLen)
		got := transform4x4WindowOK(input, tt.stride)
		if got != tt.want {
			t.Fatalf("%s: transform4x4WindowOK(input=%d stride=%d) = %v, want %v",
				tt.name, tt.inputLen, tt.stride, got, tt.want)
		}
	}
}

func TestTransform4x4BatchWindowOK(t *testing.T) {
	huge := int(^uint(0) >> 1)
	tests := []struct {
		name                string
		inputLen, outputLen int
		count               int
		want                bool
	}{
		{name: "fits-one", inputLen: 16, outputLen: 16, count: 1, want: true},
		{name: "fits-many", inputLen: 400, outputLen: 400, count: 25, want: true},
		{name: "zero-count", inputLen: 16, outputLen: 16, count: 0, want: false},
		{name: "negative-count", inputLen: 16, outputLen: 16, count: -1, want: false},
		{name: "short-input", inputLen: 15, outputLen: 16, count: 1, want: false},
		{name: "short-output", inputLen: 16, outputLen: 15, count: 1, want: false},
		{name: "count-overflow", inputLen: 16, outputLen: 16, count: huge/16 + 1, want: false},
	}
	for _, tt := range tests {
		input := make([]int16, tt.inputLen)
		output := make([]int16, tt.outputLen)
		got := transform4x4BatchWindowOK(input, output, tt.count)
		if got != tt.want {
			t.Fatalf("%s: transform4x4BatchWindowOK(input=%d output=%d count=%d) = %v, want %v",
				tt.name, tt.inputLen, tt.outputLen, tt.count, got, tt.want)
		}
	}
}
