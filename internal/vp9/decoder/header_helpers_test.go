package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestHeaderRenderSize(t *testing.T) {
	hdr := &UncompressedHeader{Width: 640, Height: 360}
	if w, h := HeaderRenderSize(hdr); w != 640 || h != 360 {
		t.Fatalf("fallback render size = %dx%d, want 640x360", w, h)
	}
	hdr.Render.Width = 320
	hdr.Render.Height = 180
	if w, h := HeaderRenderSize(hdr); w != 320 || h != 180 {
		t.Fatalf("explicit render size = %dx%d, want 320x180", w, h)
	}
	if w, h := HeaderRenderSize(nil); w != 0 || h != 0 {
		t.Fatalf("nil render size = %dx%d, want 0x0", w, h)
	}
}

func TestSupportedOutputFormat(t *testing.T) {
	hdr := &UncompressedHeader{}
	hdr.Profile = common.Profile0
	hdr.BitDepthColor.BitDepth = Bits8
	hdr.BitDepthColor.SubsamplingX = 1
	hdr.BitDepthColor.SubsamplingY = 1
	if !SupportedOutputFormat(hdr) {
		t.Fatal("profile0 8-bit 4:2:0 header rejected")
	}
	hdr.Profile = common.Profile1
	if SupportedOutputFormat(hdr) {
		t.Fatal("profile1 header accepted")
	}
	hdr.Profile = common.Profile0
	hdr.BitDepthColor.BitDepth = Bits10
	if SupportedOutputFormat(hdr) {
		t.Fatal("10-bit header accepted")
	}
}

func TestFrameRefSignBiasAndCompoundAllowedForHeader(t *testing.T) {
	hdr := &UncompressedHeader{FrameType: common.InterFrame}
	hdr.InterRef.SignBias = [common.RefsPerFrame]uint8{0, 1, 0}
	got := FrameRefSignBias(hdr)
	if got[LastFrame] != 0 || got[GoldenFrame] != 1 || got[AltrefFrame] != 0 {
		t.Fatalf("sign bias = %v, want last=0 golden=1 altref=0", got)
	}
	if !CompoundReferenceAllowedForHeader(hdr) {
		t.Fatal("inter header with mixed sign bias rejected for compound refs")
	}
	hdr.FrameType = common.KeyFrame
	if CompoundReferenceAllowedForHeader(hdr) {
		t.Fatal("key frame accepted for compound refs")
	}
}

func TestHeaderResetsPastIndependence(t *testing.T) {
	for _, hdr := range []*UncompressedHeader{
		{FrameType: common.KeyFrame},
		{IntraOnly: true},
		{ErrorResilientMode: true},
	} {
		if !HeaderResetsPastIndependence(hdr) {
			t.Fatalf("HeaderResetsPastIndependence(%+v) = false, want true", hdr)
		}
	}
	if HeaderResetsPastIndependence(&UncompressedHeader{FrameType: common.InterFrame}) {
		t.Fatal("plain inter header reset past independence")
	}
}

func TestPartitionContextUpdateWidth(t *testing.T) {
	for _, tc := range []struct {
		half int
		want int
	}{
		{0, 1},
		{1, 2},
		{2, 4},
		{4, 8},
	} {
		if got := PartitionContextUpdateWidth(tc.half); got != tc.want {
			t.Fatalf("PartitionContextUpdateWidth(%d) = %d, want %d",
				tc.half, got, tc.want)
		}
	}
}

func TestPlaneEntropyLenAndTileOffset(t *testing.T) {
	if got := PlaneEntropyLen(8, 0); got != 16 {
		t.Fatalf("PlaneEntropyLen luma = %d, want 16", got)
	}
	if got := PlaneEntropyLen(8, 1); got != 8 {
		t.Fatalf("PlaneEntropyLen chroma = %d, want 8", got)
	}
	if got := TileOffset(1, 17, 1); got != 8 {
		t.Fatalf("TileOffset(1,17,1) = %d, want 8", got)
	}
	if got := TileOffset(2, 17, 1); got != 17 {
		t.Fatalf("TileOffset(2,17,1) = %d, want 17", got)
	}
}
