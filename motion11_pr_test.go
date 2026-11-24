// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func (ctx *splitMotionSubsetContext) motion11ParentSelectMotion() (vp8enc.MotionVector, vp8common.BPredictionMode, int, int, int, int, int) {
	// MBSplitOffset is [4][16]uint8: ctx.mode.Partition ∈ [0,4) and
	// ctx.subset ∈ [0,16) by upstream validation. Pow2 AND-masks
	// elide both bounds checks on this hot per-subset load.
	block := int(vp8tables.MBSplitOffset[ctx.mode.Partition&3][ctx.subset&15])
	leftMV := vp8enc.SplitLeftMV(ctx.mode, ctx.left, block)
	aboveMV := vp8enc.SplitAboveMV(ctx.mode, ctx.above, block)
	mbRows := (ctx.src.Height + 15) >> 4
	mbCols := (ctx.src.Width + 15) >> 4
	bestMV := vp8enc.MotionVector{}
	bestMode := vp8common.Zero4x4
	bestRD := maxInt()
	bestRate := 0
	bestYRate := 0
	bestDistortion := 0
	// lastTTEOB tracks the per-block-tteob from the LAST inner-loop iteration
	// that actually called vp8_encode_inter_mb_segment (i.e. wasn't UMV-
	// trapped and wasn't gated out before the call). libvpx returns this
	// side-effect via `xd->eobs[i]` because rd_check_segment never restores
	// the eobs after the winning mode is re-selected by `labels2mode`
	// (rdopt.c:1152-1158): only the entropy contexts (t_above_b/t_left_b)
	// are restored, the per-block eob registers retain the last call's
	// values, and bsi->eobs[i] = xd->eobs[i] at rdopt.c:1180 then captures
	// that stale snapshot for the segment_rd-wins path. calculate_final_rd_
	// costs reads tteob back through those stale eobs (rdopt.c:1689-1697),
	// so the SPLITMV skip-backout gate depends on the LAST-tested mode's
	// tteob, not the RD-winning mode's. Track it independently here.
	lastTTEOB := 0
	var bestAbove [4]uint8
	var bestLeft [4]uint8
	bestHasContexts := false

	tryCandidate := func(candidateMode vp8common.BPredictionMode, mv vp8enc.MotionVector) {
		// rd_check_segment applies the same UMV trap to inherited
		// LEFT/ABOVE/ZERO labels as it does to searched NEW labels.
		if !vp8enc.InterFrameUMVFullPixelInRange(mv, ctx.mbRow, ctx.mbCol, mbRows, mbCols) {
			return
		}
		rate := vp8enc.SplitSubMotionLabelRate(candidateMode, ctx.subMVRefProbs)
		rd, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, mv, rate)
		lastTTEOB = tteob
		if rd < bestRD {
			bestRD = rd
			bestRate = labelRate
			bestYRate = labelYRate
			bestDistortion = distortion
			bestMV = mv
			bestMode = candidateMode
			bestAbove = nextAbove
			bestLeft = nextLeft
			bestHasContexts = hasContexts
		}
	}

	tryCandidate(vp8common.Left4x4, leftMV)
	if aboveMV != leftMV {
		tryCandidate(vp8common.Above4x4, aboveMV)
	}
	tryCandidate(vp8common.Zero4x4, vp8enc.MotionVector{})

	// libvpx: `if (best_label_rd < label_mv_thresh) break;` — the running
	// best label score is already below the per-label MV threshold, skip
	// the NEW4X4 motion search (the most expensive trial). When the gate
	// is disabled (labelMVThresh == 0) we keep the legacy behavior of
	// always running the NEW4X4 search. We compare in RDCOST space so
	// the threshold matches the libvpx rd_threshes scale.
	//
	// libvpx breaks out of the inner mode loop here BEFORE calling
	// vp8_encode_inter_mb_segment for NEW4X4 (rdopt.c:1027 is inside the
	// `if (this_mode == NEW4X4)` block and precedes the segment-encode
	// call at 1135), so the lastTTEOB carry from ZERO4X4 (or the prior
	// non-trapped mode) is what bsi->eobs sees. lastTTEOB stays untouched
	// in this branch — _not_ assigned bestTTEOB — exactly matching that
	// side-effect.
	if ctx.labelMVThresh > 0 && bestRD < ctx.labelMVThresh {
		if ctx.labelRD != nil && bestHasContexts {
			ctx.labelRD.yAbove = bestAbove
			ctx.labelRD.yLeft = bestLeft
		}
		return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
	}

	errorPerBit := ctx.errorPerBit
	if errorPerBit <= 0 {
		errorPerBit = vp8enc.ErrorPerBit(ctx.qIndex)
	}
	// libvpx vp8_rd_pick_best_mbsegmentation (vp8/encoder/rdopt.c:1199-
	// 1303) runs each rd_check_segment call inside one of two branches:
	//
	//   * Best mode (cpi->compressor_speed == 0, rdopt.c:1220-1226):
	//     all four segmentations (BLOCK_16X8/8X16/8X8/4X4) execute back-
	//     to-back with x->mv_col_min/x->mv_col_max untouched at their
	//     wide MB-scope UMV window. The [best_ref_mv ± MAX_FULL_PEL_VAL]
	//     intersection block (rdopt.c:1233-1248) lives in the `else`
	//     speed-mode branch and is never reached here.
	//
	//   * Speed mode (compressor_speed != 0, rdopt.c:1227-1302): the
	//     first BLOCK_8X8 call runs against the wide UMV window, then
	//     mv_col_min/max are tightened to the intersection with
	//     [best_ref_mv ± MAX_FULL_PEL_VAL] before BLOCK_8X16/16X8/4X4
	//     and restored afterwards (rdopt.c:1297-1301).
	//
	// govpx mirrors both shapes: in best mode every partition uses the
	// wide UMV window; in speed mode only partition 2 (BLOCK_8X8) does,
	// and the secondary partitions intersect with bestRefMV±MAX_FULL_PEL_
	// VAL. Earlier code intersected the window in best mode too,
	// truncating the SPLITMV per-label diamond search at MB(0,0) frame 1
	// for partitions 0/1/3 on the 1280x720 SSIM-best cohort and biasing
	// the picker away from SPLITMV.
	var bounds vp8enc.InterFrameFullPixelBounds
	if ctx.compressorSpeed == 0 || (ctx.mode != nil && ctx.mode.Partition == 2) {
		bounds = vp8enc.InterFrameUMVOnlyFullPixelSearchBounds(ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	} else {
		bounds = vp8enc.InterFrameFullPixelSearchBounds(ctx.bestRefMV, ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	}
	newMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorWithBounds(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, ctx.searchCenter, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.stepParam, ctx.fullSearchFallback, ctx.mvProbs, ctx.mvCosts, bounds)
	if refinedMV, _, ok := refineInterFrameSplitBlockSubpixelMotionVectorWithErrorPerBitAndCostTables(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, newMV, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.search, ctx.mvProbs, ctx.mvCosts); ok {
		newMV = refinedMV
	}
	newRate := vp8enc.SplitSubMotionLabelRate(vp8common.New4x4, ctx.subMVRefProbs)
	delta := vp8enc.MotionVector{Row: int16(int(newMV.Row) - int(ctx.bestRefMV.Row)), Col: int16(int(newMV.Col) - int(ctx.bestRefMV.Col))}
	if ctx.mvCosts != nil {
		newRate += vp8enc.SplitMotionVectorCost(delta, nil, ctx.mvCosts)
	} else {
		newRate += vp8enc.SplitMotionVectorCost(delta, ctx.mvProbs, nil)
	}
	newRD, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, newMV, newRate)
	lastTTEOB = tteob
	if newRD < bestRD {
		bestRD = newRD
		bestRate = labelRate
		bestYRate = labelYRate
		bestDistortion = distortion
		bestMV = newMV
		bestMode = vp8common.New4x4
		bestAbove = nextAbove
		bestLeft = nextLeft
		bestHasContexts = hasContexts
	}
	if ctx.labelRD != nil && bestHasContexts {
		ctx.labelRD.yAbove = bestAbove
		ctx.labelRD.yLeft = bestLeft
	}

	return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
}
func motion11PRContext(tb testing.TB, partition int, fast bool) splitMotionSubsetContext {
	src, ref, _, quant, q := splitMVDecisionRDFixture(tb)
	ev := new(splitMotionLabelRDEvaluator)
	ev.init(0, 0, nil, nil, fast, false)
	mode := &vp8enc.InterFrameMacroblockMode{Mode: vp8common.SplitMV, Partition: uint8(partition)}
	w, h := splitMotionPartitionBlockSize(partition)
	return splitMotionSubsetContext{src: src, ref: ref, mode: mode, subset: 0, width: w, height: h, qIndex: q, labelMVThresh: maxInt(), labelRD: ev, quant: &quant, coefProbs: &vp8tables.DefaultCoefProbs, mvProbs: &vp8tables.DefaultMVContext, search: defaultInterAnalysisSearchConfig(), stepParam: 7}
}
func TestMotion11PRDuplicateSplitLabels(t *testing.T) {
	for partition := 0; partition < 4; partition++ {
		for _, fast := range []bool{false, true} {
			for _, fallback := range []bool{false, true} {
				for _, gate := range []int{0, maxInt()} {
					ctx := motion11PRContext(t, partition, fast)
					ctx.labelMVThresh = gate
					if fallback {
						ctx.labelRD = nil
					}
					a, b := ctx, ctx
					var ea, eb splitMotionLabelRDEvaluator
					if ctx.labelRD != nil {
						ea, eb = *ctx.labelRD, *ctx.labelRD
						a.labelRD = &ea
						b.labelRD = &eb
					}
					mv, mode, rd, rate, yr, dist, te := a.selectMotion()
					wm, wb, wr, wt, wy, wd, we := b.motion11ParentSelectMotion()
					if mv != wm || mode != wb || rd != wr || rate != wt || yr != wy || dist != wd || te != we || ea != eb {
						t.Fatal(partition, fast, fallback, gate, mv, mode, rd, rate, yr, dist, te, wm, wb, wr, wt, wy, wd, we, ea, eb)
					}
				}
			}
		}
	}
}

var motion11PRInt int
var motion11PRMV vp8enc.MotionVector

func BenchmarkMotion11PRDuplicateSplitLabels(b *testing.B) {
	for _, part := range []int{0, 2, 3} {
		b.Run(fmt.Sprint("partition", part), func(b *testing.B) {
			ctx := motion11PRContext(b, part, true)
			initial := *ctx.labelRD
			b.ReportAllocs()
			for b.Loop() {
				*ctx.labelRD = initial
				mv, _, rd, rate, yr, dist, te := ctx.selectMotion()
				motion11PRMV = mv
				motion11PRInt = rd + rate + yr + dist + te
			}
		})
	}
}
