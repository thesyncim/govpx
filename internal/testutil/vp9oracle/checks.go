//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func AssertKeyframeHeaderParity(t testing.TB, got, want vp9dec.UncompressedHeader) {
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

func AssertFrameFlagsByteParityWithOptions(t testing.TB,
	frames []*image.YCbCr, flags []govpx.EncodeFlags, opts govpx.VP9EncoderOptions,
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
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		var encodeFlags govpx.EncodeFlags
		if i < len(flags) {
			encodeFlags = flags[i]
		}
		if encodeFlags&govpx.EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no libvpx frame-flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(frame, dst, encodeFlags)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, frames,
		LibvpxFrameFlags(flags), extraArgs...)
	for i, got := range govpxPackets {
		vp9test.AssertPacketByteParity(t, fmt.Sprintf("frame %d", i), got,
			libvpxPackets[i])
	}
}

func AssertKeyframeByteParity(t testing.TB, src *image.YCbCr) {
	t.Helper()
	AssertKeyframeByteParityWithOptions(t, src, govpx.VP9EncoderOptions{}, nil)
}

func AssertKeyframeByteParityWithOptions(t testing.TB, src *image.YCbCr,
	opts govpx.VP9EncoderOptions, extraArgs []string,
) {
	t.Helper()
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	govpxPacket, err := enc.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src}, extraArgs...)[0]
	vp9test.AssertPacketByteParity(t, "keyframe", govpxPacket, libvpxPacket)
}

func AssertTwoFrameByteParity(t testing.TB, first, second *image.YCbCr) {
	t.Helper()
	AssertTwoFrameByteParityWithOptions(t, first, second, govpx.VP9EncoderOptions{}, nil)
}

func AssertTwoFrameByteParityWithOptions(t testing.TB, first, second *image.YCbCr,
	opts govpx.VP9EncoderOptions, extraArgs []string,
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
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	govpxKey, err := enc.Encode(first)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	govpxInter, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode govpx inter frame: %v", err)
	}

	libvpxPackets := vp9test.VpxencPackets(t, []*image.YCbCr{first, second},
		extraArgs...)
	vp9test.AssertPacketByteParity(t, "keyframe", govpxKey, libvpxPackets[0])
	vp9test.AssertPacketByteParity(t, "inter frame", govpxInter, libvpxPackets[1])
}

func AssertFrameSequenceByteParityWithOptions(t testing.TB,
	frames []*image.YCbCr, opts govpx.VP9EncoderOptions, extraArgs []string,
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
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		packet, err := enc.Encode(frame)
		if err != nil {
			t.Fatalf("Encode govpx frame %d: %v", i, err)
		}
		govpxPackets[i] = packet
	}

	libvpxPackets := vp9test.VpxencPackets(t, frames, extraArgs...)
	for i, got := range govpxPackets {
		vp9test.AssertPacketByteParity(t, fmt.Sprintf("frame %d", i), got,
			libvpxPackets[i])
	}
}

func AssertEncoderVpxdecI420Match(t testing.TB, width, height int, packets ...[]byte) {
	t.Helper()
	want := vp9test.VpxdecI420(t, vp9test.BuildVP9IVF(width, height, packets...))
	got := DecodeVisibleI420(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func AssertImageMatchesYCbCr(t testing.TB, name string, got govpx.Image, want *image.YCbCr) {
	t.Helper()
	width := want.Rect.Dx()
	height := want.Rect.Dy()
	if got.Width != width || got.Height != height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d",
			name, got.Width, got.Height, width, height)
	}
	assertPlaneMatches(t, name, "Y", got.Y, got.YStride, want.Y, want.YStride,
		width, height)
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	assertPlaneMatches(t, name, "U", got.U, got.UStride, want.Cb, want.CStride,
		uvWidth, uvHeight)
	assertPlaneMatches(t, name, "V", got.V, got.VStride, want.Cr, want.CStride,
		uvWidth, uvHeight)
}

func assertPlaneMatches(t testing.TB, name, plane string,
	got []byte, gotStride int, want []byte, wantStride int, width, height int,
) {
	t.Helper()
	for y := range height {
		gotRow := got[y*gotStride:]
		wantRow := want[y*wantStride:]
		for x := range width {
			if gotRow[x] != wantRow[x] {
				t.Fatalf("%s %s[%d,%d] = %d, want %d",
					name, plane, y, x, gotRow[x], wantRow[x])
			}
		}
	}
}
