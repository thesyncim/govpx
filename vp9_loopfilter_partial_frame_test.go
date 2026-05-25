package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestVP9LoopFilterSubImagePickerWiringAtSpeed0 asserts the production
// wiring of the sub-image picker. When sf.LpfPick is overridden to
// LpfPickFromSubImage post-construction, the post-tile encoder branch
// must run the quadratic search with partial_frame=1 against the
// reconstructed luma (libvpx vp9_picklpf.c:201: `method ==
// LPF_PICK_FROM_SUBIMAGE`). The encoded header carries a valid 6-bit
// FilterLevel; the search must complete without bitstream corruption.
// Stock libvpx never selects SUBIMAGE through the speed-features
// dispatcher (vp9_speed_features.c only emits FROM_FULL_IMAGE and
// FROM_Q), so this test exercises the manual override path the C
// public API surfaces via VP9E_SET_LPF_PICK.
func TestVP9LoopFilterSubImagePickerWiringAtSpeed0(t *testing.T) {
	const width, height = 128, 128
	src := newVP9TexturedYCbCrForLpfPickerTest(width, height)
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		CpuUsed:  0,
		Deadline: DeadlineGoodQuality,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.sf.LpfPick = LpfPickFromSubImage
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(src, dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Loopfilter.FilterLevel > vp9dec.MaxLoopFilter {
		t.Fatalf("FilterLevel=%d, want in [0, %d]",
			hdr.Loopfilter.FilterLevel, vp9dec.MaxLoopFilter)
	}
	// Decode round-trip — the stream must remain well-formed under the
	// sub-image picker's partial-frame trials. The post-pick final
	// filter pass runs on the full frame Y+U+V, so the encoded
	// reconstruction matches a decoder's full-frame deblock.
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(dst[:n]); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned !ok")
	}
}
