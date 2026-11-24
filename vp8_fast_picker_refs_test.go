package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"reflect"
	"testing"
)

func TestVP8FastPickerRefsReference(t *testing.T) {
	for _, kind := range []string{"fast-y", "fast-uv"} {
		for pat := range 3 {
			for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 2}} {
				a, src, _ := round2Inputs(t, pat)
				b, _, _ := round2Inputs(t, pat)
				var as, bs vp8dec.IntraReconstructionScratch
				if kind == "fast-y" {
					x, y, z, ok := pickFastWholeBlockIntraYModeWithRDConstants(src, 40, rc[0], rc[1], &a.analysis.Img, &as, 128, 1)
					w, v, u, yes := frozen3pickFastWholeBlockIntraYModeWithRDConstants(src, 40, rc[0], rc[1], &b.analysis.Img, &bs, 128, 1)
					if x != w || y != v || z != u || ok != yes {
						t.Fatal(kind, pat, rc)
					}
				} else {
					x, y, ok := pickFastIntraChromaMode(src, rc[0], rc[1], &a.analysis.Img, &as)
					w, v, yes := frozen3pickFastIntraChromaMode(src, rc[0], rc[1], &b.analysis.Img, &bs)
					if x != w || y != v || ok != yes {
						t.Fatal(kind, pat, rc)
					}
				}
				// The Y scorer's discarded UV writes are not consumed by its caller; its
				// selected UV predictor is built explicitly before and after the Y loop.
				if !reflect.DeepEqual(a.analysis.Img.YFull, b.analysis.Img.YFull) {
					t.Fatal("Y state")
				}
				if kind == "fast-uv" && (!reflect.DeepEqual(a.analysis.Img.UFull, b.analysis.Img.UFull) || !reflect.DeepEqual(a.analysis.Img.VFull, b.analysis.Img.VFull)) {
					t.Fatal("UV state")
				}
			}
		}
	}
}

var vp8FastRefsSink int

func BenchmarkVP8FastPickerRefs(b *testing.B) {
	for _, kind := range []string{"fast-y", "fast-uv"} {
		for _, pat := range []int{0, 1} {
			b.Run(fmt.Sprintf("%s/p%d", kind, pat), func(b *testing.B) {
				e, src, _ := round2Inputs(b, pat)
				var scratch vp8dec.IntraReconstructionScratch
				b.ReportAllocs()
				for b.Loop() {
					if kind == "fast-y" {
						_, _, vp8FastRefsSink, _ = pickFastWholeBlockIntraYModeWithRDConstants(src, 40, 1, 1, &e.analysis.Img, &scratch, 128, 1)
					} else {
						_, vp8FastRefsSink, _ = pickFastIntraChromaMode(src, 1, 1, &e.analysis.Img, &scratch)
					}
				}
			})
		}
	}
}

func frozen3pickFastWholeBlockIntraYModeWithRDConstants(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, rdMult int, rdDiv int) (vp8common.MBPredictionMode, int, int, bool) {
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstants(qIndex)
	}
	bestMode := vp8common.DCPred
	bestRate := 0
	bestRD := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, false
		}
		dist, _ := vp8enc.MacroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
		rate := intraYModeRate(true, yMode)
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
		if i == 0 || cost < bestRD {
			bestMode = yMode
			bestRate = rate
			bestRD = cost
		}
	}
	return bestMode, bestRate, bestRD, true
}

func frozen3pickFastIntraChromaMode(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, int, bool) {
	bestMode := vp8common.DCPred
	bestSSE := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, false
		}
		sse := vp8enc.MacroblockChromaSSE(src, pred, mbRow, mbCol)
		if i == 0 || sse < bestSSE {
			bestMode = uvMode
			bestSSE = sse
		}
	}
	return bestMode, intraUVModeRate(true, bestMode), true
}
