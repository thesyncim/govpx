package govpx

import (
	"fmt"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestImprovedInterFrameSearchStartBorderModeInfoIndexing pins the sentinel
// behavior that libvpx's vp8_mv_pred and vp8_cal_sad rely on at the picture
// edges: out-of-frame above/left/above-left current-frame neighbors and
// out-of-frame above/left/right/below previous-frame neighbors must behave
// like libvpx's calloc-zeroed sentinel rows/columns (ref_frame == INTRA_FRAME,
// MV == 0, near_sad == INT_MAX). We sweep every position of a 3x3 macroblock
// grid and verify that the current-frame match path returns the expected
// neighbor MV and sr value across interior, edge, and corner positions.
func TestImprovedInterFrameSearchStartBorderModeInfoIndexingCurrentFrame(t *testing.T) {
	const mbRows, mbCols = 3, 3
	src := testImage(mbCols*16, mbRows*16)
	fillImage(src, 64, 90, 170)
	// Force a non-zero source pattern so neighbor SADs are deterministic.
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = byte((row*7 + col*11 + 23) & 0xff)
		}
	}

	// Use a uniform analysis frame so the SAD between src[srcRow,srcCol] and
	// analysis[refRow,refCol] is invariant in (refRow,refCol). That way the
	// SAD-sorted slot order is determined purely by stable insertion sort
	// over equal SADs, mirroring libvpx's tie-break order: above first, then
	// left, then above-left.
	analysis := testVP8Frame(t, src.Width, src.Height, 96, 90, 170)

	e := &VP8Encoder{analysis: analysis}
	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	// Build a 3x3 mode grid where every macroblock claims LastFrame with a
	// position-encoded MV. This lets us tell which neighbor was selected just
	// by the returned MV.
	modes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	for r := 0; r < mbRows; r++ {
		for c := 0; c < mbCols; c++ {
			modes[r*mbCols+c] = vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.LastFrame,
				Mode:     vp8common.NewMV,
				MV:       vp8enc.MotionVector{Row: int16(8 + 4*r), Col: int16(12 + 4*c)},
			}
		}
	}

	// With uniform analysis pixels every valid current-frame neighbor
	// produces the same SAD, so the SAD-sorted slot order is determined by
	// stable insertion sort. That keeps libvpx's slot order (above=0,
	// left=1, above-left=2) at the top of the sorted list and lets us assert
	// the libvpx "match in top-3" rule: first matching slot wins, sr = 3.
	type expected struct {
		mvIndex  int  // index into modes for the MB whose MV the predictor should return
		sr       int
		fallback bool // true => predictor must fall back to median, sr = 0
	}
	cases := []struct {
		name string
		row  int
		col  int
		want expected
	}{
		// (0,0): top-left corner. above/left/aboveLeft all sentinel. No
		// LastFrame neighbor exists in current frame -> median fallback.
		{"corner_top_left", 0, 0, expected{fallback: true}},
		// (0,1): top edge. above sentinel, left = (0,0), aboveLeft sentinel.
		// First (and only) LastFrame match: left at slot 1.
		{"edge_top_middle", 0, 1, expected{mvIndex: 0*mbCols + 0, sr: 3}},
		// (0,2): top-right corner. above sentinel, left = (0,1), aboveLeft
		// sentinel. Match: left at slot 1.
		{"corner_top_right", 0, 2, expected{mvIndex: 0*mbCols + 1, sr: 3}},
		// (1,0): left edge. above = (0,0), left sentinel, aboveLeft sentinel.
		// Match: above at slot 0.
		{"edge_left_middle", 1, 0, expected{mvIndex: 0*mbCols + 0, sr: 3}},
		// (1,1): interior. above = (0,1), left = (1,0), aboveLeft = (0,0).
		// All three slots are LastFrame, all SADs equal -> stable sort keeps
		// slot 0 (above) first.
		{"interior_center", 1, 1, expected{mvIndex: 0*mbCols + 1, sr: 3}},
		// (1,2): right edge. above = (0,2), left = (1,1), aboveLeft = (0,1).
		// Match: above at slot 0.
		{"edge_right_middle", 1, 2, expected{mvIndex: 0*mbCols + 2, sr: 3}},
		// (2,0): bottom-left corner. above = (1,0), left sentinel, aboveLeft
		// sentinel. Match: above at slot 0.
		{"corner_bottom_left", 2, 0, expected{mvIndex: 1*mbCols + 0, sr: 3}},
		// (2,1): bottom edge. above = (1,1), left = (2,0), aboveLeft = (1,0).
		// Match: above at slot 0.
		{"edge_bottom_middle", 2, 1, expected{mvIndex: 1*mbCols + 1, sr: 3}},
		// (2,2): bottom-right corner. above = (1,2), left = (2,1), aboveLeft
		// = (1,1). Match: above at slot 0.
		{"corner_bottom_right", 2, 2, expected{mvIndex: 1*mbCols + 2, sr: 3}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var above, left, aboveLeft *vp8enc.InterFrameMacroblockMode
			if tc.row > 0 {
				above = &modes[(tc.row-1)*mbCols+tc.col]
			}
			if tc.col > 0 {
				left = &modes[tc.row*mbCols+(tc.col-1)]
			}
			if tc.row > 0 && tc.col > 0 {
				aboveLeft = &modes[(tc.row-1)*mbCols+(tc.col-1)]
			}

			start := e.improvedInterFrameSearchStart(
				sourceImageFromPublic(src), vp8common.LastFrame,
				tc.row, tc.col, mbRows, mbCols,
				above, left, aboveLeft, search,
			)
			if !start.ok {
				t.Fatalf("expected predictor to return a start, got not-ok")
			}
			if tc.want.fallback {
				// At (0,0) every current-frame neighbor is sentinel and
				// last-frame data is disabled (no lastFrameInterModesValid),
				// so the predictor falls back to the median MV. With all
				// slots zero the median must be {0,0} and sr must be 0.
				if start.sr != 0 {
					t.Fatalf("fallback sr = %d, want 0", start.sr)
				}
				if start.mv != (vp8enc.MotionVector{}) {
					t.Fatalf("fallback mv = %+v, want zero", start.mv)
				}
				return
			}
			wantMV := modes[tc.want.mvIndex].MV
			if start.mv != wantMV {
				t.Fatalf("mv = %+v, want %+v from mode index %d", start.mv, wantMV, tc.want.mvIndex)
			}
			if start.sr != tc.want.sr {
				t.Fatalf("sr = %d, want %d", start.sr, tc.want.sr)
			}
		})
	}
}

// TestImprovedInterFrameSearchStartBorderModeInfoIndexingLastFrame verifies the
// previous-frame neighbor table at every position of a 3x3 macroblock grid.
// libvpx's lfmv/lf_ref_frame layout pads the previous-frame mode/MV grid with
// calloc-zeroed sentinel rows on top/bottom and sentinel columns on left/right.
// Out-of-range neighbors must therefore behave like INTRA_FRAME with mv == 0
// and near_sad == INT_MAX. govpx must drop those slots cleanly without
// overrunning the previous-frame mode array.
func TestImprovedInterFrameSearchStartBorderModeInfoIndexingLastFrame(t *testing.T) {
	const mbRows, mbCols = 3, 3
	width, height := mbCols*16, mbRows*16
	src := testImage(width, height)
	fillImage(src, 64, 90, 170)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			src.Y[row*src.YStride+col] = byte((row*5 + col*9 + 13) & 0xff)
		}
	}

	last := testVP8Frame(t, width, height, 0, 90, 170)
	// Copy src into lastRef so the lf-current SAD for every interior
	// macroblock evaluates to zero, sorting the lf-current slot to rank 0.
	for row := 0; row < height; row++ {
		copy(last.Img.Y[row*last.Img.YStride:row*last.Img.YStride+width], src.Y[row*src.YStride:row*src.YStride+width])
	}

	prevModes := make([]vp8enc.InterFrameMacroblockMode, mbRows*mbCols)
	for r := 0; r < mbRows; r++ {
		for c := 0; c < mbCols; c++ {
			// All previous-frame neighbors are GoldenFrame except the
			// lf-current slot at the test position, which we set per case.
			prevModes[r*mbCols+c] = vp8enc.InterFrameMacroblockMode{
				RefFrame: vp8common.GoldenFrame,
				Mode:     vp8common.NewMV,
				MV:       vp8enc.MotionVector{Row: int16(64 + r), Col: int16(64 + c)},
			}
		}
	}

	search := interAnalysisSearchConfig{improvedMVPrediction: true}

	for r := 0; r < mbRows; r++ {
		for c := 0; c < mbCols; c++ {
			row, col := r, c
			t.Run(fmt.Sprintf("row%d_col%d", row, col), func(t *testing.T) {
				modes := make([]vp8enc.InterFrameMacroblockMode, len(prevModes))
				copy(modes, prevModes)
				targetMV := vp8enc.MotionVector{Row: int16(80 + 8*row), Col: int16(80 + 8*col)}
				modes[row*mbCols+col] = vp8enc.InterFrameMacroblockMode{
					RefFrame: vp8common.LastFrame,
					Mode:     vp8common.NewMV,
					MV:       targetMV,
				}
				e := &VP8Encoder{
					lastRef:                  last,
					lastFrameInterModes:      modes,
					lastFrameInterModesValid: true,
				}
				start := e.improvedInterFrameSearchStart(
					sourceImageFromPublic(src), vp8common.LastFrame,
					row, col, mbRows, mbCols,
					nil, nil, nil, search,
				)
				if !start.ok {
					t.Fatalf("predictor returned not-ok at (%d,%d)", row, col)
				}
				if start.mv != targetMV {
					t.Fatalf("mv = %+v, want lf-current %+v", start.mv, targetMV)
				}
				// lf-current SAD is zero, sentinel slots are INT_MAX, and the
				// other (in-bounds) lf neighbors have non-zero SADs (different
				// MB pixel data). With all current-frame neighbors absent
				// (slots 0..2 sentinel = INT_MAX) the lf-current slot ranks 0
				// in SAD order, so libvpx's "i < 3 -> sr = 3" branch fires.
				if start.sr != 3 {
					t.Fatalf("sr = %d at (%d,%d), want 3 (top-3 SAD match)", start.sr, row, col)
				}
			})
		}
	}
}

// TestImprovedInterFrameSearchStartIgnoresStaleMVOnIntraNeighbor pins the
// libvpx behavior where an INTRA_FRAME current-frame neighbor contributes
// near_mvs[vcnt] = 0, regardless of any stale MV that may have been written
// to its mode entry. govpx must not let the intra neighbor's MV leak into
// the median fallback.
func TestImprovedInterFrameSearchStartIgnoresStaleMVOnIntraNeighbor(t *testing.T) {
	const mbRows, mbCols = 3, 3
	width, height := mbCols*16, mbRows*16
	src := testImage(width, height)
	fillImage(src, 32, 90, 170)
	analysis := testVP8Frame(t, width, height, 32, 90, 170)
	e := &VP8Encoder{analysis: analysis}

	// Above and left are intra. A stale (non-zero) MV on those entries
	// must NOT influence the median fallback; libvpx initializes
	// near_mvs[0..2] to 0 and only assigns when ref_frame != INTRA_FRAME.
	above := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.IntraFrame,
		Mode:     vp8common.DCPred,
		MV:       vp8enc.MotionVector{Row: 1234, Col: -5678},
	}
	left := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.IntraFrame,
		Mode:     vp8common.BPred,
		MV:       vp8enc.MotionVector{Row: -4321, Col: 8765},
	}
	aboveLeft := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.IntraFrame,
		Mode:     vp8common.DCPred,
		MV:       vp8enc.MotionVector{Row: 999, Col: -999},
	}
	search := interAnalysisSearchConfig{improvedMVPrediction: true}
	start := e.improvedInterFrameSearchStart(
		sourceImageFromPublic(src), vp8common.LastFrame,
		1, 1, mbRows, mbCols,
		&above, &left, &aboveLeft, search,
	)
	if !start.ok {
		t.Fatalf("predictor returned not-ok")
	}
	if start.sr != 0 {
		t.Fatalf("sr = %d, want 0 (median fallback when no neighbor matches)", start.sr)
	}
	if start.mv != (vp8enc.MotionVector{}) {
		t.Fatalf("median fallback mv = %+v, want zero (libvpx zeros intra slots before median)", start.mv)
	}
}
