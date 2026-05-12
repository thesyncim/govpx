//go:build govpx_oracle_trace

package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) emitFastPickerIntraCandidateTrace(mbRow int, mbCol int, modeIndex int, threshold int, bestScoreBefore int, bestSSEBefore int, becameBest bool, score int, rate int, distortion int, sse int, mode *vp8enc.InterFrameMacroblockMode) {
	e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
		Picker:          "fast",
		MBRow:           mbRow,
		MBCol:           mbCol,
		ModeIndex:       modeIndex,
		Mode:            mode.Mode,
		RefSlot:         0,
		RefFrame:        vp8common.IntraFrame,
		Threshold:       threshold,
		BestScoreBefore: bestScoreBefore,
		BestYRDBefore:   oracleTraceInterCandidateUnknown,
		BestSSEBefore:   bestSSEBefore,
		Outcome:         "tested",
		BecameBest:      becameBest,
		Score:           score,
		YRD:             oracleTraceInterCandidateUnknown,
		Rate:            rate,
		RateY:           oracleTraceInterCandidateUnknown,
		RateUV:          oracleTraceInterCandidateUnknown,
		Distortion:      distortion,
		DistortionUV:    oracleTraceInterCandidateUnknown,
		SSE:             sse,
		Skip:            mode.MBSkipCoeff,
		ModeTrace:       *mode,
		HasModeTrace:    true,
	})
}

func (e *VP8Encoder) emitFastPickerInterCandidateTrace(mbRow int, mbCol int, modeIndex int, refSlot int, refFrame vp8common.MVReferenceFrame, threshold int, bestScoreBefore int, bestSSEBefore int, becameBest bool, breakoutSkip bool, score int, rate int, distortion int, sse int, mode *vp8enc.InterFrameMacroblockMode, improvedStart interFrameSearchStart) {
	e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
		Picker:          "fast",
		MBRow:           mbRow,
		MBCol:           mbCol,
		ModeIndex:       modeIndex,
		Mode:            mode.Mode,
		RefSlot:         refSlot,
		RefFrame:        refFrame,
		Threshold:       threshold,
		BestScoreBefore: bestScoreBefore,
		BestYRDBefore:   oracleTraceInterCandidateUnknown,
		BestSSEBefore:   bestSSEBefore,
		Outcome:         "tested",
		BecameBest:      becameBest,
		LoopBreak:       breakoutSkip,
		Score:           score,
		YRD:             oracleTraceInterCandidateUnknown,
		Rate:            rate,
		RateY:           oracleTraceInterCandidateUnknown,
		RateUV:          oracleTraceInterCandidateUnknown,
		Distortion:      distortion,
		DistortionUV:    oracleTraceInterCandidateUnknown,
		SSE:             sse,
		Skip:            breakoutSkip,
		ModeTrace:       *mode,
		HasModeTrace:    true,

		ImprovedMVStart:        improvedStart.ok,
		ImprovedMVNearSADIndex: improvedStart.nearSADIndex,
		ImprovedMVRow:          improvedStart.mv.Row,
		ImprovedMVCol:          improvedStart.mv.Col,
		ImprovedMVSR:           improvedStart.sr,
	})
}
