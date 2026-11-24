//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestBlockYrdQuantScoreSignedDomain(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("SIMD disabled")
	}
	for _, n := range []int{64, 256} {
		for _, d := range [][2]int16{{3, 3}, {3, 2048}, {2048, 3}, {4, 8},
			{7, 17}, {100, 127}, {255, 256}, {511, 512}, {1023, 1024},
			{1336, 1828}, {2047, 2048}, {2048, 2048}} {
			r := [2]int16{int16(48 * int(d[0]) >> 7), int16(42 * int(d[1]) >> 7)}
			q := [2]int16{int16(65536 / int(d[0])), int16(65536 / int(d[1]))}
			var coeff [256]int16
			for base := -32768; base < 32768; base += n {
				for i := range n {
					coeff[i] = int16(base + i)
				}
				checkBlockYrdQuantScore(t, coeff[:n], r, q, d)
			}
			// Uniform extremes maximize accumulation, unlike a ramp packet.
			for _, c := range []int16{-32768, -32767, -3073, -2048, -1, 0, 1, 2048, 3073, 32767} {
				for i := range n {
					coeff[i] = c
				}
				checkBlockYrdQuantScore(t, coeff[:n], r, q, d)
			}
		}
	}
}

func checkBlockYrdQuantScore(t *testing.T, coeff []int16, r, q, d [2]int16) {
	t.Helper()
	var rate int
	var errorSum uint64
	for i, c16 := range coeff {
		p := 1
		if i == 0 {
			p = 0
		}
		c := int(c16)
		a := c
		if a < 0 {
			a = -a
		}
		value := 0
		if a+int(r[p]) >= int(d[p]) {
			value = min(a+int(r[p]), 32767) * int(q[p]) >> 16
		}
		rate += value
		if c < 0 {
			value = -value
		}
		dequantized := int(int16(value * int(d[p])))
		diff := int64(c - dequantized)
		errorSum += uint64(diff * diff)
	}
	gotRate, gotError := blockYrdQuantScore(coeff, len(coeff), r, q, d)
	if gotRate != rate || uint64(gotError) != errorSum {
		t.Fatalf("n=%d d=%v first=%d rate=%d want=%d error=%d want=%d",
			len(coeff), d, coeff[0], gotRate, rate, gotError, errorSum)
	}
}

func TestBlockYrdScoreQuantizerBoundaries(t *testing.T) {
	for _, n := range []int{8, 16} {
		for _, d := range [][2]int16{{1, 2}, {2, 3}, {3, 3}, {2048, 2048}, {2049, 2048}, {32767, 32767}} {
			for _, sse := range []uint64{0, 123456, BlockYrdUnknownSSE - 1, BlockYrdUnknownSSE} {
				for pattern := range 4 {
					var src, dst [4096]byte
					for i := range src {
						switch pattern {
						case 1:
							src[i], dst[i] = 255, 0
						case 2:
							src[i], dst[i] = byte(i&1)*255, byte(1-i&1)*255
						case 3:
							src[i], dst[i] = byte(i*19+i/31), byte(i*7+i/17)
						}
					}
					tx := common.Tx8x8
					if n == 16 {
						tx = common.Tx16x16
					}
					var scratch [16384]int16
					got := BlockYrd(src[:], 64, 0, 0, dst[:], 64, 0, 0, 64, 64, tx, d, sse, scratch[:])
					want := referenceBlockYrdScore(src[:], dst[:], 64, 0, 64, 64, n, d, sse)
					if got != want {
						t.Fatalf("n=%d d=%v sse=%d pattern=%d got=%+v want=%+v", n, d, sse, pattern, got, want)
					}
				}
			}
		}
	}
}
