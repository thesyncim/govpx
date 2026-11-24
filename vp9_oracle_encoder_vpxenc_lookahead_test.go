//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleLookaheadNoAltRefParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, byte(80+i*24), 128, 128)
	}

	govpxPackets := vp9oracle.CaptureLookaheadPackets(t,
		govpx.VP9EncoderOptions{LookaheadFrames: 2}, sources)

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

			govpxPackets := vp9oracle.CaptureLookaheadPackets(t,
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
