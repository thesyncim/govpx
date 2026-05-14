package govpx

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     header.Quant.Lossless,
		IntraOnly:    header.FrameType == common.KeyFrame || header.IntraOnly,
		KeyFrame:     header.FrameType == common.KeyFrame,
		InterpFilter: header.InterpFilter,
	})
	return comp, fc, uncSize
}
