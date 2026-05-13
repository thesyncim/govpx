package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteSkipRoundTrip emits a 0/1 skip bit via WriteSkip and
// confirms ReadSkipWithSeg recovers each value.
func TestWriteSkipRoundTrip(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fc.SkipProbs[0] = 128

	args := WriteSkipArgs{
		Seg:       &seg,
		SegID:     0,
		SkipProbs: fc.SkipProbs,
	}

	for _, skip := range []int{0, 1} {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteSkip(&bw, args, skip)
		size, _ := bw.Stop()

		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadSkipWithSeg(&r, &seg, 0, &fc, nil, nil)
		if got != skip {
			t.Errorf("skip=%d round-tripped to %d", skip, got)
		}
	}
}

// TestWriteSkipSegmentOverride: SEG_LVL_SKIP active → writer emits
// nothing, decoder returns 1.
func TestWriteSkipSegmentOverride(t *testing.T) {
	var seg vp9dec.SegmentationParams
	seg.Enabled = true
	seg.FeatureMask[2] = 1 << vp9dec.SegLvlSkip
	args := WriteSkipArgs{Seg: &seg, SegID: 2}

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteSkip(&bw, args, 1)
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Decoder reads back via ReadSkipWithSeg — it short-circuits to
	// 1 without consuming bits.
	var fc vp9dec.FrameContext
	var r bitstream.Reader
	r.Init([]byte{})
	if got := vp9dec.ReadSkipWithSeg(&r, &seg, 2, &fc, nil, nil); got != 1 {
		t.Errorf("seg override skip = %d, want 1", got)
	}
}

// TestWriteIsInterBlockRoundTrip emits intra/inter for both
// directions and confirms the decoder's ReadIntraInterFlag reads
// the matching value.
func TestWriteIsInterBlockRoundTrip(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fc.IntraInterProb[0] = 128

	for _, want := range []int{0, 1} {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteIsInterBlock(&bw, &seg, 0, fc.IntraInterProb, nil, nil, want)
		size, _ := bw.Stop()

		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadIntraInterFlag(&r, &fc, nil, nil)
		if got != want {
			t.Errorf("is_inter=%d round-tripped to %d", want, got)
		}
	}
}

// TestWriteRefFramesSingleLast emits a single-reference block with
// rf[0]=LAST_FRAME and confirms ReadRefFrames recovers (LAST, NoRef).
func TestWriteRefFramesSingleLast(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fc.ReferenceModeProbs.SingleRefProb[2][0] = 128 // ctx=2 (no edges)
	refs := vp9dec.CompoundFrameRefs{CompFixedRef: vp9dec.AltrefFrame}
	var sb [vp9dec.MaxRefFrames]uint8

	wargs := WriteRefFramesArgs{
		Seg:              &seg,
		FrameMode:        vp9dec.SingleReference,
		CompFixedRef:     refs.CompFixedRef,
		CompVarRef:       refs.CompVarRef,
		RefFrameSignBias: sb,
		CompInterProb:    fc.ReferenceModeProbs.CompInterProb,
		CompRefProb:      fc.ReferenceModeProbs.CompRefProb,
		SingleRefProb:    fc.ReferenceModeProbs.SingleRefProb,
		RefFrame:         [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteRefFrames(&bw, wargs)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	var out [2]int8
	vp9dec.ReadRefFrames(&r, vp9dec.SingleReference, sb, refs, &seg, 0, &fc, nil, nil, &out)
	if out[0] != vp9dec.LastFrame || out[1] != vp9dec.NoRefFrame {
		t.Errorf("got (%d, %d), want (LAST, NoRef)", out[0], out[1])
	}
}

// TestWriteRefFramesSingleAltref: emit bit0=1 then bit1=1 → ALTREF.
func TestWriteRefFramesSingleAltref(t *testing.T) {
	var seg vp9dec.SegmentationParams
	var fc vp9dec.FrameContext
	fc.ReferenceModeProbs.SingleRefProb[2][0] = 128
	fc.ReferenceModeProbs.SingleRefProb[2][1] = 128
	refs := vp9dec.CompoundFrameRefs{CompFixedRef: vp9dec.AltrefFrame}
	var sb [vp9dec.MaxRefFrames]uint8

	wargs := WriteRefFramesArgs{
		Seg:              &seg,
		FrameMode:        vp9dec.SingleReference,
		CompFixedRef:     refs.CompFixedRef,
		CompVarRef:       refs.CompVarRef,
		RefFrameSignBias: sb,
		SingleRefProb:    fc.ReferenceModeProbs.SingleRefProb,
		RefFrame:         [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame},
	}

	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteRefFrames(&bw, wargs)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	var out [2]int8
	vp9dec.ReadRefFrames(&r, vp9dec.SingleReference, sb, refs, &seg, 0, &fc, nil, nil, &out)
	if out[0] != vp9dec.AltrefFrame {
		t.Errorf("got out[0]=%d, want ALTREF", out[0])
	}
}
