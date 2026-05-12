package govpx

import (
	"errors"
	"testing"
)

// newResizeTestEncoder builds a realtime-CBR encoder at the supplied
// initial dimensions. Default encode/decoder settings keep the test
// surface narrow so the resize semantics under test are easy to read.
func newResizeTestEncoder(tb testing.TB, width int, height int) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    false,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder(%dx%d) returned error: %v", width, height, err)
	}
	return e
}

// resizeTestFrame returns a deterministic synthetic I420 frame at the
// given dimensions parameterized by a frame index, so consecutive
// frames produce a small amount of motion that exercises inter coding.
func resizeTestFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(48 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(112 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func encodeOneFrame(tb testing.TB, e *VP8Encoder, dst []byte, src Image, pts uint64) EncodeResult {
	tb.Helper()
	result, err := e.EncodeInto(dst, src, pts, 1, 0)
	if err != nil {
		tb.Fatalf("EncodeInto frame pts=%d: %v", pts, err)
	}
	return result
}

func TestSetRealtimeTargetResizeForcesKeyFrameAtNewSize(t *testing.T) {
	const (
		w1 = 320
		h1 = 240
		w2 = 640
		h2 = 480
	)
	e := newResizeTestEncoder(t, w1, h1)
	defer e.Close()

	dst := make([]byte, 1<<20)
	for i := range 5 {
		result := encodeOneFrame(t, e, dst, resizeTestFrame(w1, h1, i), uint64(i))
		if result.SizeBytes == 0 {
			t.Fatalf("pre-resize frame %d produced empty packet", i)
		}
		if i > 0 && result.KeyFrame {
			t.Fatalf("pre-resize frame %d unexpectedly a keyframe", i)
		}
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize up returned error: %v", err)
	}
	if e.opts.Width != w2 || e.opts.Height != h2 {
		t.Fatalf("post-resize encoder dims = %dx%d, want %dx%d", e.opts.Width, e.opts.Height, w2, h2)
	}
	if !e.forceKeyFrame {
		t.Fatalf("forceKeyFrame = false, want true after resize")
	}

	first := encodeOneFrame(t, e, dst, resizeTestFrame(w2, h2, 0), 100)
	if !first.KeyFrame {
		t.Fatalf("first post-resize frame KeyFrame = false, want true")
	}
	if first.SizeBytes == 0 {
		t.Fatalf("first post-resize frame produced empty packet")
	}
	if first.Quantizer < 0 || first.Quantizer > maxQuantizer {
		t.Fatalf("first post-resize frame quantizer = %d, out of range", first.Quantizer)
	}
	if first.InternalQuantizer < 0 || first.InternalQuantizer > 127 {
		t.Fatalf("first post-resize frame internal quantizer = %d, out of VP8 qindex range", first.InternalQuantizer)
	}
	if e.forceKeyFrame {
		t.Fatalf("forceKeyFrame still set after first post-resize encode")
	}

	for i := 1; i < 5; i++ {
		result := encodeOneFrame(t, e, dst, resizeTestFrame(w2, h2, i), uint64(100+i))
		if result.SizeBytes == 0 {
			t.Fatalf("post-resize frame %d produced empty packet", i)
		}
	}
}

func TestSetRealtimeTargetResizeRoundTripsThroughDecoder(t *testing.T) {
	const (
		w1 = 320
		h1 = 240
		w2 = 640
		h2 = 480
	)
	e := newResizeTestEncoder(t, w1, h1)
	defer e.Close()

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder: %v", err)
	}
	defer d.Close()

	dst := make([]byte, 1<<20)
	for i := range 5 {
		result := encodeOneFrame(t, e, dst, resizeTestFrame(w1, h1, i), uint64(i))
		if err := d.Decode(result.Data); err != nil {
			t.Fatalf("decode pre-resize frame %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame missing for pre-resize frame %d", i)
		}
		info, ok := d.LastFrameInfo()
		if !ok {
			t.Fatalf("LastFrameInfo missing for pre-resize frame %d", i)
		}
		if info.Width != w1 || info.Height != h1 {
			t.Fatalf("pre-resize frame %d info = %dx%d, want %dx%d", i, info.Width, info.Height, w1, h1)
		}
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize up: %v", err)
	}

	first := encodeOneFrame(t, e, dst, resizeTestFrame(w2, h2, 0), 100)
	if !first.KeyFrame {
		t.Fatalf("first post-resize frame not a key frame")
	}
	if err := d.Decode(first.Data); err != nil {
		t.Fatalf("decode first post-resize frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame missing for first post-resize frame")
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatalf("LastFrameInfo missing for first post-resize frame")
	}
	if !info.KeyFrame {
		t.Fatalf("decoder reports KeyFrame=false for first post-resize frame")
	}
	if info.Width != w2 || info.Height != h2 {
		t.Fatalf("first post-resize frame info = %dx%d, want %dx%d", info.Width, info.Height, w2, h2)
	}

	for i := 1; i < 4; i++ {
		result := encodeOneFrame(t, e, dst, resizeTestFrame(w2, h2, i), uint64(100+i))
		if err := d.Decode(result.Data); err != nil {
			t.Fatalf("decode post-resize inter frame %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame missing for post-resize inter frame %d", i)
		}
		info, ok := d.LastFrameInfo()
		if !ok {
			t.Fatalf("LastFrameInfo missing for post-resize inter frame %d", i)
		}
		if info.Width != w2 || info.Height != h2 {
			t.Fatalf("post-resize inter frame %d info = %dx%d, want %dx%d", i, info.Width, info.Height, w2, h2)
		}
	}
}

func TestSetRealtimeTargetResizeBackDownToOriginal(t *testing.T) {
	const (
		w1 = 320
		h1 = 240
		w2 = 640
		h2 = 480
	)
	e := newResizeTestEncoder(t, w1, h1)
	defer e.Close()

	dst := make([]byte, 1<<20)
	for i := range 3 {
		encodeOneFrame(t, e, dst, resizeTestFrame(w1, h1, i), uint64(i))
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize up: %v", err)
	}
	for i := range 3 {
		encodeOneFrame(t, e, dst, resizeTestFrame(w2, h2, i), uint64(100+i))
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w1, Height: h1}); err != nil {
		t.Fatalf("SetRealtimeTarget resize down: %v", err)
	}
	if e.opts.Width != w1 || e.opts.Height != h1 {
		t.Fatalf("post-down-resize encoder dims = %dx%d, want %dx%d", e.opts.Width, e.opts.Height, w1, h1)
	}
	first := encodeOneFrame(t, e, dst, resizeTestFrame(w1, h1, 0), 200)
	if !first.KeyFrame {
		t.Fatalf("first post-down-resize frame KeyFrame = false, want true")
	}
}

func TestSetRealtimeTargetResizeRejectsInvalidDimensions(t *testing.T) {
	const (
		w = 32
		h = 32
	)
	cases := []struct {
		name   string
		target RealtimeTarget
	}{
		{"width zero with positive height", RealtimeTarget{Height: 32}},
		{"width too large", RealtimeTarget{Width: maxVP8Dimension + 1, Height: 32}},
		{"height too large", RealtimeTarget{Width: 32, Height: maxVP8Dimension + 1}},
	}

	dst := make([]byte, 1<<20)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newResizeTestEncoder(t, w, h)
			defer e.Close()

			err := e.SetRealtimeTarget(tc.target)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
			if e.opts.Width != w || e.opts.Height != h {
				t.Fatalf("dims after reject = %dx%d, want %dx%d", e.opts.Width, e.opts.Height, w, h)
			}
			// Encoder must still be usable at its original size.
			result := encodeOneFrame(t, e, dst, resizeTestFrame(w, h, 0), 0)
			if !result.KeyFrame {
				t.Fatalf("first frame after reject KeyFrame = false, want true")
			}
		})
	}
}

func TestSetRealtimeTargetResizeOnClosedEncoderReturnsErrClosed(t *testing.T) {
	e := newResizeTestEncoder(t, 32, 32)
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 48}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget after Close err = %v, want ErrClosed", err)
	}
	var nilEnc *VP8Encoder
	if err := nilEnc.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 48}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget on nil encoder err = %v, want ErrClosed", err)
	}
}

func TestSetRealtimeTargetResizeRejectsWhileLookaheadNonEmpty(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		LookaheadFrames:     5,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder with lookahead: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	// First call buffers a frame in the lookahead queue and returns
	// ErrFrameNotReady. After this, lookaheadCount > 0.
	if _, err := e.EncodeInto(dst, resizeTestFrame(32, 32, 0), 0, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("EncodeInto with lookahead lag err = %v, want ErrFrameNotReady", err)
	}
	if e.lookaheadCount == 0 {
		t.Fatalf("lookaheadCount = 0 after first EncodeInto, want > 0")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 48}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("resize while lookahead non-empty err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Width != 32 || e.opts.Height != 32 {
		t.Fatalf("dims after rejected resize = %dx%d, want 32x32", e.opts.Width, e.opts.Height)
	}
}
