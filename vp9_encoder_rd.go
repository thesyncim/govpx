package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
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
	return vp9BlockSSE(src, srcStride, dst, dstStride,
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
	scoreW, scoreH, ok := vp9VisibleInterScoreBlock(x0, y0, blockW, blockH,
		srcW, srcH, dstStride, dstRows)
	if !ok {
		return 0, false
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi) {
		return 0, false
	}
	return vp9BlockSSE(src, srcStride, dst, dstStride,
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
	return vp9BlockSSE(src, srcStride, dst, dstStride,
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
	// sibling not surfaced here — the sub-8x8 picker is govpx-deferred).
	e.rdThresh.setRDSpeedThresholds(e.sf.AdaptiveRdThresh)
	e.rdThresh.setBlockThresholds(qindex, 0)
	if !e.rdThresh.initialised {
		e.rdThresh.initFreqFact()
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
	if qindex == 0 {
		if e.rc.enabled {
			if e.rc.mode == RateControlCBR {
				if traceRateSelection {
					var activeBest int
					var activeWorst int
					var correctionFactor float64
					qindex, activeBest, activeWorst, correctionFactor =
						e.rc.cbrQuantizerWithBounds(isKey || intraOnly,
							refreshFlags, e.frameIndex, macroblocks)
					e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
						correctionFactor, e.rc.onePassRecodeAllowed(), 0)
					return qindex
				}
				qindex = e.rc.cbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks)
			} else {
				e.prepareVP9SecondPassFrameTarget(isKey || intraOnly,
					refreshFlags)
				if traceRateSelection {
					var activeBest int
					var activeWorst int
					var correctionFactor float64
					qindex, activeBest, activeWorst, correctionFactor =
						e.rc.vbrQuantizerWithBounds(isKey || intraOnly,
							refreshFlags, e.frameIndex, macroblocks)
					e.recordVP9OracleRateSelectionTrace(activeBest, activeWorst,
						correctionFactor, e.rc.onePassRecodeAllowed(), 0)
					return qindex
				}
				qindex = e.rc.vbrQuantizer(isKey || intraOnly, refreshFlags,
					e.frameIndex, macroblocks)
			}
		} else {
			qindex = e.vp9EncoderPublicQModeQIndex(isKey, intraOnly,
				refreshFlags)
			if traceRateSelection {
				minQ, maxQ, _ := vp9NormalizedPublicQuantizers(e.opts)
				e.recordVP9OracleRateSelectionTrace(
					vp9PublicQuantizerToQIndex(minQ),
					vp9PublicQuantizerToQIndex(maxQ),
					1, false, 0)
			}
		}
	}
	return qindex
}

func (e *VP9Encoder) vp9EncoderPublicQModeQIndex(isKey, intraOnly bool, refreshFlags uint8) int {
	minQ, maxQ, cqLevel := vp9NormalizedPublicQuantizers(e.opts)
	best := vp9PublicQuantizerToQIndex(minQ)
	worst := vp9PublicQuantizerToQIndex(maxQ)
	cq := vp9PublicQuantizerToQIndex(cqLevel)
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
		num, den = vp9PublicQModeInterRate(e.frameIndex)
	}
	qindex := min(max(cq+vp9ComputeQDelta(best, worst, cq, num, den), best), worst)
	return qindex
}

func vp9PublicQModeInterRate(frameIndex int) (num int, den int) {
	switch frameIndex & 7 {
	case 0:
		return 1, 2
	case 2, 6:
		return 85, 100
	case 4:
		return 7, 10
	default:
		return 1, 1
	}
}

func validateVP9PublicQuantizerOptions(opts VP9EncoderOptions) error {
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.CQLevel < 0 ||
		opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer ||
		opts.CQLevel > maxQuantizer {
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

func vp9PublicQuantizerToQIndex(q int) int {
	return vp9QuantizerToQIndex[min(max(q, 0), maxQuantizer)]
}

func vp9QIndexToPublicQuantizer(qIndex int) int {
	for q, translated := range vp9QuantizerToQIndex {
		if translated >= qIndex {
			return q
		}
	}
	return maxQuantizer
}

func vp9ComputeQDelta(best, worst, qindex, num, den int) int {
	if den <= 0 {
		return 0
	}
	qindex = min(max(qindex, best), worst)
	qstart := int(tables.AcQLookup8[qindex])
	targetNumer := qstart * num
	startIndex := worst
	targetIndex := worst
	for i := best; i < worst; i++ {
		startIndex = i
		if int(tables.AcQLookup8[i]) >= qstart {
			break
		}
	}
	for i := best; i < worst; i++ {
		targetIndex = i
		if int(tables.AcQLookup8[i])*den >= targetNumer {
			break
		}
	}
	return targetIndex - startIndex
}

var vp9QuantizerToQIndex = [maxQuantizer + 1]int{
	0, 4, 8, 12, 16, 20, 24, 28,
	32, 36, 40, 44, 48, 52, 56, 60,
	64, 68, 72, 76, 80, 84, 88, 92,
	96, 100, 104, 108, 112, 116, 120, 124,
	128, 132, 136, 140, 144, 148, 152, 156,
	160, 164, 168, 172, 176, 180, 184, 188,
	192, 196, 200, 204, 208, 212, 216, 220,
	224, 228, 232, 236, 240, 244, 249, 255,
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

func vp9InterModeRateCost(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv vp9dec.MV, allowHP bool,
) int {
	return vp9InterModeRateCostN(fc, ctx, mode,
		[2]vp9dec.MV{mv}, [2]vp9dec.MV{refMv}, 1, allowHP)
}

func vp9InterModeRateCostN(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv [2]vp9dec.MV, nrefs int, allowHP bool,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.InterModeProbs) {
		return 0
	}
	if nrefs < 1 {
		nrefs = 1
	}
	if nrefs > len(mv) {
		nrefs = len(mv)
	}
	probs := fc.InterModeProbs[ctx]
	cost := 0
	switch mode {
	case common.ZeroMv:
		cost = encoder.VP9CostBit(probs[0], 0)
	case common.NearestMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 0)
	case common.NearMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 0)
	case common.NewMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 1)
		for ref := 0; ref < nrefs; ref++ {
			cost += vp9MvBitCost(mv[ref], refMv[ref], &fc.Nmvc, allowHP)
		}
	default:
		return 0
	}
	return cost
}

func vp9MvBitCost(mv, ref vp9dec.MV, ctx *vp9dec.NmvContext, allowHP bool) int {
	// libvpx vp9_mcomp.c:80-84:
	//   vp9_mv_bit_cost(..., MV_COST_WEIGHT)
	//   ROUND_POWER_OF_TWO(mv_cost(diff) * 108, 7)
	const mvCostWeight = 108
	raw := encoder.MvCostWithHP(mv, ref, ctx, allowHP)
	return (raw*mvCostWeight + 64) >> 7
}

func vp9AnyMvHasSubpel(mv [2]vp9dec.MV) bool {
	return vp9MvHasSubpel(mv[0]) || vp9MvHasSubpel(mv[1])
}

func vp9IntraInterRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, isInter int,
) int {
	if fc == nil {
		return 0
	}
	if isInter != 0 {
		isInter = 1
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	return encoder.VP9CostBit(fc.IntraInterProb[ctx], isInter)
}

func vp9ReferenceModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, isCompound bool,
) int {
	if fc == nil || frameMode != vp9dec.ReferenceModeSelect {
		return 0
	}
	ctx := vp9dec.GetReferenceModeContext(above, left, refs)
	bit := 0
	if isCompound {
		bit = 1
	}
	return encoder.VP9CostBit(fc.ReferenceModeProbs.CompInterProb[ctx], bit)
}

func vp9SingleRefModeRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, refFrame int8,
) int {
	return vp9IntraInterRateCost(fc, above, left, 1) +
		vp9ReferenceModeRateCost(fc, above, left, frameMode, refs, false) +
		vp9SingleRefRateCost(fc, above, left, refFrame)
}

func vp9SingleRefRateCost(fc *vp9dec.FrameContext, above, left *vp9dec.NeighborMi,
	refFrame int8,
) int {
	if fc == nil || refFrame <= vp9dec.IntraFrame {
		return 0
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	cost := encoder.VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx0][0], bit0)
	if bit0 == 0 {
		return cost
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	return cost + encoder.VP9CostBit(fc.ReferenceModeProbs.SingleRefProb[ctx1][1], bit1)
}

func vp9CompoundRefRateCost(fc *vp9dec.FrameContext,
	above, left *vp9dec.NeighborMi, frameMode vp9dec.ReferenceMode,
	refs vp9dec.CompoundFrameRefs, signBias [vp9dec.MaxRefFrames]uint8,
	refFrame [2]int8,
) (int, bool) {
	if fc == nil || frameMode == vp9dec.SingleReference {
		return 0, false
	}
	idx := int(signBias[refs.CompFixedRef])
	if idx < 0 || idx > 1 || refFrame[idx] != refs.CompFixedRef {
		return 0, false
	}
	varRef := refFrame[1-idx]
	bit := 0
	switch varRef {
	case refs.CompVarRef[0]:
	case refs.CompVarRef[1]:
		bit = 1
	default:
		return 0, false
	}
	ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
	cost := vp9ReferenceModeRateCost(fc, above, left, frameMode, refs, true)
	cost += encoder.VP9CostBit(fc.ReferenceModeProbs.CompRefProb[ctx], bit)
	return cost, true
}

func vp9BlockSAD(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int, limit uint64,
) uint64 {
	// libvpx's sad_function pointers (cpi->fn_ptr[bsize].sdf) compute the
	// full block SAD with no early-termination — see vpx_dsp/sad.c
	// SAD()/vpx_dsp/arm/sad_neon.c. The caller compares the returned SAD
	// against best_sad afterwards. Govpx historically used the `limit`
	// argument to early-exit a row-major scalar loop, but that bypassed
	// the SIMD kernels and was a net pessimization. Always go through the
	// size-specialized SAD path; the per-row early-exit only matters for
	// limit-driven calls on sizes outside the wrapper table.
	// libvpx: vpx_dsp/sad.c:24 — SAD() returns sum without limit check.
	srcOff := srcY*srcStride + srcX
	refOff := refY*refStride + refX
	return vp9BlockSADOffsets(src, srcOff, srcStride, ref, refOff, refStride,
		w, h, limit)
}

func vp9BlockSADOffsets(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int, limit uint64,
) uint64 {
	if sad, ok := vp9BlockSADNoLimitOffsets(src, srcOff, srcStride,
		ref, refOff, refStride, w, h); ok {
		return uint64(sad)
	}
	var sad uint64
	for y := range h {
		srcRow := src[srcOff+y*srcStride:]
		refRow := ref[refOff+y*refStride:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
		if sad >= limit {
			return sad
		}
	}
	return sad
}

func vp9BlockSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			sse += uint64(diff * diff)
		}
	}
	return sse
}

func vp9BlockDiffVariance(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) uint64 {
	variance, _ := vp9BlockDiffVarianceSSE(src, srcStride, ref, refStride,
		srcX, srcY, refX, refY, w, h)
	return variance
}

func vp9BlockDiffVarianceSSE(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) (uint64, uint64) {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int64(int(srcRow[x]) - int(refRow[x]))
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0, sse
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0, sse
	}
	return sse - meanSquares, sse
}

func vp9BlockSourceVariance128(src []byte, srcStride int, srcX, srcY, w, h int) uint64 {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		for x := range w {
			diff := int64(srcRow[x]) - 128
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0
	}
	return sse - meanSquares
}

func vp9SourceVarianceAreaPerPixel(src []byte, srcStride int, srcX, srcY, w, h int) uint {
	if w <= 0 || h <= 0 {
		return 0
	}
	variance := vp9BlockSourceVariance128(src, srcStride, srcX, srcY, w, h)
	pixels := uint64(w * h)
	return uint((variance + (pixels >> 1)) / pixels)
}

func vp9BlockSADNoLimitOffsets(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int,
) (uint32, bool) {
	switch {
	case w == 64 && h == 64:
		return vp9dsp.VpxSad64x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 64 && h == 32:
		return vp9dsp.VpxSad64x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 64:
		return vp9dsp.VpxSad32x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 32:
		return vp9dsp.VpxSad32x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 16:
		return vp9dsp.VpxSad32x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 32:
		return vp9dsp.VpxSad16x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 16:
		return vp9dsp.VpxSad16x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 8:
		return vp9dsp.VpxSad16x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 16:
		return vp9dsp.VpxSad8x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 8:
		return vp9dsp.VpxSad8x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 4:
		return vp9dsp.VpxSad8x4(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 8:
		return vp9dsp.VpxSad4x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 4:
		return vp9dsp.VpxSad4x4(src, srcOff, srcStride, ref, refOff, refStride), true
	default:
		return 0, false
	}
}
