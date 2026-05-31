package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
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

func TestVP9DecoderDecodeIntoPostProcessCopiesPostFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{PostProcessFlags: PostProcessDeblock})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(64, 64)
	packet := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	info, err := d.DecodeInto(packet, &dst)
	if err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if !info.ShowFrame || info.Width != 64 || info.Height != 64 {
		t.Fatalf("VP9FrameInfo = %+v, want visible 64x64 frame", info)
	}
	if !publicImageEqualVP8(dst, &d.post.Img) {
		t.Fatal("DecodeInto output does not match VP9 postprocess buffer")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued a frame for NextFrame")
	}
}

func TestVP9DecoderPostProcessDeblockAndDemacroblockChangeOutput(t *testing.T) {
	// vp9ColumnResidueKeyframeForMotionLoopFilterTest produces a keyframe
	// with a non-zero filter_level so the postprocess deblock chain has a
	// non-trivial q. The deblock+demacroblock pair must visibly perturb
	// the raw decoded plane compared to the post-loopfilter reconstruction.
	const width, height = 64, 64
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, width, height, 8)

	plain, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder plain: %v", err)
	}
	if err := plain.Decode(packet); err != nil {
		t.Fatalf("plain Decode: %v", err)
	}
	plainFrame, ok := plain.NextFrame()
	if !ok {
		t.Fatal("plain NextFrame returned no frame")
	}
	plainY := append([]byte(nil), plainFrame.Y...)

	filtered, err := NewVP9Decoder(VP9DecoderOptions{
		PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder filtered: %v", err)
	}
	if err := filtered.Decode(packet); err != nil {
		t.Fatalf("filtered Decode: %v", err)
	}
	filteredFrame, ok := filtered.NextFrame()
	if !ok {
		t.Fatal("filtered NextFrame returned no frame")
	}
	if testutil.PlaneEqual(plainY, plainFrame.YStride, filteredFrame.Y,
		filteredFrame.YStride, width, height) {
		t.Fatal("deblock+demacroblock postprocess produced identical luma")
	}

	// Mean absolute deviation must remain bounded: the postprocess chain
	// must not corrupt pixels far beyond the 0-255 luma range.
	totalDiff, count := 0, 0
	for y := range height {
		for x := range width {
			a := int(plainY[y*plainFrame.YStride+x])
			b := int(filteredFrame.Y[y*filteredFrame.YStride+x])
			if a > b {
				totalDiff += a - b
			} else {
				totalDiff += b - a
			}
			count++
		}
	}
	mad := float64(totalDiff) / float64(count)
	if mad > 40 {
		t.Fatalf("postprocess MAD = %.2f, want bounded perturbation (<= 40)", mad)
	}
}
