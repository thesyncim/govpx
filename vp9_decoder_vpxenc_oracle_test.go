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

func TestVP9DecoderVpxencOracleProfile0StreamMatchesLibvpx(t *testing.T) {
	requireVP9VpxdecOracle(t)
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9MotionYCbCrForTest(width, height),
		newVP9CheckerYCbCrForTest(width, height, 48, 208, 96, 160),
		newVP9HorizontalBandsForTest(width, height, 112, 144),
		newVP9ChromaHorizontalBandsForTest(width, height),
	}
	raw := make([]byte, 0, len(frames)*(width*height+2*((width+1)>>1)*((height+1)>>1)))
	for _, frame := range frames {
		raw = appendVP9YCbCrI420(raw, frame)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, len(frames),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	assertVpxencVP9StreamInfo(t, ivf)

	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}
	got, err := decodeVP9IVFVisibleI420(ivf)
	if err != nil {
		t.Fatalf("govpx Decode VP9 vpxenc IVF returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for vpxenc-vp9 Profile 0 stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9VpxencOracleDefaultCQKeyframeBaseQIndex(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	frame := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	raw := appendVP9YCbCrI420(nil, frame)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	first, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}
	h, _ := parseVP9EncoderHeaderForTest(t, first.Data)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("vpxenc-vp9 BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func requireVP9VpxencOracle(t *testing.T) {
	t.Helper()
	if _, err := coracle.VpxencVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxencVP9NotBuilt) {
			t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxencVP9Path: %v", err)
	}
}

func appendVP9YCbCrI420(out []byte, img *image.YCbCr) []byte {
	width := img.Rect.Dx()
	height := img.Rect.Dy()
	for row := range height {
		start := row * img.YStride
		out = append(out, img.Y[start:start+width]...)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		start := row * img.CStride
		out = append(out, img.Cb[start:start+uvWidth]...)
	}
	for row := range uvHeight {
		start := row * img.CStride
		out = append(out, img.Cr[start:start+uvWidth]...)
	}
	return out
}

func assertVpxencVP9StreamInfo(t *testing.T, ivf []byte) {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	seenInter := false
	for index := 0; offset < len(ivf); index++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, index)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", index, err)
		}
		info, err := PeekVP9StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP9StreamInfo[%d]: %v", index, err)
		}
		if info.Profile != 0 {
			t.Fatalf("frame %d profile = %d, want 0", index, info.Profile)
		}
		if index == 0 && !info.KeyFrame {
			t.Fatalf("first vpxenc-vp9 frame was not a keyframe")
		}
		if index > 0 && !info.KeyFrame {
			seenInter = true
		}
		offset = next
	}
	if !seenInter {
		t.Fatalf("vpxenc-vp9 corpus did not produce an inter frame")
	}
}
