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
	if vp9ARNRDebugBuild && vp9ARNRDebugEnabled() {
		vp9ARNRDebugf(
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
		if vp9ARNRDebugBuild && vp9ARNRDebugEnabled() {
			vp9ARNRDebugf(
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
	// encoder.DefineGFGroup at each GF boundary when two-pass stats are
	// available). The legacy non-adaptive branch is retained for
	// streams that explicitly request gfuBoost=0 (e.g. zero-lag
	// realtime CBR) and for the non-default ARNRType=1/2 directions
	// which libvpx's adjust_arnr_filter doesn't model.
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
	// the caller's request even under the adaptive path by routing
	// through the legacy window selector for those modes.
	if !useAdaptive || e.opts.ARNRType != 3 {
		b, f, ok := vp9ARNRFilterWindow(distance,
			int(e.lookaheadCount), maxFrames, e.opts.ARNRType)
		if !ok || b+f == 0 {
			if vp9ARNRDebugBuild && vp9ARNRDebugEnabled() {
				vp9ARNRDebugf(
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
		if vp9ARNRDebugBuild && vp9ARNRDebugEnabled() {
			vp9ARNRDebugf(
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
	if vp9ARNRDebugBuild && vp9ARNRDebugEnabled() {
		vp9ARNRDebugf(
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
	if !doChroma {
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
				processARNRMacroblock(&dst, refs, centerIdx, mbRow,
					mbCol, mbRows, mbCols, mbX, mbY, strength, false,
					accumulator[:], count[:])
			}
		}
		return
	}

	blockCols := (dst.width + 31) >> 5
	blockRows := (dst.height + 31) >> 5
	if blockCols|blockRows == 0 {
		return
	}

	var accumulator [1536]uint32
	var count [1536]uint32
	for blockRow := range blockRows {
		blockY := blockRow << 5
		for blockCol := range blockCols {
			blockX := blockCol << 5
			processVP9ARNRBlock32(&dst, refs, centerIdx, blockRow,
				blockCol, blockRows, blockCols, blockX, blockY, strength,
				accumulator[:], count[:])
		}
	}
}

func processVP9ARNRBlock32(dst *arnrFrameView, refs []arnrFrameView, centerIdx int, blockRow int, blockCol int, blockRows int, blockCols int, blockX, blockY, strength int, accumulator []uint32, count []uint32) {
	accumulator = accumulator[:1536:1536]
	count = count[:1536:1536]
	for i := range accumulator {
		accumulator[i] = 0
		count[i] = 0
	}

	var srcY [1024]byte
	gatherBlock(srcY[:], 32, dst.y, dst.yStride, blockX, blockY,
		dst.width, dst.height, 32)
	blockUVX := blockX >> 1
	blockUVY := blockY >> 1
	uvW := (dst.width + 1) >> 1
	uvH := (dst.height + 1) >> 1
	var srcU, srcV [256]byte
	gatherBlock(srcU[:], 16, dst.u, dst.uStride, blockUVX, blockUVY, uvW, uvH, 16)
	gatherBlock(srcV[:], 16, dst.v, dst.vStride, blockUVX, blockUVY, uvW, uvH, 16)
	bounds := vp9ARNRBlock32MVBounds(blockRow, blockCol, blockRows, blockCols)

	for fi, ref := range refs {
		var blkFW [4]int
		var blkMVs [4]vp9ARNRMV
		use32 := false
		refMV := vp9ARNRMV{}
		if fi == centerIdx {
			blkFW = [4]int{2, 2, 2, 2}
			use32 = true
		} else {
			err, blkErr, mv32, mvs16 := vp9ARNRFindMatchingBlock32(
				srcY[:], ref, blockX, blockY, bounds)
			refMV = mv32
			blkMVs = mvs16
			err16 := blkErr[0] + blkErr[1] + blkErr[2] + blkErr[3]
			minErr, maxErr := blkErr[0], blkErr[0]
			for k := 1; k < len(blkErr); k++ {
				minErr = min(minErr, blkErr[k])
				maxErr = max(maxErr, blkErr[k])
			}
			if ((err*15 < (err16 << 4)) && maxErr-minErr < 10000) ||
				((err*14 < (err16 << 4)) && maxErr-minErr < 5000) {
				use32 = true
				fw := vp9ARNRFilterWeight(err, arnrThreshLow<<2,
					arnrThreshHigh<<2)
				blkFW = [4]int{fw, fw, fw, fw}
			} else {
				for k := range blkFW {
					blkFW[k] = vp9ARNRFilterWeight(blkErr[k],
						arnrThreshLow, arnrThreshHigh)
				}
			}
			capWeight := 2
			switch vp9ARNRAbsInt(fi - centerIdx) {
			case 2, 3:
				capWeight = 1
			}
			for k := range blkFW {
				blkFW[k] = min(blkFW[k], capWeight)
			}
		}
		if blkFW[0]|blkFW[1]|blkFW[2]|blkFW[3] == 0 {
			continue
		}

		var predY [1024]byte
		var predU, predV [256]byte
		vp9ARNRBuildPredictor32(predY[:], predU[:], predV[:], ref,
			blockX, blockY, blockUVX, blockUVY, use32, refMV, blkMVs)
		applyVP9TemporalFilter32(srcY[:], predY[:], srcU[:], predU[:],
			srcV[:], predV[:], min(strength+3, 6), blkFW, use32,
			accumulator[:1024], count[:1024],
			accumulator[1024:1280], count[1024:1280],
			accumulator[1280:1536], count[1280:1536])
	}

	writeARNRBlock(dst.y, dst.yStride, blockX, blockY, dst.width, dst.height, 32, accumulator[:1024], count[:1024])
	writeARNRBlock(dst.u, dst.uStride, blockUVX, blockUVY, uvW, uvH, 16, accumulator[1024:1280], count[1024:1280])
	writeARNRBlock(dst.v, dst.vStride, blockUVX, blockUVY, uvW, uvH, 16, accumulator[1280:1536], count[1280:1536])
}

type vp9ARNRMV struct {
	col int
	row int
}

type vp9ARNRMVBounds struct {
	colMin int
	colMax int
	rowMin int
	rowMax int
}

func vp9ARNRBlock32MVBounds(blockRow, blockCol, blockRows, blockCols int) vp9ARNRMVBounds {
	const border = 17 - 2*6
	return vp9ARNRMVBounds{
		colMin: -((blockCol << 5) + border),
		colMax: ((blockCols - 1 - blockCol) << 5) + border,
		rowMin: -((blockRow << 5) + border),
		rowMax: ((blockRows - 1 - blockRow) << 5) + border,
	}
}

func vp9ARNRFindMatchingBlock32(srcY []byte, ref arnrFrameView, blockX, blockY int, bounds vp9ARNRMVBounds) (int, [4]int, vp9ARNRMV, [4]vp9ARNRMV) {
	_, fullX, fullY := vp9ARNRFindMatchingBlock(srcY, 32, ref,
		blockX, blockY, 32, bounds, 0, 0)
	err, mvX, mvY := vp9ARNRSubpelRefineBlock(srcY, 32, ref,
		blockX, blockY, 32, bounds, fullX, fullY)
	mv32 := vp9ARNRMV{col: mvX, row: mvY}

	var blkErr [4]int
	var blkMVs [4]vp9ARNRMV
	k := 0
	for yOff := 0; yOff < 32; yOff += 16 {
		for xOff := 0; xOff < 32; xOff += 16 {
			var sub [16 * 16]byte
			for y := range 16 {
				copy(sub[y*16:y*16+16],
					srcY[(yOff+y)*32+xOff:(yOff+y)*32+xOff+16])
			}
			_, subFullX, subFullY := vp9ARNRFindMatchingBlock(sub[:],
				16, ref, blockX+xOff, blockY+yOff, 16, bounds,
				mv32.col>>3, mv32.row>>3)
			subErr, subMVX, subMVY := vp9ARNRSubpelRefineBlock(sub[:],
				16, ref, blockX+xOff, blockY+yOff, 16, bounds,
				subFullX, subFullY)
			blkErr[k] = subErr
			blkMVs[k] = vp9ARNRMV{col: subMVX, row: subMVY}
			k++
		}
	}
	return err, blkErr, mv32, blkMVs
}

func vp9ARNRFilterWeight(err, low, high int) int {
	switch {
	case err < low:
		return 2
	case err < high:
		return 1
	default:
		return 0
	}
}

func vp9ARNRFindMatchingBlock(src []byte, srcStride int, ref arnrFrameView, x, y, size int, bounds vp9ARNRMVBounds, seedX, seedY int) (int, int, int) {
	br := arnrClamp(seedY, bounds.rowMin, bounds.rowMax)
	bc := arnrClamp(seedX, bounds.colMin, bounds.colMax)
	hex := [6][2]int{
		{-1, -2}, {1, -2}, {2, 0}, {1, 2}, {-1, 2}, {-2, 0},
	}
	nextChkpts := [6][3][2]int{
		{{-2, 0}, {-1, -2}, {1, -2}},
		{{-1, -2}, {1, -2}, {2, 0}},
		{{1, -2}, {2, 0}, {1, 2}},
		{{2, 0}, {1, 2}, {-1, 2}},
		{{1, 2}, {-1, 2}, {-2, 0}},
		{{-1, 2}, {-2, 0}, {-1, -2}},
	}
	neighbors := [4][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}}
	bestSAD := vp9ARNRSADAt(src, srcStride, ref, x, y, size, bc, br)
	bestSite := -1
	for i, step := range hex {
		row := br + step[0]
		col := bc + step[1]
		if !arnrInBounds(col, row, bounds.colMin, bounds.colMax,
			bounds.rowMin, bounds.rowMax) {
			continue
		}
		sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
		if sad < bestSAD {
			bestSAD = sad
			bestSite = i
		}
	}
	if bestSite >= 0 {
		br += hex[bestSite][0]
		bc += hex[bestSite][1]
		k := bestSite
		for j := 1; j < arnrHexRange; j++ {
			bestSite = -1
			for i, step := range nextChkpts[k] {
				row := br + step[0]
				col := bc + step[1]
				if !arnrInBounds(col, row, bounds.colMin, bounds.colMax,
					bounds.rowMin, bounds.rowMax) {
					continue
				}
				sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
				if sad < bestSAD {
					bestSAD = sad
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br += nextChkpts[k][bestSite][0]
			bc += nextChkpts[k][bestSite][1]
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}
	for range arnrDiaRange {
		bestSite = -1
		for i, step := range neighbors {
			row := br + step[0]
			col := bc + step[1]
			if !arnrInBounds(col, row, bounds.colMin, bounds.colMax,
				bounds.rowMin, bounds.rowMax) {
				continue
			}
			sad := vp9ARNRSADAt(src, srcStride, ref, x, y, size, col, row)
			if sad < bestSAD {
				bestSAD = sad
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br += neighbors[bestSite][0]
		bc += neighbors[bestSite][1]
	}
	return bestSAD, bc, br
}

func vp9ARNRSubpelRefineBlock(src []byte, srcStride int, ref arnrFrameView, x, y, size int, bounds vp9ARNRMVBounds, fullX, fullY int) (int, int, int) {
	minCol := bounds.colMin << 3
	maxCol := bounds.colMax << 3
	minRow := bounds.rowMin << 3
	maxRow := bounds.rowMax << 3
	bestRow := fullY << 3
	bestCol := fullX << 3
	bestSAD := vp9ARNRSADAtSubpel(src, srcStride, ref, x, y, size, bestCol, bestRow)
	steps := [3]int{4, 2, 1}
	for _, step := range steps {
		for range 4 {
			startRow := bestRow
			startCol := bestCol
			leftSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow, startCol-step, minRow, maxRow, minCol, maxCol)
			rightSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow, startCol+step, minRow, maxRow, minCol, maxCol)
			upSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow-step, startCol, minRow, maxRow, minCol, maxCol)
			downSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow+step, startCol, minRow, maxRow, minCol, maxCol)
			if leftSAD < bestSAD {
				bestSAD = leftSAD
				bestRow = startRow
				bestCol = startCol - step
			}
			if rightSAD < bestSAD {
				bestSAD = rightSAD
				bestRow = startRow
				bestCol = startCol + step
			}
			if upSAD < bestSAD {
				bestSAD = upSAD
				bestRow = startRow - step
				bestCol = startCol
			}
			if downSAD < bestSAD {
				bestSAD = downSAD
				bestRow = startRow + step
				bestCol = startCol
			}
			dr := -step
			dc := -step
			if downSAD < upSAD {
				dr = step
			}
			if rightSAD < leftSAD {
				dc = step
			}
			diagSAD := vp9ARNRSubpelProbe(src, srcStride, ref, x, y, size, startRow+dr, startCol+dc, minRow, maxRow, minCol, maxCol)
			if diagSAD < bestSAD {
				bestSAD = diagSAD
				bestRow = startRow + dr
				bestCol = startCol + dc
			}
			if bestRow == startRow && bestCol == startCol {
				break
			}
		}
	}
	return bestSAD, bestCol, bestRow
}

func vp9ARNRSubpelProbe(src []byte, srcStride int, ref arnrFrameView, x, y, size int, row, col, minRow, maxRow, minCol, maxCol int) int {
	if row < minRow || row > maxRow || col < minCol || col > maxCol {
		return 1<<30 - 1
	}
	return vp9ARNRSADAtSubpel(src, srcStride, ref, x, y, size, col, row)
}

func vp9ARNRSADAt(src []byte, srcStride int, ref arnrFrameView, x, y, size, mvX, mvY int) int {
	var pred [1024]byte
	gatherBlock(pred[:size*size], size, ref.y, ref.yStride, x+mvX, y+mvY,
		ref.width, ref.height, size)
	return vp9ARNRSAD(src, srcStride, pred[:], size, size)
}

func vp9ARNRSADAtSubpel(src []byte, srcStride int, ref arnrFrameView, x, y, size, col, row int) int {
	if (row|col)&7 == 0 {
		return vp9ARNRSADAt(src, srcStride, ref, x, y, size, col>>3, row>>3)
	}
	var pred [1024]byte
	vp9ARNRPredictLuma(pred[:size*size], size, ref, x, y, col, row, size, size)
	return vp9ARNRSAD(src, srcStride, pred[:], size, size)
}

func vp9ARNRSAD(src []byte, srcStride int, pred []byte, predStride int, size int) int {
	sad := 0
	for y := range size {
		srcRow := src[y*srcStride:]
		predRow := pred[y*predStride:]
		for x := range size {
			d := int(srcRow[x]) - int(predRow[x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
	}
	return sad
}

func vp9ARNRBuildPredictor32(predY, predU, predV []byte, ref arnrFrameView, blockX, blockY, blockUVX, blockUVY int, use32 bool, refMV vp9ARNRMV, blkMVs [4]vp9ARNRMV) {
	if use32 {
		vp9ARNRPredictLuma(predY, 32, ref, blockX, blockY, refMV.col,
			refMV.row, 32, 32)
		vp9ARNRPredictChroma(predU, 16, ref.u, ref.uStride,
			(ref.width+1)>>1, (ref.height+1)>>1, blockUVX, blockUVY,
			refMV.col, refMV.row, 16, 16)
		vp9ARNRPredictChroma(predV, 16, ref.v, ref.vStride,
			(ref.width+1)>>1, (ref.height+1)>>1, blockUVX, blockUVY,
			refMV.col, refMV.row, 16, 16)
		return
	}
	k := 0
	for yOff := 0; yOff < 32; yOff += 16 {
		for xOff := 0; xOff < 32; xOff += 16 {
			mv := blkMVs[k]
			var subY [256]byte
			vp9ARNRPredictLuma(subY[:], 16, ref, blockX+xOff,
				blockY+yOff, mv.col, mv.row, 16, 16)
			for y := range 16 {
				copy(predY[(yOff+y)*32+xOff:(yOff+y)*32+xOff+16],
					subY[y*16:y*16+16])
			}
			uvXOff := xOff >> 1
			uvYOff := yOff >> 1
			vp9ARNRPredictChroma(predU[uvYOff*16+uvXOff:], 16,
				ref.u, ref.uStride, (ref.width+1)>>1, (ref.height+1)>>1,
				blockUVX+uvXOff, blockUVY+uvYOff, mv.col, mv.row, 8, 8)
			vp9ARNRPredictChroma(predV[uvYOff*16+uvXOff:], 16,
				ref.v, ref.vStride, (ref.width+1)>>1, (ref.height+1)>>1,
				blockUVX+uvXOff, blockUVY+uvYOff, mv.col, mv.row, 8, 8)
			k++
		}
	}
}

func vp9ARNRPredictLuma(dst []byte, dstStride int, ref arnrFrameView, x, y, mvColQ3, mvRowQ3, w, h int) {
	mvColQ4 := mvColQ3 << 1
	mvRowQ4 := mvRowQ3 << 1
	vp9ARNRPredict12(dst, dstStride, ref.y, ref.yStride, ref.width,
		ref.height, x, y, mvColQ4, mvRowQ4, w, h)
}

func vp9ARNRPredictChroma(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, x, y, mvColQ3, mvRowQ3, w, h int) {
	vp9ARNRPredict12(dst, dstStride, plane, planeStride, planeW, planeH,
		x, y, mvColQ3, mvRowQ3, w, h)
}

func vp9ARNRPredict12(dst []byte, dstStride int, plane []byte, planeStride int, planeW, planeH int, x, y, mvColQ4, mvRowQ4, w, h int) {
	intCol := mvColQ4 >> 4
	intRow := mvRowQ4 >> 4
	fracCol := mvColQ4 & 15
	fracRow := mvRowQ4 & 15
	if (fracCol | fracRow) == 0 {
		gatherBlock(dst, dstStride, plane, planeStride, x+intCol, y+intRow,
			planeW, planeH, w)
		return
	}
	const extend = 5
	gatherW := w + 11
	gatherH := h + 11
	var scratchBuf [43 * 43]byte
	scratch := scratchBuf[:gatherW*gatherH]
	gatherBlock(scratch, gatherW, plane, planeStride, x+intCol-extend,
		y+intRow-extend, planeW, planeH, gatherW)
	var tempBuf [32 * 43]byte
	temp := tempBuf[:w*gatherH]
	xFilter := &vp9TemporalSubpelFilters12[fracCol]
	for yy := range gatherH {
		for xx := range w {
			sum := 0
			base := yy*gatherW + xx
			for k := range 12 {
				sum += int(scratch[base+k]) * int(xFilter[k])
			}
			temp[yy*w+xx] = vp9ClipPixel(vp9RoundPowerOfTwo(sum, 7))
		}
	}
	yFilter := &vp9TemporalSubpelFilters12[fracRow]
	for xx := range w {
		for yy := range h {
			sum := 0
			base := yy*w + xx
			for k := range 12 {
				sum += int(temp[base+k*w]) * int(yFilter[k])
			}
			dst[yy*dstStride+xx] = vp9ClipPixel(vp9RoundPowerOfTwo(sum, 7))
		}
	}
}

var vp9TemporalSubpelFilters12 = [16][12]int16{
	{0, 0, 0, 0, 0, 128, 0, 0, 0, 0, 0, 0},
	{0, 1, -2, 3, -7, 127, 8, -4, 2, -1, 1, 0},
	{-1, 2, -3, 6, -13, 124, 18, -8, 4, -2, 2, -1},
	{-1, 3, -4, 8, -18, 120, 28, -12, 7, -4, 2, -1},
	{-1, 3, -6, 10, -21, 115, 38, -15, 8, -5, 3, -1},
	{-2, 4, -6, 12, -24, 108, 49, -18, 10, -6, 3, -2},
	{-2, 4, -7, 13, -25, 100, 60, -21, 11, -7, 4, -2},
	{-2, 4, -7, 13, -26, 91, 71, -24, 13, -7, 4, -2},
	{-2, 4, -7, 13, -25, 81, 81, -25, 13, -7, 4, -2},
	{-2, 4, -7, 13, -24, 71, 91, -26, 13, -7, 4, -2},
	{-2, 4, -7, 11, -21, 60, 100, -25, 13, -7, 4, -2},
	{-2, 3, -6, 10, -18, 49, 108, -24, 12, -6, 4, -2},
	{-1, 3, -5, 8, -15, 38, 115, -21, 10, -6, 3, -1},
	{-1, 2, -4, 7, -12, 28, 120, -18, 8, -4, 3, -1},
	{-1, 2, -2, 4, -8, 18, 124, -13, 6, -3, 2, -1},
	{0, 1, -1, 2, -4, 8, 127, -7, 3, -2, 1, 0},
}

var vp9TemporalFilterIndexMult = [...]uint32{
	0, 0, 0, 0, 49152, 39322, 32768, 28087, 24576, 21846,
	19661, 17874, 0, 15124,
}

func applyVP9TemporalFilter32(srcY, predY, srcU, predU, srcV, predV []byte,
	strength int, blkFW [4]int, use32 bool,
	yAccumulator, yCount, uAccumulator, uCount, vAccumulator, vCount []uint32,
) {
	if blkFW[0]|blkFW[1]|blkFW[2]|blkFW[3] == 0 {
		return
	}
	if strength < 0 {
		strength = 0
	}
	if strength > 6 {
		strength = 6
	}
	rounding := (1 << uint(strength)) >> 1
	var yDiff [1024]int
	var uDiff, vDiff [256]int
	for y := range 32 {
		for x := range 32 {
			diff := int(srcY[y*32+x]) - int(predY[y*32+x])
			yDiff[y*32+x] = diff * diff
		}
	}
	for y := range 16 {
		for x := range 16 {
			u := int(srcU[y*16+x]) - int(predU[y*16+x])
			v := int(srcV[y*16+x]) - int(predV[y*16+x])
			uDiff[y*16+x] = u * u
			vDiff[y*16+x] = v * v
		}
	}
	for y := range 32 {
		for x := range 32 {
			sum := 0
			used := 0
			for dy := -1; dy <= 1; dy++ {
				yy := y + dy
				if yy < 0 || yy >= 32 {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					xx := x + dx
					if xx < 0 || xx >= 32 {
						continue
					}
					sum += yDiff[yy*32+xx]
					used++
				}
			}
			uvIdx := (y>>1)*16 + (x >> 1)
			sum += uDiff[uvIdx] + vDiff[uvIdx]
			used += 2
			filterWeight := vp9ARNRBlockFilterWeight(y, x, 32, 32,
				blkFW, use32)
			modifier := vp9TemporalFilterModIndex(sum, used, rounding,
				strength, filterWeight)
			k := y*32 + x
			yCount[k] += uint32(modifier)
			yAccumulator[k] += uint32(modifier) * uint32(predY[k])
		}
	}
	for uvY := range 16 {
		for uvX := range 16 {
			uSum, vSum := 0, 0
			used := 0
			for dy := -1; dy <= 1; dy++ {
				yy := uvY + dy
				if yy < 0 || yy >= 16 {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					xx := uvX + dx
					if xx < 0 || xx >= 16 {
						continue
					}
					idx := yy*16 + xx
					uSum += uDiff[idx]
					vSum += vDiff[idx]
					used++
				}
			}
			ySum := 0
			for yy := uvY << 1; yy < (uvY<<1)+2; yy++ {
				for xx := uvX << 1; xx < (uvX<<1)+2; xx++ {
					ySum += yDiff[yy*32+xx]
					used++
				}
			}
			uSum += ySum
			vSum += ySum
			filterWeight := vp9ARNRBlockFilterWeight(uvY, uvX, 16, 16,
				blkFW, use32)
			uMod := vp9TemporalFilterModIndex(uSum, used, rounding,
				strength, filterWeight)
			vMod := vp9TemporalFilterModIndex(vSum, used, rounding,
				strength, filterWeight)
			uv := uvY*16 + uvX
			uCount[uv] += uint32(uMod)
			uAccumulator[uv] += uint32(uMod) * uint32(predU[uv])
			vCount[uv] += uint32(vMod)
			vAccumulator[uv] += uint32(vMod) * uint32(predV[uv])
		}
	}
}

func vp9ARNRBlockFilterWeight(y, x, h, w int, blkFW [4]int, use32 bool) int {
	if use32 {
		return blkFW[0]
	}
	if y < h/2 {
		if x < w/2 {
			return blkFW[0]
		}
		return blkFW[1]
	}
	if x < w/2 {
		return blkFW[2]
	}
	return blkFW[3]
}

func vp9ARNRAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func vp9RoundPowerOfTwo(v int, n uint) int {
	return (v + (1 << (n - 1))) >> n
}

func vp9ClipPixel(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func vp9TemporalFilterModIndex(sumDist, index, rounding, strength, filterWeight int) int {
	if index < 0 || index >= len(vp9TemporalFilterIndexMult) ||
		vp9TemporalFilterIndexMult[index] == 0 {
		return 0
	}
	if sumDist < 0 {
		sumDist = 0
	}
	if sumDist > 0xffff {
		sumDist = 0xffff
	}
	modifier := (uint32(sumDist) * vp9TemporalFilterIndexMult[index]) >> 16
	mod := min((int(modifier)+rounding)>>uint(strength), 16)
	return (16 - mod) * filterWeight
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
