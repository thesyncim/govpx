//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
)

// quantizeBlockAllZeroSIMD returns true when every coefficient in coeff has
// absolute value strictly below minZbin, guaranteeing all 16 quantized outputs
// are zero without executing the per-coefficient zigzag loop.
func quantizeBlockAllZeroSIMD(coeff *[16]int16, minZbin int) bool {
	if !pixel.Enabled || minZbin <= 0 {
		return false
	}
	if minZbin > 32768 {
		return true
	}
	v0 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[0:8])).Abs().ToBits()
	v1 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[8:16])).Abs().ToBits()
	thr := archsimd.BroadcastUint16x8(uint16(minZbin - 1))
	over := v0.Max(v1).SubSaturated(thr).ReshapeToUint64s()
	return over.GetElem(0)|over.GetElem(1) == 0
}
