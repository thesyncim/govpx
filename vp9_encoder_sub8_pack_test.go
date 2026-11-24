package govpx

import (
	"bytes"
	"image"
	"testing"
)

// Odd-MI sub-8x8 pure-pack regression coverage.
//
// 640x360 has 45 MI rows (odd): the bottom partial superblocks reach
// legitimate sub-8x8 leaves under the realtime variance/ML partitioning.
// Count-token staging used to be disabled for odd MI dimensions because pure
// packing could not emit sub-8x8 leaves and the producer-token staging poisoned
// the collection at the folded-tx consume check. The pack walk now consumes
// those leaves from the committed miGrid Bmi quartet plus the staged UV-mode /
// partition / TOKENEXTRA streams, so odd-MI frames use the same single-walk
// packed write as even grids. Both prior failures were mid-stream
// undecodability, so every regression here decodes every produced frame.

func sub8PackCheckerFrame(width, height, index int) *image.YCbCr {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := &image.YCbCr{
		Y:              make([]byte, width*height),
		Cb:             make([]byte, uvWidth*uvHeight),
		Cr:             make([]byte, uvWidth*uvHeight),
		YStride:        width,
		CStride:        uvWidth,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, width, height),
	}
	const cell = 16
	for y := range height {
		for x := range width {
			cx := (x + index) / cell
			cy := (y + index/2) / cell
			if ((cx ^ cy) & 1) == 0 {
				img.Y[y*img.YStride+x] = 48
			} else {
				img.Y[y*img.YStride+x] = 208
			}
		}
	}
	for y := range uvHeight {
		for x := range uvWidth {
			cx := (2*x + index) / cell
			cy := (2*y + index/2) / cell
			if ((cx ^ cy) & 1) == 0 {
				img.Cb[y*img.CStride+x] = 112
				img.Cr[y*img.CStride+x] = 144
			} else {
				img.Cb[y*img.CStride+x] = 144
				img.Cr[y*img.CStride+x] = 112
			}
		}
	}
	return img
}

// encodeSub8PackFixture encodes `frames` checker frames at width x height and
// returns the per-frame packets (nil for dropped frames). Every produced
// packet is decoded before returning; a decode failure fails the test.
func encodeSub8PackFixture(t *testing.T, width, height, frames, threads int,
	tune func(enc *VP9Encoder),
) [][]byte {
	return encodeSub8PackFixtureOpts(t, VP9EncoderOptions{
		Width: width, Height: height, FPS: 30, Threads: threads,
		Deadline: DeadlineRealtime, CpuUsed: 8,
		TargetBitrateKbps: 600, RateControlModeSet: true,
		RateControlMode: RateControlCBR,
	}, frames, tune)
}

func encodeSub8PackFixtureOpts(t *testing.T, opts VP9EncoderOptions, frames int,
	tune func(enc *VP9Encoder),
) [][]byte {
	t.Helper()
	width, height := opts.Width, opts.Height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	if tune != nil {
		tune(enc)
	}
	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer dec.Close()
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	dst := Image{
		Width: width, Height: height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width, UStride: uvWidth, VStride: uvWidth,
	}
	packet := make([]byte, max(4096, width*height*6))
	out := make([][]byte, 0, frames)
	for i := range frames {
		res, err := enc.EncodeIntoWithResult(sub8PackCheckerFrame(width, height, i), packet)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		if res.Dropped || len(res.Data) == 0 {
			out = append(out, nil)
			continue
		}
		if _, err := dec.DecodeInto(res.Data, &dst); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		cp := make([]byte, len(res.Data))
		copy(cp, res.Data)
		out = append(out, cp)
	}
	return out
}

func TestVP9OddMICountTokenCollectionEligible(t *testing.T) {
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 640, Height: 360, FPS: 30, Threads: 1,
		Deadline: DeadlineRealtime, CpuUsed: 8,
		TargetBitrateKbps: 600, RateControlModeSet: true,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	// Prime the speed features with one encoded frame.
	packet := make([]byte, 640*360*6)
	if _, err := enc.EncodeIntoWithResult(sub8PackCheckerFrame(640, 360, 0), packet); err != nil {
		t.Fatalf("encode: %v", err)
	}
	// 640x360 => 45 x 80 MI: odd rows must be eligible for staging.
	if !enc.vp9CountTokenCollectionEligible(45, 80, 1, 1, vp9ModeTreeInterSource) {
		t.Fatalf("odd-MI (45x80) count-token collection not eligible")
	}
	// Odd cols too.
	if !enc.vp9CountTokenCollectionEligible(90, 81, 1, 1, vp9ModeTreeInterSource) {
		t.Fatalf("odd-MI (90x81) count-token collection not eligible")
	}
}

func TestVP9OddMISub8PurePackDecodesEveryFrame(t *testing.T) {
	// 66 frames reproduce the historical frame-54 undecodability window of the
	// first sub-8x8 pure-pack attempt (checker content, 640x360, cpu8 CBR).
	encodeSub8PackFixture(t, 640, 360, 66, 1, nil)
}

func TestVP9OddMISub8PurePackDecodesEveryFrameThreaded(t *testing.T) {
	// The prior mixed-path splice made the threaded checker undecodable at
	// frame 25; run well past that with tile-column workers active.
	encodeSub8PackFixture(t, 640, 360, 66, 4, nil)
}

// sub8PackParityOptions mirror the bench harness realtime parity settings; the
// buffer/quantizer trajectory reaches intra sub-8x8 fallback leaves on the
// even-MI 640x368 grid, the class whose committed mode/Bmi mismatch used to
// emit undecodable tile data (mode != bmi[3], wrong uv_mode probability row).
func sub8PackParityOptions(width, height, threads int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width: width, Height: height, FPS: 30, Threads: threads,
		Deadline: DeadlineRealtime, CpuUsed: 8,
		TargetBitrateKbps: 600, RateControlModeSet: true,
		RateControlMode: RateControlCBR,
		MinQuantizer:    2, MaxQuantizer: 56,
		MaxKeyframeInterval: 3000,
		BufferSizeMs:        1000, BufferInitialSizeMs: 500, BufferOptimalSizeMs: 600,
		UndershootPct: 100, OvershootPct: 15,
		DropFrameAllowed: true, DropFrameWaterMark: 30,
		StaticThreshold: 1,
	}
}

func TestVP9Sub8IntraLeafInvariantDecodesEveryFrame(t *testing.T) {
	for _, threads := range []int{1, 4} {
		encodeSub8PackFixtureOpts(t, sub8PackParityOptions(640, 368, threads), 66, nil)
	}
}

func TestVP9OddMIThreadedParityDecodesEveryFrame(t *testing.T) {
	for _, threads := range []int{2, 4} {
		encodeSub8PackFixtureOpts(t, sub8PackParityOptions(640, 360, threads), 66, nil)
	}
}

func TestVP9CountCollectionPoisonRecoveryKeepsBytes(t *testing.T) {
	// Clamp the staged-token arena so the count walk fails mid-frame after
	// finalized leaf stores were already omitted. The recovery rerun must make
	// the fallback write replay the count picks, keeping the output
	// byte-identical to the unclamped encode (packed and fallback writes are
	// byte-equal by construction).
	const frames = 30
	normal := encodeSub8PackFixture(t, 640, 360, frames, 1, nil)
	poisoned := encodeSub8PackFixture(t, 640, 360, frames, 1, func(enc *VP9Encoder) {
		enc.vp9TokenArenaTestCap = 2048
	})
	for i := range frames {
		if !bytes.Equal(normal[i], poisoned[i]) {
			t.Fatalf("frame %d diverged under poisoned collection: len %d vs %d",
				i, len(normal[i]), len(poisoned[i]))
		}
	}
}
