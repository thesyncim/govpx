package govpx_test

import (
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP8EncoderEncodeIntoUsesSourcePixels(t *testing.T) {
	darkEncoder := newVP8FacadeEncoder(t)
	brightEncoder := newVP8FacadeEncoder(t)
	dark := newVP8FacadeImage(16, 16)
	bright := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(bright, 220, 128, 128)
	dstDark := make([]byte, 4096)
	dstBright := make([]byte, 4096)

	darkResult, err := darkEncoder.EncodeInto(dstDark, dark, 0, 1, 0)
	if err != nil {
		t.Fatalf("dark EncodeInto returned error: %v", err)
	}
	brightResult, err := brightEncoder.EncodeInto(dstBright, bright, 0, 1, 0)
	if err != nil {
		t.Fatalf("bright EncodeInto returned error: %v", err)
	}

	darkFrame := decodeVP8FacadeFrame(t, darkResult.Data)
	brightFrame := decodeVP8FacadeFrame(t, brightResult.Data)
	if brightFrame.Y[0] <= darkFrame.Y[0] {
		t.Fatalf("decoded Y0 dark/bright = %d/%d, want bright greater", darkFrame.Y[0], brightFrame.Y[0])
	}
}

func TestVP8EncoderEncodeIntoWritesInterFrameForMatchingReference(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	src := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(src, 220, 90, 170)
	dstKey := make([]byte, 4096)
	key, err := e.EncodeInto(dstKey, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeVP8FacadeFrame(t, key.Data)
	dstInter := make([]byte, 4096)

	inter, err := e.EncodeInto(dstInter, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("second frame KeyFrame = true, want interframe")
	}

	decoded := decodeVP8FacadeSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertVP8FacadeImagesEqual(t, "inter", reconstructed, decoded[1])
}

func BenchmarkVP8EncoderEncodeIntoMatchingReferenceInterFrame(b *testing.B) {
	e := newVP8FacadeEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	src := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeVP8FacadeFrame(b, key.Data)
	interPacket := make([]byte, 4096)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.EncodeInto(interPacket, reconstructed, uint64(i+1), 1, 0); err != nil {
			b.Fatalf("inter EncodeInto returned error: %v", err)
		}
	}
}

func BenchmarkVP8EncoderEncodeIntoGoldenReferenceInterFrame(b *testing.B) {
	e := newVP8FacadeEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	first := newVP8FacadeImage(16, 16)
	second := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(first, 220, 90, 170)
	fillVP8FacadeImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeVP8FacadeFrame(b, key.Data)
	interPacket := make([]byte, 4096)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, govpx.EncodeNoUpdateGolden|govpx.EncodeNoUpdateAltRef); err != nil {
		b.Fatalf("second EncodeInto returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flags := govpx.EncodeNoReferenceLast | govpx.EncodeNoUpdateGolden | govpx.EncodeNoUpdateAltRef
		if _, err := e.EncodeInto(interPacket, keyFrame, uint64(i+2), 1, flags); err != nil {
			b.Fatalf("golden EncodeInto returned error: %v", err)
		}
	}
}

func decodeVP8FacadeFrame(t testing.TB, packet []byte) govpx.Image {
	t.Helper()
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	return frame
}

func decodeVP8FacadeSequence(t testing.TB, packets ...[]byte) []govpx.Image {
	t.Helper()
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	frames := make([]govpx.Image, 0, len(packets))
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d returned error: %v", i, err)
		}
		if frame, ok := d.NextFrame(); ok {
			frames = append(frames, frame)
		}
	}
	return frames
}
