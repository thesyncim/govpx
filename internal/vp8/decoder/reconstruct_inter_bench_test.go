package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// makeInterMBBenchScene builds a 1280x720 reference and destination scene for
// benchmarking ReconstructWholeMVInterMacroblock dispatch overhead.
func makeInterMBBenchScene(width, height int) (*common.FrameBuffer, *common.FrameBuffer) {
	srcRef, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		panic(err)
	}
	src := testImage(width, height)
	fillImage(&srcRef.Img, src)
	srcRef.ExtendBorders()
	dstRef, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		panic(err)
	}
	return srcRef, dstRef
}

// BenchmarkInterMBBuilderSixTap16x16 stresses the per-MB inter predictor
// builder on a 1280x720 grid where every MB is a sub-pel NewMV using sixtap.
func BenchmarkInterMBBuilderSixTap16x16(b *testing.B) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img

	mode := MacroblockMode{
		Mode:        common.NewMV,
		RefFrame:    common.LastFrame,
		MV:          MotionVector{Row: 3, Col: 5},
		MBSkipCoeff: true,
	}
	var tokens MacroblockTokens
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for row := range rows {
			yRow := row * 16 * dst.YStride
			uRow := row * 8 * dst.UStride
			vRow := row * 8 * dst.VStride
			for col := range cols {
				yOff := yRow + col*16
				uOff := uRow + col*8
				vOff := vRow + col*8
				if !ReconstructWholeMVInterMacroblock(&mode, &tokens, &dequants[0], src,
					dst.Y[yOff:], dst.YStride,
					dst.U[uOff:], dst.UStride,
					dst.V[vOff:], dst.VStride,
					&scratch.Residual, row, col, InterPredictionConfig{}) {
					b.Fatalf("ReconstructWholeMVInterMacroblock failed at (%d,%d)", row, col)
				}
			}
		}
	}
	b.ReportMetric(float64(rows*cols), "mb/op")
}

// BenchmarkInterMBBuilderBilinear16x16 same as above but with the bilinear
// filter selected.
func BenchmarkInterMBBuilderBilinear16x16(b *testing.B) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img

	mode := MacroblockMode{
		Mode:        common.NewMV,
		RefFrame:    common.LastFrame,
		MV:          MotionVector{Row: 3, Col: 5},
		MBSkipCoeff: true,
	}
	var tokens MacroblockTokens
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch
	cfg := InterPredictionConfig{UseBilinear: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for row := range rows {
			yRow := row * 16 * dst.YStride
			uRow := row * 8 * dst.UStride
			vRow := row * 8 * dst.VStride
			for col := range cols {
				yOff := yRow + col*16
				uOff := uRow + col*8
				vOff := vRow + col*8
				if !ReconstructWholeMVInterMacroblock(&mode, &tokens, &dequants[0], src,
					dst.Y[yOff:], dst.YStride,
					dst.U[uOff:], dst.UStride,
					dst.V[vOff:], dst.VStride,
					&scratch.Residual, row, col, cfg) {
					b.Fatalf("ReconstructWholeMVInterMacroblock failed at (%d,%d)", row, col)
				}
			}
		}
	}
	b.ReportMetric(float64(rows*cols), "mb/op")
}

// BenchmarkInterMBBuilderZeroMV measures dispatch with full-MB copies (no
// subpel filtering) — measures pure builder overhead.
func BenchmarkInterMBBuilderZeroMV(b *testing.B) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img

	mode := MacroblockMode{
		Mode:        common.ZeroMV,
		RefFrame:    common.LastFrame,
		MBSkipCoeff: true,
	}
	var tokens MacroblockTokens
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for row := range rows {
			yRow := row * 16 * dst.YStride
			uRow := row * 8 * dst.UStride
			vRow := row * 8 * dst.VStride
			for col := range cols {
				yOff := yRow + col*16
				uOff := uRow + col*8
				vOff := vRow + col*8
				if !ReconstructWholeMVInterMacroblock(&mode, &tokens, &dequants[0], src,
					dst.Y[yOff:], dst.YStride,
					dst.U[uOff:], dst.UStride,
					dst.V[vOff:], dst.VStride,
					&scratch.Residual, row, col, InterPredictionConfig{}) {
					b.Fatalf("ReconstructWholeMVInterMacroblock failed at (%d,%d)", row, col)
				}
			}
		}
	}
	b.ReportMetric(float64(rows*cols), "mb/op")
}

// BenchmarkInterMBBuilderFrame720p drives the full grid path
// (ReconstructInterFrameGridWithConfig) for a 1280x720 frame of mixed
// MBs typical of an inter-frame: ZeroMV majority + a band of NewMV subpel.
func BenchmarkInterMBBuilderFrame720p(b *testing.B) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img

	modes := make([]MacroblockMode, rows*cols)
	tokens := make([]MacroblockTokens, rows*cols)
	for r := range rows {
		for c := range cols {
			i := r*cols + c
			if (r+c)%4 == 0 {
				modes[i] = MacroblockMode{
					Mode:        common.NewMV,
					RefFrame:    common.LastFrame,
					MV:          MotionVector{Row: 3, Col: 5},
					MBSkipCoeff: true,
				}
			} else {
				modes[i] = MacroblockMode{
					Mode:        common.ZeroMV,
					RefFrame:    common.LastFrame,
					MBSkipCoeff: true,
				}
			}
		}
	}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ReconstructInterFrameGridWithConfig(dst, src, src, src, rows, cols, modes, tokens, &dequants, &scratch, InterPredictionConfig{}); err != nil {
			b.Fatalf("ReconstructInterFrameGridWithConfig: %v", err)
		}
	}
}
