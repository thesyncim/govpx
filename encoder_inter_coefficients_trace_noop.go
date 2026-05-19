//go:build !govpx_oracle_trace

package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

type predictedMacroblockCoefficientTrace struct{}

func newPretrellisUVTrace(*VP8Encoder) predictedMacroblockCoefficientTrace {
	return predictedMacroblockCoefficientTrace{}
}

func newPickerUVQuantizeTrace(*VP8Encoder, *vp8enc.InterFrameMacroblockMode) predictedMacroblockCoefficientTrace {
	return predictedMacroblockCoefficientTrace{}
}

func (trace *predictedMacroblockCoefficientTrace) pretrellisUVEnabled(bool, bool) bool {
	return false
}

func (trace *predictedMacroblockCoefficientTrace) chromaOptimizeBEnabled(bool, bool) bool {
	return false
}

func (trace *predictedMacroblockCoefficientTrace) pickerUVQuantizeEnabled() bool {
	return false
}

func (trace *predictedMacroblockCoefficientTrace) emitPretrellisUV(int, int, int, *[16]int16, *[16]int16, *[16]int16, int, int, int) {
}

func (trace *predictedMacroblockCoefficientTrace) emitChromaOptimizeB(int, int, int, *[16]int16, *[16]int16, *[16]int16, *[16]int16, int, int, int, bool) {
}

func (trace *predictedMacroblockCoefficientTrace) emitPickerUVQuantize(int, int, int, string, *[16]int16, *[16]int16, *[16]int16, *vp8enc.BlockQuant, int, int, int) {
}
