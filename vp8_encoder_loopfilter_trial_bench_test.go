package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// BenchmarkVP8LoopFilterPickTrial720p pins the production-geometry cost of one
// loop-filter picker trial at the canonical 1280x720 realtime spot, split by
// sub-phase. The full-trial shape (full-plane luma copy + full-frame luma
// filter + full-frame SSE) is what realtime cpu-used=8 actually runs on the
// canonical 720p fixture: pinned auto-Speed 4 selects libvpx's
// vp8cx_pick_filter_level (sf->auto_filter=1), not the partial fast picker.
//
// 2026-07-16 diagnosis pin: the connected 480-frame phase numbers
// (copy 19.98us / filter 323.5us / sse 33.0us per trial) match this
// cache-warm benchmark (copy ~18.5us / filter ~330us / sse ~56us), and the
// V16 inner-edge kernel's in-situ per-call cost from the CPU profile (33ns)
// matches its hot-buffer microbench. Any future re-appearance of a
// wall-vs-CPU gap in the lf-pick phase should be adjudicated against these
// numbers before assuming memory stalls.
func BenchmarkVP8LoopFilterPickTrial720p(b *testing.B) {
	const width, height = 1280, 720
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}
	e := newSizedTestEncoder(b, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.NearestMV,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}
	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})

	startRow, rowCount := vp8enc.LoopFilterPartialFrameWindow(rows)

	b.Run("full_trial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEFull(40)
		}
	})
	b.Run("full_copy_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
		}
	})
	b.Run("full_filter_only", func(b *testing.B) {
		vp8common.CopyImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(&e.loopFilterPick.Img, rows, cols, ctx.modes, ctx.frameType, ctx.filterType, 40, ctx.fullFrameConfig, &e.loopInfo)
		}
	})
	b.Run("full_sse_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vp8enc.LoopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, rows, cols, false)
		}
	})
	// The partial fast-picker shape (speed==3 || speed>4) for reference; the
	// canonical 720p realtime cpu8 spot does not exercise it because pinned
	// auto-Speed holds at 4 there.
	b.Run("partial_trial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEPartial(40)
		}
	})
	b.Run("partial_filter_only", func(b *testing.B) {
		vp8enc.CopyLoopFilterPartialLuma(&e.loopFilterPick.Img, &e.analysis.Img, startRow, rowCount)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vp8dec.ApplyLoopFilterPartialConfiguredUnchecked(&e.loopFilterPick.Img, rows, cols, ctx.modes, ctx.frameType, ctx.filterType, 40, ctx.fastFrameConfig, &e.loopInfo, startRow, rowCount)
		}
	})
}
