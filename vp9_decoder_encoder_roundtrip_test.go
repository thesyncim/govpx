package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9DecoderDecodesEncoderKeyframeModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned a second frame without another Decode")
	}
}

// TestVP9DecoderDecodesEncoderInterSkipModeTile covers the second-frame
// public encoder path. It depends on the first keyframe parse to seed
// reference state before the visible LAST/ZeroMv skip inter header,
// compressed header, and tile mode-info stream are read.

func TestVP9DecoderDecodesEncoderInterSkipModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe err = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
}

// TestVP9DecoderShowExistingFrameUsesReferenceSlot covers the first
// reference-frame-manager behavior: keyframes refresh the VP9 ring, a
// show-existing packet displays a stored slot, and that packet must not
// disturb the preserved header state needed by the following inter header.

func TestVP9DecoderDecodesEncoderEdgeClippedModeTiles(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"right-edge", 96, 64},
		{"bottom-edge", 64, 96},
		{"corner-edge", 96, 96},
		{"sub-sb", 32, 32},
		{"odd-visible", 70, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: tc.w, Height: tc.h})
			img := vp9test.NewYCbCr(tc.w, tc.h, 128, 128, 128)
			key, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			inter, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode inter: %v", err)
			}

			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := d.Decode(key); err != nil {
				t.Fatalf("Decode keyframe err = %v, want nil", err)
			}
			frame, ok := d.NextFrame()
			if !ok {
				t.Fatal("NextFrame returned !ok after visible keyframe")
			}
			assertVP9NeutralFrame(t, frame, tc.w, tc.h)
			if err := d.Decode(inter); err != nil {
				t.Fatalf("Decode inter err = %v, want nil", err)
			}
			frame, ok = d.NextFrame()
			if !ok {
				t.Fatal("NextFrame returned !ok after visible inter frame")
			}
			assertVP9NeutralFrame(t, frame, tc.w, tc.h)
			w, h := d.LastFrameSize()
			if w != tc.w || h != tc.h {
				t.Fatalf("LastFrameSize() = (%d, %d), want (%d, %d)",
					w, h, tc.w, tc.h)
			}
		})
	}
}

// TestVP9DecoderRejectsMissingModeTile ensures a packet with valid
// headers but no tile body fails in the mode-info pass before the
// decoder publishes the new frame size.

func TestVP9DecoderDecodesMultiTileModeFrame(t *testing.T) {
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 1024 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (1024, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible multi-tile keyframe")
	}
	assertVP9NeutralFrame(t, frame, 1024, 64)
}

func TestVP9DecoderInvertTileDecodeOrderMatchesForwardOrder(t *testing.T) {
	key := vp9MultiTileModePacketForTest(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)

	for _, tc := range []struct {
		name    string
		opts    VP9DecoderOptions
		packets [][]byte
	}{
		{
			name:    "keyframe",
			opts:    VP9DecoderOptions{InvertTileDecodeOrder: true},
			packets: [][]byte{key},
		},
		{
			name: "threaded inter fallback",
			opts: VP9DecoderOptions{
				Threads:               4,
				InvertTileDecodeOrder: true,
			},
			packets: [][]byte{key, inter},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseOpts := VP9DecoderOptions{Threads: tc.opts.Threads}
			want := vp9DecodeLastVisibleFrameWithOptionsForTest(t, baseOpts,
				tc.packets...)
			got := vp9DecodeLastVisibleFrameWithOptionsForTest(t, tc.opts,
				tc.packets...)
			assertVP9ImagesEqual(t, want, got)
		})
	}
}

func TestVP9DecoderSetInvertTileDecodeOrderTogglesRuntimeControl(t *testing.T) {
	packet := vp9MultiTileModePacketForTest(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.SetInvertTileDecodeOrder(true); err != nil {
		t.Fatalf("SetInvertTileDecodeOrder(true): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode inverted: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after inverted decode returned !ok")
	}
	assertVP9ImagesEqual(t, want, got)
	if d.vp9TilePool == nil {
		t.Fatal("threaded decoder did not initialise tile pool")
	}
	if got := d.vp9TilePool.lastTileJobs; got != 0 {
		t.Fatalf("inverted decode used %d tile-worker jobs, want serial fallback", got)
	}

	if err := d.SetInvertTileDecodeOrder(false); err != nil {
		t.Fatalf("SetInvertTileDecodeOrder(false): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode forward: %v", err)
	}
	got, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after forward decode returned !ok")
	}
	assertVP9ImagesEqual(t, want, got)
	if got := d.vp9TilePool.lastTileJobs; got != 2 {
		t.Fatalf("forward decode used %d tile-worker jobs, want 2", got)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetInvertTileDecodeOrder(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetInvertTileDecodeOrder err = %v, want ErrClosed", err)
	}
}

// TestVP9DecoderDecodesZeroResidueKeyframe drives a skip=0 keyframe
// through the public decoder. The tile body carries all-zero
// coefficient streams, so Decode must consume residual tokens before
// publishing reconstructed I420 output.
