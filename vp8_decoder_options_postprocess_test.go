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

func TestDecoderOptionsEffectivePostProcessDeblockingLevel(t *testing.T) {
	tests := []struct {
		name string
		opts DecoderOptions
		want int
	}{
		{name: "default", opts: DecoderOptions{}, want: vp8dec.DefaultPostProcessDeblockingLevel},
		{name: "nonzero", opts: DecoderOptions{PostProcessDeblockingLevel: 6}, want: 6},
		{name: "explicit zero", opts: DecoderOptions{PostProcessDeblockingLevelSet: true}, want: 0},
		{name: "explicit nonzero", opts: DecoderOptions{PostProcessDeblockingLevel: 12, PostProcessDeblockingLevelSet: true}, want: 12},
	}
	for _, tc := range tests {
		if got := tc.opts.effectivePostProcessDeblockingLevel(); got != tc.want {
			t.Fatalf("%s deblocking level = %d, want %d", tc.name, got, tc.want)
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

func TestSetPostProcessUpdatesRuntimePostProcessConfig(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))

	if err := d.Decode(packet); err != nil {
		t.Fatalf("initial Decode error = %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("initial NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.current.Img.Y) == 0 || &frame.Y[0] != &d.current.Img.Y[0] {
		t.Fatalf("initial frame did not alias reconstruction buffer")
	}

	if err := d.SetPostProcess(PostProcessDeblock|PostProcessDemacroblock|PostProcessMFQE, 0); err != nil {
		t.Fatalf("SetPostProcess returned error: %v", err)
	}
	if d.opts.PostProcessFlags != PostProcessDeblock|PostProcessDemacroblock|PostProcessMFQE ||
		d.opts.PostProcessNoiseLevel != 0 {
		t.Fatalf("postprocess opts = flags:%v noise:%d, want deblock/demacroblock/mfqe and 0",
			d.opts.PostProcessFlags, d.opts.PostProcessNoiseLevel)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("postprocess Decode error = %v", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatalf("postprocess NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("runtime postprocess did not return decoder postprocess buffer")
	}
}

func TestSetPostProcessInvalidUpdateDoesNotMutate(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcessFlags: PostProcessAddNoise, PostProcessNoiseLevel: 4})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.SetPostProcess(PostProcessDeblock, 4); err != ErrInvalidConfig {
		t.Fatalf("invalid SetPostProcess error = %v, want ErrInvalidConfig", err)
	}
	if d.opts.PostProcessFlags != PostProcessAddNoise || d.opts.PostProcessNoiseLevel != 4 {
		t.Fatalf("invalid SetPostProcess mutated opts to flags:%v noise:%d",
			d.opts.PostProcessFlags, d.opts.PostProcessNoiseLevel)
	}
}

func TestSetPostProcessConfigUpdatesRuntimePostProcessConfig(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithLoopFilterLevel(63))
	if err := d.Decode(packet); err != nil {
		t.Fatalf("initial Decode error = %v", err)
	}
	_, _ = d.NextFrame()

	flags := PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE
	if err := d.SetPostProcessConfig(flags, 1, 0); err != nil {
		t.Fatalf("SetPostProcessConfig returned error: %v", err)
	}
	if d.opts.PostProcessFlags != flags ||
		d.opts.PostProcessDeblockingLevel != 1 ||
		!d.opts.PostProcessDeblockingLevelSet ||
		d.opts.PostProcessNoiseLevel != 0 ||
		d.opts.effectivePostProcessDeblockingLevel() != 1 {
		t.Fatalf("postprocess opts = flags:%v deblock:%d set:%t noise:%d effective:%d, want full config",
			d.opts.PostProcessFlags,
			d.opts.PostProcessDeblockingLevel,
			d.opts.PostProcessDeblockingLevelSet,
			d.opts.PostProcessNoiseLevel,
			d.opts.effectivePostProcessDeblockingLevel())
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("postprocess Decode error = %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("postprocess NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("runtime postprocess config did not return decoder postprocess buffer")
	}

	if err := d.SetPostProcessConfig(PostProcessDeblock, 0, 0); err != nil {
		t.Fatalf("SetPostProcessConfig explicit zero returned error: %v", err)
	}
	if !d.opts.PostProcessDeblockingLevelSet || d.opts.effectivePostProcessDeblockingLevel() != 0 {
		t.Fatalf("explicit zero deblocking level = set:%t effective:%d, want explicit 0",
			d.opts.PostProcessDeblockingLevelSet, d.opts.effectivePostProcessDeblockingLevel())
	}
}

func TestSetPostProcessConfigInvalidUpdateDoesNotMutate(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{
		PostProcessFlags:              PostProcessAddNoise,
		PostProcessDeblockingLevel:    6,
		PostProcessDeblockingLevelSet: true,
		PostProcessNoiseLevel:         4,
	})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.SetPostProcessConfig(PostProcessDeblock, 17, 0); err != ErrInvalidConfig {
		t.Fatalf("invalid deblock SetPostProcessConfig error = %v, want ErrInvalidConfig", err)
	}
	if d.opts.PostProcessFlags != PostProcessAddNoise ||
		d.opts.PostProcessDeblockingLevel != 6 ||
		!d.opts.PostProcessDeblockingLevelSet ||
		d.opts.PostProcessNoiseLevel != 4 {
		t.Fatalf("invalid deblock SetPostProcessConfig mutated opts to flags:%v deblock:%d set:%t noise:%d",
			d.opts.PostProcessFlags,
			d.opts.PostProcessDeblockingLevel,
			d.opts.PostProcessDeblockingLevelSet,
			d.opts.PostProcessNoiseLevel)
	}
	if err := d.SetPostProcessConfig(PostProcessDeblock, 6, 4); err != ErrInvalidConfig {
		t.Fatalf("invalid noise SetPostProcessConfig error = %v, want ErrInvalidConfig", err)
	}
	if d.opts.PostProcessFlags != PostProcessAddNoise ||
		d.opts.PostProcessDeblockingLevel != 6 ||
		!d.opts.PostProcessDeblockingLevelSet ||
		d.opts.PostProcessNoiseLevel != 4 {
		t.Fatalf("invalid noise SetPostProcessConfig mutated opts to flags:%v deblock:%d set:%t noise:%d",
			d.opts.PostProcessFlags,
			d.opts.PostProcessDeblockingLevel,
			d.opts.PostProcessDeblockingLevelSet,
			d.opts.PostProcessNoiseLevel)
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
