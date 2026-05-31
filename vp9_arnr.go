package govpx

import (
	"image"

	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) vp9AutoAltRefSourceImage(center *vp9LookaheadEntry) *image.YCbCr {
	if center == nil {
		return nil
	}
	if e.applyVP9ARNRFilter(center) {
		return &e.vp9ARNRScratch
	}
	return &center.img
}

// vp9KeyFrameFilteringActive reports whether the libvpx-faithful VP9E_SET_
// KEY_FRAME_FILTERING gates are all satisfied for the next encode.  The
// gate set mirrors vp9/encoder/vp9_encoder.c:6347-6353 exactly:
//
//	is_key_temporal_filter_enabled =
//	    oxcf->enable_keyframe_filtering &&
//	    cpi->oxcf.mode != REALTIME &&
//	    (oxcf->pass != 1) &&
//	    !cpi->use_svc &&
//	    !is_lossless_requested(&cpi->oxcf) &&
//	    cm->frame_type == KEY_FRAME &&
//	    (oxcf->arnr_max_frames > 0) &&
//	    (oxcf->arnr_strength > 0) &&
//	    cpi->oxcf.speed < 2;
//
// govpx folds the runtime control surface into option fields:
// EnableKeyFrameFiltering, Deadline (REALTIME equivalent), Lossless,
// ARNRMaxFrames, ARNRStrength, and CPUUsed.  Two-pass / SVC are out of
// scope; both are disabled here when their option fields are off.
func (e *VP9Encoder) vp9KeyFrameFilteringActive() bool {
	if e == nil || !e.opts.EnableKeyFrameFiltering {
		return false
	}
	// libvpx: cpi->oxcf.mode != REALTIME — govpx folds the realtime gate
	// into Deadline.  DeadlineRealtime maps to libvpx's MODE_REALTIME so
	// suppress keyframe filtering there.
	if e.opts.Deadline == DeadlineRealtime {
		return false
	}
	if e.opts.Lossless {
		return false
	}
	if e.opts.ARNRMaxFrames <= 1 || e.opts.ARNRStrength <= 0 {
		return false
	}
	// libvpx: cpi->oxcf.speed < 2.  govpx exposes the speed as CpuUsed.
	if e.opts.CpuUsed >= 2 {
		return false
	}
	return true
}

// applyVP9KeyFrameFilter runs the libvpx-faithful keyframe temporal-filter
// pass against the supplied keyframe source img and forward lookahead.
// Returns the filtered image (aliasing e.vp9ARNRScratch) when the pass ran,
// or img unchanged when the gates trip or the lookahead is too short.
//
// libvpx: vp9/encoder/vp9_encoder.c:6347-6364
//
//	if (is_key_temporal_filter_enabled && source != NULL) {
//	  vp9_temporal_filter(cpi, -1);
//	  vpx_extend_frame_borders(&cpi->tf_buffer);
//	  force_src_buffer = &cpi->tf_buffer;
//	  cpi->un_scaled_source = cpi->Source = force_src_buffer;
//	}
//
// vp9_temporal_filter(cpi, -1) is the forward-only window (distance == -1
// in libvpx's adjust_arnr_filter) so start_frame = frames_to_blur_forward
// - 1 and the entire window sits AHEAD of the keyframe in source order.
// govpx mirrors this with a forward-only ARNRType=2 window plus the
// adaptive strength path.
func (e *VP9Encoder) applyVP9KeyFrameFilter(img *image.YCbCr) *image.YCbCr {
	if !e.vp9KeyFrameFilteringActive() || img == nil ||
		len(e.vp9ARNRScratch.Y) == 0 {
		return img
	}
	if !e.vp9LookaheadEnabled() || e.lookaheadCount == 0 {
		return img
	}
	maxFrames := min(e.opts.ARNRMaxFrames, maxARNRFrames)
	if maxFrames <= 1 {
		return img
	}
	// libvpx: vp9_temporal_filter.c:1255 adjust_arnr_filter with
	// distance=-1 picks a forward-only window. govpx honors ARNRType=2
	// (forward) the same way.
	// lookaheadCount frames are ahead of the keyframe (current already
	// popped out by the caller, mirroring the libvpx "source has been
	// popped out" comment at vp9_encoder.c:6358).
	framesForward := min(int(e.lookaheadCount), maxFrames-1)
	if framesForward <= 0 {
		return img
	}
	framesToBlur := framesForward + 1
	if framesToBlur > maxARNRFrames {
		framesToBlur = maxARNRFrames
		framesForward = framesToBlur - 1
	}
	strength := e.opts.ARNRStrength
	// libvpx applies adjust_arnr_filter's adaptive strength when gfu_boost
	// is populated; govpx mirrors that for parity with the alt-ref pass.
	if e.rc.gfuBoost > 0 {
		adj := vp9enc.AdjustARNRFilter(vp9enc.AdjustARNRFilterInput{
			LookaheadDepth:         int(e.lookaheadCount),
			Distance:               -1,
			GroupBoost:             int(e.rc.gfuBoost),
			ARNRMaxFrames:          e.opts.ARNRMaxFrames,
			ARNRStrengthBase:       e.opts.ARNRStrength,
			ARNRStrengthAdjustment: 0,
			Pass:                   1,
			CurrentVideoFrame:      e.frameIndex,
			AvgFrameQIndexInter:    int(e.rc.avgFrameQIndexInter),
			AvgFrameQIndexKey:      int(e.rc.avgFrameQIndexKey),
		})
		if adj.ARNRStrength > 0 {
			strength = adj.ARNRStrength
		}
	}
	copyVP9LookaheadImage(&e.vp9ARNRScratch, img, e.opts.Width,
		e.opts.Height)
	refs := e.vp9ARNRRefs[:framesToBlur:framesToBlur]
	// Index 0 is the keyframe (center); indices 1..framesToBlur-1 are the
	// forward lookahead frames in source order. IterateTemporalFilter reads
	// centerIdx as 0 because distance == -1 means no backward frames.
	refs[0] = vp9enc.TemporalFilterFrameFromYCbCr(img)
	for frame := 1; frame < framesToBlur; frame++ {
		entry, ok := e.peekVP9LookaheadAt(frame - 1)
		if !ok {
			return img
		}
		refs[frame] = vp9enc.TemporalFilterFrameFromYCbCr(&entry.img)
	}
	dst := vp9enc.TemporalFilterFrameFromYCbCr(&e.vp9ARNRScratch)
	vp9enc.IterateTemporalFilter(&dst, refs, 0, strength)
	return &e.vp9ARNRScratch
}

// SetEnableKeyFrameFiltering toggles the libvpx VP9E_SET_KEY_FRAME_FILTERING
// runtime control.  Mirrors libvpx's ctrl_set_keyframe_filtering
// (vp9/vp9_cx_iface.c:974-979) which simply assigns the new value into
// extra_cfg.enable_keyframe_filtering on every call.
//
// libvpx: vp9/vp9_cx_iface.c:974
//
//	static vpx_codec_err_t ctrl_set_keyframe_filtering(... va_list args) {
//	  struct vp9_extracfg extra_cfg = ctx->extra_cfg;
//	  extra_cfg.enable_keyframe_filtering =
//	      CAST(VP9E_SET_KEY_FRAME_FILTERING, args);
//	  return update_extra_cfg(ctx, &extra_cfg);
//	}
func (e *VP9Encoder) SetEnableKeyFrameFiltering(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.opts.EnableKeyFrameFiltering = enabled
	if enabled && e.opts.ARNRMaxFrames > 1 {
		e.ensureVP9ARNRScratch()
	}
	return nil
}

func (e *VP9Encoder) applyVP9ARNRFilter(center *vp9LookaheadEntry) bool {
	maxFrames := min(e.opts.ARNRMaxFrames, maxARNRFrames)
	if maxFrames <= 1 || len(e.vp9ARNRScratch.Y) == 0 ||
		e.lookaheadCount == 0 {
		return false
	}
	// libvpx only runs vp9_temporal_filter for alt-ref frames outside
	// REALTIME mode. Realtime VBR may still schedule a one-pass ARF, but
	// the ARNR controls do not filter its source buffer.
	if vp9ResolveDeadlineMode(e.opts.Deadline) == vp9ModeRealtime {
		return false
	}
	distance := int(e.lookaheadCount) - 1
	// libvpx vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
	// drives the adaptive temporal-filter strength + symmetric window
	// placement off the GF/ARF group boost and the running
	// avg_frame_qindex. The libvpx-faithful gfu_boost comes from
	// `define_gf_group`'s call to `compute_arf_boost` (two-pass path) or
	// the one-pass DEFAULT_GF_BOOST seed (libvpx vp9_ratectrl.c:2082).
	// Both feeds are wired: NewVP9Encoder seeds DEFAULT_GF_BOOST when
	// LookaheadFrames>0, and refreshVP9GFGroupIfDue refreshes it from
	// encoder.DefineGFGroup at each GF boundary when two-pass stats are
	// available. Streams with gfuBoost=0 (for example zero-lag realtime
	// CBR) use the fixed-window selector, as do non-default ARNRType=1/2
	// directions that libvpx's adjust_arnr_filter does not model.
	var backward, forward, strength int
	useAdaptive := e.rc.gfuBoost > 0
	if useAdaptive {
		adj := vp9enc.AdjustARNRFilter(vp9enc.AdjustARNRFilterInput{
			LookaheadDepth:         int(e.lookaheadCount),
			Distance:               distance,
			GroupBoost:             int(e.rc.gfuBoost),
			ARNRMaxFrames:          e.opts.ARNRMaxFrames,
			ARNRStrengthBase:       e.opts.ARNRStrength,
			ARNRStrengthAdjustment: 0,
			Pass:                   1,
			CurrentVideoFrame:      e.frameIndex,
			AvgFrameQIndexInter:    int(e.rc.avgFrameQIndexInter),
			AvgFrameQIndexKey:      int(e.rc.avgFrameQIndexKey),
		})
		backward = adj.FramesBackward
		forward = adj.FramesForward
		strength = adj.ARNRStrength
	}
	// libvpx's adjust_arnr_filter assumes ARNRType=3 (centered). govpx's
	// ARNRType=1/2 (backward/forward-only) are non-default modes; honor
	// the caller's request even under the adaptive path by routing through
	// the fixed-window selector for those modes.
	if !useAdaptive || e.opts.ARNRType != 3 {
		b, f, ok := vp9enc.TemporalFilterWindow(distance,
			int(e.lookaheadCount), maxFrames, e.opts.ARNRType)
		if !ok || b+f == 0 {
			return false
		}
		backward = b
		forward = f
		strength = e.opts.ARNRStrength
	}
	if backward+forward == 0 {
		return false
	}
	framesToBlur := backward + forward + 1
	if framesToBlur <= 0 || framesToBlur > maxARNRFrames {
		return false
	}

	copyVP9LookaheadImage(&e.vp9ARNRScratch, &center.img, e.opts.Width,
		e.opts.Height)
	refs := e.vp9ARNRRefs[:framesToBlur:framesToBlur]
	startFrame := distance + forward
	for frame := range framesToBlur {
		entry, ok := e.peekVP9LookaheadAt(startFrame - frame)
		if !ok {
			return false
		}
		refs[framesToBlur-1-frame] = vp9enc.TemporalFilterFrameFromYCbCr(&entry.img)
	}
	dst := vp9enc.TemporalFilterFrameFromYCbCr(&e.vp9ARNRScratch)
	vp9enc.IterateTemporalFilter(&dst, refs, backward, strength)
	return true
}
