package govpx

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
// The full vp9_update_noise_estimate body (libvpx vp9_noise_estimate.c:109-302)
// requires Last_Source, consec_zero_mv, skin detection, and SSE variance
// functions that govpx has not yet ported. Until those land, ne.value stays
// at 0 and vp9_noise_estimate_extract_level returns kLowLow, which matches
// libvpx's behaviour on the first num_frames_estimate frames before the
// update populates ne.value. The SPEED_FEATURES consumer at
// vp9_speed_features.c:777-782 is wired to read ne.enabled and the extracted
// level: pre-update, the level is kLowLow < kMedium so the consumer leaves
// sf.short_circuit_low_temp_var at its libvpx-default 3 — the safe,
// libvpx-matching pre-update behaviour.

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
	level             vp9NoiseLevel
	value             int
	thresh            int
	adaptThresh       int
	count             int
	lastW             int
	lastH             int
	numFramesEstimate int
}

// vp9NoiseEstimateInit ports libvpx's vp9_noise_estimate_init verbatim
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
func vp9NoiseEstimateInit(ne *vp9NoiseEstimateState, width, height int) {
	if ne == nil {
		return
	}
	ne.enabled = false
	if width*height < 1280*720 {
		ne.level = vp9NoiseLevelLowLow
	} else {
		ne.level = vp9NoiseLevelLow
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

// vp9NoiseEstimateExtractLevel ports libvpx's vp9_noise_estimate_extract_level
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
func vp9NoiseEstimateExtractLevel(ne *vp9NoiseEstimateState) vp9NoiseLevel {
	if ne == nil {
		return vp9NoiseLevelLowLow
	}
	if ne.value > (ne.thresh << 1) {
		return vp9NoiseLevelHigh
	}
	if ne.value > ne.thresh {
		return vp9NoiseLevelMedium
	}
	if ne.value > (ne.thresh >> 1) {
		return vp9NoiseLevelLow
	}
	return vp9NoiseLevelLowLow
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
