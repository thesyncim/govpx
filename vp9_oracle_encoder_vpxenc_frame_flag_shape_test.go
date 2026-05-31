//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9EncoderVpxencFrameFlagsRepeatNoReferenceAllModeTxShape(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 3
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, uint8(96+i*8), 128, 128)
	}
	flags := make([]EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		flags[i] = EncodeNoReferenceLast | EncodeNoReferenceGolden |
			EncodeNoReferenceAltRef
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
		result, err := e.EncodeIntoWithFlagsResult(src, dst, flags[i])
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
		vp9LibvpxFrameFlags(flags))

	govpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, govpxPackets)
	libvpxGrids := decodeVP9SequenceMiGridsForOracleTest(t, libvpxPackets)
	totalShape, matchedShape := 0, 0
	totalMode, matchedMode := 0, 0
	totalTx, matchedTx := 0, 0
	totalSkip, matchedSkip := 0, 0
	totalRef, matchedRef := 0, 0
	for frameIdx := 1; frameIdx < frames; frameIdx++ {
		gGrid := govpxGrids[frameIdx]
		lGrid := libvpxGrids[frameIdx]
		if len(gGrid) != len(lGrid) {
			t.Fatalf("frame %d MI grid length mismatch: govpx=%d libvpx=%d",
				frameIdx, len(gGrid), len(lGrid))
		}
		firstMismatch := -1
		for miIdx := range gGrid {
			g := gGrid[miIdx]
			l := lGrid[miIdx]
			shapeMatch := g.SbType == l.SbType && g.Mode == l.Mode &&
				g.TxSize == l.TxSize && g.Skip == l.Skip &&
				g.RefFrame == l.RefFrame
			totalShape++
			if shapeMatch {
				matchedShape++
			} else if firstMismatch < 0 {
				firstMismatch = miIdx
			}
			totalMode++
			if g.Mode == l.Mode {
				matchedMode++
			}
			totalTx++
			if g.TxSize == l.TxSize {
				matchedTx++
			}
			totalSkip++
			if g.Skip == l.Skip {
				matchedSkip++
			}
			totalRef++
			if g.RefFrame == l.RefFrame {
				matchedRef++
			}
		}
		gFirst, gLast := firstLastVP9MiForOracleTest(gGrid)
		lFirst, lLast := firstLastVP9MiForOracleTest(lGrid)
		firstByteDiff := testutil.FirstByteDiff(govpxPackets[frameIdx],
			libvpxPackets[frameIdx])
		t.Logf("VP9 repeat no-reference-all mode/tx frame %d: first_shape_mismatch=%d first_byte_diff=%d govpx_bytes=%d libvpx_bytes=%d govpx_first=%+v govpx_last=%+v libvpx_first=%+v libvpx_last=%+v",
			frameIdx, firstMismatch, firstByteDiff,
			len(govpxPackets[frameIdx]), len(libvpxPackets[frameIdx]),
			gFirst, gLast, lFirst, lLast)
	}
	t.Logf("VP9 repeat no-reference-all mode/tx trace: shape=%d/%d mode=%d/%d tx=%d/%d skip=%d/%d ref=%d/%d",
		matchedShape, totalShape, matchedMode, totalMode, matchedTx, totalTx,
		matchedSkip, totalSkip, matchedRef, totalRef)
	if matchedShape != totalShape {
		t.Fatalf("VP9 no-reference-all mode/tx shape matched %d/%d",
			matchedShape, totalShape)
	}
}
