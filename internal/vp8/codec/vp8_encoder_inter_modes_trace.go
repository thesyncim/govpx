//go:build govpx_oracle_trace

// VP8 codec implementation aligned with libvpx v1.16.0.
package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type predictedMacroblockCoefficientTrace struct {
	pretrellisUVTrace     *VP8Encoder
	pickerUVQuantizeTrace *VP8Encoder
	pickerUVQuantizeMode  vp8enc.InterFrameMacroblockMode
}

func newPretrellisUVTrace(e *VP8Encoder) predictedMacroblockCoefficientTrace {
	if e == nil || (!e.oracleTracePretrellisUVDumpEnabled() && !e.oracleTraceChromaOptimizeBDumpEnabled()) {
		return predictedMacroblockCoefficientTrace{}
	}
	return predictedMacroblockCoefficientTrace{pretrellisUVTrace: e}
}

func newPickerUVQuantizeTrace(e *VP8Encoder, mode *vp8enc.InterFrameMacroblockMode) predictedMacroblockCoefficientTrace {
	if e == nil || !e.oracleTracePickerUVQuantizeDumpEnabled() {
		return predictedMacroblockCoefficientTrace{}
	}
	trace := predictedMacroblockCoefficientTrace{pickerUVQuantizeTrace: e}
	if mode != nil {
		trace.pickerUVQuantizeMode = *mode
	}
	return trace
}

func (trace *predictedMacroblockCoefficientTrace) pretrellisUVEnabled(optimize bool, fastQuant bool) bool {
	return trace.pretrellisUVTrace != nil &&
		trace.pretrellisUVTrace.oracleTracePretrellisUVDumpEnabled() &&
		optimize && !fastQuant
}

func (trace *predictedMacroblockCoefficientTrace) chromaOptimizeBEnabled(optimize bool, fastQuant bool) bool {
	return trace.pretrellisUVTrace != nil &&
		trace.pretrellisUVTrace.oracleTraceChromaOptimizeBDumpEnabled() &&
		optimize && !fastQuant
}

func (trace *predictedMacroblockCoefficientTrace) pickerUVQuantizeEnabled() bool {
	return trace.pickerUVQuantizeTrace != nil &&
		trace.pickerUVQuantizeTrace.oracleTracePickerUVQuantizeDumpEnabled()
}

func (trace *predictedMacroblockCoefficientTrace) emitPretrellisUV(mbRow int, mbCol int, block int, coeff *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16, eob int, zbinExtra int, zbinOQ int) {
	if trace.pretrellisUVTrace != nil {
		trace.pretrellisUVTrace.emitOraclePretrellisUVTrace(mbRow, mbCol, block, coeff, qcoeff, dqcoeff, eob, zbinExtra, zbinOQ)
	}
}

func (trace *predictedMacroblockCoefficientTrace) emitChromaOptimizeB(mbRow int, mbCol int, block int, coeff *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16, dequant *[16]int16, eob int, rdMult int, rdDiv int, intra bool) {
	if trace.pretrellisUVTrace != nil {
		trace.pretrellisUVTrace.emitOracleChromaOptimizeBTrace(mbRow, mbCol, block, coeff, qcoeff, dqcoeff, dequant, eob, rdMult, rdDiv, intra)
	}
}

func (trace *predictedMacroblockCoefficientTrace) emitPickerUVQuantize(mbRow int, mbCol int, block int, quantPath string, coeff *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16, quant *vp8enc.BlockQuant, eob int, zbinExtra int, zbinOQ int) {
	if trace.pickerUVQuantizeTrace != nil {
		trace.pickerUVQuantizeTrace.emitOraclePickerUVQuantizeTrace(mbRow, mbCol, block, &trace.pickerUVQuantizeMode, quantPath, coeff, qcoeff, dqcoeff, quant, eob, zbinExtra, zbinOQ)
	}
}

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

		ImprovedMVStart:        improvedStart.ok(),
		ImprovedMVNearSADIndex: improvedStart.nearSADIndexInt(),
		ImprovedMVRow:          improvedStart.mv.Row,
		ImprovedMVCol:          improvedStart.mv.Col,
		ImprovedMVSR:           improvedStart.searchRange(),
	})
}
