//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"image"
	"testing"
)

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltrefInterMapReuseStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	seed := vp9SegmentedAltrefInterSkipFrameForTest(t)
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, seed, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, seed, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented altref inter map-reuse stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra skip stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraResidualStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.DcPred, common.DcPred, false, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra residual stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntegerTopRightBorderNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterIntegerTopRightBorderNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter integer top-right border newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func vp9ShowExistingOracleStreamForTest(t *testing.T, width, height int) ([][]byte, []byte) {
	t.Helper()
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	packets := [][]byte{
		key,
		inter,
		vp9test.ShowExistingFramePacket(5),
	}
	return packets, vp9IVFForTest(width, height, packets...)
}

func vp9IVFForTest(width, height int, packets ...[]byte) []byte {
	return vp9test.BuildVP9IVF(width, height, packets...)
}

func vp9DecodeVisibleI420ForTest(t *testing.T, packets ...[]byte) []byte {
	t.Helper()
	return vp9DecodeVisibleI420WithOptionsForTest(t, VP9DecoderOptions{},
		packets...)
}

func vp9DecodeVisibleI420WithOptionsForTest(t *testing.T,
	opts VP9DecoderOptions, packets ...[]byte,
) []byte {
	t.Helper()
	d, err := NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	var out []byte
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, frame)
		}
	}
	return out
}

func vp9DecodeIntoVisibleI420ForTest(t *testing.T, width, height int, packets ...[]byte) []byte {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(width, height)
	var out []byte
	for i, packet := range packets {
		info, err := d.DecodeInto(packet, &dst)
		if err != nil {
			t.Fatalf("DecodeInto packet %d: %v", i, err)
		}
		if info.ShowFrame {
			out = appendVP9I420(out, dst)
		}
	}
	return out
}
