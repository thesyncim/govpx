package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) interModeForRDLoopEntry(
	src vp8enc.SourceImage, ref interAnalysisReference, refIndex int, mbMode vp8common.MBPredictionMode,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	newMVCandidates *[3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	},
	modeMVs *interModeMVSlots,
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		signBias := e.interFrameSignBias()
		var state interModeMVSlots
		if modeMVs != nil {
			state = *modeMVs
		} else {
			state = e.interModeMVSlots([]interAnalysisReference{ref}, [4]int8{-1, 0, -1, -1}, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		}
		slot := interModeSignBiasSlotForReference(ref.Frame, signBias)
		// slot is 0 or 1 by construction; AND-mask with 1 elides BC on
		// the [2]MotionVector slot arrays.
		nearest, near := state.nearest[slot&1], state.near[slot&1]
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		mv = clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
		if mv.IsZero() {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		if !interFrameUMVFullPixelInRange(mv, mbRow, mbCol, mbRows, mbCols) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode, MV: mv}, true
	case vp8common.NewMV:
		if uint(refIndex) >= uint(len(newMVCandidates)) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		candidate := &newMVCandidates[refIndex]
		if !candidate.searched {
			signBias := e.interFrameSignBias()
			bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
			if modeMVs != nil {
				bestRefMV = modeMVs.best[interModeSignBiasSlotForReference(ref.Frame, signBias)&1]
			}
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			var motionStats interFrameMotionSearchStats
			var stats *interFrameMotionSearchStats
			if e.opts.PhaseStats != nil && !e.threadedRowsActive {
				motionStats.phase = e.opts.PhaseStats
				stats = &motionStats
			}
			searcher := interFrameMotionVectorSearch{
				src:         src,
				ref:         ref.Img,
				mbRow:       mbRow,
				mbCol:       mbCol,
				mbRows:      mbRows,
				mbCols:      mbCols,
				bestRefMV:   bestRefMV,
				qIndex:      qIndex,
				errorPerBit: e.tunedErrorPerBit(qIndex, mbRow, mbCol),
				search:      search,
				start:       start,
				mvProbs:     &e.modeProbs.MV,
				mvCosts:     e.currentMotionVectorCostTables(),
			}
			var result interFrameMotionVectorSearchResult
			if stats != nil {
				result = searcher.selectRDWithStats(stats)
			} else {
				result = searcher.selectRD()
			}
			mv := result.mv
			mv = clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
			candidate.searched = true
			candidate.ok = true
			candidate.mv = mv
			candidate.start = start
		}
		if !candidate.ok {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		mode := vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.NewMV, MV: candidate.mv}
		return mode, true
	default:
		return vp8enc.InterFrameMacroblockMode{}, false
	}
}
