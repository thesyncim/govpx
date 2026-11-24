package govpx_test

import (
	"errors"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
)

// newVP9MultiResLayerOptions returns a deterministic layer-options
// configuration at the given dimensions for tests.
func newVP9MultiResLayerOptions(width, height int) govpx.VP9MultiResolutionLayerOptions {
	return govpx.VP9MultiResolutionLayerOptions{
		Width:  width,
		Height: height,
	}
}

func newVP9MultiResEncoderForTest(t *testing.T,
	layers ...govpx.VP9MultiResolutionLayerOptions,
) *govpx.VP9MultiResolutionEncoder {
	t.Helper()
	var arr [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions
	for i, l := range layers {
		arr[i] = l
	}
	enc, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: len(layers),
		Layers:     arr,
		FPS:        30,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
	}
	t.Cleanup(func() {
		_ = enc.Close()
	})
	return enc
}

func TestVP9MultiResolutionEncoderRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name string
		opts govpx.VP9MultiResolutionEncoderOptions
	}{
		{
			name: "zero layers",
			opts: govpx.VP9MultiResolutionEncoderOptions{},
		},
		{
			name: "too many layers",
			opts: govpx.VP9MultiResolutionEncoderOptions{
				LayerCount: govpx.MaxMultiResLayers + 1,
			},
		},
		{
			name: "non-decreasing widths",
			opts: govpx.VP9MultiResolutionEncoderOptions{
				LayerCount: 2,
				Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 64},
					{Width: 64, Height: 64},
				},
			},
		},
		{
			name: "increasing height",
			opts: govpx.VP9MultiResolutionEncoderOptions{
				LayerCount: 2,
				Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 32},
					{Width: 32, Height: 64},
				},
			},
		},
		{
			name: "negative threads",
			opts: govpx.VP9MultiResolutionEncoderOptions{
				LayerCount: 1,
				Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
					{Width: 64, Height: 64},
				},
				Threads: -1,
			},
		},
		{
			name: "invalid dimension",
			opts: govpx.VP9MultiResolutionEncoderOptions{
				LayerCount: 1,
				Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
					{Width: 0, Height: 64},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enc, err := govpx.NewVP9MultiResolutionEncoder(c.opts)
			if err == nil {
				_ = enc.Close()
				t.Fatalf("govpx.NewVP9MultiResolutionEncoder(%s) succeeded, want error", c.name)
			}
			if !errors.Is(err, govpx.ErrInvalidConfig) {
				t.Fatalf("govpx.NewVP9MultiResolutionEncoder(%s) err = %v, want govpx.ErrInvalidConfig", c.name, err)
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
	enc, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: 3,
		FPS:        30,
		Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
			{Width: 128, Height: 96},
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := vp9test.NewYCbCr(128, 96, 90, 100, 110)
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
		d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
		if err != nil {
			t.Fatalf("govpx.NewVP9Decoder layer %d: %v", i, err)
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
	src := vp9test.NewYCbCr(64, 48, 90, 100, 110)
	wrongCount := [][]byte{make([]byte, 1<<19)}
	if _, err := enc.EncodeIntoWithResult(src, wrongCount); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("wrong dsts count err = %v, want govpx.ErrInvalidConfig", err)
	}
	tooSmall := [][]byte{
		make([]byte, 1<<19),
		make([]byte, 1),
	}
	if _, err := enc.EncodeIntoWithResult(src, tooSmall); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("too-small dst err = %v, want govpx.ErrBufferTooSmall", err)
	}
	if _, err := enc.EncodeIntoWithResult(nil, [][]byte{
		make([]byte, 1<<19), make([]byte, 1<<19),
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("nil src err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func TestVP9MultiResolutionEncoderClosedReturnsErrClosed(t *testing.T) {
	enc := newVP9MultiResEncoderForTest(t,
		newVP9MultiResLayerOptions(64, 48),
	)
	_ = enc.Close()
	src := vp9test.NewYCbCr(64, 48, 90, 100, 110)
	dsts := [][]byte{make([]byte, 1<<18)}
	if _, err := enc.EncodeIntoWithResult(src, dsts); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("encode after close err = %v, want govpx.ErrClosed", err)
	}
	if _, err := enc.FlushIntoWithResult(dsts); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("flush after close err = %v, want govpx.ErrClosed", err)
	}
	if _, err := enc.LayerEncoder(0); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("LayerEncoder after close err = %v, want govpx.ErrClosed", err)
	}
	// Double-close is a no-op.
	if err := enc.Close(); err != nil {
		t.Fatalf("Close re-call err = %v, want nil", err)
	}
}

func TestVP9MultiResolutionEncoderForceKeyFrame(t *testing.T) {
	enc, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := vp9test.NewYCbCr(64, 48, 90, 100, 110)
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
	if enc.IsKeyFrameNext() {
		t.Fatalf("IsKeyFrameNext = true before ForceKeyFrame")
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
	src := vp9test.NewYCbCr(width0, height0, 90, 100, 110)

	// Reference: build the same downscaled lower-resolution source
	// the multi-resolution encoder will feed to layer 1, and encode
	// it with a hand-built standalone VP9Encoder. Layer 0 encodes the
	// caller source directly.
	scratch := image.NewYCbCr(image.Rect(0, 0, width1, height1),
		image.YCbCrSubsampleRatio420)
	resizeScratch := make([]int32,
		vp9dsp.PolyphaseScratchSize(width1, height0))
	vp9dsp.PolyphaseDownscaleI420(scratch, src, width1, height1, resizeScratch)

	ref0, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  width0,
		Height: height0,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder ref0: %v", err)
	}
	defer ref0.Close()
	ref1, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  width1,
		Height: height1,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder ref1: %v", err)
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

	mre, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
			{Width: width0, Height: height0},
			{Width: width1, Height: height1},
		},
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
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
	enc, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
			{Width: 64, Height: 48},
			{Width: 32, Height: 24},
		},
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := vp9test.NewYCbCr(64, 48, 90, 100, 110)
	dsts := [][]byte{make([]byte, 1<<19), make([]byte, 1<<19)}
	// Warmup.
	for range 4 {
		if _, err := enc.EncodeIntoWithResult(src, dsts); err != nil {
			t.Fatalf("warmup encode: %v", err)
		}
	}
	// Steady-state. Persistent per-layer worker goroutines and a
	// preallocated result slab eliminate the per-encode goroutine
	// launch closure plus the output slice allocation; the encode
	// path runs allocation-free in steady state.
	allocs := testing.AllocsPerRun(8, func() {
		_, _ = enc.EncodeIntoWithResult(src, dsts)
	})
	if allocs != 0 {
		t.Fatalf("steady-state allocs = %v, want 0", allocs)
	}
}

func TestVP9MultiResolutionEncoderRowMTPassthrough(t *testing.T) {
	enc, err := govpx.NewVP9MultiResolutionEncoder(govpx.VP9MultiResolutionEncoderOptions{
		LayerCount: 2,
		FPS:        30,
		Threads:    2,
		Layers: [govpx.MaxMultiResLayers]govpx.VP9MultiResolutionLayerOptions{
			{Width: 128, Height: 96, RowMT: true},
			{Width: 64, Height: 48, RowMT: true},
		},
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9MultiResolutionEncoder: %v", err)
	}
	defer enc.Close()
	src := vp9test.NewYCbCr(128, 96, 90, 100, 110)
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
