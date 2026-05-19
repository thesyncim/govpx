package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9DecoderFindsInterMvRefs(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	topRight := &d.miGrid[3*miCols+5]
	*topRight = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Col: 64}},
	}
	bottomLeft := &d.miGrid[5*miCols+3]
	*bottomLeft = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Col: -128}},
	}

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearMv, vp9dec.LastFrame,
		[vp9dec.MaxRefFrames]uint8{})
	if count != 2 {
		t.Fatalf("mv ref count = %d, want 2", count)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearestMv); got != (vp9dec.MV{Col: 64}) {
		t.Fatalf("nearest candidate = %+v, want col 64", got)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearMv); got != (vp9dec.MV{Col: -128}) {
		t.Fatalf("near candidate = %+v, want col -128", got)
	}
}

func TestVP9DecoderFindsDiffRefMvRefsWithSignBias(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	d.miGrid[3*miCols+5] = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Row: 16, Col: -32}},
	}
	var signBias [vp9dec.MaxRefFrames]uint8
	signBias[vp9dec.GoldenFrame] = 1

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearestMv, vp9dec.LastFrame,
		signBias)
	if count != 1 {
		t.Fatalf("diff-ref mv ref count = %d, want 1", count)
	}
	if got := refs[0]; got != (vp9dec.MV{Row: -16, Col: 32}) {
		t.Fatalf("diff-ref candidate = %+v, want sign-bias inverted", got)
	}
}

func TestVP9DecoderFindsCompoundInterMvRefs(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	topLeft := &d.miGrid[3*miCols+3]
	*topLeft = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		Mv:       [2]vp9dec.MV{{Col: 64}, {Col: 96}},
	}
	topRight := &d.miGrid[3*miCols+5]
	*topRight = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		Mv:       [2]vp9dec.MV{{Col: 128}, {Col: 160}},
	}

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearMv, vp9dec.AltrefFrame,
		[vp9dec.MaxRefFrames]uint8{})
	if count != 2 {
		t.Fatalf("compound mv ref count = %d, want 2", count)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearestMv); got != (vp9dec.MV{Col: 128}) {
		t.Fatalf("compound nearest candidate = %+v, want clamped ALTREF col 128", got)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearMv); got != (vp9dec.MV{Col: 96}) {
		t.Fatalf("compound near candidate = %+v, want ALTREF col 96", got)
	}
}

func TestVP9DecoderInterPredictSourceInBounds(t *testing.T) {
	if !vp9InterPredictSourceInBounds(32, 32, 32, 32, 96, 96, 8, 8) {
		t.Fatal("interior two-axis subpel window rejected")
	}
	if vp9InterPredictSourceInBounds(32, 32, 32, 32, 64, 64, 8, 0) {
		t.Fatal("right-edge horizontal subpel window accepted without border")
	}
	if vp9InterPredictSourceInBounds(0, 32, 32, 32, 96, 96, 8, 0) {
		t.Fatal("left-edge horizontal subpel window accepted without border")
	}
	if vp9InterPredictSourceInBounds(32, 0, 32, 32, 96, 96, 0, 8) {
		t.Fatal("top-edge vertical subpel window accepted without border")
	}
	if !vp9InterPredictSourceInBounds(0, 0, 32, 32, 32, 32, 0, 0) {
		t.Fatal("integer-pel exact window rejected")
	}
}

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
	edges := vp9dec.BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(edges, mv, bw, bh, 0, 0)
	subpelX := int(mvQ4.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(mvQ4.Row) & (vp9dec.SubpelShifts - 1)
	srcX := miCol*common.MiSize + (int(mvQ4.Col) >> vp9dec.SubpelBitsConst)
	srcY := miRow*common.MiSize + (int(mvQ4.Row) >> vp9dec.SubpelBitsConst)
	predictSrc := src
	predictStride := srcStride
	predictOffset := srcY*srcStride + srcX
	if !vp9InterPredictSourceInBounds(srcX, srcY, bw, bh,
		srcWidth, srcHeight, subpelX, subpelY) {
		left, right, top, bottom := vp9InterPredictSourceMargins(subpelX, subpelY)
		extStride := bw + left + right
		extRows := bh + top + bottom
		var scratch [80 * 80]byte
		startX := srcX - left
		startY := srcY - top
		for y := range extRows {
			sy := vp9ClampInt(startY+y, 0, srcHeight-1)
			srcRow := src[sy*srcStride:]
			dstRow := scratch[y*extStride:]
			for x := range extStride {
				sx := vp9ClampInt(startX+x, 0, srcWidth-1)
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
