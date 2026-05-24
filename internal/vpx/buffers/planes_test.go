package buffers

import (
	"bytes"
	"testing"
)

func TestCopyPlaneCopiesVisibleRows(t *testing.T) {
	src := []byte{
		1, 2, 3, 99, 99,
		4, 5, 6, 99, 99,
		7, 8, 9, 99, 99,
	}
	dst := []byte{
		10, 10, 10, 55, 55, 55,
		10, 10, 10, 55, 55, 55,
		10, 10, 10, 55, 55, 55,
	}
	CopyPlane(dst, 6, src, 5, 3, 3)

	want := []byte{
		1, 2, 3, 55, 55, 55,
		4, 5, 6, 55, 55, 55,
		7, 8, 9, 55, 55, 55,
	}
	if !bytes.Equal(dst, want) {
		t.Fatalf("dst = %v, want %v", dst, want)
	}
}

func TestAveragePlaneIntoRoundsUp(t *testing.T) {
	dst := []byte{
		0, 1, 2, 77,
		10, 11, 12, 77,
	}
	src := []byte{
		1, 2, 3, 99, 99,
		11, 12, 13, 99, 99,
	}
	AveragePlaneInto(dst, 4, src, 5, 3, 2)

	want := []byte{
		1, 2, 3, 77,
		11, 12, 13, 77,
	}
	if !bytes.Equal(dst, want) {
		t.Fatalf("dst = %v, want %v", dst, want)
	}
}
