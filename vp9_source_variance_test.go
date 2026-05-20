package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9InterSkipFilterSearchFlatSource(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.DisableFilterSearchVarThresh = 100

	flat := make([]byte, 16*16)
	for i := range flat {
		flat[i] = 128
	}
	sourceVariance := encoder.SourceVarianceAreaPerPixel(flat, 16, 0, 0, 16, 16)
	if sourceVariance != 0 {
		t.Fatalf("source variance = %d, want 0", sourceVariance)
	}
	if !e.vp9InterSkipFilterSearch(sourceVariance) {
		t.Fatalf("flat source did not skip filter search")
	}
}
