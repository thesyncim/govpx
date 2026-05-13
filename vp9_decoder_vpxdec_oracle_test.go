package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVP9DecoderVpxdecOracleMatchesIntraResidualKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packet := vp9SkipResidueKeyframeForTest(t, 64, 64, true, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for nonzero-residue keyframe\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9SkipResidueKeyframeForTest(t, 64, 64, true, 32)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesTiledInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)
	ivf := vp9IVFForTest(1024, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for tiled skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesShowExistingStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for key/hidden/show-existing stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesDecodeIntoShowExistingStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeIntoVisibleI420ForTest(t, 96, 96, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("DecodeInto I420 mismatch for key/hidden/show-existing stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func requireVP9VpxdecOracle(t *testing.T) {
	t.Helper()
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
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
	hidden, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode hidden intra-only: %v", err)
	}
	packets := [][]byte{
		key,
		hidden,
		vp9ShowExistingFramePacketForTest(5),
	}
	return packets, vp9IVFForTest(width, height, packets...)
}

func vp9IVFForTest(width, height int, packets ...[]byte) []byte {
	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               width,
		Height:              height,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          uint32(len(packets)),
	}
	out := testutil.WriteIVFHeader(header)
	for i, packet := range packets {
		out = append(out, testutil.WriteIVFFrame(packet, uint64(i))...)
	}
	return out
}

func vp9DecodeVisibleI420ForTest(t *testing.T, packets ...[]byte) []byte {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
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

func appendVP9I420(out []byte, img Image) []byte {
	for row := range img.Height {
		start := row * img.YStride
		out = append(out, img.Y[start:start+img.Width]...)
	}
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	for row := range uvHeight {
		start := row * img.UStride
		out = append(out, img.U[start:start+uvWidth]...)
	}
	for row := range uvHeight {
		start := row * img.VStride
		out = append(out, img.V[start:start+uvWidth]...)
	}
	return out
}
