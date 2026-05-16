package main

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
)

// TestResizeBigJump exercises the encoder over the full demo-supported
// dimension range, jumping straight from min to max and back, to make
// sure runtime resize doesn't choke on large deltas.
func TestResizeBigJump(t *testing.T) {
	enc, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               320,
		Height:              180,
		FPS:                 30,
		Threads:             1,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   800,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 500,
		BufferOptimalSizeMs: 600,
		KeyFrameInterval:    3000,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             -6,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()

	sizes := [][2]int{
		{320, 180}, {1920, 1088}, {160, 96}, {1280, 720}, {320, 192},
		{1920, 1088}, {160, 96},
	}
	dst := make([]byte, 3<<20)
	var pts uint64
	for fi, dim := range sizes {
		if fi > 0 {
			if err := enc.SetRealtimeTarget(govpx.RealtimeTarget{
				Width:  dim[0],
				Height: dim[1],
			}); err != nil {
				t.Fatalf("resize to %dx%d: %v", dim[0], dim[1], err)
			}
		}
		img := goImageForDim(dim[0], dim[1])
		res, err := enc.EncodeInto(dst, img, pts, 3000, 0)
		pts += 3000
		if err != nil {
			t.Fatalf("encode after resize to %dx%d: %v", dim[0], dim[1], err)
		}
		if res.Dropped || len(res.Data) == 0 {
			t.Fatalf("resize to %dx%d produced no data (dropped=%v)",
				dim[0], dim[1], res.Dropped)
		}
		if !res.KeyFrame {
			t.Fatalf("resize to %dx%d did not auto-key", dim[0], dim[1])
		}
		t.Logf("%dx%d: %d bytes (q=%d, kf=%v)",
			dim[0], dim[1], res.SizeBytes, res.Quantizer, res.KeyFrame)
	}
}

func goImageForDim(w, h int) govpx.Image {
	paddedH := ((h + 15) >> 4) << 4
	uvW := (w + 1) / 2
	uvH := (h + 1) / 2
	paddedUVH := ((uvH + 15) >> 4) << 4
	img := govpx.Image{
		Width:   w,
		Height:  h,
		Y:       make([]byte, w*paddedH),
		U:       make([]byte, uvW*paddedUVH),
		V:       make([]byte, uvW*paddedUVH),
		YStride: w,
		UStride: uvW,
		VStride: uvW,
	}
	for i := range img.Y {
		img.Y[i] = byte(i & 0xff)
	}
	for i := range img.U {
		img.U[i] = 128
		img.V[i] = 128
	}
	return img
}

// silence unused import in the simple loop above.
var _ = image.YCbCrSubsampleRatio420
