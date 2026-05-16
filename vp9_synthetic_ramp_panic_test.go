package govpx

import (
	"errors"
	"image"
	"math"
	"testing"
)

// makeSyntheticRampFrame mirrors makeBenchmarkFrame from the govpx-bench
// suite: a deterministic high-frequency ramp that pixelwise rolls every
// frame. The 5-luma-sample horizontal period and 192-byte range stress the
// VP9 mode decision because the content has high gradient energy but no
// real translational motion (each pixel rolls by index*7), which leaves
// the partition-pick stage scoring adjacent partition sizes within a
// handful of cost units. Before the rate-cost-from-selectFc fix in
// pickVP9InterPartitionBlockSize this margin flipped partition decisions
// between the prepass count-collection walk and the real bit-emission
// walk, leaving the decoder's bool reader to underflow the tile body and
// surface as ErrInvalidVP9Data on the public DecodeInto path.
func makeSyntheticRampFrame(width, height, index int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.Cb[row*img.CStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.Cr[row*img.CStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

// vp9SyntheticRampOptions mirrors the bench's parityFor(cfg)+
// vp9BenchmarkEncoderOptions configuration that previously misbehaved on
// the synthetic ramp. Values match parityFor's "realtime" branch in
// cmd/govpx-bench/benchcmd/config.go (fps=30, threads=1, mode="realtime")
// composed with the bench's CpuUsed=0 default.
func vp9SyntheticRampOptions(width, height, bitrateKbps int) VP9EncoderOptions {
	const fps = 30
	maxIntraBitratePct := max(600*fps/20, 300)
	return VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		Threads:             1,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
		TargetBitrateKbps:   bitrateKbps,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 3000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 500,
		BufferOptimalSizeMs: 600,
		UndershootPct:       100,
		OvershootPct:        15,
		MaxIntraBitratePct:  maxIntraBitratePct,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  30,
		NoiseSensitivity:    4,
		StaticThreshold:     1,
	}
}

// TestVP9SyntheticRampEncoderNoPanic feeds the synthetic ramp through the
// 640x360 / 2 Mbps realtime-CBR config that previously emitted a malformed
// bitstream from writeVP9FrameTiles. The test must complete without
// panicking, every emitted packet must decode cleanly, and the first two
// frames -- key + first inter -- must reach a non-trivial PSNR. The
// synthetic ramp is pseudo-random per-pixel content with no real motion
// (every pixel rolls by index*7) so subsequent inter frames intentionally
// degrade in PSNR; the floor is set against the first two frames where the
// rate controller has not yet starved residue coding.
func TestVP9SyntheticRampEncoderNoPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping VP9 synthetic-ramp panic repro in -short mode")
	}
	const (
		width       = 640
		height      = 360
		bitrateKbps = 2000
		nFrames     = 10
	)
	enc, err := NewVP9Encoder(vp9SyntheticRampOptions(width, height, bitrateKbps))
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, max(4096, width*height*6))
	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	decoded := newSyntheticRampDecodeBuffer(width, height)
	psnrTotal := 0.0
	psnrCount := 0
	firstTwoPSNR := []float64{}
	for i := range nFrames {
		img := makeSyntheticRampFrame(width, height, i)
		result, err := enc.EncodeIntoWithResult(img, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		if _, err := dec.DecodeInto(result.Data, &decoded); err != nil {
			if errors.Is(err, ErrInvalidVP9Data) {
				t.Fatalf("frame %d rejected with ErrInvalidVP9Data: encoder emitted malformed bitstream", i)
			}
			t.Fatalf("DecodeInto frame %d: %v", i, err)
		}
		framePSNR := vp9SyntheticRampLumaPSNR(img, &decoded)
		psnrTotal += framePSNR
		psnrCount++
		if i < 2 {
			firstTwoPSNR = append(firstTwoPSNR, framePSNR)
		}
	}
	if psnrCount == 0 {
		t.Fatal("encoder produced no non-dropped, decodable packets at 2 Mbps")
	}
	// Floor: the keyframe + first inter must clear 25 dB on the synthetic
	// ramp. Prior to the prepass/realpass mode-cost fix the encoder either
	// emitted a malformed bitstream that the decoder rejected with
	// ErrInvalidVP9Data, or it desynced from the decoder via partition
	// rate-cost flipping decisions between the prepass and the real write
	// pass; both modes drop these two frames well below 25 dB or fail
	// outright.
	for idx, psnr := range firstTwoPSNR {
		if psnr < 25.0 {
			t.Fatalf("frame %d luma PSNR %.2f dB < 25 dB regression floor",
				idx, psnr)
		}
	}
}

// TestVP9SyntheticRampDecodes covers Bug 2: 640x360 / 600 kbps emits a
// packet the decoder previously rejected with ErrInvalidVP9Data. The
// regression assertion here is that every emitted (non-dropped) packet
// decodes cleanly.
func TestVP9SyntheticRampDecodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping VP9 synthetic-ramp decode repro in -short mode")
	}
	const (
		width       = 640
		height      = 360
		bitrateKbps = 600
		nFrames     = 10
	)
	enc, err := NewVP9Encoder(vp9SyntheticRampOptions(width, height, bitrateKbps))
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, max(4096, width*height*6))
	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	decoded := newSyntheticRampDecodeBuffer(width, height)
	any := false
	for i := range nFrames {
		img := makeSyntheticRampFrame(width, height, i)
		result, err := enc.EncodeIntoWithResult(img, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		any = true
		if _, err := dec.DecodeInto(result.Data, &decoded); err != nil {
			if errors.Is(err, ErrInvalidVP9Data) {
				t.Fatalf("frame %d rejected with ErrInvalidVP9Data: encoder emitted malformed bitstream", i)
			}
			t.Fatalf("DecodeInto frame %d: %v", i, err)
		}
	}
	if !any {
		t.Fatal("encoder produced no non-dropped packets; cannot exercise decode regression")
	}
}

func vp9SyntheticRampLumaPSNR(src *image.YCbCr, dst *Image) float64 {
	width := dst.Width
	height := dst.Height
	var sse uint64
	for y := range height {
		srcRow := src.Y[y*src.YStride:]
		dstRow := dst.Y[y*dst.YStride:]
		for x := range width {
			d := int(srcRow[x]) - int(dstRow[x])
			sse += uint64(d * d)
		}
	}
	if sse == 0 {
		return 100
	}
	mse := float64(sse) / float64(width*height)
	return 10 * math.Log10(65025.0/mse)
}

func newSyntheticRampDecodeBuffer(width, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}
