package decoder

// VP9 per-frame loopfilter init. Ported from libvpx v1.16.0
// vp9/common/vp9_loopfilter.c — vp9_loop_filter_init,
// vp9_loop_filter_frame_init, and update_sharpness.
//
// LoopFilterInfoN holds the two derived tables the loopfilter
// kernels consult per block:
//   - Lfthr[level]: (mblim, lim, hev_thr) thresholds for each of the
//     64 possible filter levels.
//   - Lvl[seg][ref][mode]: the actual filter level to use for a
//     given (segment, ref-frame, mode) tuple. The block's lookup
//     reduces to a single byte fetch.

const (
	// MaxLoopFilter mirrors MAX_LOOP_FILTER — the maximum filter
	// level libvpx emits + the upper bound of the per-level
	// threshold table.
	MaxLoopFilter = 63
)

// LoopFilterThresh mirrors libvpx's loop_filter_thresh — three
// per-level limits. Stored as scalars; libvpx broadcasts each to a
// SIMD_WIDTH vector for the SIMD kernels but the value is the same
// in every lane.
type LoopFilterThresh struct {
	Mblim  uint8
	Lim    uint8
	HevThr uint8
}

// LoopFilterInfoN mirrors libvpx's loop_filter_info_n.
type LoopFilterInfoN struct {
	Lfthr            [MaxLoopFilter + 1]LoopFilterThresh
	Lvl              [MaxSegments][MaxRefFrames][MaxModeLfDeltas]uint8
	LastSharpnessLvl int8 // -1 sentinel forces update_sharpness on the first init.
}

// NewLoopFilterInfoN returns a fresh LoopFilterInfoN with the
// last-sharpness sentinel set so the first call to
// LoopFilterFrameInit always rebuilds the lfthr table.
func NewLoopFilterInfoN() LoopFilterInfoN {
	return LoopFilterInfoN{LastSharpnessLvl: -1}
}

// LoopFilterInit mirrors libvpx's vp9_loop_filter_init. Seeds the
// per-level lfthr table (limit, mblim) from the sharpness and the
// hev_thr (= lvl >> 4) ladder. Should be called once when the
// LoopFilterInfoN buffer is allocated; subsequent calls to
// LoopFilterFrameInit refresh the limits if sharpness changed.
func LoopFilterInit(lfi *LoopFilterInfoN, sharpness int) {
	updateSharpness(lfi, sharpness)
	lfi.LastSharpnessLvl = int8(sharpness)
	for lvl := 0; lvl <= MaxLoopFilter; lvl++ {
		lfi.Lfthr[lvl].HevThr = uint8(lvl >> 4)
	}
}

// LoopFilterFrameInit mirrors vp9_loop_filter_frame_init. Builds
// the per-(seg, ref, mode) filter-level table from the frame-level
// default, the per-segment SEG_LVL_ALT_LF override, and the
// loop-filter mode/ref deltas. Also refreshes lfthr limits when the
// sharpness setting changes.
//
// `defaultFiltLvl` is libvpx's frame_filter_level (filter_level when
// no segment delta wins, clamped to [0, MAX_LOOP_FILTER]).
func LoopFilterFrameInit(lfi *LoopFilterInfoN, lf *LoopfilterParams,
	seg *SegmentationParams, defaultFiltLvl int,
) {
	if lfi.LastSharpnessLvl != int8(lf.SharpnessLevel) {
		updateSharpness(lfi, int(lf.SharpnessLevel))
		lfi.LastSharpnessLvl = int8(lf.SharpnessLevel)
	}
	scale := 1 << uint(defaultFiltLvl>>5)

	for segID := 0; segID < MaxSegments; segID++ {
		lvlSeg := defaultFiltLvl
		if SegFeatureActive(seg, segID, SegLvlAltLf) {
			data := int(GetSegData(seg, segID, SegLvlAltLf))
			if seg.AbsDelta {
				lvlSeg = clampLfLvl(data)
			} else {
				lvlSeg = clampLfLvl(defaultFiltLvl + data)
			}
		}

		if !lf.ModeRefDeltaEnabled {
			for ref := 0; ref < MaxRefFrames; ref++ {
				for mode := 0; mode < MaxModeLfDeltas; mode++ {
					lfi.Lvl[segID][ref][mode] = uint8(lvlSeg)
				}
			}
			continue
		}

		intraLvl := lvlSeg + int(lf.RefDeltas[IntraFrame])*scale
		lfi.Lvl[segID][IntraFrame][0] = uint8(clampLfLvl(intraLvl))
		// libvpx leaves [INTRA_FRAME][1] untouched; we follow suit.

		for ref := LastFrame; ref < MaxRefFrames; ref++ {
			for mode := 0; mode < MaxModeLfDeltas; mode++ {
				interLvl := lvlSeg +
					int(lf.RefDeltas[ref])*scale +
					int(lf.ModeDeltas[mode])*scale
				lfi.Lvl[segID][ref][mode] = uint8(clampLfLvl(interLvl))
			}
		}
	}
}

func clampLfLvl(v int) int {
	if v < 0 {
		return 0
	}
	if v > MaxLoopFilter {
		return MaxLoopFilter
	}
	return v
}

// updateSharpness mirrors libvpx's static update_sharpness. For each
// possible filter level it derives the inside-block limit (a function
// of `sharpness_lvl`) and stamps the (lim, mblim) pair. The hev_thr
// slot is left alone — LoopFilterInit seeded it once at init time.
func updateSharpness(lfi *LoopFilterInfoN, sharpnessLvl int) {
	for lvl := 0; lvl <= MaxLoopFilter; lvl++ {
		shift := 0
		if sharpnessLvl > 0 {
			shift++
		}
		if sharpnessLvl > 4 {
			shift++
		}
		blockInsideLimit := lvl >> uint(shift)
		if sharpnessLvl > 0 {
			if blockInsideLimit > 9-sharpnessLvl {
				blockInsideLimit = 9 - sharpnessLvl
			}
		}
		if blockInsideLimit < 1 {
			blockInsideLimit = 1
		}
		lfi.Lfthr[lvl].Lim = uint8(blockInsideLimit)
		lfi.Lfthr[lvl].Mblim = uint8(2*(lvl+2) + blockInsideLimit)
	}
}
