//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func AssertFrameFlagsByteParityWithOptions(t *testing.T,
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

func AssertEncoderVpxdecI420Match(t *testing.T, width, height int, packets ...[]byte) {
	t.Helper()
	want := vp9test.VpxdecI420(t, vp9test.BuildVP9IVF(width, height, packets...))
	got := DecodeVisibleI420(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}
