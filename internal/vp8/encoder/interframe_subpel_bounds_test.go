package encoder

import "testing"

func TestInterFrameSubpixelMotionVectorInRange(t *testing.T) {
	best := MotionVector{Row: 16, Col: -8}
	if !InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row + 2040, Col: best.Col - 2040}, best) {
		t.Fatalf("max full-pel delta unexpectedly out of range")
	}
	if InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row + 2048, Col: best.Col}, best) {
		t.Fatalf("delta beyond max full-pel range unexpectedly accepted")
	}
	if InterFrameSubpixelMotionVectorInRange(MotionVector{Row: best.Row, Col: best.Col - 2048}, best) {
		t.Fatalf("negative delta beyond max full-pel range unexpectedly accepted")
	}
}

func TestInterFrameSubpelSearchBoundsForFrameOrigin(t *testing.T) {
	got := InterFrameSubpelSearchBoundsFor(MotionVector{}, 0, 0, 45, 80)
	want := InterFrameSubpelSearchBounds{
		RowMin: -64,
		RowMax: subpelMVQuarterPelLongLimit,
		ColMin: -64,
		ColMax: subpelMVQuarterPelLongLimit,
	}
	if got != want {
		t.Fatalf("subpel bounds at frame origin = %+v, want %+v", got, want)
	}
	if !got.Contains(-64, 1023) {
		t.Fatalf("bounds do not contain inclusive edge")
	}
	if got.Contains(-65, 0) || got.Contains(0, 1024) {
		t.Fatalf("bounds contain point outside inclusive edge")
	}
}
