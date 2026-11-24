//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleInterModeDistributionParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
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

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources, nil)

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
	t.Logf("VP9 inter-mode distribution trace: total_mode_distance=%d total_block_distance=%d total_skip_distance=%d",
		totalModeDistance, totalBlockDistance, totalSkipDistance)
	if vp9test.StrictEnv("GOVPX_VP9_MODE_DIST_STRICT") &&
		(totalModeDistance != 0 || totalBlockDistance != 0 ||
			totalSkipDistance != 0) {
		t.Fatalf("strict VP9 inter-mode distribution mismatch: mode=%d block=%d skip=%d",
			totalModeDistance, totalBlockDistance, totalSkipDistance)
	}
}
