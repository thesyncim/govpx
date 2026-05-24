package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestDecodePostProcessFlagAddNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))
	plain, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder plain returned error: %v", err)
	}
	noisy, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
		PostProcessFlags:      govpx.PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP8Decoder noisy returned error: %v", err)
	}

	if err := plain.Decode(packet); err != nil {
		t.Fatalf("plain Decode returned error: %v", err)
	}
	if err := noisy.Decode(packet); err != nil {
		t.Fatalf("noisy Decode returned error: %v", err)
	}
	plainFrame, ok := plain.NextFrame()
	if !ok {
		t.Fatalf("plain NextFrame returned no frame")
	}
	noisyFrame, ok := noisy.NextFrame()
	if !ok {
		t.Fatalf("noisy NextFrame returned no frame")
	}

	if testutil.PlaneEqual(plainFrame.Y, plainFrame.YStride, noisyFrame.Y, noisyFrame.YStride, 16, 16) {
		t.Fatalf("postprocess noise flag left luma unchanged")
	}
	if !testutil.PlaneEqual(plainFrame.U, plainFrame.UStride, noisyFrame.U, noisyFrame.UStride, 8, 8) {
		t.Fatalf("postprocess noise flag changed U plane")
	}
	if !testutil.PlaneEqual(plainFrame.V, plainFrame.VStride, noisyFrame.V, noisyFrame.VStride, 8, 8) {
		t.Fatalf("postprocess noise flag changed V plane")
	}
}

func TestDecodePostProcessNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))
	plain, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
		PostProcessFlags: govpx.PostProcessDeblock | govpx.PostProcessDemacroblock | govpx.PostProcessMFQE,
	})
	if err != nil {
		t.Fatalf("NewVP8Decoder plain returned error: %v", err)
	}
	noisy, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
		PostProcessFlags:      govpx.PostProcessDeblock | govpx.PostProcessDemacroblock | govpx.PostProcessAddNoise | govpx.PostProcessMFQE,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP8Decoder noisy returned error: %v", err)
	}

	if err := plain.Decode(packet); err != nil {
		t.Fatalf("plain Decode returned error: %v", err)
	}
	if err := noisy.Decode(packet); err != nil {
		t.Fatalf("noisy Decode returned error: %v", err)
	}
	plainFrame, ok := plain.NextFrame()
	if !ok {
		t.Fatalf("plain NextFrame returned no frame")
	}
	noisyFrame, ok := noisy.NextFrame()
	if !ok {
		t.Fatalf("noisy NextFrame returned no frame")
	}

	if testutil.PlaneEqual(plainFrame.Y, plainFrame.YStride, noisyFrame.Y, noisyFrame.YStride, 16, 16) {
		t.Fatalf("postprocess noise left luma unchanged")
	}
	if !testutil.PlaneEqual(plainFrame.U, plainFrame.UStride, noisyFrame.U, noisyFrame.UStride, 8, 8) {
		t.Fatalf("postprocess noise changed U plane")
	}
	if !testutil.PlaneEqual(plainFrame.V, plainFrame.VStride, noisyFrame.V, noisyFrame.VStride, 8, 8) {
		t.Fatalf("postprocess noise changed V plane")
	}
}

func TestDecodeOutputsSupportedVersionKeyFrames(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}
		packet := vp8test.KeyFramePacketWithPayload(16, 16, 200, version, true)

		err = d.Decode(packet)
		if err != nil {
			t.Fatalf("version %d Decode error = %v, want nil", version, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("version %d NextFrame returned no frame", version)
		}
		if frame.Width != 16 || frame.Height != 16 {
			t.Fatalf("version %d frame dimensions = %dx%d, want 16x16", version, frame.Width, frame.Height)
		}
	}
}

func TestDecodeOutputsDefaultVersionKeyFrames(t *testing.T) {
	for _, version := range []int{4, 5, 6, 7} {
		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}
		packet := vp8test.KeyFramePacketWithPayload(16, 16, 200, version, true)

		err = d.Decode(packet)
		if err != nil {
			t.Fatalf("version %d Decode error = %v, want nil", version, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("version %d NextFrame returned no frame", version)
		}
		if frame.Width != 16 || frame.Height != 16 {
			t.Fatalf("version %d frame dimensions = %dx%d, want 16x16", version, frame.Width, frame.Height)
		}
	}
}

func TestDecodeRejectsConfiguredSizeLimits(t *testing.T) {
	tests := []struct {
		name string
		opts govpx.DecoderOptions
	}{
		{name: "width", opts: govpx.DecoderOptions{MaxWidth: 15}},
		{name: "height", opts: govpx.DecoderOptions{MaxHeight: 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := govpx.NewVP8Decoder(tt.opts)
			if err != nil {
				t.Fatalf("NewVP8Decoder returned error: %v", err)
			}

			err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
			if !errors.Is(err, govpx.ErrFrameRejected) {
				t.Fatalf("Decode error = %v, want ErrFrameRejected", err)
			}
		})
	}
}

func TestDecodeRejectsConfiguredResolutionChange(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{RejectResolutionChange: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("initial Decode returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(32, 16, 200, 0, true))
	if !errors.Is(err, govpx.ErrFrameRejected) {
		t.Fatalf("resolution-change Decode error = %v, want ErrFrameRejected", err)
	}
}
