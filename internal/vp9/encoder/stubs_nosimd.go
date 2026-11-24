//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

func blockYrdScore([]int16, int, int, common.TxSize, [2]int16, [2]int16,
	[2]int16, uint64, []int16, bool,
) (BlockYrdResult, bool) {
	return BlockYrdResult{}, false
}

func hadamardGo(_ []int16, _ int, _ []int16, _ int) bool { return false }
func forwardWHTGo(_ []int16, _ int, _ []int16) bool      { return false }

func hadamardBlockYrdGo(_ []int16, _ int, _ []int16, _ int) bool { return false }

func forwardTransformGo(_ []int16, _ int, _ []int16, _ int, _ common.TxType) bool { return false }

func forwardDCT32Go(_ []int16, _ int, _ []int16, _ bool) bool { return false }
