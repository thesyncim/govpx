package encoder

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestTPLSBGridDims(t *testing.T) {
	rows, cols := TPLSBGridDims(64, 64)
	if rows != 2 || cols != 2 {
		t.Fatalf("64x64: got rows=%d cols=%d want 2x2", rows, cols)
	}
	rows, cols = TPLSBGridDims(33, 33)
	if rows != 2 || cols != 2 {
		t.Fatalf("33x33: got rows=%d cols=%d want 2x2", rows, cols)
	}
	rows, cols = TPLSBGridDims(1920, 1080)
	if rows != 34 || cols != 60 {
		t.Fatalf("1080p: got rows=%d cols=%d want 34x60", rows, cols)
	}
	if rows, cols := TPLSBGridDims(0, 0); rows != 0 || cols != 0 {
		t.Fatalf("zero dims: %d %d", rows, cols)
	}
}

func TestTPLBlockSelfVarianceFlat(t *testing.T) {
	src := testutil.NewYCbCr(64, 64, 128, 128, 128)
	if v := TPLBlockSelfVariance(src, 0, 0); v != 0 {
		t.Fatalf("flat block variance: %d want 0", v)
	}
}

func TestTPLBlockSelfVarianceTextured(t *testing.T) {
	src := testutil.NewMotionYCbCr(64, 64)
	if v := TPLBlockSelfVariance(src, 0, 0); v == 0 {
		t.Fatalf("textured block variance: 0 expected non-zero")
	}
}

func TestTPLBlockMotionSearchStatic(t *testing.T) {
	src := testutil.NewMotionYCbCr(64, 64)
	ref := *src
	sad, mvRow, mvCol := TPLBlockMotionSearch(src, &ref, 0, 0, 64, 64)
	if sad != 0 {
		t.Fatalf("identical SB SAD: %d want 0", sad)
	}
	if mvRow != 0 || mvCol != 0 {
		t.Fatalf("identical SB MV: (%d,%d) want (0,0)", mvRow, mvCol)
	}
}

func TestTPLBlockMotionSearchPanning(t *testing.T) {
	src := testutil.NewMotionYCbCr(96, 96)
	ref := testutil.ShiftYCbCrCopy(src, 4, 4)
	sad, mvRow, mvCol := TPLBlockMotionSearch(src, ref, 1, 1, 96, 96)
	if mvRow != 4 || mvCol != 4 {
		t.Fatalf("panning MV: (%d,%d) want (4,4)", mvRow, mvCol)
	}
	if sad != 0 {
		t.Fatalf("panning MV residual: %d want 0", sad)
	}
}

// TestTPLMcFlowFormulaMatchesLibvpx pins the verbatim mc_flow recursion from
// vp9/encoder/vp9_tpl_model.c:679-694:
//
//	mc_flow = mc_dep_cost - (mc_dep_cost * inter_cost) / intra_cost
func TestTPLMcFlowFormulaMatchesLibvpx(t *testing.T) {
	const sbRows = 2
	const sbCols = 2
	slab := TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]TPLStats, sbRows*sbCols),
	}
	next := TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]TPLStats, sbRows*sbCols),
	}
	slab.Stats[0] = TPLStats{
		IntraCost: 1000,
		InterCost: 200,
		McDepCost: 1000,
		MVRow:     0,
		MVCol:     0,
	}
	next.Stats[0] = TPLStats{IntraCost: 500, McDepCost: 500}
	s := TPLState{Enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	if got, want := next.Stats[0].McFlow, int64(800); got != want {
		t.Fatalf("McFlow=%d want %d", got, want)
	}
	if got, want := next.Stats[0].McRefCost, int64(800); got != want {
		t.Fatalf("McRefCost=%d want %d", got, want)
	}
	if got, want := next.Stats[0].McDepCost, int64(1300); got != want {
		t.Fatalf("McDepCost=%d want %d (intra + flow)", got, want)
	}
}

func TestTPLPropagationOOBSafe(t *testing.T) {
	const sbRows = 2
	const sbCols = 2
	slab := TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]TPLStats, sbRows*sbCols),
	}
	next := TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]TPLStats, sbRows*sbCols),
	}
	for i := range slab.Stats {
		slab.Stats[i].IntraCost = 1000
		slab.Stats[i].InterCost = 100
		slab.Stats[i].McDepCost = 1000
		slab.Stats[i].MVRow = 1 << 14
		slab.Stats[i].MVCol = 1 << 14
	}
	s := TPLState{Enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	for i := range next.Stats {
		if next.Stats[i].McFlow != 0 || next.Stats[i].McRefCost != 0 {
			t.Fatalf("OOB MV propagated to next[%d]: flow=%d ref=%d",
				i, next.Stats[i].McFlow, next.Stats[i].McRefCost)
		}
	}
}

func TestTPLPopulateNeedsMinLookahead(t *testing.T) {
	s := TPLState{}
	s.Configure(true, 64, 64, TPLMinLookaheadFrames)
	src := testutil.NewYCbCr(64, 64, 128, 128, 128)
	frames := make([]*image.YCbCr, TPLMinLookaheadFrames)
	for i := range frames {
		frames[i] = src
	}
	s.Populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not marked Valid after populate")
	}
}

func TestTPLPopulateRejectsShortWindow(t *testing.T) {
	s := TPLState{}
	s.Configure(true, 64, 64, TPLMinLookaheadFrames)
	src := testutil.NewYCbCr(64, 64, 128, 128, 128)
	frames := make([]*image.YCbCr, TPLMinLookaheadFrames-1)
	for i := range frames {
		frames[i] = src
	}
	s.Populate(frames)
	for i := range s.frames {
		if s.frames[i].Valid {
			t.Fatalf("slab %d marked Valid with short window", i)
		}
	}
}

// TestTPLRDMultClampedToLibvpxWindow pins the libvpx clamp from
// vp9/encoder/vp9_encodeframe.c:3656-3657.
func TestTPLRDMultClampedToLibvpxWindow(t *testing.T) {
	s := TPLState{}
	s.Configure(true, 64, 64, TPLMinLookaheadFrames)
	slab := &s.frames[0]
	slab.Valid = true
	for i := range slab.Stats {
		slab.Stats[i] = TPLStats{IntraCost: 100, McDepCost: 1}
	}
	slab.R0 = 100.0
	orig := 4000
	got := s.RDMultDelta(0, 0, 8, 8, orig)
	if got < orig/2 || got > orig*3/2 {
		t.Fatalf("rdmult %d outside [%d,%d]", got, orig/2, orig*3/2)
	}
	slab.R0 = 1e9
	got = s.RDMultDelta(0, 0, 8, 8, orig)
	if got != orig/2 {
		t.Fatalf("clamp low: got %d want %d", got, orig/2)
	}
	slab.R0 = 1e-9
	got = s.RDMultDelta(0, 0, 8, 8, orig)
	if got != orig*3/2 {
		t.Fatalf("clamp high: got %d want %d", got, orig*3/2)
	}
}

func TestTPLShiftAndInvalidatePreservesCapacity(t *testing.T) {
	s := TPLState{}
	s.Configure(true, 64, 64, TPLMinLookaheadFrames)
	s.frames[0].Valid = true
	s.frames[0].R0 = 0.7
	s.frames[1].Valid = true
	s.frames[1].R0 = 0.5
	s.ShiftAndInvalidate()
	if s.frames[0].R0 != 0.5 {
		t.Fatalf("shift did not promote slab 1 to slab 0: got %v", s.frames[0].R0)
	}
	tail := &s.frames[len(s.frames)-1]
	if tail.Valid {
		t.Fatalf("tail slab not invalidated")
	}
	if tail.R0 != 0 {
		t.Fatalf("tail slab R0 = %v, want 0", tail.R0)
	}
}

func TestTPLPanningSequencePopulatesValidSlabs(t *testing.T) {
	const width = 96
	const height = 96
	frames := tplPanningSequence(width, height, TPLMinLookaheadFrames)
	s := TPLState{}
	s.Configure(true, width, height, TPLMinLookaheadFrames)
	s.Populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("first slab not Valid after populate")
	}
	foundMotion := false
	for idx := 1; idx < len(s.frames); idx++ {
		for _, st := range s.frames[idx].Stats {
			if st.MVRow != 0 || st.MVCol != 0 {
				foundMotion = true
				break
			}
		}
		if foundMotion {
			break
		}
	}
	if !foundMotion {
		t.Fatalf("no motion vectors discovered in panning sequence")
	}
}

func TestTPLProducesNonZeroR0(t *testing.T) {
	const width = 96
	const height = 96
	frames := tplPanningSequence(width, height, TPLMinLookaheadFrames)
	s := TPLState{}
	s.Configure(true, width, height, TPLMinLookaheadFrames)
	s.Populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not Valid after populate")
	}
	if s.frames[0].R0 <= 0 {
		t.Fatalf("R0=%v on TPL-friendly content, want >0", s.frames[0].R0)
	}
	if s.frames[0].R0 > 1.0+1e-9 {
		t.Fatalf("R0=%v exceeds 1.0 (intra/mc_dep invariant broken)",
			s.frames[0].R0)
	}
}

func TestTPLBetaVariesAcrossSBs(t *testing.T) {
	const width = 96
	const height = 96
	frames := tplMixedMotionSequence(width, height, TPLMinLookaheadFrames)
	s := TPLState{}
	s.Configure(true, width, height, TPLMinLookaheadFrames)
	s.Populate(frames)
	slab := &s.frames[0]
	if !slab.Valid {
		t.Fatalf("slab 0 not Valid after populate")
	}
	r0 := slab.R0
	if r0 <= 0 {
		t.Fatalf("R0=%v, want >0", r0)
	}
	seen := map[int]struct{}{}
	for i := range slab.Stats {
		st := &slab.Stats[i]
		if st.McDepCost <= 0 || st.IntraCost <= 0 {
			continue
		}
		rk := float64(st.IntraCost) / float64(st.McDepCost)
		if rk <= 0 {
			continue
		}
		key := int((r0 / rk) * 1024)
		seen[key] = struct{}{}
		if len(seen) >= 2 {
			return
		}
	}
	t.Fatalf("beta is uniform across SBs (%d distinct values)", len(seen))
}

func TestTPLRDMultDeltaVariesUnderMixedMotion(t *testing.T) {
	const width = 96
	const height = 96
	frames := tplMixedMotionSequence(width, height, TPLMinLookaheadFrames)
	s := TPLState{}
	s.Configure(true, width, height, TPLMinLookaheadFrames)
	s.Populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not Valid")
	}
	origRdmult := 4000
	seen := map[int]struct{}{}
	for sbRow := 0; sbRow < s.sbRows; sbRow++ {
		for sbCol := 0; sbCol < s.sbCols; sbCol++ {
			miRow := sbRow * (TPLSBSize / common.MiSize)
			miCol := sbCol * (TPLSBSize / common.MiSize)
			dr := s.RDMultDelta(miRow, miCol, 4, 4, origRdmult)
			seen[dr] = struct{}{}
		}
	}
	if len(seen) < 2 {
		t.Fatalf("rdmult delta is uniform across SBs (%d values)", len(seen))
	}
}

func tplPanningSequence(width, height, n int) []*image.YCbCr {
	base := testutil.NewMotionYCbCr(width, height)
	out := make([]*image.YCbCr, n)
	for i := range n {
		out[i] = testutil.ShiftYCbCrCopy(base, 2*i, 2*i)
	}
	return out
}

func tplEdgesYCbCr(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for yy := 0; yy < height; yy++ {
		row := img.Y[yy*img.YStride:]
		for xx := 0; xx < width; xx++ {
			v := 32 + (xx/4)*16
			if xx >= width/2 {
				v = 32 + (yy/4)*16
			}
			if v > 240 {
				v = 240
			}
			row[xx] = byte(v)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for yy := 0; yy < uvHeight; yy++ {
		cbRow := img.Cb[yy*img.CStride:]
		crRow := img.Cr[yy*img.CStride:]
		for xx := 0; xx < uvWidth; xx++ {
			cbRow[xx] = 128
			crRow[xx] = 128
		}
	}
	return img
}

func tplMixedMotionSequence(width, height, n int) []*image.YCbCr {
	base := tplEdgesYCbCr(width, height)
	out := make([]*image.YCbCr, n)
	mid := height / 2
	for i := range n {
		shifted := testutil.ShiftYCbCrCopy(base, 2*i, 2*i)
		composed := image.NewYCbCr(image.Rect(0, 0, width, height),
			image.YCbCrSubsampleRatio420)
		for yy := 0; yy < height; yy++ {
			src := base
			if yy >= mid {
				src = shifted
			}
			copy(composed.Y[yy*composed.YStride:yy*composed.YStride+width],
				src.Y[yy*src.YStride:yy*src.YStride+width])
		}
		uvWidth := (width + 1) >> 1
		uvHeight := (height + 1) >> 1
		uvMid := mid >> 1
		for yy := 0; yy < uvHeight; yy++ {
			src := base
			if yy >= uvMid {
				src = shifted
			}
			copy(composed.Cb[yy*composed.CStride:yy*composed.CStride+uvWidth],
				src.Cb[yy*src.CStride:yy*src.CStride+uvWidth])
			copy(composed.Cr[yy*composed.CStride:yy*composed.CStride+uvWidth],
				src.Cr[yy*src.CStride:yy*src.CStride+uvWidth])
		}
		out[i] = composed
	}
	return out
}
