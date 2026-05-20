package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_noise_estimate.go wires the internal VP9 noise-estimate value object into
// the root VP9Encoder lifecycle and frame update path.
//
// libvpx refs:
//   - vp9/encoder/vp9_noise_estimate.h:26-40 (MAX_VAR_HIST_BINS, NOISE_LEVEL,
//     NOISE_ESTIMATE struct).
//   - vp9/encoder/vp9_noise_estimate.c:33-50 (vp9_noise_estimate_init).
//   - vp9/encoder/vp9_noise_estimate.c:52-74 (enable_noise_estimation).
//   - vp9/encoder/vp9_noise_estimate.c:94-107 (vp9_noise_estimate_extract_level).
//
// vp9_update_noise_estimate's realtime cyclic-AQ update path is wired below
// from libvpx vp9_noise_estimate.c:109-302. SVC high-motion resets,
// denoiser.last_source, and use_skin_detection remain single-layer defaults in
// govpx, so the non-denoiser one-pass CBR branch is the active port.

// vp9NoiseEstimateRefreshEnabled rebinds e.noiseEstimate.Enabled from the
// internal enable predicate using the live encoder options and speed. Mirrors
// libvpx's ne->enabled = enable_noise_estimation(cpi) assignment at the top
// of vp9_update_noise_estimate (vp9_noise_estimate.c:129). Called from
// NewVP9Encoder and before each frame-setup speed-features dispatch so the
// vp9_speed_features.c:777-782 consumer reads the same predicate libvpx
// evaluates.
func (e *VP9Encoder) vp9NoiseEstimateRefreshEnabled() {
	if e == nil {
		return
	}
	e.noiseEstimate.Enabled = encoder.EnableNoiseEstimation(encoder.EnableNoiseEstimationArgs{
		UseHighBitdepth:     false,
		NoiseSensitivity:    e.opts.NoiseSensitivity,
		UseSVC:              false,
		Pass:                0,
		RcModeCBR:           e.opts.RateControlMode == RateControlCBR,
		AqModeCyclicRefresh: e.opts.AQMode == VP9AQCyclicRefresh,
		Speed:               e.vp9SpeedFeatureCPUUsed(),
		ResizeStateOrig:     true,
		ResizePending:       false,
		ScreenContent:       vp9ResolveContent(e.opts.ScreenContentMode) == vp9ContentScreen,
		Width:               e.opts.Width,
		Height:              e.opts.Height,
	})
}

// vp9UpdateNoiseEstimate ports libvpx's vp9_update_noise_estimate
// (vp9/encoder/vp9_noise_estimate.c:109-302) for govpx's single-layer
// non-denoiser realtime path. It samples steady 16x16 blocks every eighth
// frame, buckets source-vs-last-source variance into MAX_VAR_HIST_BINS, and
// updates ne->value/count/level with the same histogram smoothing and scale.
func (e *VP9Encoder) vp9UpdateNoiseEstimate(img *image.YCbCr, miRows, miCols int, intraOnly bool) {
	if e == nil || img == nil {
		return
	}
	ne := &e.noiseEstimate
	width := e.opts.Width
	height := e.opts.Height
	const framePeriod = 8
	const binSize = 100
	threshConsecZeroMV := 6
	frameCounter := int(e.frameIndex)
	if intraOnly {
		for i := range e.cyclicAQ.consecZeroMv {
			e.cyclicAQ.consecZeroMv[i] = 0
		}
	}
	if e.opts.NoiseSensitivity > 0 {
		if width > 640 && width <= 1920 {
			threshConsecZeroMV = 2
		}
	}
	e.vp9NoiseEstimateRefreshEnabled()
	lastSourceValid := e.lastSourceValid &&
		e.lastSource.Rect.Dx() == width && e.lastSource.Rect.Dy() == height
	sourceValid := img.Rect.Dx() == width && img.Rect.Dy() == height
	consecZeroMv := e.cyclicAQ.consecZeroMv
	if e.opts.NoiseSensitivity > 0 ||
		!ne.Enabled || frameCounter%framePeriod != 0 ||
		!lastSourceValid || !sourceValid ||
		miRows <= 0 || miCols <= 0 ||
		len(consecZeroMv) < miRows*miCols ||
		ne.LastW != width || ne.LastH != height {
		if lastSourceValid {
			ne.LastW = width
			ne.LastH = height
		}
		return
	}

	numLowMotion := 0
	for miRow := range miRows {
		for miCol := range miCols {
			blIndex := miRow*miCols + miCol
			if consecZeroMv[blIndex] > uint8(threshConsecZeroMV) {
				numLowMotion++
			}
		}
	}
	frameLowMotion := true
	if numLowMotion < ((3 * miRows * miCols) >> 3) {
		frameLowMotion = false
	}

	var hist [encoder.NoiseEstimateMaxVarHistBins]uint32
	for miRow := range miRows {
		for miCol := range miCols {
			if miRow%4 != 0 || miCol%4 != 0 ||
				miRow >= miRows-1 || miCol >= miCols-1 {
				continue
			}
			blIndex := miRow*miCols + miCol
			blIndex1 := blIndex + 1
			blIndex2 := blIndex + miCols
			blIndex3 := blIndex2 + 1
			if blIndex3 >= len(consecZeroMv) {
				continue
			}
			consec := min(int(consecZeroMv[blIndex]),
				min(int(consecZeroMv[blIndex1]),
					min(int(consecZeroMv[blIndex2]), int(consecZeroMv[blIndex3]))))
			if !frameLowMotion || consec <= threshConsecZeroMV {
				continue
			}
			srcX := miCol << 3
			srcY := miRow << 3
			if srcX+16 > width || srcY+16 > height {
				continue
			}
			variance, _ := encoder.BlockDiffVarianceSSE(img.Y, img.YStride,
				e.lastSource.Y, e.lastSource.YStride, srcX, srcY, srcX, srcY, 16, 16)
			histIndex := variance / binSize
			if histIndex < encoder.NoiseEstimateMaxVarHistBins {
				hist[histIndex]++
			} else if histIndex < 3*(encoder.NoiseEstimateMaxVarHistBins>>1) {
				hist[encoder.NoiseEstimateMaxVarHistBins-1]++
			}
		}
	}
	ne.LastW = width
	ne.LastH = height

	if hist[0] > 10 && hist[encoder.NoiseEstimateMaxVarHistBins-1] > hist[0]>>2 {
		hist[0] = 0
		hist[1] >>= 2
		hist[2] >>= 2
		hist[3] >>= 2
		hist[4] >>= 1
		hist[5] >>= 1
		hist[6] = 3 * hist[6] >> 1
		hist[encoder.NoiseEstimateMaxVarHistBins-1] >>= 1
	}

	var histAvg [encoder.NoiseEstimateMaxVarHistBins]uint32
	var maxBin uint32
	var maxBinCount uint32
	for binCnt := range encoder.NoiseEstimateMaxVarHistBins {
		switch {
		case binCnt == 0:
			histAvg[binCnt] = (hist[0] + hist[1] + hist[2]) / 3
		case binCnt == encoder.NoiseEstimateMaxVarHistBins-1:
			histAvg[binCnt] = hist[encoder.NoiseEstimateMaxVarHistBins-1] >> 2
		case binCnt == encoder.NoiseEstimateMaxVarHistBins-2:
			histAvg[binCnt] = (hist[binCnt-1] + 2*hist[binCnt] +
				(hist[binCnt+1] >> 1) + 2) >> 2
		default:
			histAvg[binCnt] = (hist[binCnt-1] + 2*hist[binCnt] +
				hist[binCnt+1] + 2) >> 2
		}
		if histAvg[binCnt] > maxBinCount {
			maxBinCount = histAvg[binCnt]
			maxBin = uint32(binCnt)
		}
	}

	ne.Value = (3*ne.Value + int(maxBin)*40) >> 2
	if ne.Level < encoder.NoiseLevelMedium && ne.Value > ne.AdaptThresh {
		ne.Count = ne.NumFramesEstimate
	} else {
		ne.Count++
	}
	if ne.Count == ne.NumFramesEstimate {
		ne.NumFramesEstimate = 30
		ne.Count = 0
		ne.Level = ne.ExtractLevel()
	}
}
