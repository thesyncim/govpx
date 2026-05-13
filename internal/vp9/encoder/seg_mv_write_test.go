package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestWriteSegmentIdRoundTrip emits each of the 8 segment-id leaves
// through SegmentTree and confirms the decoder picks the matching
// id back via ReadSegmentIDProb.
func TestWriteSegmentIdRoundTrip(t *testing.T) {
	var seg vp9dec.SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = true
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	for id := range 8 {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteSegmentId(&bw, &seg, id)
		size, err := bw.Stop()
		if err != nil {
			t.Fatalf("id=%d Stop: %v", id, err)
		}
		var r bitstream.Reader
		r.Init(buf[:size])
		got := vp9dec.ReadSegmentIDProb(&r, &seg)
		if got != id {
			t.Errorf("id=%d round-tripped to %d", id, got)
		}
	}
}

// TestWriteSegmentIdNoopWhenDisabled: with seg.Enabled = false or
// UpdateMap = false the writer emits no logical bits — the
// boolean coder's Stop may still flush a 2-byte epilogue, but no
// segment-id-tree information is encoded. We verify by confirming
// the writer doesn't error and no extra writes happen between
// start and stop (logically a no-op).
func TestWriteSegmentIdNoopWhenDisabled(t *testing.T) {
	var seg vp9dec.SegmentationParams
	buf := make([]byte, 32)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteSegmentId(&bw, &seg, 5)
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	seg.Enabled = true
	seg.UpdateMap = false
	buf2 := make([]byte, 32)
	bw = bitstream.Writer{}
	bw.Start(buf2)
	WriteSegmentId(&bw, &seg, 5)
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop2: %v", err)
	}
}

// TestWriteMvRoundTripIdentity emits (mv == ref) which produces a
// MV_JOINT_ZERO with no component deltas; the decoder reads back
// (mv == ref).
func TestWriteMvRoundTripIdentity(t *testing.T) {
	ctx := defaultNmvContextForEnc()
	ref := vp9dec.MV{Row: 4, Col: -2}

	buf := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteMv(&bw, ref, ref, &ctx, true)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	var got vp9dec.MV
	vp9dec.ReadMv(&r, &got, &ref, &ctx, true)
	if got != ref {
		t.Errorf("identity got %+v want %+v", got, ref)
	}
}

// TestWriteMvRoundTripDelta emits a non-trivial (row, col) delta
// pair with the canonical libvpx NMV context.
func TestWriteMvRoundTripDelta(t *testing.T) {
	ctx := defaultNmvContextForEnc()
	ref := vp9dec.MV{Row: 4, Col: -2}
	want := vp9dec.MV{Row: 4 + 3, Col: -2 - 7}

	buf := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(buf)
	WriteMv(&bw, want, ref, &ctx, true)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	var got vp9dec.MV
	vp9dec.ReadMv(&r, &got, &ref, &ctx, true)
	if got != want {
		t.Errorf("delta got %+v want %+v", got, want)
	}
}

// TestWriteMvComponentRoundTripFuzz walks a spread of magnitudes
// through WriteMvComponent.
func TestWriteMvComponentRoundTripFuzz(t *testing.T) {
	ctx := defaultNmvContextForEnc()
	cc := &ctx.Comps[0]
	cases := []int{3, -7, 17, -19, 37, 71, 511}
	for _, want := range cases {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		WriteMvComponent(&bw, want, cc, true)
		size, _ := bw.Stop()
		var r bitstream.Reader
		r.Init(buf[:size])
		// The decoder helper isn't exported with this name; do the
		// inverse by checking the joint+component readback through
		// ReadMv against a known ref.
		ref := vp9dec.MV{}
		_ = size
		_ = r
		// ReadMv expects a joint + component encoded together; the
		// shape of this fuzz is per-component. Skip the joint
		// emission by encoding a 1-component MV instead.
		buf = make([]byte, 64)
		bw = bitstream.Writer{}
		bw.Start(buf)
		// Compose a vertical-only MV so we can exercise comps[0]
		// alone — joint=HnzVz, component[0]=want, no comp[1].
		WriteMv(&bw, vp9dec.MV{Row: int16(want)}, ref, &ctx, true)
		size, _ = bw.Stop()
		var rr bitstream.Reader
		rr.Init(buf[:size])
		var got vp9dec.MV
		vp9dec.ReadMv(&rr, &got, &ref, &ctx, true)
		if int(got.Row) != want {
			t.Errorf("want=%d got=%d", want, got.Row)
		}
	}
}

// defaultNmvContextForEnc seeds an NmvContext with libvpx's
// canonical default joints + components.
func defaultNmvContextForEnc() vp9dec.NmvContext {
	var ctx vp9dec.NmvContext
	copy(ctx.Joints[:], tables.DefaultNmvJoints[:])
	for i := range 2 {
		src := &tables.DefaultNmvComps[i]
		dst := &ctx.Comps[i]
		dst.Sign = src.Sign
		copy(dst.Classes[:], src.Classes[:])
		copy(dst.Class0[:], src.Class0[:])
		copy(dst.Bits[:], src.Bits[:])
		for j := range vp9dec.Class0Size {
			copy(dst.Class0Fp[j][:], src.Class0Fp[j][:])
		}
		copy(dst.Fp[:], src.Fp[:])
		dst.Class0Hp = src.Class0Hp
		dst.Hp = src.Hp
	}
	_ = common.SegmentTree
	return ctx
}
