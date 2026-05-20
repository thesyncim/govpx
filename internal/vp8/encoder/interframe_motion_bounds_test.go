package encoder

import "testing"

func TestInterFrameFullPixelMotionBoundsConstantsMatchLibvpx(t *testing.T) {
	if InterFrameMaxMVSearchSteps != 8 {
		t.Fatalf("InterFrameMaxMVSearchSteps = %d, want 8", InterFrameMaxMVSearchSteps)
	}
	if InterFrameMaxFullPelVal != 255 {
		t.Fatalf("InterFrameMaxFullPelVal = %d, want 255", InterFrameMaxFullPelVal)
	}
	if InterFrameMVFullPixelStep != 8 {
		t.Fatalf("InterFrameMVFullPixelStep = %d, want 8", InterFrameMVFullPixelStep)
	}
	if InterFrameUMVBorderPixels != 32 {
		t.Fatalf("InterFrameUMVBorderPixels = %d, want 32", InterFrameUMVBorderPixels)
	}
}

func TestInterFrameUMVOnlyFullPixelSearchBoundsUsesFrameWindow(t *testing.T) {
	mbRows := 45
	mbCols := 80

	got := InterFrameUMVOnlyFullPixelSearchBounds(0, 0, mbRows, mbCols)
	want := InterFrameFullPixelBounds{
		RowMin: -16,
		RowMax: (mbRows-1)*16 + (InterFrameUMVBorderPixels - 16),
		ColMin: -16,
		ColMax: (mbCols-1)*16 + (InterFrameUMVBorderPixels - 16),
	}
	if got != want {
		t.Fatalf("wide UMV bounds at MB(0,0) = %+v, want %+v", got, want)
	}
}

func TestInterFrameFullPixelSearchBoundsIntersectsReferenceWindow(t *testing.T) {
	mbRows := 45
	mbCols := 80
	bestRefMV := MotionVector{}

	got := InterFrameFullPixelSearchBounds(bestRefMV, 0, 0, mbRows, mbCols)
	want := InterFrameFullPixelBounds{
		RowMin: -16,
		RowMax: 255,
		ColMin: -16,
		ColMax: 255,
	}
	if got != want {
		t.Fatalf("intersected bounds at MB(0,0) = %+v, want %+v", got, want)
	}

	wide := InterFrameUMVOnlyFullPixelSearchBounds(0, 0, mbRows, mbCols)
	if wide.RowMax <= got.RowMax || wide.ColMax <= got.ColMax {
		t.Fatalf("wide UMV bounds = %+v, want larger positive reach than %+v", wide, got)
	}
}

func TestInterFrameUMVFullPixelInRange(t *testing.T) {
	if !InterFrameUMVFullPixelInRange(MotionVector{Row: 0, Col: 0}, 3, 3, 4, 4) {
		t.Fatalf("zero MV unexpectedly out of range")
	}
	if InterFrameUMVFullPixelInRange(MotionVector{Row: 142, Col: -352}, 3, 3, 4, 4) {
		t.Fatalf("out-of-frame MV unexpectedly in range")
	}
}
