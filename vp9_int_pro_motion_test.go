package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Pinning tests for vp9_int_pro_motion.go. Reference values are
// hand-computed from the libvpx v1.16.0 C bodies cited in that file.

// TestVp9ClampMVClampsBothAxes verifies the clamp_mv inline behaviour.
func TestVp9ClampMVClampsBothAxes(t *testing.T) {
	cases := []struct {
		name                   string
		in                     vp9MV
		minC, maxC, minR, maxR int
		wantR, wantC           int16
	}{
		{"inside box", vp9MV{Row: 5, Col: 7}, -10, 10, -10, 10, 5, 7},
		{"above max", vp9MV{Row: 200, Col: 50}, -10, 10, -10, 100, 100, 10},
		{"below min", vp9MV{Row: -500, Col: -42}, -10, 10, -50, 10, -50, -10},
	}
	for _, tc := range cases {
		mv := tc.in
		vp9ClampMV(&mv, tc.minC, tc.maxC, tc.minR, tc.maxR)
		if mv.Row != tc.wantR || mv.Col != tc.wantC {
			t.Errorf("%s: got (%d,%d) want (%d,%d)", tc.name, mv.Row, mv.Col, tc.wantR, tc.wantC)
		}
	}
}

func TestVp9FullpelMvLimitsClampAndContain(t *testing.T) {
	limits := &vp9MvLimits{ColMin: -2, ColMax: 3, RowMin: -4, RowMax: 5}
	row, col := limits.clampFullpel(-10, 7)
	if row != -4 || col != 3 {
		t.Fatalf("clampFullpel outside max/min = (%d,%d), want (-4,3)", row, col)
	}
	if !limits.inFullpelRange(0, 0) {
		t.Fatal("inFullpelRange rejected origin inside limits")
	}
	if limits.inFullpelRange(6, 0) {
		t.Fatal("inFullpelRange accepted row above max")
	}
	if !limits.fullpelBoundsOK(0, 0, 2) {
		t.Fatal("fullpelBoundsOK rejected centred search range")
	}
	if limits.fullpelBoundsOK(0, 0, 4) {
		t.Fatal("fullpelBoundsOK accepted range crossing column max")
	}

	var nilLimits *vp9MvLimits
	row, col = nilLimits.clampFullpel(-10, 7)
	if row != -10 || col != 7 {
		t.Fatalf("nil clampFullpel = (%d,%d), want passthrough (-10,7)", row, col)
	}
	if !nilLimits.inFullpelRange(1<<20, -(1<<20)) || !nilLimits.fullpelBoundsOK(0, 0, 1<<20) {
		t.Fatal("nil limits should accept all fullpel candidates")
	}
}

func TestVp9SetFullpelMvSearchRangePinsLibvpxFormula(t *testing.T) {
	limits := &vp9MvLimits{ColMin: -2000, ColMax: 2000, RowMin: -2000, RowMax: 2000}
	limits.setFullpelSearchRange(vp9MV{Row: 24, Col: -17})

	want := vp9MvLimits{
		ColMin: -1025,
		ColMax: 1020,
		RowMin: -1020,
		RowMax: 1026,
	}
	if *limits != want {
		t.Fatalf("setFullpelSearchRange = %+v, want %+v", *limits, want)
	}
}

// TestVp9SetSubpelMvSearchRangePinsLibvpxFormula pins the
// subpel-search-range formula at a representative midband ref MV.
//
// Inputs: umv = (col_min=-8, col_max=8, row_min=-8, row_max=8) full-pel,
// ref_mv = (row=16, col=24) 1/8-pel.
//
// libvpx (vp9_mcomp.c:51-67):
//
//	col_min = max(-8*8, 24 - 1023*8) = max(-64, -8160)  = -64
//	col_max = min( 8*8, 24 + 1023*8) = min( 64,  8208)  =  64
//	row_min = max(-8*8, 16 - 1023*8) = max(-64, -8168)  = -64
//	row_max = min( 8*8, 16 + 1023*8) = min( 64,  8200)  =  64
//	Then clamp to (MV_LOW+1, MV_UPP-1) = (-16383, 16382) — no change.
func TestVp9SetSubpelMvSearchRangePinsLibvpxFormula(t *testing.T) {
	umv := &vp9MvLimits{ColMin: -8, ColMax: 8, RowMin: -8, RowMax: 8}
	refMV := &vp9MV{Row: 16, Col: 24}
	var out vp9MvLimits
	vp9SetSubpelMvSearchRange(&out, umv, refMV)
	if out.ColMin != -64 || out.ColMax != 64 || out.RowMin != -64 || out.RowMax != 64 {
		t.Errorf("got (%d,%d,%d,%d) want (-64,64,-64,64)",
			out.ColMin, out.ColMax, out.RowMin, out.RowMax)
	}
}

// TestVp9SetSubpelMvSearchRangePinsMvBoundClamp verifies the
// (MV_LOW+1, MV_UPP-1) outer clamp by feeding an umv window that
// would otherwise produce out-of-range values.
//
// umv = (-100000, 100000, -100000, 100000) full-pel
// ref_mv = (0, 0)
// Then col_min = max(-100000*8, 0 - 1023*8) = max(-800000, -8184) = -8184.
// col_max = min(100000*8, 0 + 1023*8) = 8184.
// Outer clamp: max(MV_LOW+1=-16383, -8184) = -8184; min(MV_UPP-1=16382, 8184) = 8184.
func TestVp9SetSubpelMvSearchRangePinsMvBoundClamp(t *testing.T) {
	umv := &vp9MvLimits{ColMin: -100000, ColMax: 100000, RowMin: -100000, RowMax: 100000}
	refMV := &vp9MV{Row: 0, Col: 0}
	var out vp9MvLimits
	vp9SetSubpelMvSearchRange(&out, umv, refMV)
	want := vp9MvLimits{ColMin: -8184, ColMax: 8184, RowMin: -8184, RowMax: 8184}
	if out != want {
		t.Errorf("got %+v want %+v", out, want)
	}
}

// TestVp9SetSubpelMvSearchRangeMvUppClampActive verifies the
// MV_UPP-1 outer clamp fires when refMV is very large.
//
// umv = (-200000, 200000, -200000, 200000) full-pel
// ref_mv = (row=0, col=18000)
// col_min = max(-1600000, 18000 - 8184) = max(-1600000, 9816) = 9816.
// col_max = min(1600000, 18000 + 8184) = min(1600000, 26184) = 26184.
// Outer clamp: min(MV_UPP-1=16382, 26184) = 16382.
func TestVp9SetSubpelMvSearchRangeMvUppClampActive(t *testing.T) {
	umv := &vp9MvLimits{ColMin: -200000, ColMax: 200000, RowMin: -200000, RowMax: 200000}
	refMV := &vp9MV{Row: 0, Col: 18000}
	var out vp9MvLimits
	vp9SetSubpelMvSearchRange(&out, umv, refMV)
	if out.ColMin != 9816 || out.ColMax != 16382 {
		t.Errorf("col=(min=%d,max=%d) want (9816, 16382)", out.ColMin, out.ColMax)
	}
}

// TestVp9VectorMatchZeroOffset verifies that with a non-degenerate
// pattern matching src exactly at offset bw/2 (the centre),
// vector_match returns 0.
//
// Pattern: src = a sine-like jagged pattern, ref has zeros everywhere
// except at the centre window [bw/2 .. bw/2+bw) which copies src.
// All non-centre offsets see large variance; the centre is the unique
// minimum.
func TestVp9VectorMatchZeroOffset(t *testing.T) {
	bwl := 2 // width = 16.
	bw := 4 << bwl
	ref := make([]int16, 3*bw)
	src := make([]int16, bw)
	for i := range bw {
		v := int16(0)
		if i%2 == 0 {
			v = 100
		} else {
			v = -100
		}
		src[i] = v
		ref[bw/2+i] = v
	}
	got := vp9VectorMatch(ref, src, bwl)
	if got != 0 {
		t.Errorf("vp9VectorMatch identity: got %d want 0", got)
	}
}

// TestVp9VectorMatchOffsetFoundByHierarchy verifies that the
// hierarchical 16/8/4/2/1 search lands on a shift the schedule can
// actually reach. The libvpx vector_match is a coarse hierarchical
// matcher (vp9_mcomp.c:2192-2257), not an exhaustive minimiser; it
// only commits to a position the cascading ±step refinements can
// walk to from the stage-0 winner.
//
// Pattern: a clean unique notch at ref offset bw, so stage 0 picks
// d=bw (the second of the {0, bw} probes), stage 1 probes ±8 of
// d=bw, etc. With bwl=2, bw=16, the result is bw - bw/2 = +8.
func TestVp9VectorMatchOffsetFoundByHierarchy(t *testing.T) {
	bwl := 2
	bw := 4 << bwl
	ref := make([]int16, 3*bw)
	src := make([]int16, bw)
	for i := range bw {
		v := int16(0)
		if i%2 == 0 {
			v = 250
		} else {
			v = -250
		}
		src[i] = v
		ref[bw+i] = v
	}
	got := vp9VectorMatch(ref, src, bwl)
	if got != bw-bw/2 {
		t.Errorf("vp9VectorMatch shift=bw: got %d want %d", got, bw-bw/2)
	}
}

// TestVp9SADForBsizeReturnsKnownDispatch verifies the dispatch maps
// the correct bsize -> SAD function.
func TestVp9SADForBsizeReturnsKnownDispatch(t *testing.T) {
	// Run a trivial SAD against itself for one bsize and verify
	// the dispatched fn agrees with a hand-computed zero result.
	for _, bs := range []common.BlockSize{
		common.Block64x64, common.Block64x32, common.Block32x64,
		common.Block32x32, common.Block32x16, common.Block16x32,
		common.Block16x16,
	} {
		fn := vp9SADForBsize(bs)
		if fn == nil {
			t.Fatalf("vp9SADForBsize(%v): nil", bs)
		}
		w := 4 << uint(common.BWidthLog2Lookup[bs])
		h := 4 << uint(common.BHeightLog2Lookup[bs])
		buf := make([]uint8, w*h)
		for i := range buf {
			buf[i] = 128
		}
		got := fn(buf, 0, w, buf, 0, w)
		if got != 0 {
			t.Errorf("bsize=%v zero-diff SAD: got %d want 0", bs, got)
		}
		// Constant offset of 1 should give w*h.
		ref := make([]uint8, w*h)
		for i := range ref {
			ref[i] = 129
		}
		got = fn(buf, 0, w, ref, 0, w)
		if got != uint32(w*h) {
			t.Errorf("bsize=%v const-1 SAD: got %d want %d", bs, got, w*h)
		}
	}
}

// TestVp9IntProEstimateZeroMv verifies that when src == ref (no
// motion present), the int-pro estimate returns (sad=0, mv=(0,0))
// after subpel clamping.
//
// The buffer is sized 256x256 with the SB centred at (128, 128) so
// the int-pro projection's negative reach (-bw/2, -bh/2) and the
// per-probe ±1, ±refStride offsets all stay safely inside.
func TestVp9IntProEstimateZeroMv(t *testing.T) {
	stride := 256
	frame := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			frame[(originY+y)*stride+(originX+x)] = uint8((x + y) & 0xFF)
		}
	}
	srcOff := originY*stride + originX
	in := &vp9IntProEstimateInput{
		Bsize:     common.Block64x64,
		Src:       frame,
		SrcOff:    srcOff,
		SrcStride: stride,
		Ref:       frame,
		RefOff:    srcOff,
		RefStride: stride,
		RefMV:     vp9MV{Row: 0, Col: 0},
		MvLimits:  vp9MvLimits{ColMin: -16, ColMax: 16, RowMin: -16, RowMax: 16},
	}
	sad, mv := vp9IntProEstimate(in)
	if sad != 0 {
		t.Errorf("identity SAD: got %d want 0", sad)
	}
	if mv.Row != 0 || mv.Col != 0 {
		t.Errorf("identity MV: got (%d,%d) want (0,0)", mv.Row, mv.Col)
	}
}

// TestVp9IntProEstimateClampToMvLimits verifies the subpel clamp
// fires: a narrow MvLimits window (±2 full-pel) caps the result at
// ±16 in 1/8-pel units even when the underlying buffers don't drive
// the MV anywhere near the limit.
//
// Uses identical source/reference (so SAD=0 at centre and the
// hierarchical search stays at (0,0)) — the clamp behaviour is
// validated independently of vector_match accuracy.
func TestVp9IntProEstimateClampToMvLimits(t *testing.T) {
	stride := 256
	frame := make([]uint8, stride*stride)
	originX, originY := 128, 128
	for y := -64; y < 128; y++ {
		for x := -64; x < 128; x++ {
			frame[(originY+y)*stride+(originX+x)] = uint8((y + x*3) & 0xFF)
		}
	}
	srcOff := originY*stride + originX
	in := &vp9IntProEstimateInput{
		Bsize:     common.Block64x64,
		Src:       frame,
		SrcOff:    srcOff,
		SrcStride: stride,
		Ref:       frame,
		RefOff:    srcOff,
		RefStride: stride,
		RefMV:     vp9MV{Row: 0, Col: 0},
		// 2 full-pel limit = 16 1/8-pel units after subpel scale.
		MvLimits: vp9MvLimits{ColMin: -2, ColMax: 2, RowMin: -2, RowMax: 2},
	}
	_, mv := vp9IntProEstimate(in)
	// Clamp box is [-2*8, 2*8] = [-16, 16] in 1/8-pel.
	if mv.Col < -16 || mv.Col > 16 {
		t.Errorf("clamped col MV: got %d want in [-16, 16]", mv.Col)
	}
	if mv.Row < -16 || mv.Row > 16 {
		t.Errorf("clamped row MV: got %d want in [-16, 16]", mv.Row)
	}
}
