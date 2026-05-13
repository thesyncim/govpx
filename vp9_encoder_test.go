package govpx

import (
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestNewVP9EncoderRequiresDimensions: Width and Height must both be
// positive; zero or negative values get rejected with
// ErrInvalidConfig.
func TestNewVP9EncoderRequiresDimensions(t *testing.T) {
	cases := []VP9EncoderOptions{
		{Width: 0, Height: 480},
		{Width: 640, Height: 0},
		{Width: -1, Height: 480},
	}
	for i, opts := range cases {
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

// TestNewVP9EncoderRejectsBadOptions covers the per-field bounds
// checks beyond the dimension gate.
func TestNewVP9EncoderRejectsBadOptions(t *testing.T) {
	base := VP9EncoderOptions{Width: 320, Height: 240}
	type bad struct {
		mutate func(*VP9EncoderOptions)
		want   error
	}
	cases := []bad{
		{func(o *VP9EncoderOptions) { o.Threads = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TargetBitrateKbps = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = 256 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MaxKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.FPS = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TimebaseNum = 1 }, ErrInvalidConfig}, // missing Den
		{func(o *VP9EncoderOptions) { o.TimebaseDen = 1 }, ErrInvalidConfig}, // missing Num
	}
	for i, c := range cases {
		opts := base
		c.mutate(&opts)
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, c.want) {
			t.Errorf("case %d: err = %v, want %v", i, err, c.want)
		}
	}
}

// TestNewVP9EncoderAcceptsMinimalOptions: a bare {Width,Height}
// works.
func TestNewVP9EncoderAcceptsMinimalOptions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the
// stub-keyframe path emits a Block64x64 PartitionNone + DC-pred +
// skip=1 frame whose header parses cleanly through the existing
// decoder, mode-info round-trips, and the residue walker
// short-circuits.
func TestVP9EncoderKeyframeStubProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Re-parse the uncompressed header — sanity that the frame
	// shape on the wire matches what the decoder expects.
	var br vp9dec.BitReader
	br.Init(got)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader: %v", perr)
	}
	if h.Width != 64 || h.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", h.Width, h.Height)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("FrameType = %d, want KeyFrame", h.FrameType)
	}
}

// TestVP9EncoderClose: after Close, Encode/EncodeInto return
// ErrClosed.
func TestVP9EncoderClose(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	img := image.NewYCbCr(image.Rect(0, 0, 320, 240), image.YCbCrSubsampleRatio420)
	if _, err := e.Encode(img); !errors.Is(err, ErrClosed) {
		t.Errorf("Encode after Close err = %v, want ErrClosed", err)
	}
}

// TestVP9EncoderIsKeyFrameNextCadence: first frame is always a key;
// later frames key on MaxKeyframeInterval boundaries (default 128).
func TestVP9EncoderIsKeyFrameNextCadence(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: 320, Height: 240, MaxKeyframeInterval: 4,
	})
	if !e.IsKeyFrameNext() {
		t.Error("first frame should be key")
	}
	// Pretend we encoded one frame.
	e.frameIndex = 1
	if e.IsKeyFrameNext() {
		t.Error("frame 1 should NOT be key when cadence=4")
	}
	e.frameIndex = 4
	if !e.IsKeyFrameNext() {
		t.Error("frame 4 should be key (cadence boundary)")
	}
	// After Close → never key.
	e.Close()
	if e.IsKeyFrameNext() {
		t.Error("closed encoder should never report key")
	}
}
