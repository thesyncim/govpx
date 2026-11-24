package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderPostProcessOutputsPostFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.frameY) == 0 {
		t.Fatal("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatal("NextFrame did not return VP9 postprocess buffer")
	}
	if &frame.Y[0] == &d.frameY[0] {
		t.Fatal("postprocessed output aliases VP9 reconstruction buffer")
	}
}
