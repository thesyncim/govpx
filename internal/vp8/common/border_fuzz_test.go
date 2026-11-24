package common

import (
	"testing"
)

// FuzzExtendBorders drives the libvpx-ported frame-border extend helper with
// random small frame dimensions, border widths, and fill bytes drawn from the
// fuzz []byte. Each iteration:
//
//  1. allocates a FrameBuffer of (width, height, border, align) chosen by the
//     fuzz bytes from a small whitelist (real production sizes are 4–32 for
//     border, 16-aligned for align — we keep the same shape so we stress the
//     same code paths libvpx exercises);
//  2. populates the visible Y/U/V coded region with deterministic samples;
//  3. calls ExtendBorders;
//  4. checks that every left-border cell on row r equals the visible
//     column-0 sample on the same row (and analogously for right/top/bottom).
//
// This is the libvpx algorithm verbatim: vp8_yv12_extend_frame_borders_c in
// libvpx v1.16.0 vpx_scale/generic/yv12extend.c replicates the edge sample.
// We rely on Go's bounds-checked slice writes (and the project's -race build)
// to catch out-of-bounds stores; the byte-level checks here pin the algorithm.
func FuzzExtendBorders(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		// (w=16, h=16, border=4, align=16, kind=0)
		{0, 0, 0, 0, 0},
		// (w=17, h=17, border=8, align=16, kind=1) — odd dimensions hit the
		// libvpx ExtendBordersFromVisible path through the visible-vs-coded
		// gap.
		{1, 1, 1, 0, 1},
		// Larger frame + max border in the whitelist.
		{2, 2, 2, 0, 0},
		// Random byte mix.
		{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	dims := []struct{ w, h int }{
		{16, 16},
		{17, 17},
		{32, 16},
		{31, 9},
		{48, 33},
	}
	borders := []int{4, 8, 16, 32}
	aligns := []int{8, 16}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("extend-borders fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		// Draw config bytes; missing bytes degenerate to 0.
		pick := func(i, n int) int {
			if i >= len(data) {
				return 0
			}
			return int(data[i]) % n
		}
		dim := dims[pick(0, len(dims))]
		border := borders[pick(1, len(borders))]
		align := aligns[pick(2, len(aligns))]
		fromVisible := false
		if pick(4, 2) == 1 {
			fromVisible = true
		}

		fb, err := NewFrameBuffer(dim.w, dim.h, border, align)
		if err != nil {
			// Some combos are out of range; not interesting here.
			return
		}

		// Populate the visible region with a deterministic pattern that depends
		// on (row, col, plane) so an off-by-one between the visible-edge column
		// and the border fill surfaces as a mismatch.
		populate := func(plane []byte, stride, width, height int, salt byte) {
			for y := range height {
				row := plane[y*stride : y*stride+width]
				for x := range width {
					row[x] = byte((y*7+x*11)&0xff) ^ salt
				}
			}
		}
		populate(fb.Img.Y, fb.Img.YStride, fb.Img.Width, fb.Img.Height, 0x11)
		uvW := (fb.Img.Width + 1) >> 1
		uvH := (fb.Img.Height + 1) >> 1
		populate(fb.Img.U, fb.Img.UStride, uvW, uvH, 0x22)
		populate(fb.Img.V, fb.Img.VStride, uvW, uvH, 0x33)

		if fromVisible {
			fb.ExtendBordersFromVisible()
		} else {
			// ExtendBorders extends from coded dims, not visible. Fill the
			// coded-but-not-visible padding rows/cols with their visible-edge
			// values first so the reference math matches the algorithm
			// (libvpx vpx_scale/generic/yv12extend.c uses coded dims for the
			// plain `extend_frame_borders` entry point).
			referenceCodedFill(fb)
			fb.ExtendBorders()
		}

		// Build a reference image from the visible region using the libvpx
		// algorithm (replicate the edge pixel) and compare to the actual
		// frame-buffer contents. We only check a few diagnostic cells per
		// plane to keep the fuzz iteration cheap; the byte checks here are
		// load-bearing because Go's bounds check only catches OOB writes,
		// not algorithm regressions.
		checkExtendedPlane(t, "Y", fb, fb.yPlaneOff, fb.uPlaneOff, fb.Img.YStride, fromVisible, fb.Img.Width, fb.Img.Height, fb.Img.CodedWidth, fb.Img.CodedHeight, fb.border)
		uvBorder := (fb.border + 1) >> 1
		codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
		codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
		checkExtendedPlane(t, "U", fb, fb.uPlaneOff, fb.vPlaneOff, fb.Img.UStride, fromVisible, uvW, uvH, codedUVWidth, codedUVHeight, uvBorder)
		checkExtendedPlane(t, "V", fb, fb.vPlaneOff, len(fb.buf), fb.Img.VStride, fromVisible, uvW, uvH, codedUVWidth, codedUVHeight, uvBorder)
	})
}

// referenceCodedFill replicates the visible edge into the coded-but-padded
// area before ExtendBorders is called. ExtendBorders extends from the coded
// dimensions, so the harness needs the coded-edge sample to equal the
// visible-edge sample; otherwise the algorithm would be checked against a
// gap that libvpx fills at a different point in the pipeline.
func referenceCodedFill(fb *FrameBuffer) {
	fillCodedRegion(fb.Img.YFull, fb.Img.YStride, fb.Img.Width, fb.Img.Height, fb.Img.CodedWidth, fb.Img.CodedHeight, fb.border, fb.border)
	uvBorder := (fb.border + 1) >> 1
	uvWidth := (fb.Img.Width + 1) >> 1
	uvHeight := (fb.Img.Height + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
	fillCodedRegion(fb.Img.UFull, fb.Img.UStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight, uvBorder, uvBorder)
	fillCodedRegion(fb.Img.VFull, fb.Img.VStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight, uvBorder, uvBorder)
}

func fillCodedRegion(plane []byte, stride, visW, visH, codedW, codedH, top, left int) {
	// Right padding cells on each visible row.
	for y := range visH {
		row := plane[(top+y)*stride:]
		edge := row[left+visW-1]
		for x := visW; x < codedW; x++ {
			row[left+x] = edge
		}
	}
	// Bottom padding rows replicate the last visible row.
	if visH < codedH {
		lastVisible := plane[(top+visH-1)*stride : (top+visH-1)*stride+left+codedW]
		for y := visH; y < codedH; y++ {
			dst := plane[(top+y)*stride : (top+y)*stride+left+codedW]
			copy(dst, lastVisible)
		}
	}
}

// checkExtendedPlane verifies the algorithm-level invariant: every cell in
// the extended border equals the visible-edge sample on the same row (for
// left/right) or the visible-edge row (for top/bottom). It probes the
// extreme corners and the midpoints; complete coverage would be O(N²) and is
// unnecessary because the algorithm is a simple replicate-edge.
func checkExtendedPlane(t *testing.T, name string, fb *FrameBuffer, planeStart, planeEnd, stride int, fromVisible bool, visW, visH, codedW, codedH, border int) {
	t.Helper()
	plane := fb.buf[planeStart:planeEnd]
	// In the `fromVisible` path the right/bottom border extends from the
	// visible edge straight through the coded padding to the border; the
	// algorithm therefore looks at width=visW. In the coded path the
	// reference assumes the visible-edge sample has been replicated to the
	// coded edge (see referenceCodedFill above), so both paths converge.
	w := visW
	h := visH
	if !fromVisible {
		w = codedW
		h = codedH
	}

	probeRows := []int{0, h / 2, h - 1}
	probeCols := []int{0, w / 2, w - 1}

	for _, r := range probeRows {
		if r < 0 {
			continue
		}
		row := (border + r) * stride
		edgeLeft := plane[row+border]
		edgeRight := plane[row+border+w-1]
		// Left border cells: index 0..border-1 must equal edgeLeft.
		for b := range border {
			if got := plane[row+b]; got != edgeLeft {
				t.Fatalf("%s left border row=%d col=%d got=%d want=%d (visW=%d visH=%d codedW=%d codedH=%d border=%d fromVisible=%v)",
					name, r, b, got, edgeLeft, visW, visH, codedW, codedH, border, fromVisible)
			}
		}
		// Right border cells: border+w..border+w+border-1 must equal edgeRight.
		// In the fromVisible path the right extension width is border +
		// (codedW - visW); checkExtendedPlane is parameterized for both.
		rightExt := border
		if fromVisible {
			rightExt = border + (codedW - visW)
		}
		for b := 0; b < rightExt; b++ {
			col := border + w + b
			if got := plane[row+col]; got != edgeRight {
				t.Fatalf("%s right border row=%d col=%d got=%d want=%d (visW=%d codedW=%d border=%d fromVisible=%v)",
					name, r, col, got, edgeRight, visW, codedW, border, fromVisible)
			}
		}
	}
	// Top/bottom borders: every cell on row b (0..border-1) must equal the
	// visible-edge row (row=border) on the same column. We sample the column
	// midpoints to keep the fuzz cheap.
	for _, c := range probeCols {
		if c < 0 {
			continue
		}
		visibleRow := border * stride
		col := border + c
		topVal := plane[visibleRow+col]
		for b := range border {
			if got := plane[b*stride+col]; got != topVal {
				t.Fatalf("%s top border row=%d col=%d got=%d want=%d (border=%d fromVisible=%v)",
					name, b, col, got, topVal, border, fromVisible)
			}
		}
		bottomExt := border
		if fromVisible {
			bottomExt = border + (codedH - visH)
		}
		bottomVisible := plane[(border+h-1)*stride+col]
		for b := 0; b < bottomExt; b++ {
			row := border + h + b
			if got := plane[row*stride+col]; got != bottomVisible {
				t.Fatalf("%s bottom border row=%d col=%d got=%d want=%d (border=%d fromVisible=%v)",
					name, row, col, got, bottomVisible, border, fromVisible)
			}
		}
	}
}
