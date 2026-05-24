//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9DecoderVpxencOracleProfile0StreamMatchesLibvpx(t *testing.T) {
	vp9test.RequireVpxdec(t)
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewMotionYCbCr(width, height),
		vp9test.NewCheckerYCbCr(width, height, 48, 208, 96, 160),
		vp9test.NewHorizontalBandsYCbCr(width, height, 112, 144),
		vp9test.NewChromaHorizontalBandsYCbCr(width, height),
	}
	packets := vp9test.VpxencPackets(t, frames,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	)
	assertVpxencVP9StreamInfo(t, packets)

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	want := vp9test.VpxdecI420(t, ivf)
	got, err := decodeVP9IVFVisibleI420(ivf)
	if err != nil {
		t.Fatalf("govpx Decode VP9 vpxenc IVF returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for vpxenc-vp9 Profile 0 stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9VpxencOracleDefaultCQKeyframeBaseQIndex(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	frame := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	first := vp9test.VpxencPackets(t, []*image.YCbCr{frame})[0]
	h, _ := vp9test.ParseHeader(t, first)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("vpxenc-vp9 BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func assertVpxencVP9StreamInfo(t *testing.T, packets [][]byte) {
	t.Helper()
	seenInter := false
	for index, packet := range packets {
		info, err := PeekVP9StreamInfo(packet)
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
	}
	if !seenInter {
		t.Fatalf("vpxenc-vp9 corpus did not produce an inter frame")
	}
}
