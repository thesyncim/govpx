package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

func TestDecoderOptionsEffectivePostProcessFlags(t *testing.T) {
	tests := []struct {
		name string
		opts DecoderOptions
		want PostProcessFlag
	}{
		{name: "off", opts: DecoderOptions{}, want: 0},
		{name: "default chain", opts: DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE}, want: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE},
		{name: "default chain noise", opts: DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE, PostProcessNoiseLevel: 4}, want: PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE},
		{name: "flags", opts: DecoderOptions{PostProcessFlags: PostProcessDeblock}, want: PostProcessDeblock},
	}
	for _, tc := range tests {
		if got := tc.opts.effectivePostProcessFlags(); got != tc.want {
			t.Fatalf("%s flags = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDecodeOutputsLoopFilteredKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(1))

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
	d, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))

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
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))

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
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))

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
	d, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))

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

func TestDecodeSkipsLoopFilterForNoLPFVersion(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartitionProfile(16, 16, 2, vp8test.FirstPartitionWithLoopFilterLevel(1))

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

func TestDecodeAcceptsKeyFrameResolutionChangeByDefault(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("initial Decode returned error: %v", err)
	}
	if frame, ok := d.NextFrame(); !ok || frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("initial NextFrame = %+v/%t, want 16x16 frame", frame, ok)
	}

	if err := d.Decode(vp8test.KeyFramePacketWithPayload(32, 16, 200, 0, true)); err != nil {
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
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithMacroblockSkip(128))

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
