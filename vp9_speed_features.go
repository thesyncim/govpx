package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) vp9DefaultSpeedFrameContext() vp9SpeedFrameContext {
	ctx := vp9SpeedFrameContext{
		width:           e.opts.Width,
		height:          e.opts.Height,
		showFrame:       true,
		frameType:       common.KeyFrame,
		intraOnly:       true,
		resizeStateOrig: true,
	}
	if e != nil {
		ctx.svc = e.svc
	} else {
		ctx.svc = encoder.DefaultSVCState()
	}
	return ctx
}

// vp9PerFrameSpeedContextArgs carries the per-frame inputs that drive
// frame_is_kf_gf_arf() and the framesize-dependent SF picks. Mirrors the
// subset of cpi->common / cpi->refresh_*_frame / cpi->rc fields libvpx reads
// at the top of encode_frame_to_data_rate.
//
// libvpx: vp9_encoder.h:1013-1016 frame_is_kf_gf_arf,
// vp9_speed_features.c:919-1096 vp9_set_speed_features_framesize_independent.
type vp9PerFrameSpeedContextArgs struct {
	IsKey              bool
	IntraOnly          bool
	ShowFrame          bool
	RefreshGoldenFrame bool
	RefreshAltRefFrame bool
	IsSrcFrameAltRef   bool
	BaseQIndex         int
}

// vp9PerFrameSpeedContext builds a configurator context for a specific frame.
// The caller supplies the per-frame state libvpx reads via cpi->common,
// cpi->refresh_alt_ref_frame, cpi->refresh_golden_frame, and
// cpi->rc.is_src_frame_alt_ref. The remaining encoder-state fields
// (framesSinceKey, avgFrameLowMotion, avgFrameQindexInter, currentVideoFrame)
// are pulled from the live rate-control / frame counters so the framesize-
// dependent dispatcher sees the same inputs libvpx feeds it.
//
// libvpx: vp9_encoder.c:2635 / 3754 — same two-step protocol invoked per
// frame at top-of-encode.
func (e *VP9Encoder) vp9PerFrameSpeedContext(args vp9PerFrameSpeedContextArgs) vp9SpeedFrameContext {
	frameType := common.InterFrame
	if args.IsKey {
		frameType = common.KeyFrame
	}
	ctx := vp9SpeedFrameContext{
		width:               e.opts.Width,
		height:              e.opts.Height,
		showFrame:           args.ShowFrame,
		frameType:           frameType,
		intraOnly:           args.IntraOnly,
		refreshAltRefFrame:  args.RefreshAltRefFrame,
		refreshGoldenFrame:  args.RefreshGoldenFrame,
		isSrcFrameAltRef:    args.IsSrcFrameAltRef,
		baseQIndex:          args.BaseQIndex,
		framesSinceKey:      int(e.rc.framesSinceKey),
		avgFrameLowMotion:   100,
		avgFrameQindexInter: int(e.rc.avgFrameQIndexInter),
		currentVideoFrame:   e.frameIndex,
		frContentType:       e.vp9FrameContentTypeForSpeedFeatures(),
		internalImageEdge:   e.vp9InternalImageEdgeForSpeedFeatures(),
		svc:                 e.svc,
		// govpx's runtime resize always triggers a full re-allocation in
		// applyVP9ResolutionChange(), so libvpx's external_resize (set only
		// when the resize *did not* realloc) is never observable here.
		// libvpx: vp9_encoder.c:2153-2166.
		externalResize: false,
		// libvpx: vp9_encoder.h cpi->last_frame_dropped.
		lastFrameDropped: e.lastFrameDropped,
		// govpx does not run libvpx's internal dynamic-resize loop, so
		// cpi->resize_state == ORIG always.
		resizeStateOrig: true,
		// Mirror VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR into the per-frame
		// speed-feature context so overshoot-detection speed gates see the
		// same runtime bit as rate control.
		// libvpx: vp9_ratectrl.h rc->disable_overshoot_maxq_cbr.
		disableOvershootMaxqCbr: e.rc.disableOvershootMaxQCBR,
	}
	return ctx
}

func (e *VP9Encoder) vp9FrameContentTypeForSpeedFeatures() vp9FrameContentType {
	if e == nil || !e.twoPass.enabled() {
		return vp9FCNormal
	}
	row := e.twoPass.statsForFrame()
	if row.IntraSkipPct >= vp9FCAnimationThresh {
		return vp9FCGraphicsAnimation
	}
	return vp9FCNormal
}

func (e *VP9Encoder) vp9InternalImageEdgeForSpeedFeatures() bool {
	if e == nil || !e.twoPass.enabled() {
		return false
	}
	row := e.twoPass.statsForFrame()
	return row.InactiveZoneRows > 0 || row.InactiveZoneCols > 0
}

func (e *VP9Encoder) vp9DeadlineModeChanged() bool {
	if e == nil || !e.deadlineModePreviousFrameSet || e.frameIndex == 0 {
		return false
	}
	return vp9ResolveDeadlineMode(e.opts.Deadline) != e.deadlineModePreviousFrame
}

func (e *VP9Encoder) vp9LatchDeadlineModePreviousFrame() {
	if e == nil {
		return
	}
	e.deadlineModePreviousFrame = vp9ResolveDeadlineMode(e.opts.Deadline)
	e.deadlineModePreviousFrameSet = true
}

// vp9ApplySpeedFeatures runs the libvpx framesize-independent and
// framesize-dependent configurators. It must be called whenever speed-affecting
// options change (CpuUsed, Deadline, ScreenContentMode, RateControlMode), and
// also at frame setup so the framesize-dependent SF picks see the actual
// per-frame state.
//
// libvpx: vp9_encoder.c:2635 / 3754 / 3765 — same two-step protocol.
func (e *VP9Encoder) vp9ApplySpeedFeatures(ctx vp9SpeedFrameContext) {
	if e == nil {
		return
	}
	// libvpx: vp9_noise_estimate.c:129 — ne->enabled is recomputed by
	// vp9_update_noise_estimate at the top of encode_frame_to_data_rate
	// (vp9_encoder.c:4142), which runs before the speed-features dispatch
	// at vp9_encoder.c:3754. Refresh here so the consumer at
	// vp9_speed_features.c:777-782 reads the same predicate libvpx
	// evaluates whether the SF dispatch fires at setup, on options
	// change, or per-frame at top-of-encode.
	e.vp9NoiseEstimateRefreshEnabled()
	vp9SetSpeedFeaturesFramesizeIndependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &e.sf, e.vp9SpeedFeatureCPUUsed(), ctx)
}

// vp9DeadlineMode is the libvpx MODE selector govpx maps from the
// DeadlineBestQuality / DeadlineGoodQuality / DeadlineRealtime enum.
//
// libvpx: vp9_encoder.h MODE enum (GOOD=0, BEST=1, REALTIME=2).
type vp9DeadlineMode int

const (
	vp9ModeGood     vp9DeadlineMode = 0
	vp9ModeBest     vp9DeadlineMode = 1
	vp9ModeRealtime vp9DeadlineMode = 2
)

// vp9ResolveDeadlineMode maps govpx Deadline to the libvpx MODE picked by the
// SPEED_FEATURES dispatcher. DeadlineBestQuality maps to GOOD (libvpx's GOOD
// path serves the best-quality preset for cpu_used==0), DeadlineGoodQuality to
// GOOD, and DeadlineRealtime to REALTIME, matching govpx's existing oracle
// rate-control routing.
func vp9ResolveDeadlineMode(d Deadline) vp9DeadlineMode {
	switch d {
	case DeadlineRealtime:
		return vp9ModeRealtime
	default:
		return vp9ModeGood
	}
}

// vp9ResolveContent maps govpx's ScreenContentMode int8 to the libvpx
// vp9e_tune_content enum value used by the configurator.
func vp9ResolveContent(c int8) vp9SpeedDispatchContent {
	switch c {
	case int8(VP9ScreenContentScreen):
		return vp9ContentScreen
	case int8(VP9ScreenContentFilm):
		return vp9ContentFilm
	default:
		return vp9ContentDefault
	}
}

// vp9MinDim returns VPXMIN(width, height).
func vp9MinDim(w, h int) int {
	if w < h {
		return w
	}
	return h
}

// vp9FrameIsIntraOnly mirrors libvpx's frame_is_intra_only().
func vp9FrameIsIntraOnly(ctx vp9SpeedFrameContext) bool {
	return ctx.frameType == common.KeyFrame || ctx.intraOnly
}

// vp9FrameIsKfGfArf mirrors libvpx's frame_is_boosted(), which delegates to
// frame_is_kf_gf_arf():
//
//	return frame_is_intra_only(&cpi->common) || cpi->refresh_alt_ref_frame ||
//	       (cpi->refresh_golden_frame && !cpi->rc.is_src_frame_alt_ref);
//
// libvpx: vp9_speed_features.c:38-40 (frame_is_boosted),
// vp9_encoder.h:1013-1016 (frame_is_kf_gf_arf).
func vp9FrameIsKfGfArf(ctx vp9SpeedFrameContext) bool {
	if vp9FrameIsIntraOnly(ctx) {
		return true
	}
	if ctx.refreshAltRefFrame {
		return true
	}
	if ctx.refreshGoldenFrame && !ctx.isSrcFrameAltRef {
		return true
	}
	return false
}

// vp9SetPartitionMinLimit mirrors set_partition_min_limit().
//
// libvpx: vp9_speed_features.c:48-62.
func vp9SetPartitionMinLimit(width, height int) common.BlockSize {
	screenArea := width * height
	if screenArea < 1280*720 {
		return common.Block4x4
	}
	if screenArea < 1920*1080 {
		return common.Block8x8
	}
	return common.Block16x16
}

// vp9SetSpeedFeaturesFramesizeIndependent is the libvpx
// vp9_set_speed_features_framesize_independent() port. It first applies the
// best-quality defaults, then dispatches to set_rt_speed_feature_framesize_
// independent or set_good_speed_feature_framesize_independent, and finally
// applies the pass-0 / pass-1 / lossless fixups.
//
// libvpx: vp9_speed_features.c:919-1096.
