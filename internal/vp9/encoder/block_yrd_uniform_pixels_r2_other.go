//go:build !goexperiment.simd || !amd64 || purego

package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

func blockYrdUniformPixelsR2(_ []byte, _, _ int, _ []byte, _, _, _, _ int, _ common.TxSize, _ [2]int16, _ uint64, _ []int16) (BlockYrdResult, bool) {
	return BlockYrdResult{}, false
}
