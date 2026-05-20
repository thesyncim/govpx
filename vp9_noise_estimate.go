package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_noise_estimate.go ports the libvpx VP9 NOISE_ESTIMATE state and the
// init / enable_noise_estimation / vp9_noise_estimate_extract_level helpers
// verbatim from libvpx v1.16.0.
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

// vp9NoiseEstimateMaxVarHistBins mirrors libvpx's MAX_VAR_HIST_BINS
// (vp9/encoder/vp9_noise_estimate.h:26).
const vp9NoiseEstimateMaxVarHistBins = 20

// vp9NoiseEstimateState mirrors libvpx's NOISE_ESTIMATE struct
// (vp9/encoder/vp9_noise_estimate.h:30-40):
//
//	typedef struct noise_estimate {
//	  int enabled;
//	  NOISE_LEVEL level;
//	  int value;
//	  int thresh;
//	  int adapt_thresh;
//	  int count;
//	  int last_w;
//	  int last_h;
//	  int num_frames_estimate;
//	} NOISE_ESTIMATE;
type vp9NoiseEstimateState struct {
	enabled           bool
	level             encoder.NoiseLevel
	value             int
	thresh            int
	adaptThresh       int
	count             int
	lastW             int
	lastH             int
	numFramesEstimate int
}

// init ports libvpx's vp9_noise_estimate_init verbatim
// (vp9/encoder/vp9_noise_estimate.c:33-50):
//
//	void vp9_noise_estimate_init(NOISE_ESTIMATE *const ne, int width, int height) {
//	  ne->enabled = 0;
//	  ne->level = (width * height < 1280 * 720) ? kLowLow : kLow;
//	  ne->value = 0;
//	  ne->count = 0;
//	  ne->thresh = 90;
//	  ne->last_w = 0;
//	  ne->last_h = 0;
//	  if (width * height >= 1920 * 1080) {
//	    ne->thresh = 200;
//	  } else if (width * height >= 1280 * 720) {
//	    ne->thresh = 140;
//	  } else if (width * height >= 640 * 360) {
//	    ne->thresh = 115;
//	  }
//	  ne->num_frames_estimate = 15;
//	  ne->adapt_thresh = (3 * ne->thresh) >> 1;
//	}
func (ne *vp9NoiseEstimateState) init(width, height int) {
	if ne == nil {
		return
	}
	ne.enabled = false
	if width*height < 1280*720 {
		ne.level = encoder.NoiseLevelLowLow
	} else {
		ne.level = encoder.NoiseLevelLow
	}
	ne.value = 0
	ne.count = 0
	ne.thresh = 90
	ne.lastW = 0
	ne.lastH = 0
	if width*height >= 1920*1080 {
		ne.thresh = 200
	} else if width*height >= 1280*720 {
		ne.thresh = 140
	} else if width*height >= 640*360 {
		ne.thresh = 115
	}
	ne.numFramesEstimate = 15
	ne.adaptThresh = (3 * ne.thresh) >> 1
}

// extractLevel ports libvpx's vp9_noise_estimate_extract_level
// verbatim (vp9/encoder/vp9_noise_estimate.c:94-107):
//
//	NOISE_LEVEL vp9_noise_estimate_extract_level(NOISE_ESTIMATE *const ne) {
//	  int noise_level = kLowLow;
//	  if (ne->value > (ne->thresh << 1)) {
//	    noise_level = kHigh;
//	  } else {
//	    if (ne->value > ne->thresh)
//	      noise_level = kMedium;
//	    else if (ne->value > (ne->thresh >> 1))
//	      noise_level = kLow;
//	    else
//	      noise_level = kLowLow;
//	  }
//	  return noise_level;
//	}
func (ne *vp9NoiseEstimateState) extractLevel() encoder.NoiseLevel {
	if ne == nil {
		return encoder.NoiseLevelLowLow
	}
	if ne.value > (ne.thresh << 1) {
		return encoder.NoiseLevelHigh
	}
	if ne.value > ne.thresh {
		return encoder.NoiseLevelMedium
	}
	if ne.value > (ne.thresh >> 1) {
		return encoder.NoiseLevelLow
	}
	return encoder.NoiseLevelLowLow
}

// vp9EnableNoiseEstimationArgs carries the predicate inputs libvpx's
// enable_noise_estimation reads off cpi-> fields.
//
// libvpx ref: vp9/encoder/vp9_noise_estimate.c:52-74.
//
// CONFIG_VP9_HIGHBITDEPTH branch (UseHighBitdepth=true → return false) and the
// CONFIG_VP9_TEMPORAL_DENOISING branch are reproduced verbatim. govpx wires
// them with:
//
//   - UseHighBitdepth: always false (govpx is 8-bit-only).
//   - NoiseSensitivity: VP9EncoderOptions.NoiseSensitivity (libvpx
//     oxcf.noise_sensitivity).
//   - UseSVC: false until SVC plumbing lands (the noise-est predicate ANDs
//     !use_svc except in the denoiser-enabled branch where noise_est_svc
//     additionally restricts to the top spatial layer).
//   - Pass: 0 (one-pass; libvpx requires oxcf.pass == 0 for the non-denoiser
//     branch).
//   - RcModeCBR: e.opts.RateControlMode == RateControlCBR.
//   - AqModeCyclicRefresh: e.opts.AQMode == VP9AQCyclicRefresh.
//   - Speed: vp9SpeedFeatureCPUUsed().
//   - ResizeStateOrig: true until resize plumbing lands (libvpx requires
//     resize_state == ORIG && resize_pending == 0).
//   - Content: vp9ResolveContent(e.opts.ScreenContentMode); the predicate
//     rejects VP9E_CONTENT_SCREEN.
//   - Width / Height: cm->width / cm->height (the configured encode size).
type vp9EnableNoiseEstimationArgs struct {
	UseHighBitdepth     bool
	NoiseSensitivity    int8
	UseSVC              bool
	Pass                int
	RcModeCBR           bool
	AqModeCyclicRefresh bool
	Speed               int
	ResizeStateOrig     bool
	ResizePending       bool
	Content             vp9SpeedDispatchContent
	Width               int
	Height              int
}

// vp9EnableNoiseEstimation ports libvpx's enable_noise_estimation verbatim
// (vp9/encoder/vp9_noise_estimate.c:52-74):
//
//	static int enable_noise_estimation(VP9_COMP *const cpi) {
//	#if CONFIG_VP9_HIGHBITDEPTH
//	  if (cpi->common.use_highbitdepth) return 0;
//	#endif
//	// Enable noise estimation if denoising is on.
//	#if CONFIG_VP9_TEMPORAL_DENOISING
//	  if (cpi->oxcf.noise_sensitivity > 0 && noise_est_svc(cpi) &&
//	      cpi->common.width >= 320 && cpi->common.height >= 180)
//	    return 1;
//	#endif
//	  // Only allow noise estimate under certain encoding mode.
//	  // Enabled for 1 pass CBR, speed >=5, and if resolution is same as original.
//	  // Not enabled for SVC mode and screen_content_mode.
//	  // Not enabled for low resolutions.
//	  if (cpi->oxcf.pass == 0 && cpi->oxcf.rc_mode == VPX_CBR &&
//	      cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ && cpi->oxcf.speed >= 5 &&
//	      cpi->resize_state == ORIG && cpi->resize_pending == 0 && !cpi->use_svc &&
//	      cpi->oxcf.content != VP9E_CONTENT_SCREEN &&
//	      cpi->common.width * cpi->common.height >= 640 * 360)
//	    return 1;
//	  else
//	    return 0;
//	}
//
// noise_est_svc (vp9/encoder/vp9_noise_estimate.c:26-30):
//
//	static INLINE int noise_est_svc(const struct VP9_COMP *const cpi) {
//	  return (!cpi->use_svc ||
//	          (cpi->use_svc &&
//	           cpi->svc.spatial_layer_id == cpi->svc.number_spatial_layers - 1));
//	}
//
// govpx is single-layer so use_svc == false ⇒ noise_est_svc == true.
func vp9EnableNoiseEstimation(args vp9EnableNoiseEstimationArgs) bool {
	if args.UseHighBitdepth {
		return false
	}
	// CONFIG_VP9_TEMPORAL_DENOISING branch. noise_est_svc reduces to !use_svc
	// in govpx's single-layer build (see comment above).
	if args.NoiseSensitivity > 0 && !args.UseSVC &&
		args.Width >= 320 && args.Height >= 180 {
		return true
	}
	if args.Pass == 0 && args.RcModeCBR && args.AqModeCyclicRefresh &&
		args.Speed >= 5 && args.ResizeStateOrig && !args.ResizePending &&
		!args.UseSVC && args.Content != vp9ContentScreen &&
		args.Width*args.Height >= 640*360 {
		return true
	}
	return false
}

// vp9NoiseEstimateRefreshEnabled rebinds e.noiseEstimate.enabled from
// vp9EnableNoiseEstimation using the live encoder options and speed. Mirrors
// libvpx's ne->enabled = enable_noise_estimation(cpi) assignment at the top
// of vp9_update_noise_estimate (vp9_noise_estimate.c:129). Called from
// NewVP9Encoder and before each frame-setup speed-features dispatch so the
// vp9_speed_features.c:777-782 consumer reads the same predicate libvpx
// evaluates.
func (e *VP9Encoder) vp9NoiseEstimateRefreshEnabled() {
	if e == nil {
		return
	}
	e.noiseEstimate.enabled = vp9EnableNoiseEstimation(vp9EnableNoiseEstimationArgs{
		UseHighBitdepth:     false,
		NoiseSensitivity:    e.opts.NoiseSensitivity,
		UseSVC:              false,
		Pass:                0,
		RcModeCBR:           e.opts.RateControlMode == RateControlCBR,
		AqModeCyclicRefresh: e.opts.AQMode == VP9AQCyclicRefresh,
		Speed:               e.vp9SpeedFeatureCPUUsed(),
		ResizeStateOrig:     true,
		ResizePending:       false,
		Content:             vp9ResolveContent(e.opts.ScreenContentMode),
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
		!ne.enabled || frameCounter%framePeriod != 0 ||
		!lastSourceValid || !sourceValid ||
		miRows <= 0 || miCols <= 0 ||
		len(consecZeroMv) < miRows*miCols ||
		ne.lastW != width || ne.lastH != height {
		if lastSourceValid {
			ne.lastW = width
			ne.lastH = height
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

	var hist [vp9NoiseEstimateMaxVarHistBins]uint32
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
			if histIndex < vp9NoiseEstimateMaxVarHistBins {
				hist[histIndex]++
			} else if histIndex < 3*(vp9NoiseEstimateMaxVarHistBins>>1) {
				hist[vp9NoiseEstimateMaxVarHistBins-1]++
			}
		}
	}
	ne.lastW = width
	ne.lastH = height

	if hist[0] > 10 && hist[vp9NoiseEstimateMaxVarHistBins-1] > hist[0]>>2 {
		hist[0] = 0
		hist[1] >>= 2
		hist[2] >>= 2
		hist[3] >>= 2
		hist[4] >>= 1
		hist[5] >>= 1
		hist[6] = 3 * hist[6] >> 1
		hist[vp9NoiseEstimateMaxVarHistBins-1] >>= 1
	}

	var histAvg [vp9NoiseEstimateMaxVarHistBins]uint32
	var maxBin uint32
	var maxBinCount uint32
	for binCnt := range vp9NoiseEstimateMaxVarHistBins {
		switch {
		case binCnt == 0:
			histAvg[binCnt] = (hist[0] + hist[1] + hist[2]) / 3
		case binCnt == vp9NoiseEstimateMaxVarHistBins-1:
			histAvg[binCnt] = hist[vp9NoiseEstimateMaxVarHistBins-1] >> 2
		case binCnt == vp9NoiseEstimateMaxVarHistBins-2:
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

	ne.value = (3*ne.value + int(maxBin)*40) >> 2
	if ne.level < encoder.NoiseLevelMedium && ne.value > ne.adaptThresh {
		ne.count = ne.numFramesEstimate
	} else {
		ne.count++
	}
	if ne.count == ne.numFramesEstimate {
		ne.numFramesEstimate = 30
		ne.count = 0
		ne.level = ne.extractLevel()
	}
}
