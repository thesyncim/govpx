package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderDecodeIntoScaledZeroMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled zero-mv info = %+v, want visible 32x32 frame", info)
	}
	left := dst.Y[8*dst.YStride+8]
	right := dst.Y[8*dst.YStride+24]
	if right <= left {
		t.Fatalf("DecodeInto scaled zero-mv right sample = %d, want above left sample %d",
			right, left)
	}
}

func TestVP9DecoderDecodeIntoScaledNewMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	if _, err := d.DecodeInto(vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64), &dst); err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	zeroI420 := appendVP9I420(nil, dst)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(vp9ScaledNewMvInterFrameForTest(t), &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled newmv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled newmv info = %+v, want visible 32x32 frame", info)
	}
	if bytes.Equal(appendVP9I420(nil, dst), zeroI420) {
		t.Fatal("DecodeInto scaled newmv frame matched the zero-mv scaled predictor")
	}
}

func TestVP9DecoderDecodeIntoCopiesInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	dst := newTestImage(64, 64)
	info, err := d.DecodeInto(key, &dst)
	if err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	if !info.ShowFrame {
		t.Fatalf("DecodeInto keyframe info = %+v, want visible frame", info)
	}
	want := appendVP9I420(nil, dst)

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	fillVP9PublicImage(&dst, 77)
	info, err = d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter skip frame: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter info = %+v, want visible non-key 64x64 frame", info)
	}
	got := appendVP9I420(nil, dst)
	if !bytes.Equal(got, want) {
		t.Fatal("DecodeInto inter skip frame did not copy the LAST reference pixels")
	}
}
