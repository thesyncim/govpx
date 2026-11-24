//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"slices"
	"testing"
)

func TestBatchedReductionsSelectNativeAndRejectInvalidSpans(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	src := bytes.Repeat([]byte{255}, 256)
	var quad [4]int32
	if !AverageQuad16(src, 16, &quad) || quad != [4]int32{255, 255, 255, 255} {
		t.Fatal("average native path not selected or incorrect")
	}
	var columns [5]int16
	if !ProjectionCols16(src, 32, 32, 5, 3, columns[:]) || columns != [5]int16{1020, 1020, 1020, 1020, 1020} {
		t.Fatal("projection native path not selected or incorrect")
	}
	maxInt := int(^uint(0) >> 1)
	for _, ss := range []int{-1, 16, maxInt} {
		saved := quad
		if AverageQuad16(src[:255], ss, &quad) || quad != saved {
			t.Fatalf("invalid average span accepted or changed: %d", ss)
		}
	}
	for _, shape := range [][4]int{{maxInt, 16, 5, 3}, {16, 16, maxInt, 3}, {16, 16, 5, -1}, {16, 16, 5, 15}, {16, 17, 5, 3}, {16, 16, 6, 3}} {
		saved := slices.Clone(columns[:])
		if ProjectionCols16(src, shape[0], shape[1], shape[2], shape[3], columns[:]) || !slices.Equal(columns[:], saved) {
			t.Fatalf("invalid projection accepted or changed: %v", shape)
		}
	}
	if ProjectionCols16(src[:15], 16, 16, 1, 3, columns[:]) {
		t.Fatal("short row accepted")
	}
	if !bytes.Equal(src, bytes.Repeat([]byte{255}, 256)) {
		t.Fatal("reduction changed input")
	}
}
