//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9EncoderVpxencOracleKeyframeUncompressedHeaderParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	assertVP9KeyframeHeaderParity(t, got, want)
}

func TestVP9EncoderVpxencOracleBlackKeyframeCompressedHeaderParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleBlackRealtimeCPU5KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	src := vp9test.NewYCbCr(64, 64, 0, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		Deadline: DeadlineRealtime,
		CpuUsed:  5,
	}, []string{"--cpu-used=5"})
}

func TestVP9EncoderVpxencOracleMidgrayKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleFlat64KeyframeModeParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewYCbCr(width, height, 80, 128, 128)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src})[0]

	govpxGrid := decodeVP9MiGridForOracleTest(t, govpxPacket)
	libvpxGrid := decodeVP9MiGridForOracleTest(t, libvpxPacket)
	if len(govpxGrid) != len(libvpxGrid) {
		t.Fatalf("mi grid length: govpx=%d libvpx=%d", len(govpxGrid), len(libvpxGrid))
	}
	modeMatches := 0
	blockMatches := 0
	skipMatches := 0
	for i := range govpxGrid {
		if govpxGrid[i].Mode == libvpxGrid[i].Mode {
			modeMatches++
		}
		if govpxGrid[i].SbType == libvpxGrid[i].SbType {
			blockMatches++
		}
		if govpxGrid[i].Skip == libvpxGrid[i].Skip {
			skipMatches++
		}
	}
	t.Logf("VP9 flat 64x64 keyframe mode scoreboard: modes=%d/%d blocks=%d/%d skips=%d/%d govpx_bytes=%d libvpx_bytes=%d",
		modeMatches, len(govpxGrid), blockMatches, len(govpxGrid),
		skipMatches, len(govpxGrid), len(govpxPacket), len(libvpxPacket))
	vp9test.AssertPacketByteParity(t, "flat 64x64 keyframe", govpxPacket, libvpxPacket)
	if blockMatches != len(govpxGrid) || skipMatches != len(govpxGrid) {
		t.Fatalf("flat keyframe block/skip regression: block_matches=%d/%d skip_matches=%d/%d",
			blockMatches, len(govpxGrid), skipMatches, len(govpxGrid))
	}
	if vp9test.StrictEnv("GOVPX_VP9_KEYFRAME_MODE_STRICT") &&
		modeMatches != len(govpxGrid) {
		t.Fatalf("strict VP9 keyframe mode parity matched %d/%d modes",
			modeMatches, len(govpxGrid))
	}
}

func TestVP9EncoderVpxencOracleCheckerKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleChecker64KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleChecker320KeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 320, 180
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleStepped320FixedQuantizerKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 320, 180
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
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
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
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
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		CQLevel: 20,
	}, []string{"--cq-level=20"})
}

func TestVP9EncoderVpxencOraclePublicQuantizerBandKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
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
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
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
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		Lossless: true,
	}, []string{"--lossless=1"})
}

func TestVP9EncoderVpxencOracleErrorResilientKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 16, 16
	src := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		ErrorResilient: true,
	}, []string{"--error-resilient=1"})
}
