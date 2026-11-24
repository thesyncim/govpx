package vp9test

import (
	"image"
	"testing"
)

func TestShiftedI420ClampsWholePixelOffsets(t *testing.T) {
	src := NewYCbCr(4, 2, 0, 0, 0)
	copy(src.Y, []byte{
		10, 20, 30, 40,
		50, 60, 70, 80,
	})
	got := ShiftedI420(4, 2, src.Y, src.Cb, src.Cr,
		src.YStride, src.CStride, src.CStride, 1, 0)
	wantY := []byte{
		20, 30, 40, 40,
		60, 70, 80, 80,
	}
	assertPlaneBytes(t, "Y", got.Y[:8], wantY)
}

func TestSplitAndQuadrantShiftedI420(t *testing.T) {
	src := NewYCbCr(4, 4, 0, 0, 0)
	copy(src.Y, []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	})

	xSplit := SplitXShiftedI420(4, 4, src.Y, src.Cb, src.Cr,
		src.YStride, src.CStride, src.CStride, 1, -1)
	assertPlaneBytes(t, "x-split Y", xSplit.Y[:16], []byte{
		2, 3, 2, 3,
		6, 7, 6, 7,
		10, 11, 10, 11,
		14, 15, 14, 15,
	})

	ySplit := SplitYShiftedI420(4, 4, src.Y, src.Cb, src.Cr,
		src.YStride, src.CStride, src.CStride, 1, -1)
	assertPlaneBytes(t, "y-split Y", ySplit.Y[:16], []byte{
		5, 6, 7, 8,
		9, 10, 11, 12,
		5, 6, 7, 8,
		9, 10, 11, 12,
	})

	quad := QuadrantShiftedI420(4, 4, src.Y, src.Cb, src.Cr,
		src.YStride, src.CStride, src.CStride,
		image.Point{X: 1}, image.Point{X: -1},
		image.Point{Y: 1}, image.Point{Y: -1})
	assertPlaneBytes(t, "quadrant Y", quad.Y[:16], []byte{
		2, 3, 2, 3,
		6, 7, 6, 7,
		13, 14, 7, 8,
		13, 14, 11, 12,
	})
}

func assertPlaneBytes(t *testing.T, name string, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d", name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %d, want %d; got=%v", name, i, got[i], want[i], got)
		}
	}
}
