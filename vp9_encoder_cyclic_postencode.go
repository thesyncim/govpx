package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9CyclicRefreshPostencodeFromMiGrid runs vp9_cyclic_refresh_postencode
// (vp9_aq_cyclicrefresh.c:261-317) on the encoded mi grid and may clear
// refresh_golden_frame before reference buffers are updated.
func (e *VP9Encoder) vp9CyclicRefreshPostencodeFromMiGrid(
	miRows, miCols int,
	header *vp9dec.UncompressedHeader,
	isKey, intraOnly bool,
) (cyclicForRC *encoder.CyclicRefreshState, clearGolden bool) {
	if e == nil || header == nil || isKey || intraOnly ||
		e.opts.AQMode != VP9AQCyclicRefresh ||
		!e.cyclicAQ.Enabled || !e.cyclicAQ.Apply ||
		!e.cyclicAQ.ContentMode || !header.Seg.Enabled {
		return nil, false
	}
	n := miRows * miCols
	if n <= 0 {
		return nil, false
	}
	isInter := buffersEnsureUint8(&e.cyclicPostIsInter, n)
	mvRow := buffersEnsureInt16(&e.cyclicPostMvRow, n)
	mvCol := buffersEnsureInt16(&e.cyclicPostMvCol, n)
	for miRow := range miRows {
		for miCol := range miCols {
			idx := miRow*miCols + miCol
			mi := e.vp9MiAt(miRows, miCols, miRow, miCol)
			if mi == nil {
				isInter[idx] = 0
				mvRow[idx] = 0
				mvCol[idx] = 0
				continue
			}
			ref := mi.RefFrame[0]
			if ref > vp9dec.IntraFrame {
				isInter[idx] = 1
				mvRow[idx] = mi.Mv[0].Row
				mvCol[idx] = mi.Mv[0].Col
			} else {
				isInter[idx] = 0
				mvRow[idx] = 0
				mvCol[idx] = 0
			}
		}
	}
	res := e.cyclicAQ.Postencode(encoder.CyclicRefreshPostencodeArgs{
		UseSVC:                      false,
		ExtRefreshFrameFlagsPending: e.extRefresh.flagsPending,
		GfCBRBoostPct:               e.opts.GFCBRBoostPct,
		ResizePending:               false,
		RefreshGoldenFrame:          header.RefreshFrameFlags&(1<<vp9GoldenRefSlot) != 0,
		FramesSinceKey:              int(e.rc.framesSinceKey),
		FramesSinceGolden:           int(e.rc.framesSinceGolden),
		IsInterBlock:                isInter,
		MvRow:                       mvRow,
		MvCol:                       mvCol,
	})
	return &e.cyclicAQ, res.ClearRefreshGolden
}

func buffersEnsureUint8(dst *[]uint8, n int) []uint8 {
	*dst = ensureLenZeroedUint8(*dst, n)
	return (*dst)[:n]
}

func buffersEnsureInt16(dst *[]int16, n int) []int16 {
	*dst = ensureLenZeroedInt16(*dst, n)
	return (*dst)[:n]
}

func ensureLenZeroedUint8(buf []uint8, n int) []uint8 {
	if cap(buf) < n {
		return make([]uint8, n)
	}
	out := buf[:n]
	for i := range out {
		out[i] = 0
	}
	return out
}

func ensureLenZeroedInt16(buf []int16, n int) []int16 {
	if cap(buf) < n {
		return make([]int16, n)
	}
	out := buf[:n]
	for i := range out {
		out[i] = 0
	}
	return out
}
