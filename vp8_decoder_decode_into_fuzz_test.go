package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// FuzzVP8DecoderDecodeInto mirrors FuzzVP9DecoderDecodeInto on the VP8 surface.
// It feeds the fuzz []byte as a single VP8 frame to Decoder.DecodeInto and
// asserts the decoder either returns a documented sentinel error or writes a
// sane I420 frame: no panic, dimensions inside the configured caps, no
// out-of-bounds plane writes (encoded by destination capacity), and chroma
// planes sized to the 4:2:0 ratio matching the visible width/height.
//
// Two destinations are exercised on the same packet: a small 64x64 buffer
// (so the validForEncode reject path is reached for over-dimensioned keys)
// and a 256x256 buffer at the decoder's MaxWidth/MaxHeight (so the happy
// path is reached when the packet is a small keyframe).
func FuzzVP8DecoderDecodeInto(f *testing.F) {
	seeds := vp8DecoderFuzzSeeds(f)
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 256, MaxHeight: 256})
		if err != nil {
			t.Fatalf("NewVP8Decoder: %v", err)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DecodeInto panicked on %d-byte input: %v", len(packet), r)
			}
			_ = d.Close()
		}()

		smallDst := newTestImage(64, 64)
		smallInfo, err := d.DecodeInto(packet, &smallDst)
		if err != nil {
			assertVP8FuzzDecodeError(t, err)
		} else {
			assertVP8FuzzFrameInfoSane(t, smallInfo, 64, 64)
			assertVP8FuzzDstShapeSane(t, smallDst)
		}

		// Reset to clear any half-applied state from the first attempt
		// and try again with a larger destination — the same packet may
		// have been rejected by the small destination's validForEncode
		// but accepted by a 256x256 destination.
		d.Reset()
		largeDst := newTestImage(256, 256)
		largeInfo, err := d.DecodeInto(packet, &largeDst)
		if err != nil {
			assertVP8FuzzDecodeError(t, err)
		} else {
			assertVP8FuzzFrameInfoSane(t, largeInfo, 256, 256)
			assertVP8FuzzDstShapeSane(t, largeDst)
		}
	})
}

// assertVP8FuzzDecodeError pins the set of errors the VP8 decoder may return
// for arbitrary inputs. Anything else means the decoder leaked an internal
// sentinel or panicked in disguise.
func assertVP8FuzzDecodeError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, ErrInvalidData):
	case errors.Is(err, ErrNeedKeyFrame):
	case errors.Is(err, ErrFrameNotReady):
	case errors.Is(err, ErrFrameRejected):
	case errors.Is(err, ErrInvalidConfig):
	case errors.Is(err, ErrBufferTooSmall):
	case errors.Is(err, ErrClosed):
	default:
		t.Fatalf("DecodeInto returned unexpected error: %v", err)
	}
}

// assertVP8FuzzFrameInfoSane checks that a successful DecodeInto returned a
// FrameInfo whose dimensions fit inside the caller's destination dimensions
// when the frame was visible. Hidden frames carry zero-dimension info and
// don't write to dst.
func assertVP8FuzzFrameInfoSane(t *testing.T, info FrameInfo, maxW, maxH int) {
	t.Helper()
	if !info.ShowFrame {
		return
	}
	if info.Width < 0 || info.Height < 0 {
		t.Fatalf("FrameInfo has negative dimensions: %dx%d", info.Width, info.Height)
	}
	if info.Width > maxW || info.Height > maxH {
		t.Fatalf("FrameInfo dimensions %dx%d exceed destination caps %dx%d",
			info.Width, info.Height, maxW, maxH)
	}
}

// assertVP8FuzzDstShapeSane checks that the destination image still has
// the expected 4:2:0 plane shape after DecodeInto. The decoder is supposed
// to leave the caller-owned strides intact and never reallocate.
func assertVP8FuzzDstShapeSane(t *testing.T, dst Image) {
	t.Helper()
	if dst.YStride < dst.Width || dst.UStride < (dst.Width+1)>>1 || dst.VStride < (dst.Width+1)>>1 {
		t.Fatalf("dst strides shrunk below visible width: y=%d u=%d v=%d w=%d",
			dst.YStride, dst.UStride, dst.VStride, dst.Width)
	}
	if len(dst.Y) < dst.YStride*dst.Height {
		t.Fatalf("dst.Y shrunk: len=%d need>=%d", len(dst.Y), dst.YStride*dst.Height)
	}
	uvH := (dst.Height + 1) >> 1
	if len(dst.U) < dst.UStride*uvH {
		t.Fatalf("dst.U shrunk: len=%d need>=%d", len(dst.U), dst.UStride*uvH)
	}
	if len(dst.V) < dst.VStride*uvH {
		t.Fatalf("dst.V shrunk: len=%d need>=%d", len(dst.V), dst.VStride*uvH)
	}
}

// vp8DecoderFuzzSeeds returns a curated seed corpus mirroring the layout of
// vp9DecoderFuzzSeeds: a mix of empty/short inputs, malformed frame tags,
// and real visible keyframes produced by the encoder so the fuzz harness
// can mutate around them.
func vp8DecoderFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		{0xff},
		{0x00, 0x00, 0x00},
		{0xff, 0xff, 0xff},
		vp8test.KeyFramePacket(16, 16, 0, 0, true),
		vp8test.KeyFramePacket(16, 16, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, false),
		vp8test.KeyFramePacketWithPayload(32, 32, 200, 0, true),
		vp8test.InterFramePacket(0, 0, true),
		vp8test.InterFramePacket(1, 0, true),
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 16, 16); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 64, 64); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 128, 128); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	return seeds
}

// vp8FuzzEncodedKeyframe encodes a single visible VP8 keyframe via the public
// encoder so the corpus contains valid bitstreams the fuzzer can mutate.
// Returns nil if encoding is unavailable; the fuzz harness still runs on
// smoke seeds in that case.
func vp8FuzzEncodedKeyframe(tb testing.TB, width, height int) []byte {
	tb.Helper()
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
		return nil
	}
	img := testImage(width, height)
	for i := range img.Y {
		img.Y[i] = 96
	}
	for i := range img.U {
		img.U[i] = 128
	}
	for i := range img.V {
		img.V[i] = 128
	}
	dst := make([]byte, width*height*4+1024)
	result, err := e.EncodeInto(dst, img, 0, 1, 0)
	if err != nil || len(result.Data) == 0 {
		return nil
	}
	return append([]byte(nil), result.Data...)
}
