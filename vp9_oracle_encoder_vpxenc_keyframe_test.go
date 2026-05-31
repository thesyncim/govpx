//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleKeyframeUncompressedHeaderParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)

	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src})[0]

	got, _ := vp9test.ParseHeader(t, govpxPacket)
	want, _ := vp9test.ParseHeader(t, libvpxPacket)
	vp9oracle.AssertKeyframeHeaderParity(t, got, want)
}

func TestVP9EncoderVpxencOracleBlackKeyframeCompressedHeaderParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src})[0]

	govpxHeader, _ := vp9test.ParseHeader(t, govpxPacket)
	libvpxHeader, _ := vp9test.ParseHeader(t, libvpxPacket)
	if got, want := govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize; got != want {
		t.Fatalf("compressed header size = %d, want vpxenc %d", got, want)
	}

	govpxComp, govpxFc, govpxUncSize := vp9test.ReadCompressedHeader(t,
		govpxPacket, govpxHeader)
	libvpxComp, libvpxFc, libvpxUncSize := vp9test.ReadCompressedHeader(t,
		libvpxPacket, libvpxHeader)
	if govpxComp != libvpxComp {
		t.Fatalf("compressed header = %+v, want vpxenc %+v", govpxComp, libvpxComp)
	}
	if govpxFc != libvpxFc {
		t.Fatalf("frame context after compressed header diverged from vpxenc")
	}

	govpxCompBytes := govpxPacket[govpxUncSize : govpxUncSize+int(govpxHeader.FirstPartitionSize)]
	libvpxCompBytes := libvpxPacket[libvpxUncSize : libvpxUncSize+int(libvpxHeader.FirstPartitionSize)]
	if !bytes.Equal(govpxCompBytes, libvpxCompBytes) {
		t.Fatalf("compressed header bytes = % x, want vpxenc % x",
			govpxCompBytes, libvpxCompBytes)
	}
}

func TestVP9EncoderVpxencOracleBlackKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	vp9oracle.AssertKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleBlackRealtimeCPU5KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	src := vp9test.NewYCbCr(64, 64, 0, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		Deadline: govpx.DeadlineRealtime,
		CpuUsed:  5,
	}, []string{"--cpu-used=5"})
}

func TestVP9EncoderVpxencOracleMidgrayKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	vp9oracle.AssertKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleCheckerKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleChecker64KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleRawTargetRateClampKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 32, 32
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	opts := govpx.VP9EncoderOptions{
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   10_000,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	}
	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=10000",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--min-q=4",
		"--max-q=56",
	}
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, opts, args)
}

func TestVP9EncoderVpxencOracleTargetLevelClampKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   10_000,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        20,
		TargetLevel:         10,
		MaxKeyframeInterval: 128,
	}
	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=10000",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--min-q=4",
		"--max-q=20",
		"--target-level=10",
	}
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, opts, args)
}

func TestVP9EncoderVpxencOracleChecker320KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 320, 180
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleStepped320FixedQuantizerKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 320, 180
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}, []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	})
}

func TestVP9EncoderVpxencOracleFixedQuantizerKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}, []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	})
}

func TestVP9EncoderVpxencOracleCQLevelKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		CQLevel: 20,
	}, []string{"--cq-level=20"})
}

func TestVP9EncoderVpxencOraclePublicQuantizerBandKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		MinQuantizer: 10,
		MaxQuantizer: 50,
		CQLevel:      30,
	}, []string{
		"--min-q=10",
		"--max-q=50",
		"--cq-level=30",
	})
}

func TestVP9EncoderVpxencOracleCBRKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewPanningYCbCr(width, height, 0)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	}, []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
	})
}

func TestVP9EncoderVpxencOracleLosslessKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		Lossless: true,
	}, []string{"--lossless=1"})
}

func TestVP9EncoderVpxencOracleErrorResilientKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	vp9oracle.AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{
		ErrorResilient: true,
	}, []string{"--error-resilient=1"})
}
