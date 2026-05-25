//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9OracleInvisibleKeyFrameByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 invisible-frame byte-parity scoreboard")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	sources := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 96, 128, 128),
	}
	flags := []EncodeFlags{EncodeInvisibleFrame}
	govpxRows, govpxPackets := captureGovpxVP9VariablePacketRows(t,
		VP9EncoderOptions{Width: width, Height: height, MinQuantizer: 32, MaxQuantizer: 32},
		sources, flags, nil)
	libvpxRows, libvpxPackets := captureLibvpxVP9VariablePacketRows(t,
		sources, flags, []bool{true},
		[]string{"--cq-level=32", "--min-q=32", "--max-q=32"})
	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 invisible keyframe byte-parity scoreboard: matches=%d/%d first_mismatch=%d stats=%s",
		matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 invisible keyframe rate rows:\n%s",
		vp9test.FormatRateScoreboardRows(govpxRows, libvpxRows))
	t.Logf("VP9 invisible keyframe byte rows:\n%s",
		vp9test.FormatStreamParityRows(t, govpxPackets, libvpxPackets))
	if vp9test.StrictEnv("GOVPX_VP9_INVISIBLE_KEY_STRICT") &&
		(stats.HasMismatch() || matches != len(govpxPackets)) {
		t.Fatalf("strict VP9 invisible keyframe parity: matches=%d/%d stats=%s",
			matches, len(govpxPackets), stats)
	}
}

func TestVP9OracleInvisibleKeyFrameStrictByteParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 invisible-frame byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	sources := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 96, 128, 128),
	}
	flags := []EncodeFlags{EncodeInvisibleFrame}
	_, govpxPackets := captureGovpxVP9VariablePacketRows(t,
		VP9EncoderOptions{Width: width, Height: height, MinQuantizer: 32, MaxQuantizer: 32},
		sources, flags, nil)
	_, libvpxPackets := captureLibvpxVP9VariablePacketRows(t,
		sources, flags, []bool{true},
		[]string{"--cq-level=32", "--min-q=32", "--max-q=32"})
	if len(govpxPackets) != len(libvpxPackets) {
		t.Fatalf("VP9 invisible keyframe packet count: govpx=%d libvpx=%d",
			len(govpxPackets), len(libvpxPackets))
	}
	for frame := range govpxPackets {
		vp9test.AssertPacketByteParity(t,
			fmt.Sprintf("VP9 invisible keyframe frame %d", frame),
			govpxPackets[frame], libvpxPackets[frame])
	}
}
