//go:build goexperiment.simd && amd64 && !purego

package encoder

import (
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// A uniform residual has only one Hadamard coefficient. Detect it directly
// before storing residuals or rechecking the same fact for each transform.
func blockYrdUniformPixelsR2(src []byte, ss, so int, dst []byte, ds, do, bw, bh int, tx common.TxSize, d [2]int16, sse uint64, scratch []int16) (BlockYrdResult, bool) {
	if (uint(ss)|uint(ds))>>30 != 0 || so < 0 || do < 0 || so > len(src) || do > len(dst) || !pixel.Enabled || (tx != common.Tx8x8 && tx != common.Tx16x16) || bw < 16 || bw > 64 || bh <= 0 || bh > 64 || bw%16 != 0 || bh%(4<<uint(tx)) != 0 || len(scratch) < 4*bw*bh || d[0] < 3 || d[0] > 2048 || d[1] < 3 || d[1] > 2048 {
		return BlockYrdResult{}, false
	}
	if len(src)-so < bw || len(dst)-do < bw || bh-1 > (len(src)-so-bw)/ss || bh-1 > (len(dst)-do-bw)/ds {
		return BlockYrdResult{}, false
	}
	delta := int16(src[so]) - int16(dst[do])
	// The existing subtract kernel already handles zero residuals cheaply.
	if delta == 0 {
		return BlockYrdResult{}, false
	}
	vd := archsimd.BroadcastInt16x16(delta)
	for y := 0; y < bh; y++ {
		srow := src[so+y*ss:]
		drow := dst[do+y*ds:]
		for x := 0; x < bw; x += 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(srow[x : x+16])).ExtendToUint16().BitsToInt16()
			b := archsimd.LoadUint8x16Array((*[16]byte)(drow[x : x+16])).ExtendToUint16().BitsToInt16()
			if !a.Sub(b).NotEqual(vd).ToInt16x16().IsZero() {
				archsimd.ClearAVXUpperBits()
				return BlockYrdResult{}, false
			}
		}
	}
	archsimd.ClearAVXUpperBits()
	n := 4 << uint(tx)
	dc := int(delta) * 64
	if n == 16 {
		dc *= 2
	}
	if dc < 0 {
		dc = -dc
	}
	r := 48 * int(d[0]) >> 7
	q := 65536 / int(d[0])
	level := min(dc+r, 32767) * q >> 16
	diff := dc - level*int(d[0])
	units := bw * bh / (n * n)
	res := BlockYrdResult{Valid: true, Skippable: level == 0}
	if sse < BlockYrdUnknownSSE {
		res.SSE = int64(sse << 4)
		if level == 0 {
			res.Dist = res.SSE
			return res, true
		}
	}
	res.Rate = (level*units)<<(2+VP9ProbCostShift) + units<<VP9ProbCostShift
	res.Dist = int64((diff*diff)>>2) * int64(units)
	return res, true
}
