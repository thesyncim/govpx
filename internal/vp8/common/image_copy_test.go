package common

import "testing"

func TestCopyExtendedImageCopiesBorderedPlanes(t *testing.T) {
	src, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer src: %v", err)
	}
	dst, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer dst: %v", err)
	}

	fillIncreasing(src.Img.YFull)
	fillIncreasing(src.Img.UFull)
	fillIncreasing(src.Img.VFull)

	CopyExtendedImage(&dst.Img, &src.Img)

	if string(dst.Img.YFull) != string(src.Img.YFull) ||
		string(dst.Img.UFull) != string(src.Img.UFull) ||
		string(dst.Img.VFull) != string(src.Img.VFull) {
		t.Fatalf("CopyExtendedImage did not copy every full plane")
	}
}

func TestCopyImageCopiesVisiblePlanesOnly(t *testing.T) {
	src, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer src: %v", err)
	}
	dst, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer dst: %v", err)
	}
	fillIncreasing(src.Img.Y)
	fillIncreasing(src.Img.U)
	fillIncreasing(src.Img.V)
	dst.Img.YFull[0] = 77

	CopyImage(&dst.Img, &src.Img)

	if string(dst.Img.Y) != string(src.Img.Y) ||
		string(dst.Img.U) != string(src.Img.U) ||
		string(dst.Img.V) != string(src.Img.V) {
		t.Fatalf("CopyImage did not copy visible planes")
	}
	if dst.Img.YFull[0] != 77 {
		t.Fatalf("CopyImage copied border byte")
	}
}

func TestCopyImageLumaCopiesOnlyOverlappingCodedLuma(t *testing.T) {
	src, err := NewFrameBuffer(16, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer src: %v", err)
	}
	dst, err := NewFrameBuffer(32, 16, 4, 16)
	if err != nil {
		t.Fatalf("NewFrameBuffer dst: %v", err)
	}
	fillIncreasing(src.Img.Y)
	fillValue(dst.Img.Y, 99)
	fillValue(dst.Img.U, 88)
	fillValue(dst.Img.V, 77)

	CopyImageLuma(&dst.Img, &src.Img)

	for row := range src.Img.CodedHeight {
		got := dst.Img.Y[row*dst.Img.YStride : row*dst.Img.YStride+src.Img.CodedWidth]
		want := src.Img.Y[row*src.Img.YStride : row*src.Img.YStride+src.Img.CodedWidth]
		if string(got) != string(want) {
			t.Fatalf("luma row %d copied mismatch", row)
		}
		if dst.Img.Y[row*dst.Img.YStride+src.Img.CodedWidth] != 99 {
			t.Fatalf("luma row %d copied past overlap", row)
		}
	}
	if dst.Img.U[0] != 88 || dst.Img.V[0] != 77 {
		t.Fatalf("CopyImageLuma changed chroma")
	}
}

func fillIncreasing(buf []byte) {
	for i := range buf {
		buf[i] = byte(i)
	}
}

func fillValue(buf []byte, value byte) {
	for i := range buf {
		buf[i] = value
	}
}
