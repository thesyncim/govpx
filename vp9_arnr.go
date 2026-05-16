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
