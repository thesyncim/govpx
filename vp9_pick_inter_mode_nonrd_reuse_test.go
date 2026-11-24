package govpx

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9NonrdReuseInterPredReconMatchesDecoder is the end-to-end gate for
// the reuse_inter_pred pick/commit handoff (libvpx vp9_pickmode.c:2668-2684
// winner retention + vp9_encodeframe.c:6073 encode-time rebuild skip): when
// the commit consumes the picker-retained luma predictor instead of
// re-convolving it, the encoder's committed reconstruction must still equal
// what a decoder reconstructs from the emitted bitstream, because the decoder
// always rebuilds the prediction from scratch. Any byte drift in the retained
// predictor would surface here as an encoder-recon/decoder-recon mismatch.
//
// The 144x80 geometry pins the clipped bottom SB row (80 luma rows = 10 mi
// rows, so the second SB row is a 16-pixel strip) whose >=8x8 walker leaves
// carry the nonrd_use_partition pred_pixel_ready seed without a matching
// var-part grid stamp — exactly the frame-edge reuse case; 128x128 pins the
// aligned interior. The panning source keeps the inter picker on non-zero
// motion so subpel predictors (not just zero-MV copies) cross the boundary.
func TestVP9NonrdReuseInterPredReconMatchesDecoder(t *testing.T) {
	for _, dims := range []struct{ w, h int }{
		{144, 80},  // clipped bottom SB strip (frame-edge reuse leaves)
		{128, 128}, // SB-aligned interior
	} {
		t.Run(fmt.Sprintf("%dx%d", dims.w, dims.h), func(t *testing.T) {
			testVP9NonrdReuseReconMatchesDecoder(t, dims.w, dims.h)
		})
	}
}

func testVP9NonrdReuseReconMatchesDecoder(t *testing.T, width, height int) {
	const frames = 12
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		FPS:               30,
		TargetBitrateKbps: 600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	uvW, uvH := (width+1)>>1, (height+1)>>1
	decoded := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvW*uvH),
		V:       make([]byte, uvW*uvH),
		YStride: width,
		UStride: uvW,
		VStride: uvW,
	}
	encodedInter := 0
	for i := range frames {
		src := vp9test.NewPanningYCbCr(width, height, i)
		data, err := e.Encode(src)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		if len(data) == 0 {
			continue // rate-control drop: recon unchanged, nothing decoded
		}
		if _, err := d.DecodeInto(data, &decoded); err != nil {
			t.Fatalf("DecodeInto frame %d: %v", i, err)
		}
		if i > 0 {
			encodedInter++
		}
		comparePlane := func(plane int, name string, dec []byte,
			decStride, w, h int,
		) {
			recon, stride := e.vp9EncoderReconPlane(plane)
			if len(recon) == 0 || stride <= 0 {
				t.Fatalf("frame %d: encoder %s recon plane unavailable", i, name)
			}
			for y := range h {
				for x := range w {
					got := recon[y*stride+x]
					want := dec[y*decStride+x]
					if got != want {
						t.Fatalf("frame %d: %s recon mismatch at (%d,%d): encoder %d decoder %d",
							i, name, x, y, got, want)
					}
				}
			}
		}
		comparePlane(0, "Y", decoded.Y, decoded.YStride, width, height)
		comparePlane(1, "U", decoded.U, decoded.UStride, uvW, uvH)
		comparePlane(2, "V", decoded.V, decoded.VStride, uvW, uvH)
	}
	if encodedInter == 0 {
		t.Fatal("no inter frames were encoded; reuse boundary never exercised")
	}
}
