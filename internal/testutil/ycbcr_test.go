package testutil

import (
	"bytes"
	"image"
	"testing"
)

func TestAppendYCbCrI420UsesVisibleSamples(t *testing.T) {
	img := image.NewYCbCr(image.Rect(0, 0, 3, 3), image.YCbCrSubsampleRatio420)
	img.YStride = 5
	img.CStride = 4
	img.Y = []byte{
		1, 2, 3, 99, 99,
		4, 5, 6, 99, 99,
		7, 8, 9, 99, 99,
	}
	img.Cb = []byte{
		10, 11, 99, 99,
		12, 13, 99, 99,
	}
	img.Cr = []byte{
		20, 21, 99, 99,
		22, 23, 99, 99,
	}

	got := AppendYCbCrI420(nil, img)
	want := []byte{
		1, 2, 3, 4, 5, 6, 7, 8, 9,
		10, 11, 12, 13,
		20, 21, 22, 23,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendYCbCrI420 = %v, want %v", got, want)
	}
}

func TestAppendI420PlanesPreservesPrefix(t *testing.T) {
	y := []byte{
		1, 2, 99,
		3, 4, 99,
	}
	u := []byte{5, 99}
	v := []byte{6, 99}

	got := AppendI420Planes([]byte{0xaa}, 2, 2, y, 3, u, 2, v, 2)
	want := []byte{0xaa, 1, 2, 3, 4, 5, 6}
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendI420Planes = %v, want %v", got, want)
	}
}

func TestAppendPlaneUsesVisibleSamples(t *testing.T) {
	plane := []byte{
		1, 2, 99,
		3, 4, 99,
	}
	got := AppendPlane([]byte{0xaa}, plane, 3, 2, 2)
	want := []byte{0xaa, 1, 2, 3, 4}
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendPlane = %v, want %v", got, want)
	}
}

func TestPlaneEqualComparesVisibleSamples(t *testing.T) {
	a := []byte{
		1, 2, 99,
		3, 4, 99,
	}
	b := []byte{
		1, 2, 42,
		3, 4, 42,
	}
	if !PlaneEqual(a, 3, b, 3, 2, 2) {
		t.Fatal("PlaneEqual reported false for equal visible samples")
	}
	b[4] = 5
	if PlaneEqual(a, 3, b, 3, 2, 2) {
		t.Fatal("PlaneEqual reported true after a visible sample changed")
	}
}

func TestTriangleByteAndClampByte(t *testing.T) {
	if got := TriangleByte(0, 8); got != 0 {
		t.Fatalf("TriangleByte(0, 8) = %d, want 0", got)
	}
	if got := TriangleByte(4, 8); got != 255 {
		t.Fatalf("TriangleByte(4, 8) = %d, want 255", got)
	}
	if got := TriangleByte(10, 8); got != 127 {
		t.Fatalf("TriangleByte wraps = %d, want 127", got)
	}
	if got := ClampByte(-1); got != 0 {
		t.Fatalf("ClampByte(-1) = %d, want 0", got)
	}
	if got := ClampByte(256); got != 255 {
		t.Fatalf("ClampByte(256) = %d, want 255", got)
	}
}

func TestSyntheticYCbCrFixturesAreDeterministic(t *testing.T) {
	panA := NewTexturedPanningYCbCr(16, 16, 3)
	panB := NewTexturedPanningYCbCr(16, 16, 3)
	if !PlaneEqual(panA.Y, panA.YStride, panB.Y, panB.YStride, 16, 16) {
		t.Fatal("NewTexturedPanningYCbCr luma is not deterministic")
	}
	if panA.Y[0] == panA.Y[1] && panA.Y[1] == panA.Y[2] {
		t.Fatal("NewTexturedPanningYCbCr luma collapsed to a flat field")
	}

	screenA := NewScreenTextWindowYCbCr(32, 32, 2)
	screenB := NewScreenTextWindowYCbCr(32, 32, 2)
	if !PlaneEqual(screenA.Y, screenA.YStride, screenB.Y, screenB.YStride, 32, 32) {
		t.Fatal("NewScreenTextWindowYCbCr luma is not deterministic")
	}
	if screenA.Cb[0] == 0 || screenA.Cr[0] == 0 {
		t.Fatal("NewScreenTextWindowYCbCr chroma was not initialized")
	}
}
