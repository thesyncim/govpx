package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestPostLoopFilterExtendDivergesFromLibvpxOnOddAxisFrames documents the
// known divergence between govpx's post-loop-filter border extend and
// libvpx's `vp8_yv12_extend_frame_borders` for frames whose visible
// dimensions are not a multiple of 16. The test is intentionally a probe
// rather than an assertion of byte parity: it observes the divergence so
// that future agents converging the pipeline can find the affected sites
// quickly.
//
// libvpx reference (v1.16.0):
//
//   - vp8/encoder/onyx_if.c:3137-3213 — `vp8_loopfilter_frame`, which
//     unconditionally ends with `vp8_yv12_extend_frame_borders(cm->frame_to_show)`
//     at line 3212. The post-LF extend is therefore the publication step
//     for the new LAST/GOLDEN/ALTREF reference.
//   - vpx_scale/generic/yv12extend.c:105-128 — `vp8_yv12_extend_frame_borders_c`
//     calls `extend_plane` with width=`y_crop_width` and height=`y_crop_height`
//     (visible) and out-extents `border + (y_height - y_crop_height)` /
//     `border + (y_width - y_crop_width)` for the bottom/right side. This
//     overwrites the coded-but-invisible MB padding with the visible-edge
//     sample in a single pass.
//   - vp8/common/extend.c:75-101 — `vp8_copy_and_extend_frame`, the SOURCE-side
//     equivalent invoked by `vp8_lookahead_push` (lookahead.c:91-145) and
//     `vp8_set_reference` (onyx_if.c:2443-2462 via `vp8_yv12_copy_frame`).
//   - vp8/encoder/onyx_if.c:2944-2950 — `update_reference_frames`'s LAST
//     refresh: an index swap (`cm->lst_fb_idx = cm->new_fb_idx`) with no
//     additional extend. libvpx's single post-LF extend on `cm->frame_to_show`
//     is the only border-extend that lands on the new LAST.
//
// govpx implementation (this branch):
//
//   - `applyReconstructionLoopFilter` (encoder_loopfilter.go:758-796) ends
//     with `e.analysis.ExtendBorders()` — a SYMMETRIC extend from the coded
//     edge (16-aligned), NOT from the visible edge. The coded-but-invisible
//     MB-padded rows/cols retain the LF-modified reconstruction samples.
//   - `refreshInterFrameReferencesFromAnalysis` / `refreshKeyFrameReferencesFromAnalysis`
//     / `copyInterFrameReferences` (encoder_reference_buffers.go) copy
//     `e.analysis` to `e.current` / `e.lastRef` / `e.goldenRef` / `e.altRef`
//     and call `ExtendBorders()` again on each (still symmetric).
//   - `copySourceToFrameBuffer*` (encoder_source_buffer.go) and
//     `setReferenceFrameNow` (encoder_reference_controls.go) use
//     `padFrameVisibleToCoded` + `ExtendBorders`. The two-step is
//     byte-equivalent to libvpx's single-pass `vp8_copy_and_extend_frame`
//     (because pad-then-symmetric-extend produces the same visible-edge
//     replication everywhere outside the visible region).
//
// Why this matters for the parity gate:
//
// On 16-aligned frames Visible == Coded and ExtendBordersFromVisible
// collapses to ExtendBorders, so the divergence is invisible. On
// odd-axis frames (33x17, 640x360, 17x33) the post-LF buffer retains
// LF-filtered reconstruction samples in the coded-but-invisible rows
// where libvpx replaces them with replicated visible-edge samples.
// Inter prediction taps that reach past the visible edge therefore see
// different reference content.
//
// Attempts to converge the post-LF extend with libvpx in a single edit:
//
//   - Wiring `ExtendBordersFromVisible` at `applyReconstructionLoopFilter`
//     (the canonical libvpx mirror site) changes frame-2 bytes on the
//     named regression seed `regression_640x360_threads1_bitrate_setref_diverge`
//     from len=684 to len=662 (libvpx target len=688) — i.e. it moves
//     bytes WITHOUT closing the seed, and simultaneously breaks all 7
//     odd-axis 33x17/17x33 fixtures in `TestOracleEncoderStreamByteParity`.
//   - The same swap inside `refreshInterFrameReferencesFromAnalysis` /
//     `refreshKeyFrameReferencesFromAnalysis` produces the identical
//     net result (both wirings ultimately affect the buffer that
//     becomes LAST/GOLDEN/ALTREF the same way) — verified by agent
//     ac2d9ea1 (see task 0xac2d9ea137dbcf3b0 sidechain).
//   - The 33x17 fixtures pass coincidentally because govpx has
//     COMPENSATING workarounds downstream of the post-LF extend
//     (see encoder_inter_rate.go:655-666 `splitBlockSADBlock` and
//     encoder_inter_rate.go:711-720 `splitBlockSubpixelSADBlock`,
//     both of which clamp reference reads to the visible extent
//     mirroring libvpx's effective post-extend state without
//     actually overwriting the live reconstruction). Flipping the
//     post-LF extend to libvpx-faithful visible-extend AND removing
//     those clamps would be the correct single-shot port, but the
//     coupling between picker reads and reconstruction writes makes
//     a one-PR refactor risky.
//
// This test does not fail; it asserts only that:
//   - The `ExtendBordersFromVisible` helper exists and matches its
//     libvpx-faithful semantics on a 33x17-shaped frame.
//   - The two paths produce DIFFERENT byte sequences on the
//     coded-but-invisible region for a frame whose visible dimensions
//     are not 16-aligned. (If a future change made them coincide, the
//     downstream clamp workarounds could likely be removed.)
//
// Concrete future fix (if/when picker-side reads are aligned with
// libvpx's visible-extent semantics so the downstream clamps can be
// removed):
//
//  1. Replace `analysis.ExtendBorders()` at encoder_loopfilter.go:786
//     and :794 with `analysis.ExtendBordersFromVisible()`.
//  2. Mirror the same swap in
//     `refreshKeyFrameReferencesFromAnalysis` /
//     `refreshInterFrameReferencesFromAnalysis` /
//     `refreshZeroInterFrameReferences` / `copyInterFrameReferences`
//     so every per-buffer "ExtendBorders" after a `copyFrameImage`
//     becomes visible-extend.
//  3. Mirror the swap in `setReferenceFrameNow` and the source-side
//     `copySourceToFrameBuffer*` (replacing
//     `padFrameVisibleToCoded` + `ExtendBorders` with
//     `ExtendBordersFromVisible`; the two are byte-equivalent today so
//     this is purely a clarity edit, but it should land in the same
//     PR for source-vs-reference symmetry).
//  4. Re-evaluate the visible-clamp fast paths in
//     `splitBlockSADBlock` / `splitBlockSubpixelSADBlock`
//     (encoder_inter_rate.go) — once the live reference buffer
//     already reflects visible-extend, the clamps become identity
//     ops on inputs in the visible window and a libvpx-faithful
//     no-op overall.
func TestPostLoopFilterExtendDivergesFromLibvpxOnOddAxisFrames(t *testing.T) {
	const visibleW = 33
	const visibleH = 17
	const border = 32
	const align = 16

	fbCoded, err := vp8common.NewFrameBuffer(visibleW, visibleH, border, align)
	if err != nil {
		t.Fatalf("NewFrameBuffer(coded) returned error: %v", err)
	}
	fbVisible, err := vp8common.NewFrameBuffer(visibleW, visibleH, border, align)
	if err != nil {
		t.Fatalf("NewFrameBuffer(visible) returned error: %v", err)
	}

	// Populate the coded region (visible + MB-padded rows/cols) with a
	// deterministic pattern. Cells in [Width, CodedWidth) and rows in
	// [Height, CodedHeight) hold a sentinel that differs from the
	// visible-edge sample. After libvpx-faithful `ExtendBordersFromVisible`
	// these sentinels are overwritten by the visible-edge sample; after
	// the symmetric `ExtendBorders` they survive.
	visibleEdge := byte(0x55)
	codedPad := byte(0xAA)
	populate := func(fb *vp8common.FrameBuffer) {
		for y := range fb.Img.CodedHeight {
			for x := range fb.Img.CodedWidth {
				v := visibleEdge
				if x >= fb.Img.Width || y >= fb.Img.Height {
					v = codedPad
				}
				fb.Img.Y[y*fb.Img.YStride+x] = v
			}
		}
		uvWidth := (fb.Img.Width + 1) >> 1
		uvHeight := (fb.Img.Height + 1) >> 1
		codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
		codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
		for y := range codedUVHeight {
			for x := range codedUVWidth {
				v := visibleEdge
				if x >= uvWidth || y >= uvHeight {
					v = codedPad
				}
				fb.Img.U[y*fb.Img.UStride+x] = v
				fb.Img.V[y*fb.Img.VStride+x] = v
			}
		}
	}
	populate(fbCoded)
	populate(fbVisible)

	// govpx-current (encoder_loopfilter.go:794): symmetric extend from the
	// coded edge — preserves the 0xAA sentinel in the coded-but-invisible
	// region.
	fbCoded.ExtendBorders()

	// libvpx-faithful (vp8/encoder/onyx_if.c:3212 +
	// vpx_scale/generic/yv12extend.c:105-128): extend from the visible
	// edge — overwrites the 0xAA sentinel with the 0x55 visible-edge
	// sample.
	fbVisible.ExtendBordersFromVisible()

	// Check the coded-but-invisible cell at (Width, 0) — adjacent to the
	// visible right edge. govpx-current must hold codedPad; libvpx-
	// faithful must hold visibleEdge. (If these ever coincide the
	// downstream clamp workarounds in encoder_inter_rate.go can be
	// reconsidered.)
	codedCell := func(fb *vp8common.FrameBuffer) byte {
		// Img.Y starts at the first coded sample (yOff = border*stride +
		// border into the full buffer), so [0, ...] indexing reads coded
		// coordinates.
		return fb.Img.Y[0*fb.Img.YStride+fbCoded.Img.Width]
	}
	if got, want := codedCell(fbCoded), codedPad; got != want {
		t.Fatalf("symmetric ExtendBorders at coded[%d,0] = %#x, want %#x (sentinel preserved)", fbCoded.Img.Width, got, want)
	}
	if got, want := codedCell(fbVisible), visibleEdge; got != want {
		t.Fatalf("ExtendBordersFromVisible at coded[%d,0] = %#x, want %#x (visible-edge replicated)", fbVisible.Img.Width, got, want)
	}

	// Check a cell deep in the right border (col CodedWidth + border/2,
	// row 0). On the symmetric path this reads the codedPad replicated
	// from the last coded col (because that's the value at
	// [coded_w-1,0] for y in [Height, CodedHeight), and the bottom
	// rows weren't filled but the row-0 right border replicates row-0
	// col-CodedWidth-1 which IS visibleEdge here because row 0 is
	// visible — different cell). To make the contrast clear, probe
	// row CodedHeight-1 (a row entirely in the padded region) where
	// symmetric extend replicates the bottom-padded codedPad and
	// visible extend replicates the visible-row last sample
	// (visibleEdge).
	bottomBorderCell := func(fb *vp8common.FrameBuffer) byte {
		row := fb.Img.CodedHeight - 1
		return fb.Img.Y[row*fb.Img.YStride+fb.Img.Width+1]
	}
	if got, want := bottomBorderCell(fbCoded), codedPad; got != want {
		t.Fatalf("symmetric ExtendBorders at coded[Width+1,CodedHeight-1] = %#x, want %#x (sentinel replicated rightward)", got, want)
	}
	if got, want := bottomBorderCell(fbVisible), visibleEdge; got != want {
		t.Fatalf("ExtendBordersFromVisible at coded[Width+1,CodedHeight-1] = %#x, want %#x (visible-edge replicated bottom-rightward)", got, want)
	}
}
