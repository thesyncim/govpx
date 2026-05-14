package govpx

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9EncoderVpxencOracleKeyframeUncompressedHeaderParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	got, _ := parseVP9EncoderHeaderForTest(t, govpxPacket)
	want, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
	assertVP9KeyframeHeaderParity(t, got, want)
}

func assertVP9KeyframeHeaderParity(t *testing.T, got, want vp9dec.UncompressedHeader) {
	t.Helper()
	if got.Profile != want.Profile ||
		got.FrameType != want.FrameType ||
		got.ShowFrame != want.ShowFrame ||
		got.ErrorResilientMode != want.ErrorResilientMode ||
		got.Width != want.Width ||
		got.Height != want.Height ||
		got.Render != want.Render ||
		got.RefreshFrameFlags != want.RefreshFrameFlags ||
		got.RefreshFrameContext != want.RefreshFrameContext ||
		got.FrameParallelDecoding != want.FrameParallelDecoding ||
		got.FrameContextIdx != want.FrameContextIdx ||
		got.InterpFilter != want.InterpFilter ||
		got.Tile != want.Tile ||
		got.Quant != want.Quant ||
		got.Loopfilter != want.Loopfilter ||
		got.Seg != want.Seg {
		t.Fatalf("govpx keyframe header = %+v\nvpxenc keyframe header = %+v",
			got, want)
	}
}

func TestVP9EncoderVpxencOracleBlackKeyframeCompressedHeaderParity(t *testing.T) {
	requireVP9VpxencOracle(t)

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
	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	govpxHeader, _ := parseVP9EncoderHeaderForTest(t, govpxPacket)
	libvpxHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
	if got, want := govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize; got != want {
		t.Fatalf("compressed header size = %d, want vpxenc %d", got, want)
	}

	govpxComp, govpxFc, govpxUncSize := readVP9CompressedHeaderForOracleTest(t,
		govpxPacket, govpxHeader)
	libvpxComp, libvpxFc, libvpxUncSize := readVP9CompressedHeaderForOracleTest(t,
		libvpxFrame.Data, libvpxHeader)
	if govpxComp != libvpxComp {
		t.Fatalf("compressed header = %+v, want vpxenc %+v", govpxComp, libvpxComp)
	}
	if govpxFc != libvpxFc {
		t.Fatalf("frame context after compressed header diverged from vpxenc")
	}

	govpxCompBytes := govpxPacket[govpxUncSize : govpxUncSize+int(govpxHeader.FirstPartitionSize)]
	libvpxCompBytes := libvpxFrame.Data[libvpxUncSize : libvpxUncSize+int(libvpxHeader.FirstPartitionSize)]
	if !bytes.Equal(govpxCompBytes, libvpxCompBytes) {
		t.Fatalf("compressed header bytes = % x, want vpxenc % x",
			govpxCompBytes, libvpxCompBytes)
	}
}

func TestVP9EncoderVpxencOracleBlackKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleMidgrayKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleLookaheadNoAltRefScoreboard(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9YCbCrForTest(width, height, byte(80+i*24), 128, 128)
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	govpxPackets := make([][]byte, 0, frames)
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		govpxPackets = append(govpxPackets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		govpxPackets = append(govpxPackets, append([]byte(nil), result.Data...))
	}
	if len(govpxPackets) != frames {
		t.Fatalf("govpx lookahead packets = %d, want %d", len(govpxPackets), frames)
	}

	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, frames,
		"--lag-in-frames=2", "--auto-alt-ref=0")
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != frames {
		t.Fatalf("libvpx lookahead packets = %d, want %d", count, frames)
	}

	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	matches := 0
	for i, got := range govpxPackets {
		var libvpxFrame testutil.IVFFrame
		libvpxFrame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		if bytes.Equal(got, libvpxFrame.Data) {
			matches++
			continue
		}
		gotHeader, _ := parseVP9EncoderHeaderForTest(t, got)
		wantHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
		t.Logf("lookahead row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
			i, len(got), gotHeader.Quant.BaseQindex, gotHeader.RefreshFrameFlags,
			gotHeader.FirstPartitionSize, len(libvpxFrame.Data),
			wantHeader.Quant.BaseQindex, wantHeader.RefreshFrameFlags,
			wantHeader.FirstPartitionSize)
	}
	t.Logf("VP9 lookahead no-alt-ref oracle: byte_matches=%d/%d", matches, frames)
	if matches != frames {
		t.Fatalf("VP9 lookahead byte parity matched %d/%d packets", matches, frames)
	}
}

func TestVP9EncoderVpxencOracleLookaheadNoAltRefMatrixScoreboard(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	cases := []struct {
		name   string
		lag    int
		frames int
	}{
		{name: "lag1", lag: 1, frames: 4},
		{name: "lag2", lag: 2, frames: 5},
		{name: "lag4", lag: 4, frames: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = newVP9YCbCrForTest(width, height,
					byte(72+i*19), 128, 128)
			}

			govpxPackets := captureVP9LookaheadPacketsForOracleTest(t,
				VP9EncoderOptions{LookaheadFrames: tc.lag}, sources)
			var raw []byte
			for _, src := range sources {
				raw = appendVP9YCbCrI420(raw, src)
			}
			ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
				tc.frames,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag),
				"--auto-alt-ref=0")
			if err != nil {
				t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
			}
			count, err := testutil.CountIVFFrames(ivf)
			if err != nil {
				t.Fatalf("CountIVFFrames: %v", err)
			}
			if count != tc.frames {
				t.Fatalf("libvpx lookahead packets = %d, want %d",
					count, tc.frames)
			}

			offset, err := testutil.FirstIVFFrameOffset(ivf)
			if err != nil {
				t.Fatalf("FirstIVFFrameOffset: %v", err)
			}
			matches := 0
			firstMismatch := -1
			for i, got := range govpxPackets {
				var libvpxFrame testutil.IVFFrame
				libvpxFrame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
				if err != nil {
					t.Fatalf("NextIVFFrame[%d]: %v", i, err)
				}
				if bytes.Equal(got, libvpxFrame.Data) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
				gotHeader, _ := parseVP9EncoderHeaderForTest(t, got)
				wantHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
				t.Logf("lookahead %s row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
					tc.name, i, len(got), gotHeader.Quant.BaseQindex,
					gotHeader.RefreshFrameFlags, gotHeader.FirstPartitionSize,
					len(libvpxFrame.Data), wantHeader.Quant.BaseQindex,
					wantHeader.RefreshFrameFlags,
					wantHeader.FirstPartitionSize)
			}
			t.Logf("VP9 lookahead no-alt-ref matrix %s: byte_matches=%d/%d first_mismatch=%d",
				tc.name, matches, tc.frames, firstMismatch)
			if os.Getenv("GOVPX_VP9_LOOKAHEAD_MATRIX_STRICT") == "1" &&
				matches != tc.frames {
				t.Fatalf("strict VP9 lookahead no-alt-ref matrix %s matched %d/%d packets",
					tc.name, matches, tc.frames)
			}
		})
	}
}

func TestVP9EncoderVpxencOracleFlat64KeyframeModeScoreboard(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9YCbCrForTest(width, height, 80, 128, 128)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	govpxGrid := decodeVP9MiGridForOracleTest(t, govpxPacket)
	libvpxGrid := decodeVP9MiGridForOracleTest(t, libvpxFrame.Data)
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
		skipMatches, len(govpxGrid), len(govpxPacket), len(libvpxFrame.Data))
	assertVP9PacketByteParity(t, "flat 64x64 keyframe", govpxPacket, libvpxFrame.Data)
	if blockMatches != len(govpxGrid) || skipMatches != len(govpxGrid) {
		t.Fatalf("flat keyframe block/skip regression: block_matches=%d/%d skip_matches=%d/%d",
			blockMatches, len(govpxGrid), skipMatches, len(govpxGrid))
	}
	if os.Getenv("GOVPX_VP9_KEYFRAME_MODE_STRICT") == "1" &&
		modeMatches != len(govpxGrid) {
		t.Fatalf("strict VP9 keyframe mode parity matched %d/%d modes",
			modeMatches, len(govpxGrid))
	}
}

func TestVP9EncoderVpxencOracleInterModeDistributionScoreboard(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 6
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, frames)
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, frames, nil)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	libvpxPackets := make([][]byte, frames)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range libvpxPackets {
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		libvpxPackets[i] = append([]byte(nil), frame.Data...)
	}

	govpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, govpxPackets)
	libvpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, libvpxPackets)
	var totalModeDistance, totalBlockDistance, totalSkipDistance int
	for i := range govpxGrids {
		g := collectVP9ModeDistribution(govpxGrids[i])
		l := collectVP9ModeDistribution(libvpxGrids[i])
		modeDistance := vp9ModeDistributionDistance(g.Modes, l.Modes)
		blockDistance := vp9BlockDistributionDistance(g.Blocks, l.Blocks)
		skipDistance := vp9AbsIntForOracleTest(g.Skip - l.Skip)
		totalModeDistance += modeDistance
		totalBlockDistance += blockDistance
		totalSkipDistance += skipDistance
		t.Logf("VP9 inter-mode distribution frame %d: mode_distance=%d block_distance=%d skip_distance=%d govpx=%s libvpx=%s",
			i, modeDistance, blockDistance, skipDistance,
			g.String(), l.String())
	}
	t.Logf("VP9 inter-mode distribution scoreboard: total_mode_distance=%d total_block_distance=%d total_skip_distance=%d",
		totalModeDistance, totalBlockDistance, totalSkipDistance)
	if os.Getenv("GOVPX_VP9_MODE_DIST_STRICT") == "1" &&
		(totalModeDistance != 0 || totalBlockDistance != 0 ||
			totalSkipDistance != 0) {
		t.Fatalf("strict VP9 inter-mode distribution mismatch: mode=%d block=%d skip=%d",
			totalModeDistance, totalBlockDistance, totalSkipDistance)
	}
}

func TestVP9EncoderVpxencOracleCheckerKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleChecker64KeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleFixedQuantizerKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
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
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		CQLevel: 20,
	}, []string{"--cq-level=20"})
}

func TestVP9EncoderVpxencOraclePublicQuantizerBandKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
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

func TestVP9EncoderVpxencOracleLosslessKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		Lossless: true,
	}, []string{"--lossless=1"})
}

func TestVP9EncoderVpxencOracleErrorResilientKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{
		ErrorResilient: true,
	}, []string{"--error-resilient=1"})
}

func TestVP9EncoderVpxencOracleIdenticalInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	assertVP9VpxencTwoFrameByteParity(t, src, src)
}

func TestVP9EncoderVpxencOracleChangedConstantInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9YCbCrForTest(width, height, 160, 128, 128)
	assertVP9VpxencTwoFrameByteParity(t, first, second)
}

func TestVP9EncoderVpxencOracleCheckerInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	assertVP9VpxencTwoFrameByteParity(t, first, second)
}

func TestVP9EncoderVpxencOracleFixedQuantizerInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9YCbCrForTest(width, height, 160, 128, 128)
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{
		MinQuantizer: 20,
		MaxQuantizer: 20,
	}, []string{
		"--cq-level=20",
		"--min-q=20",
		"--max-q=20",
		"--disable-warning-prompt",
	})
}

func TestVP9EncoderVpxencOracleCQLevelInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9YCbCrForTest(width, height, 160, 128, 128)
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{
		CQLevel: 20,
	}, []string{"--cq-level=20"})
}

func TestVP9EncoderVpxencOraclePublicQuantizerBandInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9YCbCrForTest(width, height, 160, 128, 128)
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{
		MinQuantizer: 10,
		MaxQuantizer: 50,
		CQLevel:      30,
	}, []string{
		"--min-q=10",
		"--max-q=50",
		"--cq-level=30",
	})
}

func TestVP9EncoderVpxencOracleLosslessInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{
		Lossless: true,
	}, []string{"--lossless=1"})
}

func TestVP9EncoderVpxencOracleErrorResilientInterByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	first := newVP9YCbCrForTest(width, height, 128, 128, 128)
	second := newVP9YCbCrForTest(width, height, 160, 128, 128)
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{
		ErrorResilient: true,
	}, []string{"--error-resilient=1"})
}

func TestVP9EncoderVpxencOracleMaxKeyframeIntervalByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
	}
	assertVP9VpxencFrameSequenceByteParityWithOptions(t, frames, VP9EncoderOptions{
		MaxKeyframeInterval: 2,
	}, []string{"--kf-max-dist=2"})
}

func TestVP9EncoderVpxencFrameFlagsForceKeyFrameByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 128, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeForceKeyFrame}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencForceKeyFrameAPIByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 144, 128, 128),
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
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

	libvpxFlags := []uint32{0, vp9FrameFlagsForLibvpx(EncodeForceKeyFrame)}
	var raw []byte
	for _, frame := range frames {
		raw = appendVP9YCbCrI420(raw, frame)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, len(frames), libvpxFlags)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i, got := range govpxPackets {
		var libvpxFrame testutil.IVFFrame
		libvpxFrame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		assertVP9PacketByteParity(t, fmt.Sprintf("frame %d", i), got,
			libvpxFrame.Data)
	}
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateAllByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, vp9NoUpdateRefFlags}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoReferenceGoldenAltRefByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeNoReferenceGolden | EncodeNoReferenceAltRef}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateLastByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeNoUpdateLast}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceGoldenNoUpdateLastByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeForceGoldenFrame | EncodeNoUpdateLast}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceAltRefNoUpdateGoldenByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeForceAltRefFrame | EncodeNoUpdateGolden}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateEntropyByteParity(t *testing.T) {
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		newVP9YCbCrForTest(width, height, 128, 128, 128),
		newVP9YCbCrForTest(width, height, 160, 128, 128),
	}
	flags := []EncodeFlags{0, EncodeNoUpdateEntropy}
	assertVP9VpxencFrameFlagsByteParityWithOptions(t, frames, flags, VP9EncoderOptions{}, nil)
}

func captureVP9LookaheadPacketsForOracleTest(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 lookahead source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	opts.Width = width
	opts.Height = height
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	if len(packets) != len(sources) {
		t.Fatalf("VP9 lookahead packets = %d, want %d",
			len(packets), len(sources))
	}
	return packets
}

func assertVP9VpxencKeyframeByteParity(t *testing.T, src *image.YCbCr) {
	t.Helper()
	assertVP9VpxencKeyframeByteParityWithOptions(t, src, VP9EncoderOptions{}, nil)
}

func assertVP9VpxencKeyframeByteParityWithOptions(t *testing.T, src *image.YCbCr,
	opts VP9EncoderOptions, extraArgs []string,
) {
	t.Helper()
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	opts.Width = width
	opts.Height = height
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	if !bytes.Equal(govpxPacket, libvpxFrame.Data) {
		govpxHeader, govpxTileStart := parseVP9EncoderHeaderForTest(t, govpxPacket)
		libvpxHeader, libvpxTileStart := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
		govpxGrid := decodeVP9PacketMiGridForOracleTest(t, govpxPacket)
		libvpxGrid := decodeVP9PacketMiGridForOracleTest(t, libvpxFrame.Data)
		govpxTx := decodeVP9PacketTxCoeffsForOracleTest(t, govpxPacket)
		libvpxTx := decodeVP9PacketTxCoeffsForOracleTest(t, libvpxFrame.Data)
		t.Fatalf("govpx header = %+v tileStart=%d tile=% x mi=%+v tx=%+v\nvpxenc header = %+v tileStart=%d tile=% x mi=%+v tx=%+v\ngovpx packet = % x\nvpxenc packet = % x",
			govpxHeader, govpxTileStart, govpxPacket[govpxTileStart:],
			govpxGrid, govpxTx,
			libvpxHeader, libvpxTileStart, libvpxFrame.Data[libvpxTileStart:],
			libvpxGrid, libvpxTx,
			govpxPacket, libvpxFrame.Data)
	}
}

func assertVP9VpxencTwoFrameByteParity(t *testing.T, first, second *image.YCbCr) {
	t.Helper()
	assertVP9VpxencTwoFrameByteParityWithOptions(t, first, second, VP9EncoderOptions{}, nil)
}

func assertVP9VpxencTwoFrameByteParityWithOptions(t *testing.T, first, second *image.YCbCr,
	opts VP9EncoderOptions, extraArgs []string,
) {
	t.Helper()
	width := first.Rect.Dx()
	height := first.Rect.Dy()
	if second.Rect.Dx() != width || second.Rect.Dy() != height {
		t.Fatalf("dimension mismatch: first=%dx%d second=%dx%d",
			width, height, second.Rect.Dx(), second.Rect.Dy())
	}
	opts.Width = width
	opts.Height = height
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxKey, err := e.Encode(first)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	govpxInter, err := e.Encode(second)
	if err != nil {
		t.Fatalf("Encode govpx inter frame: %v", err)
	}

	raw := appendVP9YCbCrI420(nil, first)
	raw = appendVP9YCbCrI420(raw, second)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 2, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxKey, next, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame[0]: %v", err)
	}
	libvpxInter, _, err := testutil.NextIVFFrame(ivf, next, 1)
	if err != nil {
		t.Fatalf("NextIVFFrame[1]: %v", err)
	}

	assertVP9PacketByteParity(t, "keyframe", govpxKey, libvpxKey.Data)
	assertVP9InterPacketByteParity(t, govpxKey, govpxInter, libvpxKey.Data,
		libvpxInter.Data)
}

func assertVP9VpxencFrameSequenceByteParityWithOptions(t *testing.T,
	frames []*image.YCbCr, opts VP9EncoderOptions, extraArgs []string,
) {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("empty VP9 oracle frame sequence")
	}
	width := frames[0].Rect.Dx()
	height := frames[0].Rect.Dy()
	for i, frame := range frames {
		if frame.Rect.Dx() != width || frame.Rect.Dy() != height {
			t.Fatalf("frame %d dimension mismatch: got %dx%d want %dx%d",
				i, frame.Rect.Dx(), frame.Rect.Dy(), width, height)
		}
	}

	opts.Width = width
	opts.Height = height
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		packet, err := e.Encode(frame)
		if err != nil {
			t.Fatalf("Encode govpx frame %d: %v", i, err)
		}
		govpxPackets[i] = packet
	}

	var raw []byte
	for _, frame := range frames {
		raw = appendVP9YCbCrI420(raw, frame)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, len(frames), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i, got := range govpxPackets {
		var libvpxFrame testutil.IVFFrame
		libvpxFrame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		assertVP9PacketByteParity(t, fmt.Sprintf("frame %d", i), got, libvpxFrame.Data)
	}
}

func decodeVP9MiGridForOracleTest(t *testing.T, packet []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode VP9 packet: %v", err)
	}
	grid := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(grid, d.miGrid)
	return grid
}

func requireVP9VpxencFrameFlagsOracle(t *testing.T) {
	t.Helper()
	if _, err := coracle.VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, coracle.ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}
}

func assertVP9VpxencFrameFlagsByteParityWithOptions(t *testing.T,
	frames []*image.YCbCr, flags []EncodeFlags, opts VP9EncoderOptions,
	extraArgs []string,
) {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("empty VP9 frame-flags oracle sequence")
	}
	if len(flags) > len(frames) {
		t.Fatalf("frame flag count = %d, want <= %d", len(flags), len(frames))
	}
	width := frames[0].Rect.Dx()
	height := frames[0].Rect.Dy()
	for i, frame := range frames {
		if frame.Rect.Dx() != width || frame.Rect.Dy() != height {
			t.Fatalf("frame %d dimension mismatch: got %dx%d want %dx%d",
				i, frame.Rect.Dx(), frame.Rect.Dy(), width, height)
		}
	}

	opts.Width = width
	opts.Height = height
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		var encodeFlags EncodeFlags
		if i < len(flags) {
			encodeFlags = flags[i]
		}
		if encodeFlags&EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no libvpx frame-flag bit", i)
		}
		result, err := e.EncodeIntoWithFlagsResult(frame, dst, encodeFlags)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxFlags := make([]uint32, len(flags))
	for i, encodeFlags := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(encodeFlags)
	}
	var raw []byte
	for _, frame := range frames {
		raw = appendVP9YCbCrI420(raw, frame)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width, height,
		len(frames), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != len(frames) {
		t.Fatalf("IVF frame count = %d, want %d", count, len(frames))
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i, got := range govpxPackets {
		var libvpxFrame testutil.IVFFrame
		libvpxFrame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		assertVP9PacketByteParity(t, fmt.Sprintf("frame %d", i), got, libvpxFrame.Data)
	}
}

func vp9FrameFlagsForLibvpx(f EncodeFlags) uint32 {
	const (
		libvpxForceKF      = 1 << 0
		libvpxNoRefLast    = 1 << 16
		libvpxNoRefGF      = 1 << 17
		libvpxNoUpdLast    = 1 << 18
		libvpxForceGF      = 1 << 19
		libvpxNoUpdEntropy = 1 << 20
		libvpxNoRefARF     = 1 << 21
		libvpxNoUpdGF      = 1 << 22
		libvpxNoUpdARF     = 1 << 23
		libvpxForceARF     = 1 << 24
	)
	var out uint32
	if f&EncodeForceKeyFrame != 0 {
		out |= libvpxForceKF
	}
	if f&EncodeNoReferenceLast != 0 {
		out |= libvpxNoRefLast
	}
	if f&EncodeNoReferenceGolden != 0 {
		out |= libvpxNoRefGF
	}
	if f&EncodeNoUpdateLast != 0 {
		out |= libvpxNoUpdLast
	}
	if f&EncodeForceGoldenFrame != 0 {
		out |= libvpxForceGF
	}
	if f&EncodeNoUpdateEntropy != 0 {
		out |= libvpxNoUpdEntropy
	}
	if f&EncodeNoReferenceAltRef != 0 {
		out |= libvpxNoRefARF
	}
	if f&EncodeNoUpdateGolden != 0 {
		out |= libvpxNoUpdGF
	}
	if f&EncodeNoUpdateAltRef != 0 {
		out |= libvpxNoUpdARF
	}
	if f&EncodeForceAltRefFrame != 0 {
		out |= libvpxForceARF
	}
	return out
}

func assertVP9InterPacketByteParity(t *testing.T, govpxKey, govpxInter, libvpxKey, libvpxInter []byte) {
	t.Helper()
	if bytes.Equal(govpxInter, libvpxInter) {
		return
	}
	gotHeader, gotTileStart := parseVP9EncoderHeaderForTest(t, govpxInter)
	wantHeader, wantTileStart := parseVP9EncoderHeaderForTest(t, libvpxInter)
	govpxGrid := decodeVP9TwoFrameInterMiGridForOracleTest(t, govpxKey, govpxInter)
	libvpxGrid := decodeVP9TwoFrameInterMiGridForOracleTest(t, libvpxKey, libvpxInter)
	govpxFirst, govpxLast := firstLastVP9MiForOracleTest(govpxGrid)
	libvpxFirst, libvpxLast := firstLastVP9MiForOracleTest(libvpxGrid)
	t.Fatalf("inter packet diverged firstDiff=%d\ngovpx header=%+v tileStart=%d tile=% x mi0=%+v miLast=%+v\nvpxenc header=%+v tileStart=%d tile=% x mi0=%+v miLast=%+v\ngovpx packet=% x\nvpxenc packet=% x",
		firstVP9PacketDiffForTest(govpxInter, libvpxInter),
		gotHeader, gotTileStart, govpxInter[gotTileStart:], govpxFirst, govpxLast,
		wantHeader, wantTileStart, libvpxInter[wantTileStart:], libvpxFirst,
		libvpxLast,
		govpxInter, libvpxInter)
}

func assertVP9PacketByteParity(t *testing.T, label string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	gotHeader, gotTileStart := parseVP9EncoderHeaderForTest(t, got)
	wantHeader, wantTileStart := parseVP9EncoderHeaderForTest(t, want)
	t.Fatalf("%s packet diverged firstDiff=%d\ngovpx header=%+v tileStart=%d tile=% x\nvpxenc header=%+v tileStart=%d tile=% x\ngovpx packet=% x\nvpxenc packet=% x",
		label, firstVP9PacketDiffForTest(got, want),
		gotHeader, gotTileStart, got[gotTileStart:],
		wantHeader, wantTileStart, want[wantTileStart:],
		got, want)
}

func firstVP9PacketDiffForTest(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

type vp9OracleTxCoeffs struct {
	Plane   int
	Mode    common.PredictionMode
	TxSize  common.TxSize
	InitCtx int
	EOB     int
	Coeffs  []int16
}

func decodeVP9PacketTxCoeffsForOracleTest(t *testing.T, packet []byte) []vp9OracleTxCoeffs {
	t.Helper()
	hdr, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	uncSize := tileStart - int(hdr.FirstPartitionSize)

	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:tileStart]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     hdr.Quant.Lossless,
		IntraOnly:    hdr.FrameType == common.KeyFrame || hdr.IntraOnly,
		KeyFrame:     hdr.FrameType == common.KeyFrame,
		InterpFilter: hdr.InterpFilter,
	})

	var r bitstream.Reader
	if err := r.Init(packet[tileStart:]); err != nil {
		t.Fatalf("tile reader Init: %v", err)
	}

	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	d.fc = fc
	vp9dec.SetupBlockPlanes(&d.planes, hdr.BitDepthColor.SubsamplingX,
		hdr.BitDepthColor.SubsamplingY)
	d.ensureVP9DecoderModeBuffers(miRows, miCols)
	d.resetVP9AboveEntropyContexts()
	d.resetVP9LeftEntropyContexts()
	vp9dec.SetupSegmentationDequant(&hdr.Seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(hdr.Quant.BaseQindex),
		YDcDeltaQ:  int(hdr.Quant.YDcDeltaQ),
		UvDcDeltaQ: int(hdr.Quant.UvDcDeltaQ),
		UvAcDeltaQ: int(hdr.Quant.UvAcDeltaQ),
		BitDepth:   vp9dec.BitDepth(hdr.BitDepthColor.BitDepth),
	}, &d.dq)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: d.segMap,
		LastFrameSegMap:    d.lastSegMap,
		MiCols:             miCols,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	out := make([]vp9OracleTxCoeffs, 0, 3)

	var collectBlock func(miRow, miCol int, bsize common.BlockSize)
	collectBlock = func(miRow, miCol int, bsize common.BlockSize) {
		xMis := min(int(common.Num8x8BlocksWideLookup[bsize]), miCols-miCol)
		yMis := min(int(common.Num8x8BlocksHighLookup[bsize]), miRows-miRow)
		mi := &d.miGrid[miRow*miCols+miCol]
		*mi = vp9dec.NeighborMi{SbType: bsize}
		above := d.vp9DecoderMiAt(miRows, miCols, miRow-1, miCol)
		var left *vp9dec.NeighborMi
		if miCol > tile.MiColStart {
			left = d.vp9DecoderMiAt(miRows, miCols, miRow, miCol-1)
		}
		modeOut := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
			Reader:   &r,
			Fc:       &d.fc,
			Seg:      &hdr.Seg,
			Maps:     &maps,
			TxMode:   comp.TxMode,
			MiOffset: miRow*miCols + miCol,
			XMis:     xMis,
			YMis:     yMis,
			Above:    above,
			Left:     left,
		}, mi)
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		if mi.Skip != 0 {
			aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
			vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
			d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
			return
		}
		out = append(out, collectVP9PacketResidueCoeffsForOracleTest(t, d, &r,
			&hdr, mi, modeOut.UvMode, miRow, miCol, reconBsize)...)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	}
	var walk func(miRow, miCol int, bsize common.BlockSize)
	walk = func(miRow, miCol int, bsize common.BlockSize) {
		if miRow >= miRows || miCol >= miCols {
			return
		}
		bsl := int(common.BWidthLog2Lookup[bsize])
		bs := (1 << uint(bsl)) / 4
		ctx := vp9dec.PartitionPlaneContext(d.aboveSegCtx, d.leftSegCtx, miRow, miCol, bsize)
		probs := tables.KfPartitionProbs[ctx][:]
		hasRows := miRow+bs < miRows
		hasCols := miCol+bs < miCols
		partition := vp9dec.ReadPartition(&r, probs, hasRows, hasCols)
		subsize := common.SubsizeLookup[partition][bsize]
		switch partition {
		case common.PartitionNone:
			collectBlock(miRow, miCol, subsize)
		case common.PartitionHorz:
			collectBlock(miRow, miCol, subsize)
			if miRow+bs < miRows {
				collectBlock(miRow+bs, miCol, subsize)
			}
		case common.PartitionVert:
			collectBlock(miRow, miCol, subsize)
			if miCol+bs < miCols {
				collectBlock(miRow, miCol+bs, subsize)
			}
		case common.PartitionSplit:
			walk(miRow, miCol, subsize)
			walk(miRow, miCol+bs, subsize)
			walk(miRow+bs, miCol, subsize)
			walk(miRow+bs, miCol+bs, subsize)
		default:
			t.Fatalf("invalid partition %d", partition)
		}
		if bsize >= common.Block8x8 &&
			(bsize == common.Block8x8 || partition != common.PartitionSplit) {
			vp9dec.UpdatePartitionContext(d.aboveSegCtx, d.leftSegCtx,
				miRow, miCol, subsize, vp9PartitionContextUpdateWidth(bs))
		}
	}
	walk(0, 0, common.Block64x64)
	return out
}

func collectVP9PacketResidueCoeffsForOracleTest(t *testing.T, d *VP9Decoder,
	r *bitstream.Reader, hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, miRow, miCol int, bsize common.BlockSize,
) []vp9OracleTxCoeffs {
	t.Helper()
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	out := make([]vp9OracleTxCoeffs, 0, 3)
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeType := 0
		dequant := d.dq.Y[mi.SegmentID]
		if plane > 0 {
			planeType = 1
			dequant = d.dq.Uv[mi.SegmentID]
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4W, num4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
			bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - num4x4W) >> txSize) * blockStep
		aboveBase := aboveOffsets[plane]
		leftBase := leftOffsets[plane]
		blockIdx := 0
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				aboveCtx := pd.AboveContext[aboveBase+cc : aboveBase+cc+step]
				leftCtx := pd.LeftContext[leftBase+rr : leftBase+rr+step]
				initCtx := vp9dec.GetEntropyContext(txSize, aboveCtx, leftCtx)
				scanOrder := common.GetScan(txSize, planeType, 0,
					hdr.Quant.Lossless, mode)
				maxEob := vp9dec.MaxEobForTxSize(txSize)
				coeffs := make([]int16, maxEob)
				eob := vp9dec.DecodeCoefs(r, txSize, planeType, 0, dequant,
					initCtx, scanOrder.Scan, scanOrder.Neighbors, &d.fc.CoefProbs,
					coeffs)
				out = append(out, vp9OracleTxCoeffs{
					Plane:   plane,
					Mode:    mode,
					TxSize:  txSize,
					InitCtx: initCtx,
					EOB:     eob,
					Coeffs:  coeffs,
				})
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for i := range step {
					aboveCtx[i] = hasResidue
					leftCtx[i] = hasResidue
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return out
}

func decodeVP9PacketMiGridForOracleTest(t *testing.T, packet []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode packet: %v", err)
	}
	out := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(out, d.miGrid)
	return out
}

func decodeVP9TwoFrameInterMiGridForOracleTest(t *testing.T, key, inter []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode key packet: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after key packet")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter packet: %v", err)
	}
	out := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(out, d.miGrid)
	return out
}

func decodeVP9SequenceMiGridsForOracleTest(t *testing.T, packets [][]byte) [][]vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	out := make([][]vp9dec.NeighborMi, len(packets))
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		out[i] = make([]vp9dec.NeighborMi, len(d.miGrid))
		copy(out[i], d.miGrid)
		if i+1 < len(packets) {
			if _, ok := d.NextFrame(); !ok {
				t.Fatalf("NextFrame returned !ok after packet %d", i)
			}
		}
	}
	return out
}

type vp9ModeDistributionForOracleTest struct {
	Total  int
	Skip   int
	Modes  [common.MbModeCount]int
	Blocks [common.BlockSizes]int
}

func collectVP9ModeDistribution(grid []vp9dec.NeighborMi) vp9ModeDistributionForOracleTest {
	var dist vp9ModeDistributionForOracleTest
	for i := range grid {
		mi := &grid[i]
		dist.Total++
		if mi.Skip != 0 {
			dist.Skip++
		}
		if mode := int(mi.Mode); mode >= 0 && mode < len(dist.Modes) {
			dist.Modes[mode]++
		}
		if block := int(mi.SbType); block >= 0 && block < len(dist.Blocks) {
			dist.Blocks[block]++
		}
	}
	return dist
}

func (dist vp9ModeDistributionForOracleTest) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "total=%d skip=%d modes=[", dist.Total, dist.Skip)
	writeVP9IntHistogramForOracleTest(&b, dist.Modes[:])
	b.WriteString("] blocks=[")
	writeVP9IntHistogramForOracleTest(&b, dist.Blocks[:])
	b.WriteByte(']')
	return b.String()
}

func writeVP9IntHistogramForOracleTest(b *bytes.Buffer, hist []int) {
	first := true
	for i, count := range hist {
		if count == 0 {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		fmt.Fprintf(b, "%d:%d", i, count)
		first = false
	}
	if first {
		b.WriteString("empty")
	}
}

func vp9ModeDistributionDistance(a, b [common.MbModeCount]int) int {
	distance := 0
	for i := range a {
		distance += vp9AbsIntForOracleTest(a[i] - b[i])
	}
	return distance
}

func vp9BlockDistributionDistance(a, b [common.BlockSizes]int) int {
	distance := 0
	for i := range a {
		distance += vp9AbsIntForOracleTest(a[i] - b[i])
	}
	return distance
}

func vp9AbsIntForOracleTest(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func newVP9PanningYCbCrForRateTest(width, height int, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = byte(24 + ((x+frame*3)*7+y*11+(x*y+frame*13)%37)%208)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(64 + ((x+frame)*5+y*3)%128)
			cr[x] = byte(72 + (x*3+(y+frame)*7)%112)
		}
	}
	return img
}

func firstLastVP9MiForOracleTest(grid []vp9dec.NeighborMi) (vp9dec.NeighborMi, vp9dec.NeighborMi) {
	if len(grid) == 0 {
		return vp9dec.NeighborMi{}, vp9dec.NeighborMi{}
	}
	return grid[0], grid[len(grid)-1]
}

func readVP9CompressedHeaderForOracleTest(t *testing.T, packet []byte,
	header vp9dec.UncompressedHeader,
) (vp9dec.CompressedHeader, vp9dec.FrameContext, int) {
	t.Helper()
	var br vp9dec.BitReader
	br.Init(packet)
	if _, err := vp9dec.ReadUncompressedHeader(&br, nil, nil); err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	uncSize := br.BytesRead()
	compEnd := uncSize + int(header.FirstPartitionSize)
	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	compoundAllowed := header.FrameType != common.KeyFrame && !header.IntraOnly &&
		vp9dec.CompoundReferenceAllowed(vp9FrameRefSignBias(&header))
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             header.Quant.Lossless,
		IntraOnly:            header.FrameType == common.KeyFrame || header.IntraOnly,
		KeyFrame:             header.FrameType == common.KeyFrame,
		InterpFilter:         header.InterpFilter,
		AllowHighPrecisionMv: header.AllowHighPrecisionMv,
		CompoundRefAllowed:   compoundAllowed,
	})
	return comp, fc, uncSize
}
