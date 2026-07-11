package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9BlockCoeffOffsetCompactsEveryTxLayout(t *testing.T) {
	for bsize := common.Block4x4; bsize < common.BlockSizes; bsize++ {
		w := int(common.Num4x4BlocksWideLookup[bsize])
		h := int(common.Num4x4BlocksHighLookup[bsize])
		for tx := common.Tx4x4; tx < common.TxSizes; tx++ {
			step := 1 << uint(tx)
			if w%step != 0 || h%step != 0 {
				continue
			}
			var used [vp9EncoderBlockCoeffSlots]bool
			maxEnd := 0
			for r := 0; r < h; r += step {
				for c := 0; c < w; c += step {
					off, n, ok := vp9BlockCoeffOffset(bsize, r, c, tx)
					if !ok {
						t.Fatalf("offset(%v, %d, %d, %v) returned !ok",
							bsize, r, c, tx)
					}
					if off+n > len(used) {
						t.Fatalf("offset(%v, %d, %d, %v) ends at %d, capacity %d",
							bsize, r, c, tx, off+n, len(used))
					}
					for i := off; i < off+n; i++ {
						if used[i] {
							t.Fatalf("offset(%v, %d, %d, %v) overlaps slot %d",
								bsize, r, c, tx, i)
						}
						used[i] = true
					}
					maxEnd = max(maxEnd, off+n)
				}
			}
			if want := w * h * 16; maxEnd != want {
				t.Fatalf("layout(%v, %v) end = %d, want %d",
					bsize, tx, maxEnd, want)
			}
		}
	}
}

func TestVP9BlockCoeffOffsetRejectsMisalignedTxOrigin(t *testing.T) {
	if _, _, ok := vp9BlockCoeffOffset(common.Block64x64, 1, 0,
		common.Tx8x8); ok {
		t.Fatal("misaligned tx row returned ok")
	}
	if _, _, ok := vp9BlockCoeffOffset(common.Block64x64, 0, 1,
		common.Tx8x8); ok {
		t.Fatal("misaligned tx column returned ok")
	}
}
