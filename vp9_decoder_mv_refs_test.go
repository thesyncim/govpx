package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/arith"
)

func vp9InterPredictorWithBorderForTest(src []byte, srcStride, srcWidth, srcHeight int,
	dst []byte, dstStride int,
	miRow, miCol int,
	bsize common.BlockSize,
	mv vp9dec.MV,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	bw := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	bh := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	miRows := (srcHeight + 7) >> 3
	miCols := (srcWidth + 7) >> 3
	edges := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(edges, mv, bw, bh, 0, 0)
	subpelX := int(mvQ4.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(mvQ4.Row) & (vp9dec.SubpelShifts - 1)
	srcX := miCol*common.MiSize + (int(mvQ4.Col) >> vp9dec.SubpelBitsConst)
	srcY := miRow*common.MiSize + (int(mvQ4.Row) >> vp9dec.SubpelBitsConst)
	predictSrc := src
	predictStride := srcStride
	predictOffset := srcY*srcStride + srcX
	if !vp9dec.InterPredictSourceInBounds(srcX, srcY, bw, bh,
		srcWidth, srcHeight, subpelX, subpelY) {
		left, right, top, bottom := vp9dec.InterPredictSourceMargins(subpelX, subpelY)
		extStride := bw + left + right
		extRows := bh + top + bottom
		var scratch [80 * 80]byte
		startX := srcX - left
		startY := srcY - top
		for y := range extRows {
			sy := arith.ClampInt(startY+y, 0, srcHeight-1)
			srcRow := src[sy*srcStride:]
			dstRow := scratch[y*extStride:]
			for x := range extStride {
				sx := arith.ClampInt(startX+x, 0, srcWidth-1)
				dstRow[x] = srcRow[sx]
			}
		}
		predictSrc = scratch[:extStride*extRows]
		predictStride = extStride
		predictOffset = top*extStride + left
	}
	vp9dec.InterPredictor(predictSrc, predictStride, dst, dstStride,
		subpelX, subpelY, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, bw, bh, 0,
		predictOffset)
}

func TestVP9DecoderReconstructsInterSkipEdgeFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 96, 96, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode edge keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterSkipFrameForTest(t, 96, 96)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode edge inter skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("edge inter skip frame did not publish output")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if len(d.miGrid) != miColsForSize(96)*miColsForSize(96) {
		t.Fatalf("miGrid len = %d, want full edge grid", len(d.miGrid))
	}
}

func TestVP9DecoderReconstructsInterSkipTiledFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode tiled keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode tiled inter skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("tiled inter skip frame did not publish output")
	}
	assertVP9NeutralFrame(t, frame, 1024, 64)
	if len(d.miGrid) != miColsForSize(1024)*miColsForSize(64) {
		t.Fatalf("miGrid len = %d, want full tiled grid", len(d.miGrid))
	}
}
