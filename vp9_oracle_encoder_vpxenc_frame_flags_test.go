//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencFrameFlagsForceKeyFrameByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeForceKeyFrame}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencForceKeyFrameAPIByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 144, 128, 128),
	}
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9oracle.EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		if i == 1 {
			e.ForceKeyFrame()
		}
		result, err := e.EncodeIntoWithResult(frame, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, frames,
		[]uint32{0, vp9oracle.FrameFlagsForLibvpx(govpx.EncodeForceKeyFrame)})
	for i, got := range govpxPackets {
		vp9test.AssertPacketByteParity(t, fmt.Sprintf("frame %d", i), got,
			libvpxPackets[i])
	}
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateAllByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeNoUpdateLast | govpx.EncodeNoUpdateGolden |
			govpx.EncodeNoUpdateAltRef,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoReferenceGoldenAltRefByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeNoReferenceGolden | govpx.EncodeNoReferenceAltRef,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateLastByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeNoUpdateLast}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceGoldenNoUpdateLastByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeForceGoldenFrame | govpx.EncodeNoUpdateLast,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceAltRefNoUpdateGoldenByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeForceAltRefFrame | govpx.EncodeNoUpdateGolden,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateEntropyByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeNoUpdateEntropy}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}
