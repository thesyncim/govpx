package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"image"
	"testing"
)

var mlCacheNativeSink common.BlockSize
var mlCacheNativeReady bool

func BenchmarkMLPartitionAndReuseNative(b *testing.B) {
	for _, bs := range []common.BlockSize{common.Block64x64, common.Block32x32, common.Block16x16} {
		b.Run(fmt.Sprintf("block%d", 4*int(common.Num4x4BlocksWideLookup[bs])), func(b *testing.B) {
			var e VP9Encoder
			e.sf.PartitionSearchType = MlBasedPartition
			e.sf.ReuseInterPredSby = 1
			e.sf.UseSquarePartitionOnly = 1
			e.vp9ResetMLPartitionCache(16, 16)
			ctx := &e.mlPartitionCtx[0]
			src := make([]byte, 64*64)
			for i := range src {
				src[i] = byte(i * 13)
				ctx.estPred[i] = byte(i * 7)
			}
			ctx.src, ctx.srcStride, ctx.baseQindex, ctx.speed = src, 64, 128, 8
			ctx.ready, ctx.frameValid = true, true
			var img image.YCbCr
			var dq vp9dec.DequantTables
			inter := vp9InterEncodeState{img: &img, dq: &dq}
			b.ReportAllocs()
			for b.Loop() {
				// One immutable classifier input per frame's SB; the partition walk
				// and the subsequent pickmode predictor-reuse query both consume it.
				e.vp9ResetMLPartitionCache(16, 16)
				ctx.frameValid = true
				mlCacheNativeSink, _ = e.vp9NonrdPickPartition(ctx, 16, 16, 0, 0, bs)
				mlCacheNativeReady = e.vp9NonrdReuseInterPredReady(&inter, 16, 16, 0, 0, bs, true)
			}
		})
	}
}
