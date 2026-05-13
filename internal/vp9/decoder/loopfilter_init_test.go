package decoder

import "testing"

// expectedThresh mirrors libvpx's update_sharpness exactly so the
// test can compare against it side-by-side.
func expectedThresh(lvl, sharpness int) (lim, mblim uint8) {
	shift := 0
	if sharpness > 0 {
		shift++
	}
	if sharpness > 4 {
		shift++
	}
	bi := lvl >> uint(shift)
	if sharpness > 0 && bi > 9-sharpness {
		bi = 9 - sharpness
	}
	if bi < 1 {
		bi = 1
	}
	return uint8(bi), uint8(2*(lvl+2) + bi)
}

// TestLoopFilterInitSeedsLfthr walks every (level, sharpness) pair
// in the canonical range and confirms the (lim, mblim, hev_thr)
// triple matches libvpx's update_sharpness output.
func TestLoopFilterInitSeedsLfthr(t *testing.T) {
	for _, sharp := range []int{0, 1, 4, 5, 7} {
		lfi := NewLoopFilterInfoN()
		LoopFilterInit(&lfi, sharp)
		for lvl := 0; lvl <= MaxLoopFilter; lvl++ {
			wantLim, wantMblim := expectedThresh(lvl, sharp)
			if lfi.Lfthr[lvl].Lim != wantLim {
				t.Errorf("sharp=%d lvl=%d Lim=%d want %d", sharp, lvl, lfi.Lfthr[lvl].Lim, wantLim)
			}
			if lfi.Lfthr[lvl].Mblim != wantMblim {
				t.Errorf("sharp=%d lvl=%d Mblim=%d want %d", sharp, lvl, lfi.Lfthr[lvl].Mblim, wantMblim)
			}
			if lfi.Lfthr[lvl].HevThr != uint8(lvl>>4) {
				t.Errorf("sharp=%d lvl=%d HevThr=%d want %d", sharp, lvl, lfi.Lfthr[lvl].HevThr, lvl>>4)
			}
		}
	}
}

// TestLoopFilterFrameInitDefaultNoDeltas: no mode/ref delta, no
// segment override → every Lvl[seg][ref][mode] equals defaultFiltLvl.
func TestLoopFilterFrameInitDefaultNoDeltas(t *testing.T) {
	lfi := NewLoopFilterInfoN()
	lf := &LoopfilterParams{}
	seg := &SegmentationParams{}
	LoopFilterFrameInit(&lfi, lf, seg, 25)
	for s := 0; s < MaxSegments; s++ {
		for r := 0; r < MaxRefFrames; r++ {
			for m := 0; m < MaxModeLfDeltas; m++ {
				if lfi.Lvl[s][r][m] != 25 {
					t.Errorf("Lvl[%d][%d][%d]=%d want 25", s, r, m, lfi.Lvl[s][r][m])
				}
			}
		}
	}
}

// TestLoopFilterFrameInitWithRefDelta exercises the mode_ref_delta
// path: intra slot picks lvlSeg + ref_deltas[INTRA]*scale, inter
// slots pick lvlSeg + ref_deltas[ref]*scale + mode_deltas[mode]*scale.
func TestLoopFilterFrameInitWithRefDelta(t *testing.T) {
	lfi := NewLoopFilterInfoN()
	lf := &LoopfilterParams{
		ModeRefDeltaEnabled: true,
		RefDeltas:           [MaxRefLfDeltas]int8{5, -3, 7, -1},
		ModeDeltas:          [MaxModeLfDeltas]int8{2, -4},
	}
	seg := &SegmentationParams{}
	// defaultFiltLvl = 40 → scale = 1 << (40>>5) = 1 << 1 = 2.
	LoopFilterFrameInit(&lfi, lf, seg, 40)

	// Intra: 40 + 5*2 = 50.
	if got := lfi.Lvl[0][IntraFrame][0]; got != 50 {
		t.Errorf("intra slot got %d want 50", got)
	}
	// Inter LastFrame mode 0: 40 + (-3)*2 + 2*2 = 38.
	if got := lfi.Lvl[0][LastFrame][0]; got != 38 {
		t.Errorf("inter L mode 0 got %d want 38", got)
	}
	// Inter LastFrame mode 1: 40 + (-3)*2 + (-4)*2 = 26.
	if got := lfi.Lvl[0][LastFrame][1]; got != 26 {
		t.Errorf("inter L mode 1 got %d want 26", got)
	}
	// Clamp at MaxLoopFilter=63: AltrefFrame ref delta -1, mode delta 2.
	// 40 + (-1)*2 + 2*2 = 42 (in-range).
	if got := lfi.Lvl[0][AltrefFrame][0]; got != 42 {
		t.Errorf("inter Altref mode 0 got %d want 42", got)
	}
}

// TestLoopFilterFrameInitSegAltLf: SEG_LVL_ALT_LF override replaces
// (AbsDelta) or offsets (delta) the per-segment base level.
func TestLoopFilterFrameInitSegAltLf(t *testing.T) {
	lfi := NewLoopFilterInfoN()
	lf := &LoopfilterParams{}
	seg := &SegmentationParams{Enabled: true}
	seg.FeatureMask[3] = 1 << SegLvlAltLf
	seg.FeatureData[3][SegLvlAltLf] = 10
	// AbsDelta=false → seg 3 base = 25 + 10 = 35.
	LoopFilterFrameInit(&lfi, lf, seg, 25)
	if got := lfi.Lvl[3][0][0]; got != 35 {
		t.Errorf("seg=3 delta got %d want 35", got)
	}
	if got := lfi.Lvl[0][0][0]; got != 25 {
		t.Errorf("seg=0 base got %d want 25", got)
	}
	// AbsDelta=true → seg 3 base = 10 (the data is the new lvl).
	seg.AbsDelta = true
	LoopFilterFrameInit(&lfi, lf, seg, 25)
	if got := lfi.Lvl[3][0][0]; got != 10 {
		t.Errorf("seg=3 absdata got %d want 10", got)
	}
}

// TestLoopFilterFrameInitClamps confirms that the final Lvl never
// escapes [0, MaxLoopFilter] even when the deltas push the sum out
// of range.
func TestLoopFilterFrameInitClamps(t *testing.T) {
	lfi := NewLoopFilterInfoN()
	lf := &LoopfilterParams{
		ModeRefDeltaEnabled: true,
		RefDeltas:           [MaxRefLfDeltas]int8{0, 30, -30, 0},
		ModeDeltas:          [MaxModeLfDeltas]int8{0, 0},
	}
	seg := &SegmentationParams{}
	// scale=2 → +60 / -60 deltas; with base 40 the inter LAST is
	// 40 + 30*2 = 100 → clamped to 63.
	LoopFilterFrameInit(&lfi, lf, seg, 40)
	if got := lfi.Lvl[0][LastFrame][0]; got != MaxLoopFilter {
		t.Errorf("upper clamp got %d want %d", got, MaxLoopFilter)
	}
	// 40 + (-30)*2 = -20 → clamped to 0.
	if got := lfi.Lvl[0][GoldenFrame][0]; got != 0 {
		t.Errorf("lower clamp got %d", got)
	}
}
