package govpx

import "testing"

func TestVP9SourceVarianceAreaPerPixel(t *testing.T) {
	const side = 16
	buf := make([]byte, side*side)
	for i := range buf {
		buf[i] = 200
	}
	if got := vp9SourceVarianceAreaPerPixel(buf, side, 0, 0, side, side); got != 0 {
		t.Fatalf("flat source variance = %d, want 0", got)
	}

	for i := range buf {
		if i%2 == 0 {
			buf[i] = 0
		} else {
			buf[i] = 255
		}
	}
	if got := vp9SourceVarianceAreaPerPixel(buf, side, 0, 0, side, side); got != 16256 {
		t.Fatalf("checker source variance = %d, want 16256", got)
	}
}

func TestVP9InterSkipFilterSearchFlatSource(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.DisableFilterSearchVarThresh = 100

	flat := make([]byte, 16*16)
	for i := range flat {
		flat[i] = 128
	}
	sourceVariance := vp9SourceVarianceAreaPerPixel(flat, 16, 0, 0, 16, 16)
	if sourceVariance != 0 {
		t.Fatalf("source variance = %d, want 0", sourceVariance)
	}
	if !e.vp9InterSkipFilterSearch(sourceVariance) {
		t.Fatalf("flat source did not skip filter search")
	}
}
