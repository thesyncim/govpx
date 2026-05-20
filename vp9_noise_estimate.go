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
	width := e.opts.Width
	height := e.opts.Height
	e.vp9NoiseEstimateRefreshEnabled()
	e.noiseEstimate.Update(encoder.NoiseEstimateUpdateArgs{
		Width:             width,
		Height:            height,
		FrameCounter:      int(e.frameIndex),
		NoiseSensitivity:  e.opts.NoiseSensitivity,
		MIRows:            miRows,
		MICols:            miCols,
		IntraOnly:         intraOnly,
		SourceY:           img.Y,
		SourceYStride:     img.YStride,
		SourceWidth:       img.Rect.Dx(),
		SourceHeight:      img.Rect.Dy(),
		LastSourceY:       e.lastSource.Y,
		LastSourceYStride: e.lastSource.YStride,
		LastSourceWidth:   e.lastSource.Rect.Dx(),
		LastSourceHeight:  e.lastSource.Rect.Dy(),
		LastSourceValid:   e.lastSourceValid,
		ConsecZeroMV:      e.cyclicAQ.ConsecZeroMV,
	})
}
