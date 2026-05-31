//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"errors"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vpxbuffers "github.com/thesyncim/govpx/internal/vpx/buffers"
	"image"
	"testing"
)

func TestVP9EncoderVpxencOracleLookaheadNoAltRefParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, byte(80+i*24), 128, 128)
	}

	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 2,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	govpxPackets := make([][]byte, 0, frames)
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		govpxPackets = append(govpxPackets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
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

	libvpxPackets := vp9test.VpxencPackets(t, sources,
		"--lag-in-frames=2", "--auto-alt-ref=0")
	matches := 0
	for i, got := range govpxPackets {
		if bytes.Equal(got, libvpxPackets[i]) {
			matches++
			continue
		}
		gotHeader, _ := vp9test.ParseHeader(t, got)
		wantHeader, _ := vp9test.ParseHeader(t, libvpxPackets[i])
		t.Logf("lookahead row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
			i, len(got), gotHeader.Quant.BaseQindex, gotHeader.RefreshFrameFlags,
			gotHeader.FirstPartitionSize, len(libvpxPackets[i]),
			wantHeader.Quant.BaseQindex, wantHeader.RefreshFrameFlags,
			wantHeader.FirstPartitionSize)
	}
	t.Logf("VP9 lookahead no-alt-ref oracle: byte_matches=%d/%d", matches, frames)
	if matches != frames {
		t.Fatalf("VP9 lookahead byte parity matched %d/%d packets", matches, frames)
	}
}

func TestVP9EncoderVpxencOracleLookaheadNoAltRefMatrixParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

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
				sources[i] = vp9test.NewYCbCr(width, height,
					byte(72+i*19), 128, 128)
			}

			govpxPackets := captureVP9LookaheadPacketsForOracleTest(t,
				govpx.VP9EncoderOptions{LookaheadFrames: tc.lag}, sources)
			libvpxPackets := vp9test.VpxencPackets(t, sources,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag),
				"--auto-alt-ref=0")

			matches := 0
			firstMismatch := -1
			for i, got := range govpxPackets {
				if bytes.Equal(got, libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
				gotHeader, _ := vp9test.ParseHeader(t, got)
				wantHeader, _ := vp9test.ParseHeader(t, libvpxPackets[i])
				t.Logf("lookahead %s row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
					tc.name, i, len(got), gotHeader.Quant.BaseQindex,
					gotHeader.RefreshFrameFlags, gotHeader.FirstPartitionSize,
					len(libvpxPackets[i]), wantHeader.Quant.BaseQindex,
					wantHeader.RefreshFrameFlags,
					wantHeader.FirstPartitionSize)
			}
			t.Logf("VP9 lookahead no-alt-ref matrix %s: byte_matches=%d/%d first_mismatch=%d",
				tc.name, matches, tc.frames, firstMismatch)
			if vp9test.StrictEnv("GOVPX_VP9_LOOKAHEAD_MATRIX_STRICT") &&
				matches != tc.frames {
				t.Fatalf("strict VP9 lookahead no-alt-ref matrix %s matched %d/%d packets",
					tc.name, matches, tc.frames)
			}
		})
	}
}

func captureVP9LookaheadPacketsForOracleTest(t *testing.T, opts govpx.VP9EncoderOptions,
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
	e, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dstSize, err := vpxbuffers.I420EncodeBufferSize(width, height, 4096, 65536)
	if err != nil {
		t.Fatalf("I420EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := e.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
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
