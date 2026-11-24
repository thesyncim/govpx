package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9MLPartitionClassifierCache(t *testing.T) {
	var e VP9Encoder
	e.vp9ResetMLPartitionCache(8, 8)
	src := make([]byte, 64*64)
	for frame := range 5 {
		e.vp9ResetMLPartitionCache(8, 8)
		ctx := &e.mlPartitionCtx[0]
		if ctx.predictValid != 0 {
			t.Fatal("classifier cache survived frame reset")
		}
		ctx.src, ctx.srcStride = src, 64
		ctx.baseQindex, ctx.speed = 32+frame*47, 4+frame
		ctx.ready, ctx.frameValid = true, true
		for i := range src {
			src[i] = byte(i*13 + frame*59)
			ctx.estPred[i] = byte(i*7 + frame*19)
		}
		for _, bs := range []common.BlockSize{common.Block64x64, common.Block32x32, common.Block16x16} {
			step := int(common.Num8x8BlocksWideLookup[bs])
			for r := 0; r < 8; r += step {
				for c := 0; c < 8; c += step {
					want := vp9MLPredictVarPartitioning(bs, r, c, ctx)
					for repeat := range 3 {
						if got := vp9MLPredictVarPartitioningCached(bs, r, c, ctx); got != want {
							t.Fatalf("frame=%d block=%d row=%d col=%d repeat=%d got=%d want=%d", frame, bs, r, c, repeat, got, want)
						}
						// The live prediction surface changes during mode search; it is not
						// an input to the classifier and must not invalidate the fixed result.
						for i := range ctx.pickPred {
							ctx.pickPred[i]++
						}
					}
				}
			}
		}
		if ctx.predictValid != (1<<21)-1 {
			t.Fatalf("cache keys = %#x, want every square node", ctx.predictValid)
		}
		mask := ctx.predictValid
		for _, tc := range []struct {
			bs   common.BlockSize
			r, c int
		}{{common.Block8x8, 0, 0}, {common.Block32x32, 1, 0}, {common.Block16x16, 1, 1}} {
			got := vp9MLPredictVarPartitioningCached(tc.bs, tc.r, tc.c, ctx)
			want := vp9MLPredictVarPartitioning(tc.bs, tc.r, tc.c, ctx)
			if got != want || ctx.predictValid != mask {
				t.Fatal("non-tree window changed classification/cache")
			}
		}
		ctx.ready = false
		if got := vp9MLPredictVarPartitioningCached(common.Block64x64, 0, 0, ctx); got != vp9MLPredictNone1 {
			t.Fatal("unready context returned cached classification")
		}
	}
}
