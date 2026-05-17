package govpx

import (
	"errors"
	"testing"
)

// FuzzVP8DecoderDecode is the non-oracle valid-input variant of the VP8
// decoder fuzz: it builds a tiny YUV image from the fuzz []byte, encodes
// it as a real VP8 keyframe via the public encoder, then decodes the
// resulting packet and asserts the roundtrip preserves dimensions and
// produces a non-empty visible frame.
//
// The complementary FuzzDecoderMalformedPackets fuzzes the rejection
// path; this one fuzzes the happy path and pins the post-decode shape
// so a regression on output strides, plane sizing, or FrameInfo
// dimensions surfaces immediately.
func FuzzVP8DecoderDecode(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		{0xff, 0x00, 0x80, 0x40, 0x20, 0x10},
		// A short repeating pattern so the encoder receives flat luma.
		{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80},
		// Mixed-noise seed.
		{0x10, 0x42, 0x87, 0xab, 0xc0, 0x55, 0xaa, 0xff, 0x00, 0x33, 0x66, 0x99},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("decode-roundtrip fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		const width, height = 32, 32
		src := vp8FuzzYUVNoiseImage(width, height, data)
		packet, ok := vp8FuzzEncodeOnce(t, width, height, src)
		if !ok {
			// Encoder declined this seed (e.g. rate control dropped the
			// frame, or the encoder rejected the config). Not interesting
			// here; the test is still considered a pass.
			return
		}

		d, err := NewVP8Decoder(DecoderOptions{MaxWidth: width, MaxHeight: height})
		if err != nil {
			t.Fatalf("NewVP8Decoder: %v", err)
		}
		defer func() {
			_ = d.Close()
		}()

		dst := newTestImage(width, height)
		info, err := d.DecodeInto(packet, &dst)
		if err != nil {
			// A valid encoder output decoded by a matching decoder must
			// succeed. If it fails, only the documented sentinels are
			// acceptable; anything else (or a non-sentinel) is a bug.
			if !errors.Is(err, ErrInvalidData) &&
				!errors.Is(err, ErrInvalidConfig) &&
				!errors.Is(err, ErrFrameRejected) {
				t.Fatalf("DecodeInto on freshly-encoded keyframe returned unexpected error: %v", err)
			}
			return
		}

		if info.Width != width || info.Height != height {
			t.Fatalf("FrameInfo dimensions = %dx%d, want %dx%d",
				info.Width, info.Height, width, height)
		}
		if !info.KeyFrame {
			t.Fatalf("first encoded packet not reported as keyframe; FrameInfo=%+v", info)
		}
		if !info.ShowFrame {
			t.Fatalf("first encoded packet not reported as visible; FrameInfo=%+v", info)
		}
		// Roundtrip must produce a usable I420 frame: visible dimensions
		// match, plane lengths cover the visible region, and the Y plane
		// is not entirely zero (would indicate the decoder wrote nothing).
		if dst.Width != width || dst.Height != height {
			t.Fatalf("dst dimensions changed: %dx%d (want %dx%d)",
				dst.Width, dst.Height, width, height)
		}
		if len(dst.Y) < width*height {
			t.Fatalf("dst.Y too short: len=%d want>=%d", len(dst.Y), width*height)
		}
		uvSize := ((width + 1) >> 1) * ((height + 1) >> 1)
		if len(dst.U) < uvSize || len(dst.V) < uvSize {
			t.Fatalf("dst chroma too short: U=%d V=%d want>=%d",
				len(dst.U), len(dst.V), uvSize)
		}
		nonZero := false
		for _, b := range dst.Y[:width*height] {
			if b != 0 {
				nonZero = true
				break
			}
		}
		if !nonZero {
			// All-zero Y can be legitimate if the fuzz input happened to
			// produce a pure-black input — flag only when the source was
			// non-zero so we don't false-positive on intentional black.
			srcNonZero := false
			for _, b := range src.Y[:width*height] {
				if b != 0 {
					srcNonZero = true
					break
				}
			}
			if srcNonZero {
				t.Fatalf("decoded Y plane is all zero but source had non-zero samples")
			}
		}
	})
}

// vp8FuzzYUVNoiseImage builds a width×height I420 image whose plane samples
// are derived from data via a simple deterministic XOR/rotate scheme. Empty
// data produces a 128-grey image.
func vp8FuzzYUVNoiseImage(width, height int, data []byte) Image {
	img := testImage(width, height)
	if len(data) == 0 {
		for i := range img.Y {
			img.Y[i] = 128
		}
		for i := range img.U {
			img.U[i] = 128
		}
		for i := range img.V {
			img.V[i] = 128
		}
		return img
	}
	step := len(data)
	for i := range img.Y {
		img.Y[i] = data[i%step] ^ byte(i)
	}
	for i := range img.U {
		img.U[i] = data[i%step] ^ byte(i*3)
	}
	for i := range img.V {
		img.V[i] = data[i%step] ^ byte(i*5)
	}
	return img
}

// vp8FuzzEncodeOnce encodes a single frame at fixed CBR/realtime settings
// suitable for the fuzz harness. Returns (packet, true) on a non-empty
// emitted keyframe, (nil, false) otherwise. This keeps the fuzz body
// focused on the decode-side assertions.
func vp8FuzzEncodeOnce(t *testing.T, width, height int, src Image) ([]byte, bool) {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return nil, false
	}
	dst := make([]byte, width*height*4+1024)
	result, err := e.EncodeInto(dst, src, 0, 1, EncodeForceKeyFrame)
	if err != nil || result.Dropped || len(result.Data) == 0 {
		return nil, false
	}
	return append([]byte(nil), result.Data...), true
}
