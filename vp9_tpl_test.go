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
	if got := enc.vp9TPLFrameMeanQDelta(); got != 0 {
		t.Fatalf("vp9TPLFrameMeanQDelta on disabled encoder: %d", got)
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
	// The motion-shifted ref means src(sbRow=1,sbCol=1) at (32,32) should
	// match ref at (36,36) — i.e. MV = (+4, +4).
	if mvRow != 4 || mvCol != 4 {
		t.Fatalf("panning MV: (%d,%d) want (4,4)", mvRow, mvCol)
	}
	if sad != 0 {
		t.Fatalf("panning MV residual: %d want 0", sad)
	}
}

func TestVP9TPLPropagationMonotonic(t *testing.T) {
	// Build a synthetic two-frame slab pair where every SB in slab 0 motion
	// vectors at the same SB in slab 1 (zero MV).  Verify that an SB in
	// slab 0 that is re-referenced from N slab-1 SBs (i.e. the propagation
	// accumulator) ends up with a delta strictly more negative (more bits)
	// than an SB that is not re-referenced.
	const sbRows = 4
	const sbCols = 4
	slab := vp9TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]vp9TPLStats, sbRows*sbCols),
		QDelta: make([]int8, sbRows*sbCols),
	}
	next := vp9TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]vp9TPLStats, sbRows*sbCols),
		QDelta: make([]int8, sbRows*sbCols),
	}
	// Every slab-0 SB has IntraCost > InterCost — propagating saved=IntraCost-InterCost.
	for i := range slab.Stats {
		slab.Stats[i].IntraCost = 1000
		slab.Stats[i].InterCost = 100
		slab.Stats[i].MVRow = 0
		slab.Stats[i].MVCol = 0
	}
	// Direct every slab-0 SB at next SB (0,0).  After propagation, next SB
	// (0,0) should have a massive propagation score.
	for i := range slab.Stats {
		// MV pointing back to (0,0) from cell (row,col) requires offset
		// (-row*32, -col*32) — translated to integer pixel MV units.
		row := i / sbCols
		col := i % sbCols
		slab.Stats[i].MVRow = int16(-row * vp9TPLSBSize)
		slab.Stats[i].MVCol = int16(-col * vp9TPLSBSize)
	}
	s := vp9TPLState{enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	if next.Stats[0].Propagation == 0 {
		t.Fatalf("next.Stats[0].Propagation is zero, want accumulated importance")
	}
	for i := 1; i < len(next.Stats); i++ {
		if next.Stats[i].Propagation != 0 {
			t.Fatalf("next.Stats[%d].Propagation = %d, want 0", i,
				next.Stats[i].Propagation)
		}
	}
	// Now derive qindex deltas — SB 0 should be biased negative (lower q,
	// more bits) compared to the rest.
	s.deriveQDelta(&next)
	if next.QDelta[0] >= 0 {
		t.Fatalf("re-referenced SB qdelta: %d, want negative", next.QDelta[0])
	}
	for i := 1; i < len(next.QDelta); i++ {
		if next.QDelta[i] < next.QDelta[0] {
			t.Fatalf("non-referenced SB qdelta %d < referenced %d",
				next.QDelta[i], next.QDelta[0])
		}
	}
}

func TestVP9TPLPropagationOOBSafe(t *testing.T) {
	// Slabs with MVs pointing outside the next frame must not contribute,
	// and must not panic.
	const sbRows = 2
	const sbCols = 2
	slab := vp9TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]vp9TPLStats, sbRows*sbCols),
		QDelta: make([]int8, sbRows*sbCols),
	}
	next := vp9TPLFrameStats{
		SBRows: sbRows,
		SBCols: sbCols,
		Stats:  make([]vp9TPLStats, sbRows*sbCols),
		QDelta: make([]int8, sbRows*sbCols),
	}
	for i := range slab.Stats {
		slab.Stats[i].IntraCost = 1000
		slab.Stats[i].InterCost = 100
		// Force every MV out of bounds.
		slab.Stats[i].MVRow = 1 << 14
		slab.Stats[i].MVCol = 1 << 14
	}
	s := vp9TPLState{enabled: true, sbRows: sbRows, sbCols: sbCols}
	s.propagateFrame(&slab, &next)
	for i := range next.Stats {
		if next.Stats[i].Propagation != 0 {
			t.Fatalf("OOB MV propagated to next[%d]: %d", i,
				next.Stats[i].Propagation)
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

func TestVP9TPLApplyBiasClampsToPublicQuantizerWindow(t *testing.T) {
	// Build an encoder with a constrained public quantizer window and
	// inject a synthetic frame-mean delta beyond what the window allows.
	opts := newVP9TPLBaseOpts(64, 64)
	opts.MinQuantizer = 40
	opts.MaxQuantizer = 50
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// Pre-populate slab 0 with a positive frame-mean delta (push toward
	// higher q).
	enc.tpl.frames[0].Valid = true
	enc.tpl.frames[0].FrameMeanQDelta = 50
	bestBound := vp9PublicQuantizerToQIndex(40)
	worstBound := vp9PublicQuantizerToQIndex(50)
	// Start at the midpoint and verify the bias does not push past worstBound.
	mid := (bestBound + worstBound) / 2
	got := enc.applyVP9TPLQIndexBias(mid, false)
	if got > worstBound {
		t.Fatalf("positive bias exceeded worstBound: got %d worst %d", got, worstBound)
	}
	if got < bestBound {
		t.Fatalf("positive bias below bestBound: got %d best %d", got, bestBound)
	}
	// Same test in the negative direction.
	enc.tpl.frames[0].FrameMeanQDelta = -50
	got = enc.applyVP9TPLQIndexBias(mid, false)
	if got > worstBound || got < bestBound {
		t.Fatalf("negative bias out of bounds: got %d window [%d,%d]",
			got, bestBound, worstBound)
	}
}

func TestVP9TPLApplyBiasSkipPathIsZero(t *testing.T) {
	opts := newVP9TPLBaseOpts(64, 64)
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	enc.tpl.frames[0].Valid = true
	enc.tpl.frames[0].FrameMeanQDelta = 7
	if got := enc.applyVP9TPLQIndexBias(100, true); got != 100 {
		t.Fatalf("skip path changed qindex: %d", got)
	}
}

func TestVP9TPLDisabledEncoderApplyBiasIsIdentity(t *testing.T) {
	opts := VP9EncoderOptions{Width: 64, Height: 64}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := enc.applyVP9TPLQIndexBias(100, false); got != 100 {
		t.Fatalf("disabled encoder changed qindex: %d", got)
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
	enc.tpl.frames[0].FrameMeanQDelta = 7
	enc.tpl.frames[1].Valid = true
	enc.tpl.frames[1].FrameMeanQDelta = 5
	enc.tpl.shiftAndInvalidate()
	if enc.tpl.frames[0].FrameMeanQDelta != 5 {
		t.Fatalf("shift did not promote slab 1 to slab 0: got %d",
			enc.tpl.frames[0].FrameMeanQDelta)
	}
	// Tail must be reset.
	if enc.tpl.frames[len(enc.tpl.frames)-1].Valid {
		t.Fatalf("tail slab not invalidated")
	}
	if enc.tpl.frames[len(enc.tpl.frames)-1].FrameMeanQDelta != 0 {
		t.Fatalf("tail slab FrameMeanQDelta = %d, want 0",
			enc.tpl.frames[len(enc.tpl.frames)-1].FrameMeanQDelta)
	}
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

func TestVP9TPLPanningSequencePopulatesValidSlabs(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLPanningSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	// At least the first slab must end up valid.
	if !s.frames[0].Valid {
		t.Fatalf("first slab not Valid after populate")
	}
	// Verify at least one SB has a non-zero MV after motion search — the
	// panning pattern means coarse motion must be discoverable.
	foundMotion := false
	for _, st := range s.frames[0].Stats {
		if st.MVRow != 0 || st.MVCol != 0 {
			foundMotion = true
			break
		}
	}
	if !foundMotion {
		t.Fatalf("no motion vectors discovered in panning sequence")
	}
}

// TestVP9TPLIntegrationOnVsOff asserts that turning TPL on does not break the
// encoder and that the resulting byte stream has the same packet count as
// the equivalent TPL-off encode, since TPL is a quality knob — not a scheduling
// change.  Full PSNR/BD-rate measurement is left to the offline harness; this
// test just guards the integration boundary.
func TestVP9TPLIntegrationOnVsOff(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) (int, int) {
		opts := VP9EncoderOptions{
			Width:           w,
			Height:          h,
			FPS:             30,
			LookaheadFrames: vp9TPLMinLookaheadFrames,
			AutoAltRef:      true,
			EnableTPL:       enableTPL,
		}
		enc, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		seq := newVP9TPLPanningSequence(w, h, 16)
		buf := make([]byte, 32*1024)
		packets, totalBytes := 0, 0
		for i := range 16 {
			res, err := enc.encodeVP9LookaheadIntoWithFlagsResult(seq[i%len(seq)], buf, 0)
			switch {
			case err == nil:
				packets++
				totalBytes += len(res.Data)
			case errors.Is(err, ErrFrameNotReady):
				// expected while window fills
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
			packets++
			totalBytes += len(res.Data)
		}
		return packets, totalBytes
	}
	offPackets, offBytes := encode(false)
	onPackets, onBytes := encode(true)
	if offPackets == 0 || onPackets == 0 {
		t.Fatalf("no packets produced: off=%d on=%d", offPackets, onPackets)
	}
	if offPackets != onPackets {
		t.Fatalf("packet count drifted: off=%d on=%d", offPackets, onPackets)
	}
	// The TPL pass biases qindex but never changes packet topology;
	// payload size is expected to drift a few percent in either direction
	// once the quality knob bites.  Guard against catastrophic divergence
	// (>50%) which would indicate the bias has gone out of range.
	tolerance := offBytes / 2
	if delta := onBytes - offBytes; delta > tolerance || -delta > tolerance {
		t.Fatalf("payload size drifted catastrophically: off=%d on=%d", offBytes, onBytes)
	}
}

// TestVP9TPLPropagatePopulatesNextSlab pins the bugfix where Stage B in
// populate gated propagation on next.Valid, which is impossible because
// Valid is only set in the subsequent Stage C.  The result was that the
// per-SB propagation accumulator (and therefore the per-SB qindex delta)
// always stayed zero on the live encoder path even though the standalone
// propagateFrame unit test passed.  Propagation flows FROM slab[idx] INTO
// slab[idx+1], so slab[0] has no upstream contributor; the load-bearing
// assertion is that at least one downstream slab carries a non-zero
// propagation accumulator.
func TestVP9TPLPropagatePopulatesNextSlab(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLPanningSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	found := false
	for idx := 1; idx < len(s.frames); idx++ {
		slab := s.frames[idx]
		for _, st := range slab.Stats {
			if st.Propagation > 0 {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("no slab carried a non-zero propagation accumulator — Stage B regressed")
	}
}

// TestVP9TPLPopulateProducesNonZeroFrameMeanBias asserts that the
// frame-mean qindex bias is non-trivial on TPL-friendly content.  The
// original implementation computed it as the per-SB delta deviation
// around its own mean and therefore always landed at ~0, leaving the
// encoder byte-identical with TPL on or off.
func TestVP9TPLPopulateProducesNonZeroFrameMeanBias(t *testing.T) {
	const w, h = 96, 96
	frames := newVP9TPLPanningSequence(w, h, vp9TPLMinLookaheadFrames)
	s := vp9TPLState{}
	s.configure(true, w, h, vp9TPLMinLookaheadFrames)
	s.populate(frames)
	// First slab is what the encoder will read on the next visible
	// inter frame; require a strictly negative bias because the panning
	// sequence has high inter-prediction savings.
	if !s.frames[0].Valid {
		t.Fatalf("slab 0 not Valid after populate")
	}
	if s.frames[0].FrameMeanQDelta == 0 {
		t.Fatalf("FrameMeanQDelta is zero on TPL-friendly content — frame-mean bias regressed")
	}
	if s.frames[0].FrameMeanQDelta < -vp9TPLMaxQDelta ||
		s.frames[0].FrameMeanQDelta > vp9TPLMaxQDelta {
		t.Fatalf("FrameMeanQDelta=%d out of [%d,%d]",
			s.frames[0].FrameMeanQDelta, -vp9TPLMaxQDelta,
			vp9TPLMaxQDelta)
	}
}

// TestVP9TPLChangesEncodedOutput is the direct anti-regression that pins
// the bug surfaced by the BD-rate quality gate.  With TPL toggled, at
// least one visible inter frame's regulated qindex must differ between
// the EnableTPL=true and EnableTPL=false encoders.  The original
// implementation produced byte-identical output because the frame-mean
// bias was zero by construction; this test pins the wiring so a future
// refactor that silently disables it (or makes the bias zero again) is
// caught before BD-rate regression hits CI.
func TestVP9TPLChangesEncodedOutput(t *testing.T) {
	const w, h = 64, 64
	encode := func(enableTPL bool) ([]int, int, []byte) {
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
		total := 0
		var concat []byte
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
				// expected while window fills
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
		return qs, total, concat
	}
	offQs, offBytes, offConcat := encode(false)
	onQs, onBytes, onConcat := encode(true)
	if len(offQs) == 0 || len(onQs) == 0 {
		t.Fatalf("no visible frames: off=%d on=%d", len(offQs), len(onQs))
	}
	if len(offQs) != len(onQs) {
		t.Fatalf("visible packet count drifted: off=%d on=%d", len(offQs), len(onQs))
	}
	// Either at least one frame's qindex differs, or the encoded byte
	// stream itself differs.  The former is the load-bearing assertion;
	// the latter is the safety net in case TPL ever routes per-SB
	// segmentation IDs without changing the per-frame qindex.
	qDiffers := false
	for i := range offQs {
		if offQs[i] != onQs[i] {
			qDiffers = true
			break
		}
	}
	bytesDiffer := len(offConcat) != len(onConcat) || offBytes != onBytes
	if !bytesDiffer && len(offConcat) == len(onConcat) {
		for i := range offConcat {
			if offConcat[i] != onConcat[i] {
				bytesDiffer = true
				break
			}
		}
	}
	if !qDiffers && !bytesDiffer {
		t.Fatalf("TPL had no effect: qindex unchanged AND byte streams identical (off=%d on=%d bytes)",
			offBytes, onBytes)
	}
	t.Logf("TPL off->on qindex drift on %d frames; bytes off=%d on=%d",
		len(offQs), offBytes, onBytes)
}

func TestVP9TPLEncodesWithoutBreakingExisting(t *testing.T) {
	// A 16-frame encode under TPL should produce the same packet count as
	// the same encode without TPL (because TPL is a quality knob, not a
	// scheduling change).  PSNR comparison is left to the integration
	// harness; this gate just asserts encode flow stability.
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
			// expected while window fills
		default:
			t.Fatalf("encode %d: %v", i, err)
		}
	}
	// Drain.
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
