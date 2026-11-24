package govpx

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type astraDenoiserFixture struct {
	e                    *VP8Encoder
	source, filtered     vp8enc.SourceImage
	decision             interFrameModeDecision
	row, col, rows, cols int
}

func astraNewDenoiserFixture(tb testing.TB, pattern string, partial, alias, spatial bool) astraDenoiserFixture {
	tb.Helper()
	w, h, row, col := 64, 64, 0, 0
	if spatial {
		row, col = 1, 1
	}
	if partial {
		w, h, row, col = 49, 35, 2, 3
	}
	e := newSizedTestEncoder(tb, w, h)
	e.opts.NoiseSensitivity = 2
	if err := e.denoiser.ensureAllocated(w, h); err != nil {
		tb.Fatal(err)
	}
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserOnYUV)
	if pattern == "y-only" {
		e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserOnYOnly)
	}
	for k := range e.denoiser.runningAvg {
		img := &e.denoiser.runningAvg[k].Img
		fillBenchmarkVP8Image(img, 96, 90, 170)
		for y := range h {
			for x := range w {
				img.Y[y*img.YStride+x] = byte(95 + (x+3*y)%3)
			}
		}
		e.denoiser.runningAvg[k].ExtendBorders()
	}
	for k := range e.denoiser.mcY {
		e.denoiser.mcY[k] = 231
	}
	for k := range e.denoiser.mcU {
		e.denoiser.mcU[k], e.denoiser.mcV[k] = 232, 233
	}
	srcFrame := testVP8Frame(tb, w, h, 96, 90, 170)
	if pattern == "rejected" {
		fillBenchmarkVP8Image(&srcFrame.Img, 150, 90, 170)
	}
	source := vp8enc.CodedSourceImageFromImage(&srcFrame.Img)
	overlayFrame := testVP8Frame(tb, w, h, 47, 48, 49)
	filtered := vp8enc.CodedSourceImageFromImage(&overlayFrame.Img)
	if alias {
		filtered = source
	}
	d := interFrameModeDecision{denoise: denoiserMacroblockDecision{
		bestReferenceFrame: vp8common.LastFrame, zeroMVReferenceFrame: vp8common.LastFrame,
		bestMode: vp8common.NewMV, bestMV: vp8enc.MotionVector{Row: 3, Col: 5},
		bestSSE: 0, zeroMVSSE: 20000,
	}}
	if pattern == "odd-integer" {
		d.denoise.bestMV = vp8enc.MotionVector{Row: 8}
	}
	if pattern == "fullpel" {
		d.denoise.bestMV = vp8enc.MotionVector{Row: 16}
	}
	if pattern == "y-only" || pattern == "zero-yuv" {
		d.denoise.bestReferenceFrame = vp8common.IntraFrame
		d.denoise.zeroMVSSE = 0
	}
	if pattern == "split-fallback" {
		d.denoise.bestMode = vp8common.SplitMV
	}
	if pattern == "state-fallback" {
		e.denoiser.refStates[denoiserAvgLast] = vp8dec.InterFrameRefState{}
	}
	return astraDenoiserFixture{e: e, source: source, filtered: filtered, decision: d,
		row: row, col: col, rows: (h + 15) / 16, cols: (w + 15) / 16}
}

func astraDenoiserCompare(t *testing.T, got, want *astraDenoiserFixture, gm, wm denoiserPlaneOverlay) {
	t.Helper()
	if (gm.y || gm.yAverage) != (wm.y || wm.yAverage) || gm.u != wm.u || gm.v != wm.v || !bytes.Equal(got.e.denoiser.state, want.e.denoiser.state) {
		t.Fatalf("overlay/state mismatch: %+v/%+v, %v/%v", gm, wm, got.e.denoiser.state, want.e.denoiser.state)
	}
	for _, pair := range [][2][]byte{
		{got.source.Y, want.source.Y}, {got.source.U, want.source.U}, {got.source.V, want.source.V},
		{got.filtered.U, want.filtered.U}, {got.filtered.V, want.filtered.V},
		{got.e.denoiser.mcY[:], want.e.denoiser.mcY[:]},
		{got.e.denoiser.mcRunning.Img.YFull, want.e.denoiser.mcRunning.Img.YFull},
		{got.e.denoiser.mcRunning.Img.UFull, want.e.denoiser.mcRunning.Img.UFull},
		{got.e.denoiser.mcRunning.Img.VFull, want.e.denoiser.mcRunning.Img.VFull},
	} {
		if !bytes.Equal(pair[0], pair[1]) {
			t.Fatal("source, overlay, luma scratch, or fallback image mismatch")
		}
	}
	// The overlay's Y plane is scratch when its signal is average-backed or
	// raw-backed. Compare exactly the Y view the coefficient consumer receives.
	selectedY := func(f *astraDenoiserFixture, mask denoiserPlaneOverlay) ([]byte, int) {
		if mask.yAverage {
			img := &f.e.denoiser.runningAvg[denoiserAvgIntra].Img
			return img.Y, img.YStride
		}
		if mask.y {
			return f.filtered.Y, f.filtered.YStride
		}
		return f.source.Y, f.source.YStride
	}
	gy, gs := selectedY(got, gm)
	wy, ws := selectedY(want, wm)
	for r := range 16 {
		goff := (got.row*16+r)*gs + got.col*16
		woff := (want.row*16+r)*ws + want.col*16
		if !bytes.Equal(gy[goff:goff+16], wy[woff:woff+16]) {
			t.Fatal("selected Y signal differs")
		}
	}
	for k := range got.e.denoiser.runningAvg {
		g, w := &got.e.denoiser.runningAvg[k].Img, &want.e.denoiser.runningAvg[k].Img
		if !bytes.Equal(g.YFull, w.YFull) || !bytes.Equal(g.UFull, w.UFull) || !bytes.Equal(g.VFull, w.VFull) {
			t.Fatalf("running average %d mismatch", k)
		}
	}
	if !reflect.DeepEqual(got.e.reconstructScratch, want.e.reconstructScratch) {
		t.Fatal("reconstruction scratch side effect differs")
	}
}

func TestVP8Algorithm2DenoiserLumaFrozen(t *testing.T) {
	for _, pattern := range []string{"subpel", "odd-integer", "fullpel", "y-only", "zero-yuv", "rejected", "split-fallback", "state-fallback"} {
		for _, partial := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/partial%t/alias%t", pattern, partial, alias), func(t *testing.T) {
					got, want := astraNewDenoiserFixture(t, pattern, partial, alias, true), astraNewDenoiserFixture(t, pattern, partial, alias, true)
					for trial := 0; trial < 3; trial++ {
						gm := got.e.applyDenoiserToInterMacroblock(got.source, got.filtered, got.rows, got.cols, got.row, got.col, &got.decision)
						wm := want.e.astraFrozenApplyDenoiserToInterMacroblock(want.source, want.filtered, want.rows, want.cols, want.row, want.col, &want.decision)
						astraDenoiserCompare(t, &got, &want, gm, wm)
					}
					// A following zero-motion UV consumer must overwrite any skipped
					// chroma scratch before reading it, including after Y-only mode.
					got.e.denoiser.mode, want.e.denoiser.mode = vp8enc.DenoiserOnYUV, vp8enc.DenoiserOnYUV
					got.decision.denoise.bestReferenceFrame = vp8common.IntraFrame
					got.decision.denoise.zeroMVSSE = 0
					want.decision = got.decision
					gm := got.e.applyDenoiserToInterMacroblock(got.source, got.filtered, got.rows, got.cols, got.row, got.col, &got.decision)
					wm := want.e.astraFrozenApplyDenoiserToInterMacroblock(want.source, want.filtered, want.rows, want.cols, want.row, want.col, &want.decision)
					astraDenoiserCompare(t, &got, &want, gm, wm)
					if got.e.denoiser.mcU != want.e.denoiser.mcU || got.e.denoiser.mcV != want.e.denoiser.mcV {
						t.Fatal("UV consumer retained stale scratch")
					}
				})
			}
		}
	}
}

var astraDenoiserLumaSink denoiserPlaneOverlay

func TestVP8Algorithm2DenoiserLumaAllocs(t *testing.T) {
	f := astraNewDenoiserFixture(t, "subpel", false, false, false)
	if got := testing.AllocsPerRun(100, func() {
		astraDenoiserLumaSink = f.e.applyDenoiserToInterMacroblock(f.source, f.filtered, f.rows, f.cols, f.row, f.col, &f.decision)
	}); got != 0 {
		t.Fatalf("%v allocations", got)
	}
}

func BenchmarkAstraDenoiserLuma(b *testing.B) {
	for _, pattern := range []string{"subpel", "odd-integer", "fullpel", "y-only", "zero-yuv", "rejected", "split-fallback", "state-fallback"} {
		b.Run(pattern, func(b *testing.B) {
			f := astraNewDenoiserFixture(b, pattern, false, false, false)
			var overlay denoiserPlaneOverlay
			b.ReportAllocs()
			for b.Loop() {
				overlay = f.e.applyDenoiserToInterMacroblock(f.source, f.filtered, f.rows, f.cols, f.row, f.col, &f.decision)
			}
			astraDenoiserLumaSink = overlay
		})
	}
}

func (e *VP8Encoder) astraFrozenApplyDenoiserToInterMacroblock(source vp8enc.SourceImage, filtered vp8enc.SourceImage, rows int, cols int, row int, col int, decision *interFrameModeDecision) denoiserPlaneOverlay {
	var overlay denoiserPlaneOverlay
	if e.opts.NoiseSensitivity <= 0 || !e.denoiser.allocated || decision == nil {
		return overlay
	}
	if rows <= 0 || cols <= 0 || row < 0 || row >= rows || col < 0 || col >= cols {
		return overlay
	}
	index := row*cols + col
	if len(e.denoiser.state) <= index {
		return overlay
	}
	d := decision.denoise
	if d.zeroMVReferenceFrame == vp8common.IntraFrame {
		return e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
	}

	frame := d.bestReferenceFrame
	mode := d.bestMode
	mv := d.bestMV
	bestSSE := d.bestSSE
	zeroSSE := uint32(uint64(d.zeroMVSSE) * uint64(e.denoiser.params.DenoiseMVBias) / 100)
	sseDiff := int64(zeroSSE) - int64(bestSSE)
	motionMag := uint32(int(mv.Row)*int(mv.Row) + int(mv.Col)*int(mv.Col))
	sseDiffThresh := 0
	if motionMag <= denoiserNoiseMotionThreshold {
		sseDiffThresh = vp8enc.DenoiserSSEDiffThreshold
	}
	denoiseZeroMV := frame == vp8common.IntraFrame || sseDiff <= int64(sseDiffThresh)
	if denoiseZeroMV {
		frame = d.zeroMVReferenceFrame
		mode = vp8common.ZeroMV
		mv = vp8enc.MotionVector{}
		bestSSE = zeroSSE
		motionMag = 0
	}

	avgIndex, ok := denoiserReferenceAvgIndexForMVRef(frame)
	if !ok {
		return e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
	}
	increase := motionMag < uint32(e.denoiser.params.ScaleIncreaseFilter)*denoiserNoiseMotionThreshold
	sseThresh := uint32(e.denoiser.params.ScaleSSEThresh * vp8enc.DenoiserSSEThreshold)
	if increase {
		sseThresh = uint32(e.denoiser.params.ScaleSSEThresh * vp8enc.DenoiserSSEThresholdHigh)
	}
	motionThresh := uint32(e.denoiser.params.ScaleMotionThresh) * denoiserNoiseMotionThreshold
	skinBlocksFilter := d.useSkinGate && e.denoiserSkinGateBlocksFilter(row, col, cols, index, motionMag)
	if bestSSE > sseThresh || motionMag > motionThresh || skinBlocksFilter {
		return e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
	}

	decMode := vp8dec.MacroblockMode{
		RefFrame:    frame,
		Mode:        mode,
		MV:          vp8dec.MotionVector{Row: mv.Row, Col: mv.Col},
		UVMode:      vp8common.DCPred,
		Is4x4:       mode == vp8common.SplitMV,
		MBSkipCoeff: true,
	}
	var zeroTokens vp8dec.MacroblockTokens
	mcY := e.denoiser.mcY[:]
	mcU := e.denoiser.mcU[:]
	mcV := e.denoiser.mcV[:]
	mcYStride, mcUStride, mcVStride := 16, 8, 8
	if !vp8dec.ReconstructWholeMVInterMacroblockWithState(
		&e.denoiser.refStates[avgIndex], &decMode, &zeroTokens, &e.dequants[0],
		mcY, mcYStride, mcU, mcUStride, mcV, mcVStride,
		&e.reconstructScratch.Residual, row, col,
	) {
		if !reconstructInterAnalysisMacroblockWithState(&e.denoiser.mcRunning.Img, &e.denoiser.runningAvg[avgIndex].Img, &e.denoiser.refStates[avgIndex], row, col, &decMode, &zeroTokens, &e.dequants[0], &e.reconstructScratch) {
			return e.copyDenoiserNoFilterMacroblock(source, filtered, row, col, cols, index)
		}
		yMcOff := row*16*e.denoiser.mcRunning.Img.YStride + col*16
		uMcOff := row*8*e.denoiser.mcRunning.Img.UStride + col*8
		vMcOff := row*8*e.denoiser.mcRunning.Img.VStride + col*8
		mcY = e.denoiser.mcRunning.Img.Y[yMcOff:]
		mcU = e.denoiser.mcRunning.Img.U[uMcOff:]
		mcV = e.denoiser.mcRunning.Img.V[vMcOff:]
		mcYStride = e.denoiser.mcRunning.Img.YStride
		mcUStride = e.denoiser.mcRunning.Img.UStride
		mcVStride = e.denoiser.mcRunning.Img.VStride
	}

	avg := &e.denoiser.runningAvg[denoiserAvgIntra]
	yOff := row*16*source.YStride + col*16
	uOff := row*8*source.UStride + col*8
	vOff := row*8*source.VStride + col*8
	ySigOff := row*16*filtered.YStride + col*16
	uSigOff := row*8*filtered.UStride + col*8
	vSigOff := row*8*filtered.VStride + col*8
	yAvgOff := row*16*avg.Img.YStride + col*16
	uAvgOff := row*8*avg.Img.UStride + col*8
	vAvgOff := row*8*avg.Img.VStride + col*8
	aliased := sourceImagePlaneMatches(source.Y, source.YStride, filtered.Y, filtered.YStride)

	// DenoiserFilterY reads the signal block through its sig pointer
	// (libvpx vp8_denoiser_filter reads x->thismb). On the split
	// source/overlay path the overlay's MB region holds stale pixels, so
	// stage the raw signal there first — this is the per-MB thismb copy
	// libvpx pays on every MB, here only on filter-candidate MBs.
	if !aliased {
		copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, source.Y[yOff:], source.YStride)
	}
	filterDecision := vp8enc.DenoiserFilterY(
		mcY, mcYStride,
		avg.Img.Y[yAvgOff:], avg.Img.YStride,
		filtered.Y[ySigOff:], filtered.YStride,
		motionMag, increase,
	)
	if filterDecision == vp8enc.DenoiserFilterBlock {
		overlay.y = !aliased
		if motionMag > 0 {
			e.denoiser.state[index] = vp8enc.DenoiserStateFilterNonZero
		} else {
			e.denoiser.state[index] = vp8enc.DenoiserStateFilterZeroMV
		}
	} else {
		e.denoiser.state[index] = vp8enc.DenoiserStateNoFilter
		copyMacroblockY(avg.Img.Y[yAvgOff:], avg.Img.YStride, source.Y[yOff:], source.YStride)
	}

	applySpatialFilter := func() {
		if e.applyDenoiserSpatialLoopFilter(filtered, avg, row, col, cols, index, ySigOff, yAvgOff) {
			copyMacroblockY(filtered.Y[ySigOff:], filtered.YStride, avg.Img.Y[yAvgOff:], avg.Img.YStride)
			overlay.y = !aliased
		}
	}
	if e.denoiser.mode == vp8enc.DenoiserOnYOnly {
		applySpatialFilter()
		return overlay
	}
	if motionMag == 0 && filterDecision == vp8enc.DenoiserFilterBlock {
		if !aliased {
			copyMacroblock8x8(filtered.U[uSigOff:], filtered.UStride, source.U[uOff:], source.UStride)
			copyMacroblock8x8(filtered.V[vSigOff:], filtered.VStride, source.V[vOff:], source.VStride)
		}
		if vp8enc.DenoiserFilterUV(
			mcU, mcUStride,
			avg.Img.U[uAvgOff:], avg.Img.UStride,
			filtered.U[uSigOff:], filtered.UStride,
			motionMag, false,
		) == vp8enc.DenoiserCopyBlock {
			copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
		} else {
			overlay.u = !aliased
		}
		if vp8enc.DenoiserFilterUV(
			mcV, mcVStride,
			avg.Img.V[vAvgOff:], avg.Img.VStride,
			filtered.V[vSigOff:], filtered.VStride,
			motionMag, false,
		) == vp8enc.DenoiserCopyBlock {
			copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
		} else {
			overlay.v = !aliased
		}
		applySpatialFilter()
		return overlay
	}
	copyMacroblock8x8(avg.Img.U[uAvgOff:], avg.Img.UStride, source.U[uOff:], source.UStride)
	copyMacroblock8x8(avg.Img.V[vAvgOff:], avg.Img.VStride, source.V[vOff:], source.VStride)
	applySpatialFilter()
	return overlay
}
