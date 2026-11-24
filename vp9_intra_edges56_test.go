package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
	"testing"
)

// Frozen c4a8bd9f gather implementation, before the edge-read specialization.
func (e *VP9Encoder) predictVP9KeyframeTxScratchLiveFrozen56(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
	dstData []byte, dstStride int, dstOriginX, dstOriginY int,
	refData []byte, refStride int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	stride = dstStride
	if hdr == nil || pd == nil || dstStride <= 0 || len(dstData) == 0 ||
		refStride <= 0 || len(refData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, 0, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, 0, 0, false
	}
	rows := len(dstData) / dstStride
	refRows := len(refData) / refStride
	alignedWidth := buffers.Align(int(hdr.Width), 8)
	alignedHeight := buffers.Align(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	originX := dstOriginX >> pd.SubsamplingX
	originY := dstOriginY >> pd.SubsamplingY
	localX := x0 - originX
	localY := y0 - originY

	bs := 4 << uint(txSize)
	if localX < 0 || localY < 0 || localX+bs > dstStride || localY+bs > rows {
		return nil, 0, 0, 0, false
	}

	readEdge := func(px, py int) uint8 {
		if lx, ly := px-originX, py-originY; lx >= 0 && ly >= 0 &&
			lx < dstStride && ly < rows {
			return dstData[ly*dstStride+lx]
		}
		if py < 0 {
			py = 0
		} else if py >= refRows {
			py = refRows - 1
		}
		if px < 0 {
			px = 0
		} else if px >= refStride {
			px = refStride - 1
		}
		return refData[py*refStride+px]
	}

	bounds := vp9dec.BlockBoundsEdgesForMI(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = readEdge(x0-1, sy)
		}
	}

	upAvailable := blockRow4x4 != 0 || miRow > 0
	above := e.intraScratch.Above[1 : 1+2*bs]
	aboveLeft := uint8(127)
	if upAvailable {
		for i := range 2 * bs {
			above[i] = readEdge(x0+i, y0-1)
		}
		if leftAvailable {
			aboveLeft = readEdge(x0-1, y0-1)
		}
	}

	planeBlock4x4W := vp9IntraPredictWidth4x4(bsize, planeBsize, pd)
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
	dst = dstData[localY*dstStride+localX:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      dstStride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          vp9dec.IntraEdgeRefs{Above: above, AboveLeft: aboveLeft, Left: left},
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)
	return dst, dstStride, x0, y0, true
}

func TestVP9ScratchLiveEdgeReads56(t *testing.T) {
	e, wantE := new(VP9Encoder), new(VP9Encoder)
	pd := vp9dec.MacroblockdPlane{}
	for _, dims := range [][2]int{{128, 128}, {130, 122}} {
		hdr := vp9dec.UncompressedHeader{Width: uint32(dims[0]), Height: uint32(dims[1])}
		miRows, miCols := (dims[1]+7)/8, (dims[0]+7)/8
		ref := make([]byte, 176*176)
		for i := range ref {
			ref[i] = byte(i*23 + i/17)
		}
		savedRef := append([]byte(nil), ref...)
		for _, stride := range []int{64, 80} {
			for _, origin := range [][2]int{{0, 0}, {8, 8}, {16, 16}} {
				for _, tx := range []common.TxSize{common.Tx4x4, common.Tx8x8, common.Tx16x16, common.Tx32x32} {
					n := 4 << uint(tx)
					for _, pos := range [][2]int{{0, 0}, {16, 16}, {64 - n, 64 - n}} {
						for mode := common.DcPred; int(mode) < common.IntraModes; mode++ {
							got, want := make([]byte, stride*64+13), make([]byte, stride*64+13)
							for i := range got {
								got[i] = byte(i*7 + i/31)
								want[i] = got[i]
							}
							tile := vp9dec.TileBounds{MiRowEnd: miRows, MiColEnd: miCols}
							g, gs, gx, gy, gok := e.predictVP9KeyframeTxScratchLive(&hdr, &pd, mode, tx, tile, miRows, miCols, origin[0], origin[1], common.Block64x64, pos[0]/4, pos[1]/4, got[:stride*64], stride, origin[1]*8, origin[0]*8, ref, 176)
							w, ws, wx, wy, wok := wantE.predictVP9KeyframeTxScratchLiveFrozen56(&hdr, &pd, mode, tx, tile, miRows, miCols, origin[0], origin[1], common.Block64x64, pos[0]/4, pos[1]/4, want[:stride*64], stride, origin[1]*8, origin[0]*8, ref, 176)
							if gok != wok || gs != ws || gx != wx || gy != wy || len(g) != len(w) || !bytes.Equal(got, want) {
								t.Fatalf("dims=%v stride=%d origin=%v tx=%d pos=%v mode=%d outputs differ", dims, stride, origin, tx, pos, mode)
							}
							if !bytes.Equal(ref, savedRef) {
								t.Fatal("reference mutated")
							}
						}
					}
				}
			}
		}
	}
}

func BenchmarkVP9ScratchLiveEdges56(b *testing.B) {
	for _, tx := range []common.TxSize{common.Tx8x8, common.Tx16x16} {
		for _, mode := range []common.PredictionMode{common.DcPred, common.VPred, common.HPred, common.TmPred, common.D45Pred} {
			for _, interior := range []bool{false, true} {
				b.Run(fmt.Sprintf("tx%d/mode%d/interior%t", 4<<uint(tx), mode, interior), func(b *testing.B) {
					e := new(VP9Encoder)
					hdr := vp9dec.UncompressedHeader{Width: 128, Height: 128}
					pd := vp9dec.MacroblockdPlane{}
					ref := make([]byte, 160*160)
					var dst [4096]byte
					for i := range ref {
						ref[i] = byte(i*23 + i/17)
					}
					for i := range dst {
						dst[i] = byte(i*7 + i/31)
					}
					tile := vp9dec.TileBounds{MiRowEnd: 16, MiColEnd: 16}
					off := 0
					if interior {
						off = 4
					}
					b.ReportAllocs()
					for b.Loop() {
						_, _, _, _, ok := e.predictVP9KeyframeTxScratchLive(&hdr, &pd, mode, tx, tile, 16, 16, 8, 8, common.Block64x64, off, off, dst[:], 64, 64, 64, ref, 160)
						if !ok {
							b.Fatal("invalid")
						}
					}
				})
			}
		}
	}
}

func BenchmarkVP9ScratchMissingEdges56(b *testing.B) {
	for _, tx := range []common.TxSize{common.Tx8x8, common.Tx16x16} {
		for _, origin := range [][2]int{{0, 0}, {0, 8}, {8, 0}} {
			b.Run(fmt.Sprintf("tx%d/origin%d-%d", 4<<uint(tx), origin[0], origin[1]), func(b *testing.B) {
				e := new(VP9Encoder)
				hdr := vp9dec.UncompressedHeader{Width: 128, Height: 128}
				pd := vp9dec.MacroblockdPlane{}
				ref := make([]byte, 160*160)
				var dst [4096]byte
				for i := range ref {
					ref[i] = byte(i*23 + i/17)
				}
				for i := range dst {
					dst[i] = byte(i*7 + i/31)
				}
				tile := vp9dec.TileBounds{MiRowEnd: 16, MiColEnd: 16}
				b.ReportAllocs()
				for b.Loop() {
					_, _, _, _, ok := e.predictVP9KeyframeTxScratchLive(&hdr, &pd, common.DcPred, tx, tile, 16, 16, origin[0], origin[1], common.Block64x64, 0, 0, dst[:], 64, origin[1]*8, origin[0]*8, ref, 160)
					if !ok {
						b.Fatal("invalid")
					}
				}
			})
		}
	}
}
