package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

func TestMacroblockCornerGradientMatchesLibvpxFormula(t *testing.T) {
	// 16x16 plane with stride 16: every value 50 except the top-left corner pixel = 90.
	// Top-left corner (offRow=0, offCol=0, sgnRow=1, sgnCol=1) should yield max(|90-50|, ...) = 40.
	plane := make([]byte, 16*16)
	for i := range plane {
		plane[i] = 50
	}
	plane[0] = 90
	if got := macroblockCornerGradient(plane, 16, 0, 0, 1, 1); got != 40 {
		t.Fatalf("top-left gradient = %d, want 40", got)
	}
	// Flat plane: all corners should yield 0.
	for i := range plane {
		plane[i] = 50
	}
	if got := macroblockCornerGradient(plane, 16, 0, 15, 1, -1); got != 0 {
		t.Fatalf("flat top-right gradient = %d, want 0", got)
	}
}

func TestDotArtifactCornerCandidateYDetectsSharpRefAndFlatSrc(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	last := vp8common.FrameBuffer{}
	if err := last.Resize(16, 16, 16, 16); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Flat last reference: not a candidate.
	for i := range last.Img.Y {
		last.Img.Y[i] = 128
	}
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("flat last_ref should not be a dot-artifact candidate")
	}
	// Sharp gradient at top-left corner of last_ref: should be a candidate.
	last.Img.Y[0] = 200
	if !dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("sharp last_ref corner over flat src should be a candidate")
	}
	// If source also has sharp gradient, no longer a candidate.
	src.Y[0] = 200
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("matching sharp source should suppress candidate")
	}
}

func TestCheckDotArtifactCandidateGatesOnLayerScreenContentAndConsecZeroLast(t *testing.T) {
	// Use a 64x64 encoder (16 MBs => cap = 16/10 = 1) so the cap is non-zero.
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner of MB(0,0) on last_ref Y plane.
	e.lastRef.Img.Y[0] = 230

	// mvbias counter below threshold => not a candidate.
	e.consecZeroLastMVBias[0] = 5
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("low consec_zero_last_mvbias should not trigger dot-artifact bias")
	}
	if e.dotArtifactChecked[0] {
		t.Fatalf("ineligible MB should not set dotArtifactChecked")
	}
	// Above threshold => candidate; sets the per-MB checked flag.
	e.consecZeroLastMVBias[0] = 50
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("high mvbias counter with sharp last_ref should be a candidate")
	}
	if !e.dotArtifactChecked[0] {
		t.Fatalf("eligible MB should set dotArtifactChecked")
	}
	if e.mbsZeroLastDotSuppress != 1 {
		t.Fatalf("mbsZeroLastDotSuppress = %d, want 1 after candidate", e.mbsZeroLastDotSuppress)
	}
	// Screen content disables it.
	e.mbsZeroLastDotSuppress = 0
	e.opts.ScreenContentMode = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("screen content should disable dot-artifact bias")
	}
	e.opts.ScreenContentMode = 0
	// Non-base layer disables it.
	e.currentTemporalLayer = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("non-base temporal layer should disable dot-artifact bias")
	}
	e.currentTemporalLayer = 0
	// Cap reached.
	e.mbsZeroLastDotSuppress = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("cap-reached suppression should disable further bias")
	}
}

func TestCheckDotArtifactCandidateChecksUVChannelsWhenYIsFlat(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	// Flat Y on last_ref so Y check returns false.
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner on U plane only.
	for i := range e.lastRef.Img.U {
		e.lastRef.Img.U[i] = 128
	}
	for i := range e.lastRef.Img.V {
		e.lastRef.Img.V[i] = 128
	}
	e.lastRef.Img.U[0] = 230
	e.consecZeroLastMVBias[0] = 50
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp U-plane corner should trigger dot-artifact bias when Y is flat")
	}
	// Reset and probe V plane only.
	e.lastRef.Img.U[0] = 128
	e.lastRef.Img.V[0] = 230
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp V-plane corner should trigger dot-artifact bias when Y/U are flat")
	}
}

// TestCheckDotArtifactCandidatePerThreadCapIsIndependent mirrors libvpx's
// per-MACROBLOCK x->mbs_zero_last_dot_suppress cap of MBs/10 in
// vp8/encoder/pickinter.c:80. The threaded path gives each row worker its
// own shallow VP8Encoder copy (see rowEncoderState.reset), so the cap must
// apply per-worker, NOT through a shared frame-global atomic. This ensures
// MT runs can produce up to N*MBs/10 dot-suppress triggers (one per thread)
// — same as libvpx's ethreading.c:486 per-thread reset — and the per-MB
// gating is deterministic regardless of scheduling order.

func TestCheckDotArtifactCandidatePerThreadCapIsIndependent(t *testing.T) {
	// 64x64 frame => 16 MBs => cap = 16/10 = 1 hit per thread.
	const w, h = 64, 64
	src := testImage(w, h)
	fillImage(src, 128, 128, 128)

	// Build two independent worker views: each shallow-copies the master
	// encoder and gets its own mbsZeroLastDotSuppress counter just like
	// rowEncoderState.reset does (matching libvpx setup_mbby_copy +
	// vp8cx_init_mbrthread_data which both clear the field per thread).
	master := newSizedTestEncoder(t, w, h)
	for i := range master.lastRef.Img.Y {
		master.lastRef.Img.Y[i] = 128
	}
	master.lastRef.Img.Y[0] = 230 // sharp top-left of MB(0,0) on last_ref
	master.consecZeroLastMVBias[0] = 50
	// Sharp top-left of MB(0,1) on last_ref: a second-MB candidate to prove
	// the second per-thread slot is independently available.
	master.lastRef.Img.Y[16] = 230
	master.consecZeroLastMVBias[1] = 50

	// Worker A: a shallow encoder view (analogous to rowEncoderState.enc).
	workerA := *master
	workerA.threadedRowsActive = true
	workerA.mbsZeroLastDotSuppress = 0
	// Worker B: an independent shallow view sharing the same source/refs.
	workerB := *master
	workerB.threadedRowsActive = true
	workerB.mbsZeroLastDotSuppress = 0

	if !workerA.checkDotArtifactCandidate(sourceImageFromPublic(src), &workerA.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("worker A: first eligible MB should be a candidate")
	}
	if workerA.mbsZeroLastDotSuppress != 1 {
		t.Fatalf("worker A counter = %d after first trigger, want 1", workerA.mbsZeroLastDotSuppress)
	}
	// Worker A is now at its per-thread cap (1) and must reject the next MB.
	if workerA.checkDotArtifactCandidate(sourceImageFromPublic(src), &workerA.lastRef.Img, 0, 1, 4, 4) {
		t.Fatalf("worker A: second MB should be capped (per-thread MBs/10 limit)")
	}

	// Worker B's slot must still be available — libvpx caps PER THREAD, not
	// frame-globally. Pre-fix this assertion would fail because a shared
	// atomic budget on the master would have been consumed by worker A.
	if !workerB.checkDotArtifactCandidate(sourceImageFromPublic(src), &workerB.lastRef.Img, 0, 1, 4, 4) {
		t.Fatalf("worker B: per-thread cap should be independent of worker A")
	}
	if workerB.mbsZeroLastDotSuppress != 1 {
		t.Fatalf("worker B counter = %d after first trigger, want 1", workerB.mbsZeroLastDotSuppress)
	}
}
