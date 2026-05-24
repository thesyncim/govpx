package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func TestVP9DecoderPostProcessOutputsPostFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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

func TestVP9DecoderPostProcessAddNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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
	noisy, err := NewVP9Decoder(VP9DecoderOptions{
		PostProcessFlags:      PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder noisy: %v", err)
	}
	if err := noisy.Decode(packet); err != nil {
		t.Fatalf("noisy Decode: %v", err)
	}
	noisyFrame, ok := noisy.NextFrame()
	if !ok {
		t.Fatal("noisy NextFrame returned no frame")
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(plainFrame.Width, plainFrame.Height)
	if testutil.PlaneEqual(plainFrame.Y, plainFrame.YStride, noisyFrame.Y,
		noisyFrame.YStride, plainFrame.Width, plainFrame.Height) {
		t.Fatal("VP9 postprocess add-noise left luma unchanged")
	}
	if !testutil.PlaneEqual(plainFrame.U, plainFrame.UStride, noisyFrame.U,
		noisyFrame.UStride, uvWidth, uvHeight) {
		t.Fatal("VP9 postprocess add-noise changed U plane")
	}
	if !testutil.PlaneEqual(plainFrame.V, plainFrame.VStride, noisyFrame.V,
		noisyFrame.VStride, uvWidth, uvHeight) {
		t.Fatal("VP9 postprocess add-noise changed V plane")
	}
}

func TestVP9DecoderPostProcessSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{
		PostProcessFlags:      PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	for i := range 3 {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("warm Decode[%d]: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("warm NextFrame[%d] returned no frame", i)
		}
	}
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode alloc run: %v", err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatal("NextFrame alloc run returned no frame")
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 postprocess steady-state allocs = %f, want 0", allocs)
	}
}

func TestVP9DecoderPostProcessFlagsRoundTripIndividually(t *testing.T) {
	cases := []struct {
		name string
		opts VP9DecoderOptions
	}{
		{name: "Deblock", opts: VP9DecoderOptions{PostProcessFlags: PostProcessDeblock}},
		{name: "Demacroblock", opts: VP9DecoderOptions{PostProcessFlags: PostProcessDemacroblock}},
		{name: "MFQE", opts: VP9DecoderOptions{PostProcessFlags: PostProcessMFQE}},
		{name: "AddNoise", opts: VP9DecoderOptions{
			PostProcessFlags:      PostProcessAddNoise,
			PostProcessNoiseLevel: 4,
		}},
		{name: "All", opts: VP9DecoderOptions{
			PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock |
				PostProcessMFQE | PostProcessAddNoise,
			PostProcessNoiseLevel: 2,
		}},
		{name: "DeblockDemacroblockNoise", opts: VP9DecoderOptions{
			PostProcessFlags:      PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise,
			PostProcessNoiseLevel: 1,
		}},
	}
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(tc.opts)
			if err != nil {
				t.Fatalf("NewVP9Decoder(%s): %v", tc.name, err)
			}
			if err := d.Decode(packet); err != nil {
				t.Fatalf("Decode(%s): %v", tc.name, err)
			}
			frame, ok := d.NextFrame()
			if !ok {
				t.Fatalf("NextFrame(%s) returned no frame", tc.name)
			}
			if frame.Width != 64 || frame.Height != 64 {
				t.Fatalf("frame = %dx%d, want 64x64", frame.Width, frame.Height)
			}
			if len(frame.Y) == 0 || len(frame.U) == 0 || len(frame.V) == 0 {
				t.Fatalf("frame planes empty after %s postprocess", tc.name)
			}
		})
	}
}

func TestVP9DecoderPostProcessMFQEHandlesPartialSuperblocks(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{
		PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock |
			PostProcessMFQE | PostProcessAddNoise,
		PostProcessNoiseLevel: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9StubPacketForTest(t, 48, 40, 0, common.DcPred)
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no frame")
	}
	if frame.Width != 48 || frame.Height != 40 {
		t.Fatalf("frame = %dx%d, want 48x40", frame.Width, frame.Height)
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

func TestVP9DecoderErrorConcealmentReturnsPreviousFrameInsteadOfError(t *testing.T) {
	// Without concealment a truncated packet must surface as an error.
	strict, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder strict: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := strict.Decode(key); err != nil {
		t.Fatalf("strict Decode keyframe: %v", err)
	}
	if _, ok := strict.NextFrame(); !ok {
		t.Fatal("strict NextFrame returned no keyframe")
	}
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	tileStart, err := vp9TileStartForTest(inter)
	if err != nil {
		t.Fatalf("vp9TileStartForTest: %v", err)
	}
	if err := strict.DecodeWithPTS(inter[:tileStart], 1); err == nil {
		t.Fatal("strict decode of truncated frame returned no error")
	}

	// With concealment the truncated decode succeeds and surfaces the
	// previous reference's bits.
	conceal, err := NewVP9Decoder(VP9DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder conceal: %v", err)
	}
	if err := conceal.Decode(key); err != nil {
		t.Fatalf("conceal Decode keyframe: %v", err)
	}
	previous, ok := conceal.NextFrame()
	if !ok {
		t.Fatal("conceal NextFrame returned no keyframe")
	}
	previousY := append([]byte(nil), previous.Y...)
	previousU := append([]byte(nil), previous.U...)
	previousV := append([]byte(nil), previous.V...)
	if err := conceal.DecodeWithPTS(inter[:tileStart], 1); err != nil {
		t.Fatalf("conceal truncated DecodeWithPTS returned error: %v", err)
	}
	frame, ok := conceal.NextFrame()
	if !ok {
		t.Fatal("conceal NextFrame after truncated frame returned no frame")
	}
	if !testutil.PlaneEqual(previousY, previous.YStride, frame.Y, frame.YStride,
		previous.Width, previous.Height) {
		t.Fatal("error concealment did not surface previous frame Y")
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(previous.Width, previous.Height)
	if !testutil.PlaneEqual(previousU, previous.UStride, frame.U, frame.UStride,
		uvWidth, uvHeight) {
		t.Fatal("error concealment did not surface previous frame U")
	}
	if !testutil.PlaneEqual(previousV, previous.VStride, frame.V, frame.VStride,
		uvWidth, uvHeight) {
		t.Fatal("error concealment did not surface previous frame V")
	}
	info, ok := conceal.LastFrameInfo()
	if !ok || !info.Corrupted {
		t.Fatalf("LastFrameInfo = %+v ok=%v, want corrupted=true", info, ok)
	}
}

func TestVP9DecoderRejectsPostProcessFlagOutsideAllPostProcessFlags(t *testing.T) {
	// Bits outside allPostProcessFlags must be rejected upfront.
	bad := []PostProcessFlag{
		1 << 4,
		1 << 8,
		1 << 12,
		PostProcessDeblock | (1 << 5),
		PostProcessFlag(^uint32(allPostProcessFlags)),
	}
	for _, flag := range bad {
		_, err := NewVP9Decoder(VP9DecoderOptions{PostProcessFlags: flag})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("NewVP9Decoder(PostProcessFlags=0x%x) err = %v, want ErrInvalidConfig",
				uint32(flag), err)
		}
	}
	// AddNoise without NoiseLevel must be rejected too.
	if _, err := NewVP9Decoder(VP9DecoderOptions{
		PostProcessNoiseLevel: 4,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("NoiseLevel without AddNoise flag err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9DecoderErrorConcealmentConcealsCorruptInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	previous, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe NextFrame returned no frame")
	}
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	tileStart, err := vp9TileStartForTest(inter)
	if err != nil {
		t.Fatalf("vp9TileStartForTest: %v", err)
	}
	if err := d.DecodeWithPTS(inter[:tileStart], 99); err != nil {
		t.Fatalf("corrupt inter DecodeWithPTS: %v", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after concealment returned !ok")
	}
	if !info.Corrupted || info.PTS != 99 || info.Width != 64 ||
		info.Height != 64 {
		t.Fatalf("concealed info = %+v, want corrupted 64x64 PTS 99", info)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("concealed NextFrame returned no frame")
	}
	assertImagesEqual(t, "concealed VP9 frame", previous, frame)
}

func TestVP9DecoderErrorConcealmentConcealsMissingFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	previous, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe NextFrame returned no frame")
	}
	if err := d.DecodeWithPTS(nil, 100); err != nil {
		t.Fatalf("missing DecodeWithPTS: %v", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after missing-frame concealment returned !ok")
	}
	if !info.Corrupted || info.PTS != 100 || info.Width != 64 ||
		info.Height != 64 {
		t.Fatalf("missing-frame info = %+v, want corrupted 64x64 PTS 100", info)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("missing-frame NextFrame returned no frame")
	}
	assertImagesEqual(t, "missing-frame VP9 concealment", previous, frame)
}
