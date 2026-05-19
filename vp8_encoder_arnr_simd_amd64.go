//go:build amd64 && !purego

package govpx

import "unsafe"

const temporalFilterWordRepeat = 0x0001000100010001

var temporalFilterMaxDiffNoSaturate = [...]uint64{2, 3, 4, 6, 9, 13, 18}

//go:noescape
func applyTemporalFilterSSE2(src *byte, srcStride int, pred *byte, predStride int, blockSize int, strength uint64, rounding uint64, filterWeight uint64, maxDiff uint64, accumulator *uint32, count *uint32)

func repeatedTemporalFilterWord(v uint64) uint64 {
	return v * temporalFilterWordRepeat
}

func applyTemporalFilterSIMD(src []byte, srcStride int, pred []byte, predStride int, blockSize int, strength int, filterWeight int, accumulator []uint32, count []uint32) bool {
	if (blockSize != 8 && blockSize != 16) || strength < 0 || strength >= len(temporalFilterMaxDiffNoSaturate) || filterWeight <= 0 {
		return false
	}
	n := blockSize * blockSize
	if len(accumulator) < n || len(count) < n {
		return false
	}
	if len(src) < (blockSize-1)*srcStride+blockSize || len(pred) < (blockSize-1)*predStride+blockSize {
		return false
	}

	rounding := uint64(0)
	if strength > 0 {
		rounding = uint64(1 << (strength - 1))
	}
	applyTemporalFilterSSE2(
		unsafe.SliceData(src),
		srcStride,
		unsafe.SliceData(pred),
		predStride,
		blockSize,
		uint64(strength),
		repeatedTemporalFilterWord(rounding),
		repeatedTemporalFilterWord(uint64(filterWeight)),
		repeatedTemporalFilterWord(temporalFilterMaxDiffNoSaturate[strength]),
		unsafe.SliceData(accumulator),
		unsafe.SliceData(count),
	)
	return true
}
