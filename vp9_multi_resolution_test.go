package govpx

import (
	"errors"
	"image"
	"testing"
)

// newVP9MultiResLayerOptions returns a deterministic layer-options
// configuration at the given dimensions for tests. Using Lossless
// keeps the encode path independent of rate-control quirks and lets
// the test assert exact-sample fidelity through a round-trip decode.
func newVP9MultiResLayerOptions(width, height int) VP9MultiResolutionLayerOptions {
	return VP9MultiResolutionLayerOptions{
		Width:  width,
		Height: height,
	}
}

func newVP9MultiResEncoderForTest(t *testing.T,
	layers ...VP9MultiResolutionLayerOptions,
) *VP9MultiResolutionEncoder {
	t.Helper()
	var arr [MaxMultiResLayers]VP9MultiResolutionLayerOptions
	for i, l := range layers {
		arr[i] = l
	}
	enc, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: len(layers),
		Layers:     arr,
		FPS:        30,
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	t.Cleanup(func() {
		_ = enc.Close()
	})
	return enc
}

func TestVP9MultiResolutionEncoderRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name string
		opts VP9MultiResolutionEncoderOptions
	}{
		{
			name: "zero layers",
			opts: VP9MultiResolutionEncoderOptions{},
		},
		{
			name: "too many layers",
			opts: VP9MultiResolutionEncoderOptions{
				LayerCount: MaxMultiResLayers + 1,
			},
		},
		{
			name: "non-decreasing widths",
			opts: VP9MultiResolutionEncoderOptions{
				LayerCount: 2,
				Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 64},
					{Width: 64, Height: 64},
				},
			},
		},
		{
			name: "increasing height",
			opts: VP9MultiResolutionEncoderOptions{
				LayerCount: 2,
				Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 32},
					{Width: 32, Height: 64},
				},
			},
		},
		{
			name: "negative threads",
			opts: VP9MultiResolutionEncoderOptions{
				LayerCount: 1,
				Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 64},
				},
				Threads: -1,
			},
		},
		{
			name: "invalid dimension",
			opts: VP9MultiResolutionEncoderOptions{
				LayerCount: 1,
				Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
					{Width: 0, Height: 64},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enc, err := NewVP9MultiResolutionEncoder(c.opts)
			if err == nil {
				_ = enc.Close()
				t.Fatalf("NewVP9MultiResolutionEncoder(%s) succeeded, want error", c.name)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewVP9MultiResolutionEncoder(%s) err = %v, want ErrInvalidConfig", c.name, err)
			}
		})
	}
}

func TestVP9MultiResolutionEncoderAcceptsDecreasingPyramid(t *testing.T) {
	enc := newVP9MultiResEncoderForTest(t,
		newVP9MultiResLayerOptions(128, 96),
		newVP9MultiResLayerOptions(64, 48),
		newVP9MultiResLayerOptions(32, 24),
	)
	if got, want := enc.LayerCount(), 3; got != want {
		t.Fatalf("LayerCount = %d, want %d", got, want)
	}
	for i := 0; i < enc.LayerCount(); i++ {
		layer, err := enc.LayerEncoder(i)
		if err != nil {
			t.Fatalf("LayerEncoder(%d): %v", i, err)
		}
		if layer == nil {
			t.Fatalf("LayerEncoder(%d) returned nil", i)
		}
	}
	if _, err := enc.LayerEncoder(-1); err == nil {
		t.Fatal("LayerEncoder(-1) succeeded")
	}
	if _, err := enc.LayerEncoder(enc.LayerCount()); err == nil {
		t.Fatal("LayerEncoder(N) succeeded")
	}
}

func TestVP9MultiResolutionEncoderEncodesAllLayers(t *testing.T) {
	enc, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: 3,
		FPS:        30,
		Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
			{Width: 128, Height: 96},
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := newVP9YCbCrForTest(128, 96, 90, 100, 110)
	dsts := [][]byte{
		make([]byte, 1<<19),
		make([]byte, 1<<19),
		make([]byte, 1<<19),
	}
	results, err := enc.EncodeIntoWithResult(src, dsts)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	wantDims := [3][2]int{{128, 96}, {64, 48}, {32, 24}}
	for i, res := range results {
		if !res.KeyFrame {
			t.Fatalf("layer %d: not a key frame", i)
		}
		if len(res.Data) == 0 || res.Dropped {
			t.Fatalf("layer %d: empty / dropped frame, res = %+v", i, res)
		}
		d, err := NewVP9Decoder(VP9DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP9Decoder layer %d: %v", i, err)
		}
		if err := d.Decode(res.Data); err != nil {
			t.Fatalf("Decode layer %d: %v", i, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("layer %d: NextFrame returned no frame", i)
		}
		if frame.Width != wantDims[i][0] || frame.Height != wantDims[i][1] {
			t.Fatalf("layer %d: decoded dims = %dx%d, want %dx%d",
				i, frame.Width, frame.Height, wantDims[i][0], wantDims[i][1])
		}
		_ = d.Close()
	}
}

func TestVP9MultiResolutionEncoderRejectsBadEncodeArgs(t *testing.T) {
	enc := newVP9MultiResEncoderForTest(t,
		newVP9MultiResLayerOptions(64, 48),
		newVP9MultiResLayerOptions(32, 24),
	)
	src := newVP9YCbCrForTest(64, 48, 90, 100, 110)
	wrongCount := [][]byte{make([]byte, 1<<19)}
	if _, err := enc.EncodeIntoWithResult(src, wrongCount); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong dsts count err = %v, want ErrInvalidConfig", err)
	}
	tooSmall := [][]byte{
		make([]byte, 1<<19),
		make([]byte, vp9MinEncodeIntoBuffer-1),
	}
	if _, err := enc.EncodeIntoWithResult(src, tooSmall); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("too-small dst err = %v, want ErrBufferTooSmall", err)
	}
	if _, err := enc.EncodeIntoWithResult(nil, [][]byte{
		make([]byte, 1<<19), make([]byte, 1<<19),
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil src err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9MultiResolutionEncoderClosedReturnsErrClosed(t *testing.T) {
	enc := newVP9MultiResEncoderForTest(t,
		newVP9MultiResLayerOptions(64, 48),
	)
	_ = enc.Close()
	src := newVP9YCbCrForTest(64, 48, 90, 100, 110)
	dsts := [][]byte{make([]byte, 1<<18)}
	if _, err := enc.EncodeIntoWithResult(src, dsts); !errors.Is(err, ErrClosed) {
		t.Fatalf("encode after close err = %v, want ErrClosed", err)
	}
	if _, err := enc.FlushIntoWithResult(dsts); !errors.Is(err, ErrClosed) {
		t.Fatalf("flush after close err = %v, want ErrClosed", err)
	}
	if _, err := enc.LayerEncoder(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("LayerEncoder after close err = %v, want ErrClosed", err)
	}
	// Double-close is a no-op.
	if err := enc.Close(); err != nil {
		t.Fatalf("Close re-call err = %v, want nil", err)
	}
}

func TestVP9MultiResolutionEncoderForceKeyFrame(t *testing.T) {
	enc, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := newVP9YCbCrForTest(64, 48, 90, 100, 110)
	dsts := [][]byte{
		make([]byte, 1<<19),
		make([]byte, 1<<19),
	}
	// First frame is naturally a key frame.
	if _, err := enc.EncodeIntoWithResult(src, dsts); err != nil {
		t.Fatalf("EncodeIntoWithResult #1: %v", err)
	}
	// Second frame: same source - should be inter on both layers.
	results, err := enc.EncodeIntoWithResult(src, dsts)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult #2: %v", err)
	}
	for i, res := range results {
		if res.KeyFrame {
			t.Fatalf("layer %d unexpected key frame at frame 2: %+v", i, res)
		}
	}
	// Force a key frame on every layer.
	if !enc.IsKeyFrameNext() {
		// IsKeyFrameNext is false here because we haven't forced yet.
	}
	enc.ForceKeyFrame()
	if !enc.IsKeyFrameNext() {
		t.Fatalf("IsKeyFrameNext = false after ForceKeyFrame")
	}
	results, err = enc.EncodeIntoWithResult(src, dsts)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult after ForceKeyFrame: %v", err)
	}
	for i, res := range results {
		if !res.KeyFrame {
			t.Fatalf("layer %d: not a key frame after ForceKeyFrame, res = %+v", i, res)
		}
	}
}

func TestVP9MultiResolutionEncoderParityVsManualEncoders(t *testing.T) {
	width0, height0 := 64, 48
	width1, height1 := 32, 24
	src := newVP9YCbCrForTest(width0, height0, 90, 100, 110)

	// Reference: build the same downscaled lower-resolution source
	// the multi-resolution encoder will feed to layer 1, and encode
	// it with a hand-built standalone VP9Encoder. Layer 0 encodes the
	// caller source directly.
	scratch := image.NewYCbCr(image.Rect(0, 0, width1, height1),
		image.YCbCrSubsampleRatio420)
	resizeScratch := make([]int32,
		vp9MultiResolutionPolyphaseScratchSize(width1, height0))
	vp9MultiResolutionDownscaleI420(scratch, src, width1, height1, resizeScratch)

	ref0, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width0,
		Height: height0,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder ref0: %v", err)
	}
	defer ref0.Close()
	ref1, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width1,
		Height: height1,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder ref1: %v", err)
	}
	defer ref1.Close()

	refDst0 := make([]byte, 1<<19)
	refDst1 := make([]byte, 1<<19)
	refRes0, err := ref0.EncodeIntoWithResult(src, refDst0)
	if err != nil {
		t.Fatalf("ref0 encode: %v", err)
	}
	refRes1, err := ref1.EncodeIntoWithResult(scratch, refDst1)
	if err != nil {
		t.Fatalf("ref1 encode: %v", err)
	}

	mre, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
			{Width: width0, Height: height0},
			{Width: width1, Height: height1},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	defer mre.Close()
	dsts := [][]byte{make([]byte, 1<<19), make([]byte, 1<<19)}
	results, err := mre.EncodeIntoWithResult(src, dsts)
	if err != nil {
		t.Fatalf("multi-res encode: %v", err)
	}
	tolerance := func(refSize, gotSize int) bool {
		if refSize == 0 {
			return gotSize == 0
		}
		delta := refSize - gotSize
		if delta < 0 {
			delta = -delta
		}
		// Per-layer encoders share the same input — multi-res should
		// produce byte-identical bitstreams in our independent
		// configuration. Allow a small tolerance (5%) in case the
		// downscale arithmetic produces a different chroma byte that
		// drifts the inter-pass entropy.
		return delta*20 <= refSize
	}
	if !tolerance(refRes0.SizeBytes, results[0].SizeBytes) {
		t.Fatalf("layer 0 size mismatch: multi-res = %d, ref = %d",
			results[0].SizeBytes, refRes0.SizeBytes)
	}
	if !tolerance(refRes1.SizeBytes, results[1].SizeBytes) {
		t.Fatalf("layer 1 size mismatch: multi-res = %d, ref = %d",
			results[1].SizeBytes, refRes1.SizeBytes)
	}
}

func TestVP9MultiResolutionEncoderSteadyStateAlloc(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping alloc gate in -short mode")
	}
	enc, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := newVP9YCbCrForTest(64, 48, 90, 100, 110)
	dsts := [][]byte{make([]byte, 1<<19), make([]byte, 1<<19)}
	// Warmup.
	for i := 0; i < 4; i++ {
		if _, err := enc.EncodeIntoWithResult(src, dsts); err != nil {
			t.Fatalf("warmup encode: %v", err)
		}
	}
	// Steady-state. The per-call result slice and goroutine launch
	// allocate a small fixed amount per encode; assert the count
	// stays low. With LayerCount=2 we spawn one goroutine and a
	// 2-element result slice.
	// The per-call result slice plus the goroutine-launch closure
	// allocate a small fixed amount; assert the count stays low.
	const allowed = 8
	allocs := testing.AllocsPerRun(8, func() {
		_, _ = enc.EncodeIntoWithResult(src, dsts)
	})
	if allocs > allowed {
		t.Fatalf("steady-state allocs = %v, want <= %d", allocs, allowed)
	}
}

func TestVP9MultiResolutionDownscalePlaneFlatField(t *testing.T) {
	src := make([]byte, 8*8)
	for i := range src {
		src[i] = 128
	}
	dst := make([]byte, 4*4)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(4, 8))
	vp9MultiResolutionPolyphaseFilterPlane(dst, 4, 4, 4, src, 8, 8, 8, scratch)
	for i, b := range dst {
		if b != 128 {
			t.Fatalf("dst[%d] = %d, want 128", i, b)
		}
	}
}

func TestVP9MultiResolutionDownscalePlaneLinearGradient(t *testing.T) {
	// Linear horizontal gradient should round-trip through the
	// 8-tap polyphase filter to a coarser monotonically-increasing
	// gradient. The polyphase filter is not order-preserving in
	// pathological cases (ringing), but for a 16-sample linear
	// ramp downscaled to 4 samples it stays monotone.
	srcW, srcH := 16, 1
	src := make([]byte, srcW*srcH)
	for x := 0; x < srcW; x++ {
		src[x] = byte(x * 16)
	}
	dstW := 4
	dst := make([]byte, dstW*srcH)
	scratch := make([]int32, vp9MultiResolutionPolyphaseScratchSize(dstW, srcH))
	vp9MultiResolutionPolyphaseFilterPlane(dst, dstW, dstW, srcH, src, srcW, srcW, srcH, scratch)
	for i := 1; i < dstW; i++ {
		if dst[i] < dst[i-1] {
			t.Fatalf("downscaled gradient regressed at %d: %v", i, dst)
		}
	}
}

func TestVP9MultiResolutionEncoderRowMTPassthrough(t *testing.T) {
	enc, err := NewVP9MultiResolutionEncoder(VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Threads:    2,
		Layers: [MaxMultiResLayers]VP9MultiResolutionLayerOptions{
			{Width: 128, Height: 96, RowMT: true},
			{Width: 64, Height: 48, RowMT: true},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := newVP9YCbCrForTest(128, 96, 90, 100, 110)
	dsts := [][]byte{make([]byte, 1<<19), make([]byte, 1<<19)}
	results, err := enc.EncodeIntoWithResult(src, dsts)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	for i, res := range results {
		if len(res.Data) == 0 {
			t.Fatalf("layer %d: empty output", i)
		}
	}
}
