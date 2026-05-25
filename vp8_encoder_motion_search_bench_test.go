package govpx

import "testing"

func BenchmarkInterFrameSubpelStepNoStats(b *testing.B) {
	search := newTestInterFrameSubpixelStepSearch(b, interAnalysisFractionalSearchStep)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, _, _, ok := search.refine(); !ok {
			b.Fatal("refine returned ok=false")
		}
	}
}

func BenchmarkInterFrameSubpelHalfNoStats(b *testing.B) {
	search := newTestInterFrameSubpixelStepSearch(b, interAnalysisFractionalSearchHalf)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, _, _, ok := search.refine(); !ok {
			b.Fatal("refine returned ok=false")
		}
	}
}
