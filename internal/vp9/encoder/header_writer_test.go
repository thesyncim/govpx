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

// TestWriteFrameSizeWithRefsCascadeBitExact pins the exact bit sequence
// writeFrameSizeWithRefs emits across the three per-slot "found" outcomes
// libvpx supports (write_frame_size_with_refs at vp9/encoder/vp9_bitstream.c:
// 1180-1212):
//
//   - slot 0 matches: emits a single 1 bit, breaks (no further found bits,
//     no explicit width/height literal).
//
//   - slot 1 matches: emits 0, then 1, breaks. No explicit literal.
//
//   - slot 2 matches: emits 0, 0, then 1. No explicit literal.
//
//   - no slot matches: emits 0, 0, 0 followed by the 16-bit width-1 and
//     16-bit height-1 literals.
//
// The libvpx loop terminates on the first found and never emits the
// remaining cascade bits (vp9_bitstream.c:1201-1203). Each row below pins
// the encoded prefix so a future refactor cannot accidentally re-introduce
// a "remaining zero bits" emit (which would silently shift the inter
// header by 1-2 bits and desynchronise every byte from refDims forward).
func TestWriteFrameSizeWithRefsCascadeBitExact(t *testing.T) {
	const (
		width  = 320
		height = 240
	)

	header := func() vp9dec.UncompressedHeader {
		h := vp9dec.UncompressedHeader{
			Width:  width,
			Height: height,
		}
		// Render = (Width, Height) → render_size emits a single 0 bit.
		h.Render = vp9dec.RenderSize{Width: width, Height: height}
		return h
	}

	mkRefDims := func(matchSlot int) func(uint8) (uint32, uint32) {
		return func(slot uint8) (uint32, uint32) {
			if int(slot) == matchSlot {
				return width, height
			}
			return 640, 480
		}
	}

	bitPrefix := func(buf []byte, nbits int) string {
		out := make([]byte, nbits)
		for i := range nbits {
			byteIdx := i / 8
			bitIdx := 7 - (i % 8)
			if buf[byteIdx]&(1<<bitIdx) != 0 {
				out[i] = '1'
			} else {
				out[i] = '0'
			}
		}
		return string(out)
	}

	cases := []struct {
		name      string
		matchSlot int
		// wantPrefix is the exact bit sequence emitted by
		// writeFrameSizeWithRefs (excluding the trailing render_size
		// bit which is always 0 in these cases).
		wantPrefix string
		// wantBits is the total bit count writeFrameSizeWithRefs
		// emits including the render_size bit.
		wantBits int
	}{
		// matched on slot 0: 1 + render_size(0) = 2 bits total.
		{"match_slot0", 0, "1", 2},
		// matched on slot 1: 0,1 + render_size(0) = 3 bits.
		{"match_slot1", 1, "01", 3},
		// matched on slot 2: 0,0,1 + render_size(0) = 4 bits.
		{"match_slot2", 2, "001", 4},
		// no match: 0,0,0 + 16-bit width-1 + 16-bit height-1 +
		// render_size(0) = 3 + 16 + 16 + 1 = 36 bits.
		{"no_match", -1, "000", 36},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := header()
			// Use distinct ref indices per slot so refDims is
			// keyed by slot, not by ref index.
			h.InterRef.RefIndex = [3]uint8{0, 1, 2}
			h.InterRef.SignBias = [3]uint8{0, 0, 0}

			var buf [16]byte
			w := NewBitWriter(buf[:])
			writeFrameSizeWithRefs(w, &h, mkRefDims(tc.matchSlot))
			gotBits := w.BitsWritten()
			if gotBits != tc.wantBits {
				t.Fatalf("BitsWritten = %d, want %d (prefix=%q)",
					gotBits, tc.wantBits, bitPrefix(buf[:], gotBits))
			}
			got := bitPrefix(buf[:], len(tc.wantPrefix))
			if got != tc.wantPrefix {
				t.Errorf("found-cascade prefix = %q, want %q (full=%q)",
					got, tc.wantPrefix, bitPrefix(buf[:], gotBits))
			}
		})
	}
}

// TestWriteFrameSizeWithRefsNilCallbackEmitsZeros guards the
// refDims==nil path: govpx skips the dimension comparison when the
// caller hasn't supplied a refDims hook, which is functionally
// equivalent to libvpx's "every cfg is NULL, found stays 0" case
// (vp9_bitstream.c:1186-1199). The result must be 3 zero bits and
// then the explicit (width-1, height-1) literals.
func TestWriteFrameSizeWithRefsNilCallbackEmitsZeros(t *testing.T) {
	h := vp9dec.UncompressedHeader{Width: 320, Height: 240}
	h.Render = vp9dec.RenderSize{Width: 320, Height: 240}

	var buf [16]byte
	w := NewBitWriter(buf[:])
	writeFrameSizeWithRefs(w, &h, nil)
	// 3 found=0 bits + 16-bit width-1 + 16-bit height-1 + 1
	// render_size bit = 36 bits.
	if got := w.BitsWritten(); got != 36 {
		t.Fatalf("BitsWritten = %d, want 36", got)
	}
	if buf[0]&0xe0 != 0 {
		t.Errorf("first three bits = %#x, want 0", buf[0]&0xe0)
	}
}
