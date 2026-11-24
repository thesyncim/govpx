//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

func blockYrdScore([]int16, int, int, common.TxSize, [2]int16, [2]int16,
	[2]int16, uint64, []int16, bool,
) (BlockYrdResult, bool) {
	return BlockYrdResult{}, false
}
