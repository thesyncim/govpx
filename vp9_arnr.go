package govpx

import (
	"fmt"
	"image"
	"os"
	"sync"
)

var (
	vp9ARNRDebugOnce sync.Once
	vp9ARNRDebugFlag bool
)

// vp9ARNRDebugEnabled gates a single-shot log line per encoder instance
// describing the ARNR boundary state (max frames, type, picked
// backward/forward window, whether the filter actually ran). It is
// guarded by GOVPX_VP9_ARNR_DEBUG=1 so production builds pay nothing
// for the assertion. The log helps catch regressions where ARNR is
// configured but silently skipped (e.g. the centered-clamp-to-zero
// bug that surfaced in the BD-rate gate).
func vp9ARNRDebugEnabled() bool {
	vp9ARNRDebugOnce.Do(func() {
		vp9ARNRDebugFlag = os.Getenv("GOVPX_VP9_ARNR_DEBUG") == "1"
	})
	return vp9ARNRDebugFlag
}

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
	// distance=-1 picks a forward-only window.  govpx's
	// vp9ARNRFilterWindow honours ARNRType=2 (forward) the same way.
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
		adj := VP9AdjustARNRFilter(VP9AdjustARNRFilterInput{
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
	// forward lookahead frames in source order.  iterateVP9TemporalFilter
	// reads centerIdx as 0 because distance == -1 means no backward frames.
	refs[0] = arnrViewFromYCbCr(img)
	for frame := 1; frame < framesToBlur; frame++ {
		entry, ok := e.peekVP9Lookahead(frame - 1)
		if !ok {
			return img
		}
		refs[frame] = arnrViewFromYCbCr(&entry.img)
	}
	e.iterateVP9TemporalFilter(strength, refs, 0, true)
	if vp9ARNRDebugEnabled() {
		fmt.Fprintf(os.Stderr,
			"govpx vp9 kf-tf: filtered (look=%d frames=%d strength=%d max=%d)\n",
			e.lookaheadCount, framesToBlur, strength, maxFrames)
	}
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
		if vp9ARNRDebugEnabled() {
			fmt.Fprintf(os.Stderr,
				"govpx vp9 arnr: skip (maxFrames=%d scratch=%d look=%d)\n",
				maxFrames, len(e.vp9ARNRScratch.Y), e.lookaheadCount)
		}
		return false
	}
	distance := int(e.lookaheadCount) - 1
	// libvpx vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
	// drives the adaptive temporal-filter strength + symmetric window
	// placement off the GF/ARF group boost and the running
	// avg_frame_qindex. The libvpx-faithful gfu_boost comes from
	// `define_gf_group`'s call to `compute_arf_boost` (two-pass path) or
	// the one-pass DEFAULT_GF_BOOST seed (libvpx vp9_ratectrl.c:2082).
	// Both feeds are now wired (NewVP9Encoder seeds DEFAULT_GF_BOOST
	// when LookaheadFrames>0; refreshVP9GFGroupIfDue refreshes it from
	// vp9DefineGFGroup at each GF boundary when two-pass stats are
	// available). The legacy non-adaptive branch is retained for
	// streams that explicitly request gfuBoost=0 (e.g. zero-lag
	// realtime CBR) and for the non-default ARNRType=1/2 directions
	// which libvpx's adjust_arnr_filter doesn't model.
	var backward, forward, strength int
	useAdaptive := e.rc.gfuBoost > 0
	if useAdaptive {
		adj := VP9AdjustARNRFilter(VP9AdjustARNRFilterInput{
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
	// the caller's request even under the adaptive path by routing
	// through the legacy window selector for those modes.
	if !useAdaptive || e.opts.ARNRType != 3 {
		b, f, ok := vp9ARNRFilterWindow(distance,
			int(e.lookaheadCount), maxFrames, e.opts.ARNRType)
		if !ok || b+f == 0 {
			if vp9ARNRDebugEnabled() {
				fmt.Fprintf(os.Stderr,
					"govpx vp9 arnr: window empty (distance=%d look=%d max=%d type=%d back=%d fwd=%d ok=%v)\n",
					distance, e.lookaheadCount, maxFrames,
					e.opts.ARNRType, b, f, ok)
			}
			return false
		}
		backward = b
		forward = f
		strength = e.opts.ARNRStrength
	}
	if backward+forward == 0 {
		if vp9ARNRDebugEnabled() {
			fmt.Fprintf(os.Stderr,
				"govpx vp9 arnr: adaptive window empty (distance=%d look=%d max=%d boost=%d type=%d)\n",
				distance, e.lookaheadCount, maxFrames,
				e.rc.gfuBoost, e.opts.ARNRType)
		}
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
		entry, ok := e.peekVP9Lookahead(startFrame - frame)
		if !ok {
			return false
		}
		refs[framesToBlur-1-frame] = arnrViewFromYCbCr(&entry.img)
	}
	e.iterateVP9TemporalFilter(strength, refs, backward, true)
	if vp9ARNRDebugEnabled() {
		fmt.Fprintf(os.Stderr,
			"govpx vp9 arnr: filtered (distance=%d look=%d back=%d fwd=%d strength=%d adapted=%v(base=%d) type=%d boost=%d)\n",
			distance, e.lookaheadCount, backward, forward,
			strength, useAdaptive, e.opts.ARNRStrength,
			e.opts.ARNRType, e.rc.gfuBoost)
	}
	return true
}

func vp9ARNRFilterWindow(distance int, lookaheadCount int, maxFrames int, filterType int) (int, int, bool) {
	if distance < 0 || lookaheadCount <= 0 || maxFrames <= 1 {
		return 0, 0, false
	}
	numFramesBackward := distance
	numFramesForward := lookaheadCount - (numFramesBackward + 1)
	if numFramesForward < 0 {
		return 0, 0, false
	}
	framesBackward := 0
	framesForward := 0
	switch filterType {
	case 1:
		framesBackward = numFramesBackward
		if framesBackward >= maxFrames {
			framesBackward = maxFrames - 1
		}
	case 2:
		framesForward = numFramesForward
		if framesForward >= maxFrames {
			framesForward = maxFrames - 1
		}
	case 3:
		// libvpx VP9 places the alt-ref at the end of the GF
		// group, so when the lookahead-driven driver picks the
		// newest queued frame as the alt-ref source we have no
		// forward refs available. The previous symmetric clamp
		// (forward = backward = min(forward,backward)) collapsed
		// both sides to 0 in that case, which silently disabled
		// the temporal filter pass. Match libvpx's
		// vp9_temporal_filter.c behavior: when one side is short,
		// use what is available on the other side capped to
		// maxFrames-1 so the filter still runs.
		framesForward = numFramesForward
		framesBackward = numFramesBackward
		if framesForward == 0 {
			if framesBackward > maxFrames-1 {
				framesBackward = maxFrames - 1
			}
			break
		}
		if framesBackward == 0 {
			if framesForward > maxFrames-1 {
				framesForward = maxFrames - 1
			}
			break
		}
		if framesForward > framesBackward {
			framesForward = framesBackward
		}
		if framesBackward > framesForward {
			framesBackward = framesForward
		}
		if framesForward > (maxFrames-1)/2 {
			framesForward = (maxFrames - 1) / 2
		}
		if framesBackward > maxFrames/2 {
			framesBackward = maxFrames / 2
		}
	default:
		return 0, 0, false
	}
	return framesBackward, framesForward, true
}

func (e *VP9Encoder) peekVP9Lookahead(offset int) (*vp9LookaheadEntry, bool) {
	if !e.vp9LookaheadEnabled() || offset < 0 || offset >= int(e.lookaheadCount) {
		return nil, false
	}
	idx := int(e.lookaheadRead) + offset
	if idx >= len(e.lookahead) {
		idx -= len(e.lookahead)
	}
	return &e.lookahead[idx], true
}

func (e *VP9Encoder) iterateVP9TemporalFilter(strength int, refs []arnrFrameView, centerIdx int, doChroma bool) {
	if uint(centerIdx) >= uint(len(refs)) {
		return
	}
	dst := arnrViewFromYCbCr(&e.vp9ARNRScratch)
	mbCols := (dst.width + 15) >> 4
	mbRows := (dst.height + 15) >> 4
	if mbCols|mbRows == 0 {
		return
	}

	var accumulator [384]uint32
	var count [384]uint32
	for mbRow := range mbRows {
		mbY := mbRow << 4
		for mbCol := range mbCols {
			mbX := mbCol << 4
			processARNRMacroblock(&dst, refs, centerIdx, mbRow, mbCol,
				mbRows, mbCols, mbX, mbY, strength, doChroma,
				accumulator[:], count[:])
		}
	}
}

func arnrViewFromYCbCr(img *image.YCbCr) arnrFrameView {
	return arnrFrameView{
		width:   img.Rect.Dx(),
		height:  img.Rect.Dy(),
		y:       img.Y,
		u:       img.Cb,
		v:       img.Cr,
		yStride: img.YStride,
		uStride: img.CStride,
		vStride: img.CStride,
	}
}
