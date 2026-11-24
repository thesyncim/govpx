package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

func TestSegFeatureActiveAndGetSegData(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.FeatureMask[3] = (1 << SegLvlRefFrame) | (1 << SegLvlSkip)
	seg.FeatureData[3][SegLvlRefFrame] = AltrefFrame
	seg.FeatureData[3][SegLvlSkip] = 0

	if !SegFeatureActive(&seg, 3, SegLvlRefFrame) {
		t.Error("ref-frame should be active for seg 3")
	}
	if !SegFeatureActive(&seg, 3, SegLvlSkip) {
		t.Error("skip should be active for seg 3")
	}
	if SegFeatureActive(&seg, 3, SegLvlAltQ) {
		t.Error("alt-q should be inactive for seg 3")
	}
	if SegFeatureActive(&seg, 5, SegLvlRefFrame) {
		t.Error("ref-frame should be inactive for seg 5")
	}
	if got := GetSegData(&seg, 3, SegLvlRefFrame); got != AltrefFrame {
		t.Errorf("seg-data = %d, want %d", got, AltrefFrame)
	}

	// Disabled segmentation forces every feature off.
	seg.Enabled = false
	if SegFeatureActive(&seg, 3, SegLvlRefFrame) {
		t.Error("disabled segmentation should report all features inactive")
	}
}

// TestReadSkipWithSegForcedByFeature: when SEG_LVL_SKIP is set the
// per-block bit isn't sent — caller never touches the reader.
func TestReadSkipWithSegForcedByFeature(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.FeatureMask[2] = 1 << SegLvlSkip
	var fc FrameContext
	var r bitstream.Reader
	if got := ReadSkipWithSeg(&r, &seg, 2, &fc, nil, nil); got != 1 {
		t.Errorf("forced-skip got %d, want 1", got)
	}
}

// TestReadSkipWithSegReadsBit covers the normal path: ctx-0 (no
// neighbors), reader sees a 0 bit, output is 0.
func TestReadSkipWithSegReadsBit(t *testing.T) {
	var seg SegmentationParams
	var fc FrameContext
	fc.SkipProbs[0] = 128

	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128)
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// TestReadBlockReferenceModeFrameLevel: when the frame-level mode
// isn't ReferenceModeSelect, no bit is consumed.
func TestReadBlockReferenceModeFrameLevel(t *testing.T) {
	var fc FrameContext
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}
	var r bitstream.Reader
	if got := ReadBlockReferenceMode(&r, SingleReference, &fc, refs, nil, nil); got != SingleReference {
		t.Errorf("got %d, want SingleReference", got)
	}
	if got := ReadBlockReferenceMode(&r, CompoundReference, &fc, refs, nil, nil); got != CompoundReference {
		t.Errorf("got %d, want CompoundReference", got)
	}
}

// TestReadBlockReferenceModeSelect: select mode reads one bit. Bit=0
// → Single, bit=1 → Compound.
func TestReadBlockReferenceModeSelect(t *testing.T) {
	var fc FrameContext
	fc.ReferenceModeProbs.CompInterProb[1] = 128 // ctx=1 when no edges
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}

	for _, c := range []struct {
		bit  uint32
		want ReferenceMode
	}{
		{0, SingleReference},
		{1, CompoundReference},
	} {
		buf := make([]byte, 8)
		var w bitstream.Writer
		w.Start(buf)
		w.Write(c.bit, 128)
		size, _ := w.Stop()
		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if got := ReadBlockReferenceMode(&r, ReferenceModeSelect, &fc, refs, nil, nil); got != c.want {
			t.Errorf("bit=%d: got %d, want %d", c.bit, got, c.want)
		}
	}
}

// TestReadRefFramesSegmentOverride: with SEG_LVL_REF_FRAME active the
// segment data wins and nothing is read.
func TestReadRefFramesSegmentOverride(t *testing.T) {
	var seg SegmentationParams
	seg.Enabled = true
	seg.FeatureMask[1] = 1 << SegLvlRefFrame
	seg.FeatureData[1][SegLvlRefFrame] = GoldenFrame

	var fc FrameContext
	var sb [MaxRefFrames]uint8
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}
	var out [2]int8
	var r bitstream.Reader
	ReadRefFrames(&r, ReferenceModeSelect, sb, refs, &seg, 1, &fc, nil, nil, &out)
	if out[0] != GoldenFrame || out[1] != NoRefFrame {
		t.Errorf("got (%d,%d), want (%d,%d)", out[0], out[1], GoldenFrame, NoRefFrame)
	}
}

// TestReadRefFramesSingleLast: frame mode = SingleReference (so no
// comp_inter bit is read); bit0 = 0 → LastFrame.
func TestReadRefFramesSingleLast(t *testing.T) {
	var seg SegmentationParams
	var fc FrameContext
	fc.ReferenceModeProbs.SingleRefProb[2][0] = 128 // ctx=2 (intra/intra, both nil → 2)
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}
	var sb [MaxRefFrames]uint8

	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, 128) // bit0=0 → LAST_FRAME
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])

	var out [2]int8
	ReadRefFrames(&r, SingleReference, sb, refs, &seg, 0, &fc, nil, nil, &out)
	if out[0] != LastFrame || out[1] != NoRefFrame {
		t.Errorf("got (%d,%d), want (%d,%d)", out[0], out[1], LastFrame, NoRefFrame)
	}
}

// TestReadRefFramesSingleAltref: bit0=1 then bit1=1 → AltrefFrame.
func TestReadRefFramesSingleAltref(t *testing.T) {
	var seg SegmentationParams
	var fc FrameContext
	fc.ReferenceModeProbs.SingleRefProb[2][0] = 128
	fc.ReferenceModeProbs.SingleRefProb[2][1] = 128
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}
	var sb [MaxRefFrames]uint8

	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, 128)
	w.Write(1, 128)
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])

	var out [2]int8
	ReadRefFrames(&r, SingleReference, sb, refs, &seg, 0, &fc, nil, nil, &out)
	if out[0] != AltrefFrame || out[1] != NoRefFrame {
		t.Errorf("got (%d,%d), want (%d,%d)", out[0], out[1], AltrefFrame, NoRefFrame)
	}
}

// TestReadRefFramesCompound: CompoundReference frame mode + sign-bias
// puts CompFixedRef at idx=0; the comp_ref bit picks the var slot.
func TestReadRefFramesCompound(t *testing.T) {
	var seg SegmentationParams
	var fc FrameContext
	fc.ReferenceModeProbs.CompRefProb[2] = 128 // ctx=2 (no edges)
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame, CompVarRef: [2]int8{LastFrame, GoldenFrame}}

	var sb [MaxRefFrames]uint8
	// signBias[AltrefFrame]=0 → idx=0; out[0]=AltrefFrame, out[1]=var[bit].

	buf := make([]byte, 8)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, 128) // bit=1 → CompVarRef[1]=GoldenFrame
	size, _ := w.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])

	var out [2]int8
	ReadRefFrames(&r, CompoundReference, sb, refs, &seg, 0, &fc, nil, nil, &out)
	if out[0] != AltrefFrame || out[1] != GoldenFrame {
		t.Errorf("got (%d,%d), want (%d,%d)", out[0], out[1], AltrefFrame, GoldenFrame)
	}
}

// TestDecGetSegmentId: returns the minimum seg-id in the window.
func TestDecGetSegmentId(t *testing.T) {
	cm := []uint8{
		3, 3, 1, 5,
		2, 4, 1, 5,
		3, 3, 2, 5,
		3, 3, 3, 5,
	}
	miCols := 4
	got := DecGetSegmentId(cm, miCols, 0, 3, 3)
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
	// Empty window returns MaxSegments.
	got = DecGetSegmentId(cm, miCols, 0, 0, 0)
	if got != MaxSegments {
		t.Errorf("empty window: got %d, want %d", got, MaxSegments)
	}
}
