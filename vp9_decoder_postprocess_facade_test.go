package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func TestVP9DecoderPostProcessAddNoiseChangesOnlyLuma(t *testing.T) {
	packet := vp9EncodedKeyframeForTest(t, 64, 64, 128)
	plain, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
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
	noisy, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		PostProcessFlags:      govpx.PostProcessAddNoise,
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
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		PostProcessFlags: govpx.PostProcessDeblock |
			govpx.PostProcessDemacroblock |
			govpx.PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9EncodedKeyframeForTest(t, 64, 64, 128)
	for i := range 3 {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("warm Decode[%d]: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("warm NextFrame[%d] returned no frame", i)
		}
	}
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRunsForTest, func() {
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
		opts govpx.VP9DecoderOptions
	}{
		{name: "Deblock", opts: govpx.VP9DecoderOptions{PostProcessFlags: govpx.PostProcessDeblock}},
		{name: "Demacroblock", opts: govpx.VP9DecoderOptions{PostProcessFlags: govpx.PostProcessDemacroblock}},
		{name: "MFQE", opts: govpx.VP9DecoderOptions{PostProcessFlags: govpx.PostProcessMFQE}},
		{name: "AddNoise", opts: govpx.VP9DecoderOptions{
			PostProcessFlags:      govpx.PostProcessAddNoise,
			PostProcessNoiseLevel: 4,
		}},
		{name: "All", opts: govpx.VP9DecoderOptions{
			PostProcessFlags: govpx.PostProcessDeblock |
				govpx.PostProcessDemacroblock |
				govpx.PostProcessMFQE |
				govpx.PostProcessAddNoise,
			PostProcessNoiseLevel: 2,
		}},
		{name: "DeblockDemacroblockNoise", opts: govpx.VP9DecoderOptions{
			PostProcessFlags: govpx.PostProcessDeblock |
				govpx.PostProcessDemacroblock |
				govpx.PostProcessAddNoise,
			PostProcessNoiseLevel: 1,
		}},
	}
	packet := vp9EncodedKeyframeForTest(t, 64, 64, 128)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := govpx.NewVP9Decoder(tc.opts)
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
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		PostProcessFlags: govpx.PostProcessDeblock |
			govpx.PostProcessDemacroblock |
			govpx.PostProcessMFQE |
			govpx.PostProcessAddNoise,
		PostProcessNoiseLevel: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	packet := vp9EncodedKeyframeForTest(t, 48, 40, 128)
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

func TestVP9DecoderRejectsPostProcessFlagOutsideAllPostProcessFlags(t *testing.T) {
	bad := []govpx.PostProcessFlag{
		1 << 4,
		1 << 8,
		1 << 12,
		govpx.PostProcessDeblock | (1 << 5),
		govpx.PostProcessFlag(^uint32(0)),
	}
	for _, flag := range bad {
		_, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{PostProcessFlags: flag})
		if !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Errorf("NewVP9Decoder(PostProcessFlags=0x%x) err = %v, want ErrInvalidConfig",
				uint32(flag), err)
		}
	}
	if _, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		PostProcessNoiseLevel: 4,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Errorf("NoiseLevel without AddNoise flag err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9DecoderErrorConcealmentReturnsPreviousFrameInsteadOfError(t *testing.T) {
	key, _, truncated := vp9ConcealmentPacketsForTest(t)

	strict, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder strict: %v", err)
	}
	if err := strict.Decode(key); err != nil {
		t.Fatalf("strict Decode keyframe: %v", err)
	}
	if _, ok := strict.NextFrame(); !ok {
		t.Fatal("strict NextFrame returned no keyframe")
	}
	if err := strict.DecodeWithPTS(truncated, 1); err == nil {
		t.Fatal("strict decode of truncated frame returned no error")
	}

	conceal, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{ErrorConcealment: true})
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
	if err := conceal.DecodeWithPTS(truncated, 1); err != nil {
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

func TestVP9DecoderErrorConcealmentConcealsCorruptInterFrame(t *testing.T) {
	key, _, truncated := vp9ConcealmentPacketsForTest(t)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	previous, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe NextFrame returned no frame")
	}
	if err := d.DecodeWithPTS(truncated, 99); err != nil {
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
	assertVP9ImagesEqualForTest(t, previous, frame)
}

func TestVP9DecoderErrorConcealmentConcealsMissingFrame(t *testing.T) {
	key := vp9EncodedKeyframeForTest(t, 64, 64, 128)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
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
	assertVP9ImagesEqualForTest(t, previous, frame)
}

func vp9ConcealmentPacketsForTest(t testing.TB) (key, inter, truncated []byte) {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	key, err = e.Encode(vp9test.NewYCbCr(64, 64, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err = e.Encode(vp9test.NewYCbCr(64, 64, 128, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	_, tileStart := vp9test.ParseHeader(t, inter)
	return key, inter, inter[:tileStart]
}
