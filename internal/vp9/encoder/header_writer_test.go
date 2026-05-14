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

// TestIntraOnlyHeaderRoundTrip exercises the intra-only branch:
// non-key frame, show_frame=0, intra_only=1. Refresh flags + frame
// size round-trip; profile-0 inherits the 8-bit 4:2:0 defaults from
// the parser.
func TestIntraOnlyHeaderRoundTrip(t *testing.T) {
	want := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             false,
		ErrorResilientMode:    false,
		IntraOnly:             true,
		ResetFrameContext:     2,
		RefreshFrameFlags:     0b1010_0001,
		Width:                 320,
		Height:                240,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		FrameContextIdx:       3,
		// Profile-0 intra-only inherits the (8-bit, BT601, studio,
		// 4:2:0) defaults from the parser; we don't write the
		// bitdepth/colorspace bits in this branch.
		FirstPartitionSize: 64,
	}
	want.Loopfilter.FilterLevel = 12
	want.Quant.BaseQindex = 96

	buf := make([]byte, 128)
	w := NewBitWriter(buf)
	n := WriteIntraOnlyUncompressedHeader(w, &want)
	if n <= 0 {
		t.Fatalf("WriteIntraOnlyUncompressedHeader returned %d bytes", n)
	}

	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.FrameType != want.FrameType || got.ShowFrame {
		t.Errorf("frame flags = (FrameType=%d, ShowFrame=%v), want (Inter, false)",
			got.FrameType, got.ShowFrame)
	}
	if !got.IntraOnly {
		t.Error("IntraOnly = false, want true")
	}
	if got.ResetFrameContext != want.ResetFrameContext {
		t.Errorf("ResetFrameContext = %d, want %d", got.ResetFrameContext, want.ResetFrameContext)
	}
	if got.RefreshFrameFlags != want.RefreshFrameFlags {
		t.Errorf("RefreshFrameFlags = %#b, want %#b", got.RefreshFrameFlags, want.RefreshFrameFlags)
	}
	if got.Width != want.Width || got.Height != want.Height {
		t.Errorf("size = (%d, %d), want (%d, %d)", got.Width, got.Height, want.Width, want.Height)
	}
	if got.Loopfilter.FilterLevel != want.Loopfilter.FilterLevel {
		t.Errorf("FilterLevel = %d, want %d", got.Loopfilter.FilterLevel, want.Loopfilter.FilterLevel)
	}
	if got.Quant.BaseQindex != want.Quant.BaseQindex {
		t.Errorf("BaseQindex = %d, want %d", got.Quant.BaseQindex, want.Quant.BaseQindex)
	}
	if got.FirstPartitionSize != want.FirstPartitionSize {
		t.Errorf("FirstPartitionSize = %d, want %d", got.FirstPartitionSize, want.FirstPartitionSize)
	}
	if got.FrameContextIdx != want.FrameContextIdx {
		t.Errorf("FrameContextIdx = %d, want %d", got.FrameContextIdx, want.FrameContextIdx)
	}
}

func TestShowExistingFrameHeaderRoundTrip(t *testing.T) {
	buf := make([]byte, 8)
	w := NewBitWriter(buf)
	n := WriteShowExistingFrameHeader(w, common.Profile0, 5)
	if n != 1 {
		t.Fatalf("WriteShowExistingFrameHeader returned %d bytes, want 1", n)
	}

	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if !got.ShowExistingFrame || got.ExistingFrameSlot != 5 {
		t.Fatalf("show-existing = %v slot %d, want true slot 5",
			got.ShowExistingFrame, got.ExistingFrameSlot)
	}
	if br.BytesRead() != n {
		t.Fatalf("BytesRead = %d, want %d", br.BytesRead(), n)
	}
}

// TestInterHeaderRoundTripNoSizeMatch exercises the inter path with
// reference frames whose dimensions don't match the current frame —
// libvpx then emits three "found=0" bits followed by the explicit
// (width-1, height-1) literals.
func TestInterHeaderRoundTripNoSizeMatch(t *testing.T) {
	want := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		ErrorResilientMode:    false,
		IntraOnly:             false,
		ResetFrameContext:     1,
		RefreshFrameFlags:     0x07,
		Width:                 320,
		Height:                240,
		AllowHighPrecisionMv:  true,
		InterpFilter:          vp9dec.InterpEighttapSmooth,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		FrameContextIdx:       0,
		InterRef: vp9dec.InterRefBlock{
			RefIndex: [3]uint8{1, 3, 5},
			SignBias: [3]uint8{0, 1, 0},
		},
		FirstPartitionSize: 100,
	}
	want.Loopfilter.FilterLevel = 16
	want.Quant.BaseQindex = 80

	// refDims: every ref slot is a different size from the current
	// frame, so the "found" scan emits 000 and an explicit size
	// follows.
	refDims := func(slot uint8) (uint32, uint32) {
		return 640, 480 // mismatch
	}

	buf := make([]byte, 128)
	w := NewBitWriter(buf)
	n := WriteInterUncompressedHeader(w, &want, refDims)
	if n <= 0 {
		t.Fatalf("WriteInterUncompressedHeader returned %d bytes", n)
	}

	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, refDims)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.FrameType != common.InterFrame || !got.ShowFrame || got.IntraOnly {
		t.Errorf("frame flags = (FrameType=%d, ShowFrame=%v, IntraOnly=%v)",
			got.FrameType, got.ShowFrame, got.IntraOnly)
	}
	if got.RefreshFrameFlags != want.RefreshFrameFlags {
		t.Errorf("RefreshFrameFlags = %#x, want %#x", got.RefreshFrameFlags, want.RefreshFrameFlags)
	}
	for i := range 3 {
		if got.InterRef.RefIndex[i] != want.InterRef.RefIndex[i] ||
			got.InterRef.SignBias[i] != want.InterRef.SignBias[i] {
			t.Errorf("InterRef[%d] = (%d, %d), want (%d, %d)", i,
				got.InterRef.RefIndex[i], got.InterRef.SignBias[i],
				want.InterRef.RefIndex[i], want.InterRef.SignBias[i])
		}
	}
	if got.Width != want.Width || got.Height != want.Height {
		t.Errorf("size = (%d, %d), want (%d, %d)", got.Width, got.Height, want.Width, want.Height)
	}
	if !got.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv lost")
	}
	if got.InterpFilter != vp9dec.InterpEighttapSmooth {
		t.Errorf("InterpFilter = %d, want %d", got.InterpFilter, vp9dec.InterpEighttapSmooth)
	}
}

// TestInterHeaderRoundTripWithSizeMatch: when a reference frame
// matches the current frame's dimensions, the writer emits a 1-bit
// "found" flag at the matching slot and skips the explicit size.
func TestInterHeaderRoundTripWithSizeMatch(t *testing.T) {
	want := vp9dec.UncompressedHeader{
		Profile:            common.Profile0,
		FrameType:          common.InterFrame,
		ShowFrame:          true,
		Width:              320,
		Height:             240,
		InterpFilter:       vp9dec.InterpSwitchable,
		RefreshFrameFlags:  0x01,
		FirstPartitionSize: 50,
	}
	want.InterRef.RefIndex = [3]uint8{2, 4, 6}
	want.Loopfilter.FilterLevel = 4
	want.Quant.BaseQindex = 50

	// refDims: slot 2 (the first ref) matches the current frame, so
	// the writer emits "found=1" at slot 0 and skips the size.
	refDims := func(slot uint8) (uint32, uint32) {
		if slot == 2 {
			return 320, 240
		}
		return 640, 480
	}

	buf := make([]byte, 128)
	w := NewBitWriter(buf)
	n := WriteInterUncompressedHeader(w, &want, refDims)

	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, refDims)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.Width != 320 || got.Height != 240 {
		t.Errorf("size = (%d, %d), want (320, 240)", got.Width, got.Height)
	}
	if got.InterpFilter != vp9dec.InterpSwitchable {
		t.Errorf("InterpFilter = %d, want Switchable", got.InterpFilter)
	}
}
