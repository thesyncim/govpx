package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

// reset_frame_context is the 2-bit VP9 uncompressed-header field that
// controls how per-frame entropy state inherits or resets between
// frames. The matrix is:
//
//	0 — no reset; the current frame inherits frame_context_idx as is
//	1 — no reset (reserved/idem; libvpx treats 0 and 1 identically)
//	2 — reset the slot at frame_context_idx (intra-only: collapses to slot 0)
//	3 — reset every frame_context to libvpx defaults
//
// The test below drives the four values through the writer/reader
// round-trip and through the encoder's prepareVP9EncoderFrameContext
// state machine. error_resilient_mode is left off so the
// reset_frame_context bits are actually emitted.

func TestVP9ResetFrameContextHeaderRoundTrip(t *testing.T) {
	for _, value := range []uint8{0, 1, 2, 3} {
		t.Run(resetFrameContextName(value), func(t *testing.T) {
			want := vp9dec.UncompressedHeader{
				Profile:               common.Profile0,
				FrameType:             common.InterFrame,
				ShowFrame:             false,
				ErrorResilientMode:    false,
				IntraOnly:             true,
				ResetFrameContext:     value,
				RefreshFrameFlags:     0xff,
				Width:                 320,
				Height:                240,
				RefreshFrameContext:   true,
				FrameParallelDecoding: false,
				FrameContextIdx:       2,
				FirstPartitionSize:    16,
			}
			want.Loopfilter.FilterLevel = 8
			want.Quant.BaseQindex = 64

			buf := make([]byte, 128)
			w := vp9enc.NewBitWriter(buf)
			n := vp9enc.WriteIntraOnlyUncompressedHeader(w, &want)
			if n <= 0 {
				t.Fatalf("WriteIntraOnlyUncompressedHeader: returned %d", n)
			}

			var br vp9dec.BitReader
			br.Init(buf[:n])
			got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
			if err != nil {
				t.Fatalf("ReadUncompressedHeader: %v", err)
			}
			if got.ResetFrameContext != value {
				t.Fatalf("ResetFrameContext = %d, want %d",
					got.ResetFrameContext, value)
			}
		})
	}
}

// TestVP9ResetFrameContextErrorResilientSkipsField pins the libvpx
// rule that error_resilient_mode=1 suppresses the reset_frame_context
// emit. The parser then leaves the field zero.
func TestVP9ResetFrameContextErrorResilientSkipsField(t *testing.T) {
	hdr := vp9dec.UncompressedHeader{
		Profile:            common.Profile0,
		FrameType:          common.InterFrame,
		ShowFrame:          false,
		ErrorResilientMode: true,
		IntraOnly:          true,
		// ResetFrameContext = 3 would normally be emitted, but
		// error_resilient_mode suppresses the bits entirely.
		ResetFrameContext:  3,
		RefreshFrameFlags:  0xff,
		Width:              320,
		Height:             240,
		FrameContextIdx:    0,
		FirstPartitionSize: 16,
	}
	hdr.Loopfilter.FilterLevel = 0
	hdr.Quant.BaseQindex = 64

	buf := make([]byte, 128)
	w := vp9enc.NewBitWriter(buf)
	n := vp9enc.WriteIntraOnlyUncompressedHeader(w, &hdr)
	if n <= 0 {
		t.Fatalf("WriteIntraOnlyUncompressedHeader: returned %d", n)
	}
	var br vp9dec.BitReader
	br.Init(buf[:n])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.ResetFrameContext != 0 {
		t.Fatalf("error_resilient ResetFrameContext = %d, want 0 (field skipped)",
			got.ResetFrameContext)
	}
	if !got.ErrorResilientMode {
		t.Fatal("ErrorResilientMode lost during round-trip")
	}
}

// TestVP9ResetFrameContextPrepareMatrix drives the encoder's
// prepareVP9EncoderFrameContext state machine for each of the four
// reset_frame_context values and asserts the surviving frame-context
// slots match libvpx's decoder semantics. After the call, the
// active e.fc must equal e.frameContexts[idx] (the slot the prepared
// frame is bound to).
func TestVP9ResetFrameContextPrepareMatrix(t *testing.T) {
	for _, tc := range []struct {
		name              string
		reset             uint8
		intraOnly         bool
		errorResilient    bool
		wantSlotReset     []int    // slots expected to be reset
		wantSlotPreserved []int    // slots expected to keep their seed
		wantIdx           int      // expected returned frame_context_idx
	}{
		{
			name:              "0_no_reset_inter",
			reset:             0,
			intraOnly:         false,
			wantSlotReset:     nil,
			wantSlotPreserved: []int{0, 1, 2, 3},
			wantIdx:           1,
		},
		{
			name:              "1_no_reset_inter",
			reset:             1,
			intraOnly:         false,
			wantSlotReset:     nil,
			wantSlotPreserved: []int{0, 1, 2, 3},
			wantIdx:           1,
		},
		{
			name:              "2_inter_resets_indexed_slot",
			reset:             2,
			intraOnly:         false,
			wantSlotReset:     []int{1},
			wantSlotPreserved: []int{0, 2, 3},
			wantIdx:           1,
		},
		{
			name:              "2_intra_only_collapses_to_slot0",
			reset:             2,
			intraOnly:         true,
			wantSlotReset:     []int{1},
			wantSlotPreserved: []int{0, 2, 3},
			wantIdx:           0,
		},
		{
			name:              "3_resets_every_slot",
			reset:             3,
			intraOnly:         false,
			wantSlotReset:     []int{0, 1, 2, 3},
			wantSlotPreserved: nil,
			wantIdx:           0,
		},
		{
			name:              "error_resilient_resets_every_slot",
			reset:             0,
			intraOnly:         false,
			errorResilient:    true,
			wantSlotReset:     []int{0, 1, 2, 3},
			wantSlotPreserved: nil,
			wantIdx:           0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width: 64, Height: 64,
				ErrorResilient: tc.errorResilient,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			t.Cleanup(func() { _ = e.Close() })

			// Seed every frame_context slot to a recognisable shape so
			// "reset" vs "preserved" can be distinguished. The seed is
			// libvpx's defaults followed by a per-slot stamp on
			// Nmvc.Joints[0] — a value that ResetFrameContext clobbers
			// back to its default.
			for i := range e.frameContexts {
				vp9dec.ResetFrameContext(&e.frameContexts[i])
				e.frameContexts[i].Nmvc.Joints[0] = byte(0x10 + i)
			}
			var defaultFc vp9dec.FrameContext
			vp9dec.ResetFrameContext(&defaultFc)
			defaultProbe := defaultFc.Nmvc.Joints[0]

			hdr := vp9dec.UncompressedHeader{
				FrameType:          common.InterFrame,
				IntraOnly:          tc.intraOnly,
				ErrorResilientMode: tc.errorResilient,
				ResetFrameContext:  tc.reset,
				FrameContextIdx:    1,
			}
			idx := e.prepareVP9EncoderFrameContext(&hdr)
			if idx != tc.wantIdx {
				t.Fatalf("prepareVP9EncoderFrameContext idx = %d, want %d",
					idx, tc.wantIdx)
			}
			for _, slot := range tc.wantSlotReset {
				got := e.frameContexts[slot].Nmvc.Joints[0]
				if got != defaultProbe {
					t.Errorf("slot %d Nmvc.Joints[0] = %#x, want default %#x (slot should be reset)",
						slot, got, defaultProbe)
				}
			}
			for _, slot := range tc.wantSlotPreserved {
				wantProbe := byte(0x10 + slot)
				got := e.frameContexts[slot].Nmvc.Joints[0]
				if got != wantProbe {
					t.Errorf("slot %d Nmvc.Joints[0] = %#x, want %#x (slot should be preserved)",
						slot, got, wantProbe)
				}
			}
			// e.fc tracks the slot the prepared frame is bound to.
			activeProbe := e.fc.Nmvc.Joints[0]
			activeSlotProbe := e.frameContexts[idx].Nmvc.Joints[0]
			if activeProbe != activeSlotProbe {
				t.Errorf("active fc probe = %#x, want slot %d probe %#x",
					activeProbe, idx, activeSlotProbe)
			}
		})
	}
}

// TestVP9ResetFrameContextKeyframeAlwaysResetsAll covers the libvpx
// invariant that keyframes always reset every frame_context regardless
// of the reset_frame_context value (the field is not transmitted on
// keyframes). Even when ResetFrameContext is 0 the prepare step must
// clobber every slot.
func TestVP9ResetFrameContextKeyframeAlwaysResetsAll(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	for i := range e.frameContexts {
		vp9dec.ResetFrameContext(&e.frameContexts[i])
		e.frameContexts[i].Nmvc.Joints[0] = byte(0x20 + i)
	}
	var defaultFc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&defaultFc)
	defaultProbe := defaultFc.Nmvc.Joints[0]

	hdr := vp9dec.UncompressedHeader{
		FrameType:         common.KeyFrame,
		ResetFrameContext: 0, // libvpx does not transmit on KEY; ignored anyway
		FrameContextIdx:   2,
	}
	idx := e.prepareVP9EncoderFrameContext(&hdr)
	if idx != 0 {
		t.Fatalf("prepareVP9EncoderFrameContext on KEY = %d, want 0", idx)
	}
	for i := range e.frameContexts {
		if got := e.frameContexts[i].Nmvc.Joints[0]; got != defaultProbe {
			t.Errorf("KEY slot %d Nmvc.Joints[0] = %#x, want default %#x",
				i, got, defaultProbe)
		}
	}
}

func resetFrameContextName(v uint8) string {
	switch v {
	case 0:
		return "reset0_no_reset"
	case 1:
		return "reset1_no_reset"
	case 2:
		return "reset2_index_only"
	case 3:
		return "reset3_full"
	default:
		return "resetX"
	}
}
