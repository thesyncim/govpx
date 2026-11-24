package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestResetFrameContextHeaderRoundTrip(t *testing.T) {
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
			w := NewBitWriter(buf)
			n := WriteIntraOnlyUncompressedHeader(w, &want)
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

func TestResetFrameContextErrorResilientSkipsField(t *testing.T) {
	hdr := vp9dec.UncompressedHeader{
		Profile:            common.Profile0,
		FrameType:          common.InterFrame,
		ShowFrame:          false,
		ErrorResilientMode: true,
		IntraOnly:          true,
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
	w := NewBitWriter(buf)
	n := WriteIntraOnlyUncompressedHeader(w, &hdr)
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

func TestPrepareFrameContextResetMatrix(t *testing.T) {
	for _, tc := range []struct {
		name              string
		reset             uint8
		intraOnly         bool
		errorResilient    bool
		wantSlotReset     []int
		wantSlotPreserved []int
		wantIdx           int
	}{
		{
			name:              "0_no_reset_inter",
			reset:             0,
			wantSlotReset:     nil,
			wantSlotPreserved: []int{0, 1, 2, 3},
			wantIdx:           1,
		},
		{
			name:              "1_no_reset_inter",
			reset:             1,
			wantSlotReset:     nil,
			wantSlotPreserved: []int{0, 1, 2, 3},
			wantIdx:           1,
		},
		{
			name:              "2_inter_resets_indexed_slot",
			reset:             2,
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
			wantSlotReset:     []int{0, 1, 2, 3},
			wantSlotPreserved: nil,
			wantIdx:           0,
		},
		{
			name:              "error_resilient_resets_every_slot",
			reset:             0,
			errorResilient:    true,
			wantSlotReset:     []int{0, 1, 2, 3},
			wantSlotPreserved: nil,
			wantIdx:           0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var frameContexts [common.FrameContexts]vp9dec.FrameContext
			seedFrameContexts(&frameContexts, 0x10)
			defaultProbe := defaultFrameContextProbe()

			hdr := vp9dec.UncompressedHeader{
				FrameType:          common.InterFrame,
				IntraOnly:          tc.intraOnly,
				ErrorResilientMode: tc.errorResilient,
				ResetFrameContext:  tc.reset,
				FrameContextIdx:    1,
			}
			idx, active := PrepareFrameContext(&frameContexts, &hdr)
			if idx != tc.wantIdx {
				t.Fatalf("PrepareFrameContext idx = %d, want %d",
					idx, tc.wantIdx)
			}
			for _, slot := range tc.wantSlotReset {
				got := frameContexts[slot].Nmvc.Joints[0]
				if got != defaultProbe {
					t.Errorf("slot %d Nmvc.Joints[0] = %#x, want default %#x (slot should be reset)",
						slot, got, defaultProbe)
				}
			}
			for _, slot := range tc.wantSlotPreserved {
				wantProbe := byte(0x10 + slot)
				got := frameContexts[slot].Nmvc.Joints[0]
				if got != wantProbe {
					t.Errorf("slot %d Nmvc.Joints[0] = %#x, want %#x (slot should be preserved)",
						slot, got, wantProbe)
				}
			}
			if activeProbe := active.Nmvc.Joints[0]; activeProbe != frameContexts[idx].Nmvc.Joints[0] {
				t.Errorf("active fc probe = %#x, want slot %d probe %#x",
					activeProbe, idx, frameContexts[idx].Nmvc.Joints[0])
			}
		})
	}
}

func TestPrepareFrameContextKeyframeAlwaysResetsAll(t *testing.T) {
	var frameContexts [common.FrameContexts]vp9dec.FrameContext
	seedFrameContexts(&frameContexts, 0x20)
	defaultProbe := defaultFrameContextProbe()

	hdr := vp9dec.UncompressedHeader{
		FrameType:         common.KeyFrame,
		ResetFrameContext: 0,
		FrameContextIdx:   2,
	}
	idx, active := PrepareFrameContext(&frameContexts, &hdr)
	if idx != 0 {
		t.Fatalf("PrepareFrameContext on KEY = %d, want 0", idx)
	}
	for i := range frameContexts {
		if got := frameContexts[i].Nmvc.Joints[0]; got != defaultProbe {
			t.Errorf("KEY slot %d Nmvc.Joints[0] = %#x, want default %#x",
				i, got, defaultProbe)
		}
	}
	if active.Nmvc.Joints[0] != defaultProbe {
		t.Fatalf("active context probe = %#x, want default %#x",
			active.Nmvc.Joints[0], defaultProbe)
	}
}

func TestCommitFrameContextHonorsRefreshFlagAndSlotBounds(t *testing.T) {
	var frameContexts [common.FrameContexts]vp9dec.FrameContext
	seedFrameContexts(&frameContexts, 0x30)
	active := frameContexts[0]
	active.Nmvc.Joints[0] = 0xaa

	CommitFrameContext(&frameContexts, active,
		&vp9dec.UncompressedHeader{RefreshFrameContext: false}, 2)
	if got := frameContexts[2].Nmvc.Joints[0]; got != 0x32 {
		t.Fatalf("no-refresh slot changed to %#x", got)
	}

	CommitFrameContext(&frameContexts, active,
		&vp9dec.UncompressedHeader{RefreshFrameContext: true}, -1)
	CommitFrameContext(&frameContexts, active,
		&vp9dec.UncompressedHeader{RefreshFrameContext: true}, common.FrameContexts)
	if got := frameContexts[0].Nmvc.Joints[0]; got != 0x30 {
		t.Fatalf("out-of-range commit changed slot 0 to %#x", got)
	}

	CommitFrameContext(&frameContexts, active,
		&vp9dec.UncompressedHeader{RefreshFrameContext: true}, 2)
	if got := frameContexts[2].Nmvc.Joints[0]; got != 0xaa {
		t.Fatalf("refreshed slot probe = %#x, want active probe 0xaa", got)
	}
}

func seedFrameContexts(frameContexts *[common.FrameContexts]vp9dec.FrameContext,
	base byte,
) {
	for i := range frameContexts {
		vp9dec.ResetFrameContext(&frameContexts[i])
		frameContexts[i].Nmvc.Joints[0] = base + byte(i)
	}
}

func defaultFrameContextProbe() byte {
	var defaultFc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&defaultFc)
	return defaultFc.Nmvc.Joints[0]
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
