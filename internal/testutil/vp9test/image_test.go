package vp9test

import "testing"

func TestNewCheckerYCbCr(t *testing.T) {
	img := NewCheckerYCbCr(4, 2, 10, 200, 80, 160)
	if img.Y[0] != 10 || img.Y[1] != 200 || img.Y[img.YStride] != 200 {
		t.Fatalf("checker Y pattern = [%d %d %d], want alternating lo/hi",
			img.Y[0], img.Y[1], img.Y[img.YStride])
	}
	if img.Cb[0] != 80 || img.Cr[0] != 160 {
		t.Fatalf("checker chroma = %d/%d, want 80/160", img.Cb[0], img.Cr[0])
	}
}

func TestNewHorizontalBandsYCbCr(t *testing.T) {
	img := NewHorizontalBandsYCbCr(4, 3, 90, 170)
	if img.Y[0] != 32 {
		t.Fatalf("row 0 Y = %d, want 32", img.Y[0])
	}
	if img.Y[img.YStride] != 37 {
		t.Fatalf("row 1 Y = %d, want 37", img.Y[img.YStride])
	}
	if img.Cb[0] != 90 || img.Cr[0] != 170 {
		t.Fatalf("chroma = %d/%d, want 90/170", img.Cb[0], img.Cr[0])
	}
}

func TestNewChromaHorizontalBandsYCbCr(t *testing.T) {
	img := NewChromaHorizontalBandsYCbCr(4, 4)
	if img.Y[0] != 128 || img.Y[len(img.Y)-1] != 128 {
		t.Fatalf("Y plane not neutral: first=%d last=%d", img.Y[0], img.Y[len(img.Y)-1])
	}
	if img.Cb[0] != 32 || img.Cr[0] != 48 {
		t.Fatalf("first chroma row = %d/%d, want 32/48", img.Cb[0], img.Cr[0])
	}
	if img.Cb[img.CStride] != 39 || img.Cr[img.CStride] != 59 {
		t.Fatalf("second chroma row = %d/%d, want 39/59",
			img.Cb[img.CStride], img.Cr[img.CStride])
	}
}

func TestNewMotionYCbCr(t *testing.T) {
	img := NewMotionYCbCr(4, 4)
	if img.Y[0] == img.Y[1] {
		t.Fatalf("motion Y row is flat: %d == %d", img.Y[0], img.Y[1])
	}
	if img.Cb[0] == img.Cb[1] {
		t.Fatalf("motion Cb row is flat: %d == %d", img.Cb[0], img.Cb[1])
	}
}

func TestCompoundYCbCrFixtures(t *testing.T) {
	low := NewCompoundAverageYCbCr(4, 4, -16)
	high := NewCompoundAverageYCbCr(4, 4, 16)
	avg := AverageYCbCr(low, high)
	if avg.Rect.Dx() != 4 || avg.Rect.Dy() != 4 {
		t.Fatalf("average dimensions = %dx%d, want 4x4", avg.Rect.Dx(), avg.Rect.Dy())
	}
	if avg.Y[0] != 96 {
		t.Fatalf("average Y[0] = %d, want 96", avg.Y[0])
	}

	a := NewCompoundPairYCbCr(4, 4, false)
	b := NewCompoundPairYCbCr(4, 4, true)
	if EqualYCbCr(a, b, 4, 4) {
		t.Fatal("compound pair variants are identical")
	}
}
