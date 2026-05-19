//go:build govpx_oracle_trace

package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

type predictedMacroblockCoefficientTrace struct {
	pretrellisUVTrace     *VP8Encoder
	pickerUVQuantizeTrace *VP8Encoder
	pickerUVQuantizeMode  vp8enc.InterFrameMacroblockMode
}

func newPretrellisUVTrace(e *VP8Encoder) predictedMacroblockCoefficientTrace {
	return predictedMacroblockCoefficientTrace{pretrellisUVTrace: e}
}

func newPickerUVQuantizeTrace(e *VP8Encoder, mode *vp8enc.InterFrameMacroblockMode) predictedMacroblockCoefficientTrace {
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
