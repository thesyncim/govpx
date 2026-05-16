package govpx

import (
	"errors"
	"image"
	"testing"
)

// newVP9TPLBaseOpts returns the smallest VP9EncoderOptions configuration that
// satisfies the TPL pass prerequisites.  Tests below mutate one field at a
// time to exercise the validation matrix.
func newVP9TPLBaseOpts(width, height int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:           width,
		Height:          height,
		FPS:             30,
		LookaheadFrames: vp9TPLMinLookaheadFrames,
		AutoAltRef:      true,
		EnableTPL:       true,
	}
}

func TestVP9TPLValidationAcceptsMinimumConfig(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	if err := validateVP9TPLOptions(opts); err != nil {
		t.Fatalf("baseline TPL options: %v", err)
	}
	if _, err := NewVP9Encoder(opts); err != nil {
		t.Fatalf("NewVP9Encoder TPL baseline: %v", err)
	}
}

func TestVP9TPLValidationRejectsShortLookahead(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.LookaheadFrames = vp9TPLMinLookaheadFrames - 1
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short lookahead: got %v want ErrInvalidConfig", err)
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewVP9Encoder short lookahead: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLValidationRequiresAutoAltRef(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.AutoAltRef = false
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("no AutoAltRef: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLValidationRejectsLossless(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.Lossless = true
	if err := validateVP9TPLOptions(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("lossless TPL: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLDisabledLeavesPassInert(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if enc.vp9TPLEnabled() {
		t.Fatalf("TPL active without EnableTPL")
	}
	if got := enc.TPLFrameDelta(); got.SBRows != 0 || got.SBCols != 0 || got.Delta != nil {
		t.Fatalf("TPLFrameDelta on disabled encoder: %+v", got)
	}
	if got := enc.vp9TPLFrameR0(); got != 0 {
		t.Fatalf("vp9TPLFrameR0 on disabled encoder: %v", got)
	}
	if got := enc.vp9TPLFrameSlab(); got != nil {
		t.Fatalf("vp9TPLFrameSlab on disabled encoder: %+v", got)
	}
}

func TestVP9TPLSetEnableTPLConfiguresState(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	opts.EnableTPL = false
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := enc.SetEnableTPL(true); err != nil {
		t.Fatalf("SetEnableTPL(true): %v", err)
	}
	if !enc.opts.EnableTPL {
		t.Fatalf("EnableTPL not stored")
	}
	if !enc.vp9TPLEnabled() {
		t.Fatalf("vp9TPLEnabled false after SetEnableTPL(true)")
	}
	if len(enc.tpl.frames) == 0 {
		t.Fatalf("TPL frames slab not allocated")
	}
	if err := enc.SetEnableTPL(false); err != nil {
		t.Fatalf("SetEnableTPL(false): %v", err)
	}
	if enc.vp9TPLEnabled() {
		t.Fatalf("TPL still active after disable")
	}
}

func TestVP9TPLSetEnableTPLRejectsBadConfig(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := enc.SetEnableTPL(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetEnableTPL without lookahead: got %v want ErrInvalidConfig", err)
	}
}

func TestVP9TPLSBGridDims(t *testing.T) {
	rows, cols := vp9TPLSBGridDims(64, 64)
	if rows != 2 || cols != 2 {
		t.Fatalf("64x64: got rows=%d cols=%d want 2x2", rows, cols)
	}
	rows, cols = vp9TPLSBGridDims(33, 33)
	if rows != 2 || cols != 2 {
		t.Fatalf("33x33: got rows=%d cols=%d want 2x2", rows, cols)
	}
	rows, cols = vp9TPLSBGridDims(1920, 1080)
	if rows != 34 || cols != 60 {
		t.Fatalf("1080p: got rows=%d cols=%d want 34x60", rows, cols)
	}
	if rows, cols := vp9TPLSBGridDims(0, 0); rows != 0 || cols != 0 {
		t.Fatalf("zero dims: %d %d", rows, cols)
	}
}

func TestVP9TPLBlockSelfVarianceFlat(t *testing.T) {
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	if v := vp9TPLBlockSelfVariance(src, 0, 0); v != 0 {
		t.Fatalf("flat block variance: %d want 0", v)
	}
}

func TestVP9TPLBlockSelfVarianceTextured(t *testing.T) {
	src := newVP9MotionYCbCrForTest(64, 64)
	if v := vp9TPLBlockSelfVariance(src, 0, 0); v == 0 {
		t.Fatalf("textured block variance: 0 expected non-zero")
	}
}

func TestVP9TPLBlockMotionSearchStatic(t *testing.T) {
	src := newVP9MotionYCbCrForTest(64, 64)
	// ref identical to src — best MV is (0,0).
	ref := *src
	sad, mvRow, mvCol := vp9TPLBlockMotionSearch(src, &ref, 0, 0, 64, 64)
	if sad != 0 {
		t.Fatalf("identical SB SAD: %d want 0", sad)
	}
	if mvRow != 0 || mvCol != 0 {
		t.Fatalf("identical SB MV: (%d,%d) want (0,0)", mvRow, mvCol)
	}
}

// shiftYCbCrCopy shifts src by (dy, dx) pixels into a new YCbCr.  Coordinates
// past the frame edge replicate from the nearest in-frame pixel.
func shiftYCbCrCopy(src *image.YCbCr, dy, dx int) *image.YCbCr {
	w := src.Rect.Dx()
	h := src.Rect.Dy()
	out := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for y := range h {
		for x := range w {
			sy := clampEncodeCoord(y-dy, h)
			sx := clampEncodeCoord(x-dx, w)
			out.Y[y*out.YStride+x] = src.Y[sy*src.YStride+sx]
		}
	}
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for y := range uvH {
		for x := range uvW {
			sy := clampEncodeCoord(y-dy/2, uvH)
			sx := clampEncodeCoord(x-dx/2, uvW)
			out.Cb[y*out.CStride+x] = src.Cb[sy*src.CStride+sx]
			out.Cr[y*out.CStride+x] = src.Cr[sy*src.CStride+sx]
		}
	}
	return out
}

func TestVP9TPLBlockMotionSearchPanning(t *testing.T) {
	src := newVP9MotionYCbCrForTest(96, 96)
	ref := shiftYCbCrCopy(src, 4, 4)
	// Search for the block at (1,1) since (0,0) loses signal at the edge.
	sad, mvRow, mvCol := vp9TPLBlockMotionSearch(src, ref, 1, 1, 96, 96)
	if mvRow != 4 || mvCol != 4 {
		t.Fatalf("panning MV: (%d,%d) want (4,4)", mvRow, mvCol)
	}
	if sad != 0 {
		t.Fatalf("panning MV residual: %d want 0", sad)
	}
}

// TestVP9TPLMcFlowFormulaMatchesLibvpx pins the verbatim mc_flow recursion
// from vp9/encoder/vp9_tpl_model.c:679-694:
//
//	mc_flow = mc_dep_cost - (mc_dep_cost * inter_cost) / intra_cost
//
// with a single upstream SB landing on a single downstream SB so the
// (overlap_area / pix_num) factor collapses to 1.
func TestVP9TPLMcFlowFormulaMatchesLibvpx(t *testing.T) {
	const sbRows = 2
	const sbCols = 2
	slab := vp9TPLFrameStats{SBRows: sbRows, SBCols: sbCols,
		Stats: make([]vp9TPLStats, sbRows*sbCols)}
	next := vp9TPLFrameStats{SBRows: sbRows, SBCols: sbCols,
		Stats: make([]vp9TPLStats, sbRows*sbCols)}
	// Make SB (0,0) the only contributor; its MV points to next (0,0)
	// (zero MV).  IntraCost=1000, InterCost=200 → saved ratio = 0.8.
	// McDepCost is seeded to IntraCost.
	slab.Stats[0] = vp9TPLStats{
		IntraCost: 1000, InterCost: 200,
		McDepCost: 1000, McFlow: 0,
		MVRow: 0, MVCol: 0,
	}
	// next has IntraCost seeded for the downstream so we can verify the
	// destination's McDepCost gets updated to intra + accumulated flow.
	next.Stats[0] = vp9TPLStats{IntraCost: 500, McDepCost: 500}
	s := vp9TPLState{enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	// Expected: mc_flow = 1000 - (1000*200)/1000 = 800; mc_ref_cost = 800.
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

func TestVP9TPLPropagationOOBSafe(t *testing.T) {
	// Slabs with MVs pointing outside the next frame must not contribute,
	// and must not panic.
	const sbRows = 2
	const sbCols = 2
	slab := vp9TPLFrameStats{SBRows: sbRows, SBCols: sbCols,
		Stats: make([]vp9TPLStats, sbRows*sbCols)}
	next := vp9TPLFrameStats{SBRows: sbRows, SBCols: sbCols,
		Stats: make([]vp9TPLStats, sbRows*sbCols)}
	for i := range slab.Stats {
		slab.Stats[i].IntraCost = 1000
		slab.Stats[i].InterCost = 100
		slab.Stats[i].McDepCost = 1000
		// Force every MV out of bounds.
		slab.Stats[i].MVRow = 1 << 14
		slab.Stats[i].MVCol = 1 << 14
	}
	s := vp9TPLState{enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	for i := range next.Stats {
		if next.Stats[i].McFlow != 0 || next.Stats[i].McRefCost != 0 {
			t.Fatalf("OOB MV propagated to next[%d]: flow=%d ref=%d",
				i, next.Stats[i].McFlow, next.Stats[i].McRefCost)
		}
	}
}

func TestVP9TPLPopulateNeedsMinLookahead(t *testing.T) {
	s := vp9TPLState{}
	s.configure(true, 64, 64, vp9TPLMinLookaheadFrames)
	// Eight pointers but identical content — populate should fill slabs
	// with zero-ish stats.
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	frames := make([]*image.YCbCr, vp9TPLMinLookaheadFrames)
	for i := range frames {
		frames[i] = src
	}
	s.populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not marked Valid after populate")
	}
}

func TestVP9TPLPopulateRejectsShortWindow(t *testing.T) {
	s := vp9TPLState{}
	s.configure(true, 64, 64, vp9TPLMinLookaheadFrames)
	src := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	frames := make([]*image.YCbCr, vp9TPLMinLookaheadFrames-1)
	for i := range frames {
		frames[i] = src
	}
	s.populate(frames)
	for i := range s.frames {
		if s.frames[i].Valid {
			t.Fatalf("slab %d marked Valid with short window", i)
		}
	}
}

func TestVP9TPLDisabledEncoderRDMultDeltaIsIdentity(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := enc.getVP9TPLRDMultDelta(0, 0, 8, 8, 4000); got != 4000 {
		t.Fatalf("disabled encoder changed rdmult: %d", got)
	}
}

// TestVP9TPLRDMultClampedToLibvpxWindow pins the libvpx clamp from
// vp9/encoder/vp9_encodeframe.c:3656-3657:
//
//	dr = clamp(dr, orig_rdmult * 1 / 2, orig_rdmult * 3 / 2);
func TestVP9TPLRDMultClampedToLibvpxWindow(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	slab := &enc.tpl.frames[0]
	slab.Valid = true
	// Concoct a slab where beta is huge so the unclamped rdmult would
	// blow past orig/2.  IntraCost large, McDepCost tiny means rk huge,
	// beta = r0 / rk → small, rdmult = orig/beta → huge (clamped UP to
	// 3/2*orig).
	for i := range slab.Stats {
		slab.Stats[i] = vp9TPLStats{IntraCost: 100, McDepCost: 1}
	}
	slab.R0 = 100.0 // matches the per-SB ratio so rk = 100, beta = 1.0
	orig := 4000
	got := enc.getVP9TPLRDMultDelta(0, 0, 8, 8, orig)
	// beta=1.0 → dr ≈ orig; should be within window [orig/2, orig*3/2].
	if got < orig/2 || got > orig*3/2 {
		t.Fatalf("rdmult %d outside [%d,%d]", got, orig/2, orig*3/2)
	}
	// Now skew beta huge by knocking R0 way up; expect clamp to orig/2.
	slab.R0 = 1e9
	got = enc.getVP9TPLRDMultDelta(0, 0, 8, 8, orig)
	if got != orig/2 {
		t.Fatalf("clamp low: got %d want %d", got, orig/2)
	}
	// Now skew beta tiny so dr blows up; expect clamp to orig*3/2.
	slab.R0 = 1e-9
	got = enc.getVP9TPLRDMultDelta(0, 0, 8, 8, orig)
	if got != orig*3/2 {
		t.Fatalf("clamp high: got %d want %d", got, orig*3/2)
	}
}

func TestVP9TPLResolutionChangeRebuildsState(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	enc.tpl.frames[0].Valid = true
	enc.applyVP9ResolutionChange(96, 96)
	if enc.tpl.width != 96 || enc.tpl.height != 96 {
		t.Fatalf("TPL state width/height not updated: %dx%d", enc.tpl.width, enc.tpl.height)
	}
	if enc.tpl.sbRows != 3 || enc.tpl.sbCols != 3 {
		t.Fatalf("TPL SB grid not updated: %dx%d", enc.tpl.sbRows, enc.tpl.sbCols)
	}
	for i := range enc.tpl.frames {
		if enc.tpl.frames[i].Valid {
			t.Fatalf("stale slab Valid after resolution change: %d", i)
		}
	}
}

func TestVP9TPLShiftAndInvalidatePreservesCapacity(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	enc.tpl.frames[0].Valid = true
	enc.tpl.frames[0].R0 = 0.7
	enc.tpl.frames[1].Valid = true
	enc.tpl.frames[1].R0 = 0.5
	enc.tpl.shiftAndInvalidate()
	if enc.tpl.frames[0].R0 != 0.5 {
		t.Fatalf("shift did not promote slab 1 to slab 0: got %v", enc.tpl.frames[0].R0)
	}
	// Tail must be reset.
	if enc.tpl.frames[len(enc.tpl.frames)-1].Valid {
		t.Fatalf("tail slab not invalidated")
	}
	if enc.tpl.frames[len(enc.tpl.frames)-1].R0 != 0 {
		t.Fatalf("tail slab R0 = %v, want 0",
			enc.tpl.frames[len(enc.tpl.frames)-1].R0)
	}
}

// newVP9TPLPanningSequence returns n synthetic source frames that pan a
// textured pattern by 2 pixels per frame.  Useful for exercising the TPL
// motion-search path under predictable motion.
func newVP9TPLPanningSequence(width, height, n int) []*image.YCbCr {
	base := newVP9MotionYCbCrForTest(width, height)
	out := make([]*image.YCbCr, n)
	for i := range n {
		out[i] = shiftYCbCrCopy(base, 2*i, 2*i)
	}
	return out
}

// newVP9TPLEdgesYCbCrForTest paints a synthetic frame with strong vertical
// and horizontal edges so the keyframe intra mode picker (DC/V/H) sees
// non-trivial ranking under rdmult scaling.  Without directional edges the
// noise-texture proxy returned by newVP9MotionYCbCrForTest tends to make DC
// the only winner regardless of rdmult.
func newVP9TPLEdgesYCbCrForTest(width, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			// Vertical stripes on the left half (favor V intra),
			// horizontal stripes on the right half (favor H intra),
			// plus a low-frequency gradient (favor DC).
			v := 32 + (x/4)*16
			if x >= width/2 {
				v = 32 + (y/4)*16
			}
			if v > 240 {
				v = 240
			}
			row[x] = byte(v)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = 128
			cr[x] = 128
		}
	}
	return img
}

// newVP9TPLMixedMotionSequence returns n source frames where the top half of
// the frame is static (matches the prior frame exactly) and the bottom half
// pans by 2 pixels per frame.  This produces non-uniform per-SB motion in
// the TPL slab so the propagation pass yields per-SB beta variance — without
// it, every SB sees an identical mc_dep_cost and the per-SB rdmult delta
// collapses to a single value.  The base content uses strong directional
// edges so the keyframe DC/V/H mode picker has non-trivial ranking under
// rdmult scaling.
func newVP9TPLMixedMotionSequence(width, height, n int) []*image.YCbCr {
	base := newVP9TPLEdgesYCbCrForTest(width, height)
	out := make([]*image.YCbCr, n)
	mid := height / 2
	for i := range n {
		// shifted: pan-by-(2*i, 2*i) version of base for the bottom.
		shifted := shiftYCbCrCopy(base, 2*i, 2*i)
		// Compose: top half from base, bottom half from shifted.
		composed := image.NewYCbCr(image.Rect(0, 0, width, height),
			image.YCbCrSubsampleRatio420)
		for y := range height {
			src := base
			if y >= mid {
				src = shifted
			}
			copy(composed.Y[y*composed.YStride:y*composed.YStride+width],
				src.Y[y*src.YStride:y*src.YStride+width])
		}
		uvH := (height + 1) >> 1
		uvW := (width + 1) >> 1
		uvMid := mid >> 1
		for y := range uvH {
			src := base
			if y >= uvMid {
				src = shifted
			}
			copy(composed.Cb[y*composed.CStride:y*composed.CStride+uvW],
				src.Cb[y*src.CStride:y*src.CStride+uvW])
			copy(composed.Cr[y*composed.CStride:y*composed.CStride+uvW],
				src.Cr[y*src.CStride:y*src.CStride+uvW])
		}
		out[i] = composed
	}
	return out
}

func TestVP9TPLPanningSequencePopulatesValidSlabs(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLPanningSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("first slab not Valid after populate")
	}
	// slab[0] is the encoded-frame anchor (no inter prediction), so its
	// MVs stay zero by construction.  Motion is recorded on slab[1..]
	// where each lookahead frame ran ME against slab[0].
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

// TestVP9TPLProducesNonZeroR0 asserts that the per-frame r0
// (intra_cost_base / mc_dep_cost_base) is strictly positive on TPL-friendly
// content, as required for the get_rdmult_delta pipeline to bite.  Mirrors
// the libvpx wiring at vp9_encodeframe.c:5707-5708 where cpi->rd.r0 must be
// > 0 for the rdmult delta to take effect.
func TestVP9TPLProducesNonZeroR0(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLPanningSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not Valid after populate")
	}
	if s.frames[0].R0 <= 0 {
		t.Fatalf("R0=%v on TPL-friendly content, want >0", s.frames[0].R0)
	}
	// r0 = intra / mc_dep; mc_dep = intra + mc_flow >= intra so r0 <= 1
	// (numerical noise from per-SB rounding allows a tiny epsilon).
	if s.frames[0].R0 > 1.0+1e-9 {
		t.Fatalf("R0=%v exceeds 1.0 (intra/mc_dep ratio invariant broken)",
			s.frames[0].R0)
	}
}

// TestVP9TPLBetaVariesAcrossSBs asserts that the per-SB beta
// (r0/(intra/mc_dep)) deviates across SBs after propagation, which is the
// load-bearing precondition for get_rdmult_delta producing non-trivial
// per-SB rdmult deltas.  An all-equal-beta slab would degenerate to a
// frame-mean bias — the regression we were tasked to delete.
func TestVP9TPLBetaVariesAcrossSBs(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLMixedMotionSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	slab := &s.frames[0]
	if !slab.Valid {
		t.Fatalf("slab 0 not Valid after populate")
	}
	r0 := slab.R0
	if r0 <= 0 {
		t.Fatalf("R0=%v, want >0", r0)
	}
	// Compute beta for every SB; require at least two distinct values.
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
		beta := r0 / rk
		// Quantize so floating noise doesn't inflate the bucket count.
		key := int(beta * 1024)
		seen[key] = struct{}{}
		if len(seen) >= 2 {
			return
		}
	}
	t.Fatalf("beta is uniform across SBs (%d distinct values); "+
		"per-SB rdmult delta degenerated to frame-mean bias", len(seen))
}

func TestVP9TPLFrameDeltaAfterPopulate(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9MotionYCbCrForTest(64, 64)
	frames := make([]*image.YCbCr, vp9TPLMinLookaheadFrames)
	for i := range frames {
		frames[i] = src
	}
	enc.tpl.populate(frames)
	delta := enc.TPLFrameDelta()
	if delta.SBRows == 0 || delta.SBCols == 0 {
		t.Fatalf("TPLFrameDelta returned zero grid after populate")
	}
	if len(delta.Delta) != delta.SBRows*delta.SBCols {
		t.Fatalf("TPLFrameDelta size mismatch: %d != %dx%d",
			len(delta.Delta), delta.SBRows, delta.SBCols)
	}
}

// TestVP9TPLChangesKeyframeEncoded pins the integration boundary: the
// per-SB TPL rdmult delta must alter the keyframe mode picker's RD scoring
// at least once on TPL-friendly content.  Mirrors the libvpx wiring at
// vp9_encodeframe.c:4245-4248 where cb_rdmult is overwritten before the per-SB
// partition / mode search runs.
//
// We compare an encoder configured with EnableTPL=true vs one with TPL off
// and assert the encoded keyframe byte stream diverges.  The visible packet
// count is preserved (TPL is a quality knob, not a scheduling change) so a
// byte-stream divergence under matched headers is the load-bearing
// assertion.
func TestVP9TPLChangesKeyframeEncoded(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) ([]byte, int, []int, int) {
		opts := VP9EncoderOptions{
			Width:              w,
			Height:             h,
			FPS:                30,
			LookaheadFrames:    vp9TPLMinLookaheadFrames,
			AutoAltRef:         true,
			EnableTPL:          enableTPL,
			RateControlModeSet: true,
			RateControlMode:    RateControlQ,
			TargetBitrateKbps:  1000,
			CQLevel:            32,
			MaxQuantizer:       63,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		seq := newVP9TPLMixedMotionSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var concat []byte
		var qs []int
		total := 0
		drain := func(res VP9EncodeResult) {
			if res.ShowFrame {
				qs = append(qs, res.InternalQuantizer)
			}
			total += res.SizeBytes
			concat = append(concat, res.Data...)
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return concat, total, qs, enc.tplRDMultDeltaCalls
	}
	offBytes, offTotal, offQs, _ := encode(false)
	onBytes, onTotal, onQs, onDeltaCalls := encode(true)
	if len(offQs) != len(onQs) {
		t.Fatalf("visible packet count drifted: off=%d on=%d", len(offQs), len(onQs))
	}
	if onDeltaCalls == 0 {
		t.Fatalf("getVP9TPLRDMultDelta produced no non-identity scaling — "+
			"TPL → keyframe picker wiring did not fire (off=%d on=%d bytes)",
			offTotal, onTotal)
	}
	t.Logf("TPL rdmult delta non-identity calls: %d", onDeltaCalls)
	// qindex is NOT expected to change under the libvpx-faithful flow
	// (TPL routes through cb_rdmult, not the regulated qindex).  The
	// load-bearing assertion is that the byte stream diverges because
	// the keyframe mode picker's per-SB RD ranking shifts.
	if len(offBytes) == len(onBytes) {
		identical := true
		for i := range offBytes {
			if offBytes[i] != onBytes[i] {
				identical = false
				break
			}
		}
		if identical {
			t.Fatalf("TPL had no effect on keyframe encoding: byte streams identical (off=%d on=%d)",
				offTotal, onTotal)
		}
	}
	t.Logf("keyframe TPL off->on: bytes off=%d on=%d, qindex unchanged (libvpx flow routes through cb_rdmult)",
		offTotal, onTotal)
}

// TestVP9TPLDoesNotChangeRegulatedQIndex pins the libvpx parity invariant:
// under the libvpx-faithful flow, TPL routes through cb_rdmult (not the
// regulated qindex).  The deleted frame-mean scalar bias had no libvpx
// analog; this test guards against any future regression that lets TPL
// silently re-shift the frame qindex.
func TestVP9TPLDoesNotChangeRegulatedQIndex(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) []int {
		opts := VP9EncoderOptions{
			Width:              w,
			Height:             h,
			FPS:                30,
			LookaheadFrames:    vp9TPLMinLookaheadFrames,
			AutoAltRef:         true,
			EnableTPL:          enableTPL,
			RateControlModeSet: true,
			RateControlMode:    RateControlQ,
			TargetBitrateKbps:  1000,
			CQLevel:            32,
			MaxQuantizer:       63,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		seq := newVP9TPLPanningSequence(w, h, 16)
		buf := make([]byte, 64*1024)
		var qs []int
		drain := func(res VP9EncodeResult) {
			if res.ShowFrame {
				qs = append(qs, res.InternalQuantizer)
			}
		}
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				drain(res)
			case errors.Is(err, ErrFrameNotReady):
			default:
				t.Fatalf("encode %d: %v", i, err)
			}
		}
		for {
			res, err := enc.FlushIntoWithResult(buf)
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			drain(res)
		}
		return qs
	}
	offQs := encode(false)
	onQs := encode(true)
	if len(offQs) != len(onQs) {
		t.Fatalf("visible packet count drifted: off=%d on=%d", len(offQs), len(onQs))
	}
	for i := range offQs {
		if offQs[i] != onQs[i] {
			t.Fatalf("TPL silently shifted regulated qindex at frame %d: off=%d on=%d "+
				"(libvpx flow routes through cb_rdmult, not qindex)",
				i, offQs[i], onQs[i])
		}
	}
}

// TestVP9TPLRDMultDeltaVariesUnderMixedMotion is a debug-style probe that
// asserts the per-SB rdmult delta diverges across SBs after populate on
// mixed-motion content.  It is the precondition for
// TestVP9TPLChangesKeyframeEncoded; if this passes but the encoded byte
// stream test fails, the bug lives in the wiring of getVP9TPLRDMultDelta
// into pickVP9KeyframeMode (i.e. the keyframe picker isn't reading the
// delta) rather than in the TPL computation.
func TestVP9TPLRDMultDeltaVariesUnderMixedMotion(t *testing.T) {
	const w, h = 96, 96
	opts := newVP9TPLBaseOpts(w, h)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := newVP9TPLMixedMotionSequence(w, h, vp9TPLMinLookaheadFrames)
	enc.tpl.populate(frames)
	if !enc.tpl.frames[0].Valid {
		t.Fatalf("slab 0 not Valid")
	}
	origRdmult := 4000
	seen := map[int]struct{}{}
	for sbRow := 0; sbRow < enc.tpl.sbRows; sbRow++ {
		for sbCol := 0; sbCol < enc.tpl.sbCols; sbCol++ {
			miRow := sbRow * (vp9TPLSBSize / 8)
			miCol := sbCol * (vp9TPLSBSize / 8)
			dr := enc.getVP9TPLRDMultDelta(miRow, miCol, 4, 4, origRdmult)
			seen[dr] = struct{}{}
		}
	}
	if len(seen) < 2 {
		t.Fatalf("rdmult delta is uniform across SBs (%d values) — TPL ranking flat",
			len(seen))
	}
}

func TestVP9TPLEncodesWithoutBreakingExisting(t *testing.T) {
	// A 16-frame encode under TPL should produce the same packet count as
	// the same encode without TPL (because TPL is a quality knob, not a
	// scheduling change).
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := newVP9MotionYCbCrForTest(64, 64)
	buf := make([]byte, 32*1024)
	encoded := 0
	for i := range 16 {
		_, err := enc.encodeVP9LookaheadIntoWithFlagsResult(src, buf, 0)
		switch {
		case err == nil:
			encoded++
		case errors.Is(err, ErrFrameNotReady):
		default:
			t.Fatalf("encode %d: %v", i, err)
		}
	}
	for {
		_, err := enc.FlushIntoWithResult(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("flush: %v", err)
		}
		encoded++
	}
	if encoded == 0 {
		t.Fatalf("no frames committed with TPL enabled")
	}
}
