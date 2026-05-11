package govpx

import (
	"errors"
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

func TestNewVP8DecoderValidation(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{Threads: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewVP8DecoderRejectsInvalidPostProcessNoise(t *testing.T) {
	tests := []DecoderOptions{
		{PostProcess: true, PostProcessNoiseLevel: -1},
		{PostProcess: true, PostProcessNoiseLevel: 17},
		{PostProcessNoiseLevel: 4},
		{PostProcessFlags: PostProcessDeblock, PostProcessNoiseLevel: 4},
	}
	for _, opts := range tests {
		_, err := NewVP8Decoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewVP8Decoder(%+v) error = %v, want ErrInvalidConfig", opts, err)
		}
	}
}

func TestNewVP8DecoderAcceptsPostProcessNoiseFlag(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessAddNoise, PostProcessNoiseLevel: 4})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v, want nil", err)
	}
}

func TestNewVP8DecoderRejectsUnknownPostProcessFlags(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessFlag(1 << 12)})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewVP8Decoder error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecoderOptionsEffectivePostProcessFlags(t *testing.T) {
	tests := []struct {
		name string
		opts DecoderOptions
		want PostProcessFlag
	}{
		{name: "off", opts: DecoderOptions{}, want: 0},
		{name: "legacy", opts: DecoderOptions{PostProcess: true}, want: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE},
		{name: "legacy-noise", opts: DecoderOptions{PostProcess: true, PostProcessNoiseLevel: 4}, want: PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE},
		{name: "flags", opts: DecoderOptions{PostProcessFlags: PostProcessDeblock}, want: PostProcessDeblock},
	}
	for _, tc := range tests {
		if got := tc.opts.effectivePostProcessFlags(); got != tc.want {
			t.Fatalf("%s flags = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDecodeRequiresInitialKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8InterFramePacket(0, 0, true))
	if !errors.Is(err, ErrNeedKeyFrame) {
		t.Fatalf("error = %v, want ErrNeedKeyFrame", err)
	}
}

func TestDecodeQueuesSupportedKeyFrameAfterValidation(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 640, MaxHeight: 480})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacketWithPayload(320, 240, 200, 0, true), 44)
	if err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	if d.lastInfo.Width != 320 || d.lastInfo.Height != 240 || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want validated frame metadata", d.lastInfo)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 320 || frame.Height != 240 || frame.YStride == 0 {
		t.Fatalf("frame = %+v, want decoded 320x240 frame", frame)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned the same frame twice")
	}
}

func TestDecodeInvisibleKeyFrameUpdatesStateWithoutOutput(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, false), 44)
	if err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	if d.needKey {
		t.Fatalf("needKey = true, want false after invisible keyframe")
	}
	if d.lastInfo.ShowFrame || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want invisible frame metadata", d.lastInfo)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}
}

func TestDecodeOutputsLoopFilteredKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(1))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if d.loopInfo.MBLimit[1] == 0 || d.loopInfo.BLimit[1] == 0 {
		t.Fatalf("loop filter tables were not initialized")
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for filtered output")
	}
}

func TestDecodePostProcessOutputsPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.current.Img.Y) == 0 {
		t.Fatalf("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("NextFrame did not return decoder postprocess buffer")
	}
	if &frame.Y[0] == &d.current.Img.Y[0] {
		t.Fatalf("postprocessed output aliases reconstruction buffer")
	}
}

func TestDecodePostProcessFlagsOutputPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessDeblock})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.current.Img.Y) == 0 {
		t.Fatalf("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("NextFrame did not return decoder postprocess buffer")
	}
	if &frame.Y[0] == &d.current.Img.Y[0] {
		t.Fatalf("postprocessed output aliases reconstruction buffer")
	}
}

func TestDecodePostProcessFlagsMFQEOnlyOutputPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessMFQE})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("NextFrame did not return decoder postprocess buffer")
	}
}

func TestDecodeIntoPostProcessCopiesPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	info, err := d.DecodeInto(packet, &dst)
	if err != nil {
		t.Fatalf("DecodeInto error = %v, want nil", err)
	}
	if !info.ShowFrame || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want visible 16x16 frame", info)
	}
	if !publicImageEqualVP8(dst, &d.post.Img) {
		t.Fatalf("DecodeInto output does not match decoder postprocess buffer")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued a frame for NextFrame")
	}
}

func TestDecodePostProcessFlagAddNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	plain, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder plain returned error: %v", err)
	}
	noisy, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessAddNoise, PostProcessNoiseLevel: 4})
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

	if planeEqual(plainFrame.Y, plainFrame.YStride, noisyFrame.Y, noisyFrame.YStride, 16, 16) {
		t.Fatalf("postprocess noise flag left luma unchanged")
	}
	if !planeEqual(plainFrame.U, plainFrame.UStride, noisyFrame.U, noisyFrame.UStride, 8, 8) {
		t.Fatalf("postprocess noise flag changed U plane")
	}
	if !planeEqual(plainFrame.V, plainFrame.VStride, noisyFrame.V, noisyFrame.VStride, 8, 8) {
		t.Fatalf("postprocess noise flag changed V plane")
	}
}

func TestDecodePostProcessNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	plain, err := NewVP8Decoder(DecoderOptions{PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder plain returned error: %v", err)
	}
	noisy, err := NewVP8Decoder(DecoderOptions{PostProcess: true, PostProcessNoiseLevel: 4})
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

	if planeEqual(plainFrame.Y, plainFrame.YStride, noisyFrame.Y, noisyFrame.YStride, 16, 16) {
		t.Fatalf("postprocess noise left luma unchanged")
	}
	if !planeEqual(plainFrame.U, plainFrame.UStride, noisyFrame.U, noisyFrame.UStride, 8, 8) {
		t.Fatalf("postprocess noise changed U plane")
	}
	if !planeEqual(plainFrame.V, plainFrame.VStride, noisyFrame.V, noisyFrame.VStride, 8, 8) {
		t.Fatalf("postprocess noise changed V plane")
	}
}

func TestDecodeOutputsSupportedVersionKeyFrames(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		d, err := NewVP8Decoder(DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}
		packet := vp8KeyFramePacketWithPayload(16, 16, 200, version, true)

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

func TestDecodeSkipsLoopFilterForNoLPFVersion(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartitionProfile(16, 16, 2, vp8FirstPartitionWithLoopFilterLevel(1))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if d.loopInfo.MBLimit[1] != 0 || d.loopInfo.BLimit[1] != 0 {
		t.Fatalf("loop filter tables = mb:%d b:%d, want skipped", d.loopInfo.MBLimit[1], d.loopInfo.BLimit[1])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for no-lpf version")
	}
}

func TestDecodeOutputsDefaultVersionKeyFrames(t *testing.T) {
	for _, version := range []int{4, 5, 6, 7} {
		d, err := NewVP8Decoder(DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}
		packet := vp8KeyFramePacketWithPayload(16, 16, 200, version, true)

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
		opts DecoderOptions
	}{
		{name: "width", opts: DecoderOptions{MaxWidth: 15}},
		{name: "height", opts: DecoderOptions{MaxHeight: 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewVP8Decoder(tt.opts)
			if err != nil {
				t.Fatalf("NewVP8Decoder returned error: %v", err)
			}

			err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
			if !errors.Is(err, ErrFrameRejected) {
				t.Fatalf("Decode error = %v, want ErrFrameRejected", err)
			}
		})
	}
}

func TestDecodeRejectsConfiguredResolutionChange(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{RejectResolutionChange: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("initial Decode returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(32, 16, 200, 0, true))
	if !errors.Is(err, ErrFrameRejected) {
		t.Fatalf("resolution-change Decode error = %v, want ErrFrameRejected", err)
	}
}

func TestDecodeAcceptsKeyFrameResolutionChangeByDefault(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("initial Decode returned error: %v", err)
	}
	if frame, ok := d.NextFrame(); !ok || frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("initial NextFrame = %+v/%t, want 16x16 frame", frame, ok)
	}

	if err := d.Decode(vp8KeyFramePacketWithPayload(32, 16, 200, 0, true)); err != nil {
		t.Fatalf("resolution-change Decode returned error: %v", err)
	}
	if d.frameWidth != 32 || d.frameHeight != 16 || d.mbCols != 2 || d.mbRows != 1 {
		t.Fatalf("decoder dimensions = frame %dx%d mb %dx%d, want 32x16 mb 2x1", d.frameWidth, d.frameHeight, d.mbCols, d.mbRows)
	}
	if d.current.Img.Width != 32 || d.lastRef.Img.Width != 32 || d.goldenRef.Img.Width != 32 || d.altRef.Img.Width != 32 {
		t.Fatalf("reference widths = current:%d last:%d golden:%d alt:%d, want 32", d.current.Img.Width, d.lastRef.Img.Width, d.goldenRef.Img.Width, d.altRef.Img.Width)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("resolution-change NextFrame returned no frame")
	}
	if frame.Width != 32 || frame.Height != 16 {
		t.Fatalf("resolution-change frame = %dx%d, want 32x16", frame.Width, frame.Height)
	}
}

func TestDecodeOutputsMacroblockSkipKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithMacroblockSkip(128))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if len(d.modes) != 1 || !d.modes[0].MBSkipCoeff {
		t.Fatalf("mode skip = %+v, want skipped macroblock", d.modes)
	}
	if d.tokens[0] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("tokens[0] = %+v, want zero tokens for skipped macroblock", d.tokens[0])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for skipped keyframe")
	}
}
