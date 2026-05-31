package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

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
