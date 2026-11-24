package govpx

import (
	"runtime"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

func encoderMacroblockRows(height int) int {
	return geometry.MacroblockRows(height)
}

func encoderMacroblockCols(width int) int {
	return geometry.MacroblockCols(width)
}

func sourceImageFromImage(src Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}

// LastQuantizer returns the public 0..63 quantizer and the internal VP8
// qindex for the most recently committed encoded frame. ok is false on a
// nil or closed encoder, and before any frame has been committed (which
// includes dropped or buffered-by-lookahead inputs).
func (e *VP8Encoder) LastQuantizer() (public int, internal int, ok bool) {
	if e == nil || e.closed || !e.lastQuantizerValid {
		return 0, 0, false
	}
	return e.lastQuantizerPublic, e.lastQuantizerInternal, true
}

func (e *VP8Encoder) setEncodeResultQuantizer(result *EncodeResult, qIndex int) {
	public := vp8common.QIndexToPublicQuantizer(qIndex)
	result.Quantizer = public
	result.InternalQuantizer = qIndex
	e.lastQuantizerPublic = public
	e.lastQuantizerInternal = qIndex
	e.lastQuantizerValid = true
}

// Keyframes use the fixed default coefficient probabilities. Inter frames
// already provide a per-frame table; retain the same prepared representation.
var vp8KeyFrameCoefCosts = func() (costs vp8enc.CoefficientTokenCostTable) {
	vp8enc.FillCoefficientTokenCostTable(&vp8tables.DefaultCoefProbs, &costs)
	return
}()

// Keyframe probabilities are fixed by VP8. Like libvpx's vp8_init_mode_costs,
// resolve each immutable tree once rather than walking it for every candidate.
var vp8StaticModeCosts = func() (out struct {
	b  [10][10][10]int
	y  [5]int
	uv [4]int
}) {
	for a := range 10 {
		for l := range 10 {
			for m := range 10 {
				out.b[a][l][m] = vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[a][l][:], m)
			}
		}
	}
	for m := range out.y {
		out.y[m] = vp8enc.TreeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], m)
	}
	for m := range out.uv {
		out.uv[m] = vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], m)
	}
	return
}()

func (e *VP8Encoder) baseSkipFalseProb(qIndex int) uint8 {
	qIndex = min(max(qIndex, 0), len(vp8enc.DefaultBaseSkipFalseProbs)-1)
	if e.baseSkipFalseProbs[qIndex] == 0 {
		return vp8enc.DefaultBaseSkipFalseProbs[qIndex]
	}
	return e.baseSkipFalseProbs[qIndex]
}

func (e *VP8Encoder) interFrameAnalysisSkipFalseProb(qIndex int, refreshGolden bool, refreshAltRef bool, singleLayerSrcAltRef bool) uint8 {
	prob := e.baseSkipFalseProb(qIndex)
	if last := e.lastSkipFalseProbs[vp8enc.SkipFalseReferenceIndex(refreshGolden, refreshAltRef)]; last != 0 {
		prob = last
	}
	prob = vp8enc.ClampInterAnalysisSkipFalseProb(prob)
	if singleLayerSrcAltRef {
		return 1
	}
	return prob
}

func (e *VP8Encoder) commitInterFrameSkipFalseProb(attempt interFrameEncodeAttempt) {
	if attempt.Config.ProbSkipFalse == 0 {
		return
	}
	prob := attempt.Config.ProbSkipFalse
	qIndex := min(max(int(attempt.Config.BaseQIndex), 0), len(e.baseSkipFalseProbs)-1)
	idx := vp8enc.SkipFalseReferenceIndex(attempt.Config.RefreshGolden, attempt.Config.RefreshAltRef)
	e.probSkipFalse = prob
	e.lastSkipFalseProbs[idx] = prob
	if !attempt.Config.RefreshGolden && !attempt.Config.RefreshAltRef {
		e.baseSkipFalseProbs[qIndex] = prob
	}
}

// maxEncoderThreads bounds EncoderOptions.Threads to cap persistent row-worker state.
const maxEncoderThreads = 64

// effectiveThreadCount returns the runtime worker-goroutine count for
// row-parallel inter-frame work, clamped against runtime.NumCPU() and maxEncoderThreads.
func (e *VP8Encoder) effectiveThreadCount() int {
	threads := e.opts.Threads
	if threads <= 0 {
		threads = 1
	}
	cores := max(runtime.NumCPU(), 1)
	if threads > cores {
		threads = cores
	}
	if threads > maxEncoderThreads {
		threads = maxEncoderThreads
	}
	return threads
}
