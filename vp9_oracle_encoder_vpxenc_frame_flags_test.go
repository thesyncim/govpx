//go:build govpx_oracle_trace

package govpx_test

import (
	"fmt"
	"image"
	"strconv"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9EncoderVpxencFrameFlagsDoubleKeyframeNonRDByteParity exercises the
// non-RD (cpu_used=8 realtime CBR) double-keyframe path: an initial keyframe at
// frame 0 plus a FORCED keyframe at frame 1 (two adjacent keyframes), then plain
// inter frames over a panning clip. The first inter frame after the double KF
// (frame 2) runs libvpx's nonrd NEWMV subpel search with a zero MV-entropy cost
// table: vp9_build_nmv_cost_table / vp9_build_inter_mode_cost
// (vp9_rd.c:439-444) only rebuild x->nmvcost and cpi->inter_mode_cost on
// non-intra frames satisfying (!use_nonrd_pick_mode || current_video_frame&7 ==
// 1), and both arrays are vpx_calloc'd to zero. Two adjacent keyframes never
// build them and frame 2 (&7 != 1) does not either, so the NEWMV refinement
// minimises pure variance (no MV cost), which previously diverged from govpx's
// always-on MV cost. Regression for that fix.
func TestVP9EncoderVpxencFrameFlagsDoubleKeyframeNonRDByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	const frames = 5
	const targetKbps = 300

	sources := vp9test.NewPanningSources(width, height, frames)

	opts := govpx.VP9EncoderOptions{
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}

	flags := make([]govpx.EncodeFlags, frames)
	flags[1] = govpx.EncodeForceKeyFrame

	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--cpu-used=8",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--exact-fps-timebase",
	}

	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, sources, flags, opts,
		extraArgs)
}

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
