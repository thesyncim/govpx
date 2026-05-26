package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) vp9InterPredictionDistortion(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, ok := encoder.VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame: [2]int8{
			refFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return encoder.BlockSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH), true
}

func (e *VP9Encoder) vp9InterPredictionDistortionForMi(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	scoreW, scoreH, ok := encoder.VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi) {
		return 0, false
	}
	return encoder.BlockSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, scoreW, scoreH), true
}

func (e *VP9Encoder) vp9CompoundPredictionDistortion(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame [2]int8, refSlot [2]int,
	mv [2]vp9dec.MV, filter vp9dec.InterpFilter,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > dstStride || y0+blockH > dstRows {
		return 0, false
	}
	if refSlot[0] < 0 || refSlot[0] >= len(e.refFrames) ||
		refSlot[1] < 0 || refSlot[1] >= len(e.refFrames) ||
		!e.refFrames[refSlot[0]].valid || !e.refFrames[refSlot[1]].valid {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     refFrame,
		Mv:           mv,
	}
	inter.ref = &e.refFrames[refSlot[0]]
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return encoder.BlockSSE(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, blockW, blockH), true
}

func (e *VP9Encoder) vp9EncoderModeDecisionQIndex() int {
	if e.vp9ModeDecisionQIndexSet {
		return int(e.vp9ModeDecisionQIndex)
	}
	return e.vp9EncoderFrameQIndex(true, false, 0, 0xff, 1)
}

// vp9EncoderInitializeRDConsts is the libvpx-faithful entry point that
// populates rc.rdmult / rc.rddiv before any per-block Lagrangian RD
// scoring runs.  Called once per frame, after the mode-decision qindex
// has been resolved by vp9EncoderModeDecisionQIndex.  Mirrors libvpx's
// vp9_initialize_rd_consts.
//
// libvpx: vp9/encoder/vp9_rd.c:396-407
//
//	rd->RDDIV = RDDIV_BITS;  // In bits (to multiply D by 128).
//	rd->RDMULT = vp9_compute_rd_mult(cpi, cm->base_qindex + cm->y_dc_delta_q);
//	set_error_per_bit(x, rd->RDMULT);
//
// y_dc_delta_q is zero for govpx today; when the active-segment Q delta
// path lands it should be added to qindex here before the rdmult lookup.
func (e *VP9Encoder) vp9EncoderInitializeRDConsts(qindex int,
	frameType encoder.RDFrameType,
) {
	e.rc.rddiv = encoder.RDDivBits
	e.rc.rdmult = encoder.ComputeRDMult(qindex, frameType)
	// Reset the per-SB cb_rdmult cache so a stale value from the prior
	// frame does not leak into the first SB picker call.  libvpx clears
	// it inline before each rd_pick_sb_modes invocation; we mirror that
	// reset at the frame boundary so the first SB sees a clean state.
	e.cbRdmult = 0

	// Mode-RD-thresh state. libvpx vp9_rd.c:413-415: per-frame,
	// vp9_set_rd_speed_thresholds + set_block_thresholds run at the
	// vp9_initialize_rd_consts tail. The per-tile thresh_freq_fact is
	// primed once (RD_THRESH_INIT_FACT) at tile birth and adapts across
	// frames via update_thresh_freq_fact; libvpx never re-inits between
	// frames inside an encode session.
	//
	// libvpx: vp9_encoder.c:3755-3756 vp9_set_rd_speed_thresholds (+sub8x8
	// sibling not surfaced here).
	e.rdThresh.SetRDSpeedThresholds(e.sf.AdaptiveRdThresh)
	e.rdThresh.SetBlockThresholds(qindex, 0)
	if !e.rdThresh.Initialized() {
		e.rdThresh.InitFreqFact()
	}
}

func (e *VP9Encoder) vp9EncoderFrameQIndex(isKey, intraOnly bool, flags EncodeFlags, refreshFlags uint8, macroblocks int) int {
	traceRateSelection := vp9OracleTraceBuild && e.vp9OracleTraceEnabled()
	if traceRateSelection {
		e.resetVP9OracleRateSelectionTrace()
	}
	if e.opts.Lossless {
		return 0
	}
	if e.rc.nextFrameQIndexSet {
		qindex := int(e.rc.nextFrameQIndex)
		e.rc.nextFrameQIndexSet = false
		e.opts.NextFrameQIndexSet = false
		e.opts.NextFrameQIndex = 0
		if traceRateSelection {
			e.recordVP9OracleRateSelectionTrace(qindex, qindex, 1, false, 0)
		}
		return qindex
	}
	qindex := e.opts.Quantizer
	var cyclic *encoder.CyclicRefreshState
	var encodeSpeed int
	if e.opts.AQMode == VP9AQCyclicRefresh && e.cyclicAQ.Enabled {
		cyclic = &e.cyclicAQ
		encodeSpeed = e.vp9SpeedFeatureCPUUsed()
	}
	applyCyclicLimitQ := func(q int) int {
		if cyclic != nil && cyclic.ApplyCyclicRefresh && !isKey && !intraOnly && e.rc.enabled {
			cyclic.LimitQ(int(e.rc.q1Frame), &q)
		}
		return q
	}
	if qindex == 0 {
		if e.rc.enabled {
			if e.rc.mode == RateControlCBR {
				if traceRateSelection {
					var activeBest int
					var activeWorst int
					var correctionFactor float64
					qindex, activeBest, activeWorst, correctionFactor =
						e.rc.cbrQuantizerWithBounds(isKey || intraOnly,
							refreshFlags, e.frameIndex, macroblocks, cyclic, encodeSpeed)
					e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
						correctionFactor, e.rc.onePassRecodeAllowed(), 0)
					return applyCyclicLimitQ(qindex)
				}
				qindex = e.rc.cbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks, cyclic, encodeSpeed)
			} else {
				e.prepareVP9SecondPassFrameTarget(isKey || intraOnly,
					refreshFlags)
				if traceRateSelection {
					var activeBest int
					var activeWorst int
					var correctionFactor float64
					qindex, activeBest, activeWorst, correctionFactor =
						e.rc.vbrQuantizerWithBounds(isKey || intraOnly,
							refreshFlags, e.frameIndex, macroblocks, cyclic, encodeSpeed)
					e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
						correctionFactor, e.rc.onePassRecodeAllowed(), 0)
					return applyCyclicLimitQ(qindex)
				}
				qindex = e.rc.vbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks, cyclic, encodeSpeed)
			}
		} else {
			qindex = e.vp9EncoderPublicQModeQIndex(isKey, intraOnly,
				refreshFlags)
			if traceRateSelection {
				minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
				e.recordVP9OracleRateSelectionTrace(
					encoder.PublicQuantizerToQIndex(minQ),
					encoder.PublicQuantizerToQIndex(maxQ),
					1, false, 0)
			}
		}
	}
	if cyclic != nil && cyclic.ApplyCyclicRefresh && !isKey && !intraOnly && e.rc.enabled {
		return applyCyclicLimitQ(qindex)
	}
	return qindex
}

func (e *VP9Encoder) vp9EncoderPublicQModeQIndex(isKey, intraOnly bool, refreshFlags uint8) int {
	minQ, maxQ, cqLevel := vp9NormalizedPublicQuantizers(e.opts)
	best := encoder.PublicQuantizerToQIndex(minQ)
	worst := encoder.PublicQuantizerToQIndex(maxQ)
	cq := encoder.PublicQuantizerToQIndex(cqLevel)
	if best >= worst {
		return best
	}

	num, den := 1, 1
	if isKey || intraOnly {
		num, den = 1, 4
	} else if refreshFlags&(1<<vp9AltRefSlot) != 0 {
		num, den = 2, 5
	} else if refreshFlags&(1<<vp9GoldenRefSlot) != 0 {
		num, den = 1, 2
	} else {
		num, den = encoder.PublicQModeInterRate(e.frameIndex)
	}
	qindex := min(max(cq+encoder.ComputeQDelta(best, worst, cq, num, den), best), worst)
	return qindex
}

func validateVP9PublicQuantizerOptions(opts VP9EncoderOptions) error {
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.CQLevel < 0 ||
		opts.MinQuantizer > encoder.MaxPublicQuantizer ||
		opts.MaxQuantizer > encoder.MaxPublicQuantizer ||
		opts.CQLevel > encoder.MaxPublicQuantizer {
		return ErrInvalidQuantizer
	}
	if (opts.MinQuantizer != 0 || opts.MaxQuantizer != 0) &&
		opts.MinQuantizer > opts.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if opts.Quantizer != 0 &&
		(opts.MinQuantizer != 0 || opts.MaxQuantizer != 0 || opts.CQLevel != 0) {
		return ErrInvalidQuantizer
	}
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(opts)
	if opts.CQLevel != 0 && (opts.CQLevel < minQ || opts.CQLevel > maxQ) {
		return ErrInvalidQuantizer
	}
	return nil
}

func vp9NormalizedPublicQuantizers(opts VP9EncoderOptions) (minQ, maxQ, cqLevel int) {
	minQ = opts.MinQuantizer
	maxQ = opts.MaxQuantizer
	if minQ == 0 && maxQ == 0 {
		minQ = vp9DefaultMinQuantizer
		maxQ = vp9DefaultMaxQuantizer
	}
	cqLevel = opts.CQLevel
	if cqLevel == 0 {
		if minQ == maxQ {
			cqLevel = minQ
		} else {
			cqLevel = min(max(vp9DefaultCQLevel, minQ), maxQ)
		}
	}
	return minQ, maxQ, cqLevel
}

// vp9InterModeScore / vp9ModeDecisionScore / vp9AddModeDecisionRate /
// vp9ModeDecisionRateScore were the legacy linear-lambda scorers
// (rate*(1+qindex/32)).  They now route through the libvpx-faithful
// Lagrangian RDCOST macro (vp9/encoder/vp9_rd.h:29-30) using the per-SB
// cb_rdmult cache when primed, falling back to the per-frame rd.rdmult,
// then to a freshly-computed rdmult for the supplied qindex. When neither
// cb_rdmult nor rd.rdmult is populated, qindex seeds the libvpx inter-frame
// multiplier table.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (RDCOST) and vp9/encoder/vp9_rd.c
// vp9_compute_rd_mult_based_on_qindex.
func (e *VP9Encoder) vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return e.vp9ModeDecisionScore(sad, rate, qindex)
}

func (e *VP9Encoder) vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	return encoder.RDCost(e.activeRDMult(qindex), encoder.RDDivBits, rate, distortion)
}

func (e *VP9Encoder) vp9AddModeDecisionRate(score uint64, rate, qindex int) uint64 {
	return score + encoder.RDCostFromRate(e.activeRDMult(qindex), rate)
}

func (e *VP9Encoder) vp9ModeDecisionRateScore(rate, qindex int) uint64 {
	return encoder.RDCostFromRate(e.activeRDMult(qindex), rate)
}

// activeRDMult returns the per-frame/per-SB Lagrange multiplier.
func (e *VP9Encoder) activeRDMult(qindex int) int {
	if e.cbRdmult > 0 {
		return e.cbRdmult
	}
	if e.rc.rdmult > 0 {
		return e.rc.rdmult
	}
	return encoder.ComputeRDMultBasedOnQindex(qindex, encoder.RDFrameInter)
}

// vp9InterModeScore / vp9ModeDecisionScore (package-level, no receiver)
// preserve the pre-Lagrangian scoring API surface for the small handful
// of unit tests that assert pure rate/distortion ordering with no
// encoder context.  They synthesize the multiplier from the supplied
// qindex via the same libvpx-faithful inter-frame table the production
// path uses.  Production call sites must use the encoder-bound helpers
// above so the per-frame / per-SB rdmult state is honoured.
func vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return vp9ModeDecisionScore(sad, rate, qindex)
}

func vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	rdmult := encoder.ComputeRDMultBasedOnQindex(qindex, encoder.RDFrameInter)
	return encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
}

func vp9AnyMvHasSubpel(mv [2]vp9dec.MV) bool {
	return vp9MvHasSubpel(mv[0]) || vp9MvHasSubpel(mv[1])
}
