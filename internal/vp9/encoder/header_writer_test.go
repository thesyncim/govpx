package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestKeyframeHeaderRoundTrip writes a profile-0 keyframe header and
// then re-parses it through the decoder. Every field that survives
// the wire format must round-trip byte-identical.
func TestKeyframeHeaderRoundTrip(t *testing.T) {
	want := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		ErrorResilientMode:    false,
		RefreshFrameFlags:     0xff, // keyframes refresh all slots
		Width:                 320,
		Height:                240,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		FrameContextIdx:       1,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSBT601,
			ColorRange: common.CRStudioRange,
		},
		FirstPartitionSize: 42,
	}
	want.Loopfilter.FilterLevel = 8
	want.Loopfilter.SharpnessLevel = 2
	want.Quant.BaseQindex = 64

	buf := make([]byte, 128)
	w := NewBitWriter(buf)
	n := WriteKeyframeUncompressedHeader(w, &want)
	if n <= 0 {
		t.Fatalf("WriteKeyframeUncompressedHeader returned %d bytes", n)
	}

	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.Profile != want.Profile {
		t.Errorf("Profile = %d, want %d", got.Profile, want.Profile)
	}
	if got.FrameType != want.FrameType {
		t.Errorf("FrameType = %d, want %d", got.FrameType, want.FrameType)
	}
	if got.ShowFrame != want.ShowFrame {
		t.Errorf("ShowFrame = %v, want %v", got.ShowFrame, want.ShowFrame)
	}
	if got.Width != want.Width || got.Height != want.Height {
		t.Errorf("size = (%d, %d), want (%d, %d)", got.Width, got.Height, want.Width, want.Height)
	}
	if got.FrameContextIdx != want.FrameContextIdx {
		t.Errorf("FrameContextIdx = %d, want %d", got.FrameContextIdx, want.FrameContextIdx)
	}
	if got.RefreshFrameContext != want.RefreshFrameContext {
		t.Errorf("RefreshFrameContext = %v, want %v", got.RefreshFrameContext, want.RefreshFrameContext)
	}
	if got.Loopfilter.FilterLevel != want.Loopfilter.FilterLevel ||
		got.Loopfilter.SharpnessLevel != want.Loopfilter.SharpnessLevel {
		t.Errorf("Loopfilter = %+v, want %+v", got.Loopfilter, want.Loopfilter)
	}
	if got.Quant.BaseQindex != want.Quant.BaseQindex {
		t.Errorf("BaseQindex = %d, want %d", got.Quant.BaseQindex, want.Quant.BaseQindex)
	}
	if got.FirstPartitionSize != want.FirstPartitionSize {
		t.Errorf("FirstPartitionSize = %d, want %d", got.FirstPartitionSize, want.FirstPartitionSize)
	}
	if got.BitDepthColor.ColorSpace != want.BitDepthColor.ColorSpace {
		t.Errorf("ColorSpace = %d, want %d", got.BitDepthColor.ColorSpace, want.BitDepthColor.ColorSpace)
	}
}
