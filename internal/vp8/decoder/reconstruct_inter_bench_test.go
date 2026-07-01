package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
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

func BenchmarkInterMBBuilderSplitMV(b *testing.B) {
	cases := []struct {
		name string
		mode MacroblockMode
		cfg  InterPredictionConfig
	}{
		{
			name: "partition0_sixTap8x8",
			mode: splitMVBenchMode(0, []MotionVector{
				{Row: 3, Col: 5},
				{Row: 7, Col: 1},
			}),
		},
		{
			name: "partition2_sixTap8x8",
			mode: splitMVBenchMode(2, []MotionVector{
				{Row: 3, Col: 5},
				{Row: 7, Col: 1},
				{Row: 5, Col: 3},
				{Row: 1, Col: 7},
			}),
		},
		{
			name: "partition3_equalPairs8x4",
			mode: splitMVBenchModePartition3(true),
		},
		{
			name: "partition3_mixedPairs4x4",
			mode: splitMVBenchModePartition3(false),
		},
		{
			name: "partition3_equalPairs8x4_bilinear",
			mode: splitMVBenchModePartition3(true),
			cfg:  InterPredictionConfig{UseBilinear: true},
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkInterMBBuilderSplitMV(b, tc.mode, tc.cfg)
		})
	}
}

func benchmarkInterMBBuilderSplitMV(b *testing.B, mode MacroblockMode, cfg InterPredictionConfig) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img
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
				if !ReconstructSplitMVInterMacroblock(&mode, &tokens, &dequants[0], src,
					dst.Y[yOff:], dst.YStride,
					dst.U[uOff:], dst.UStride,
					dst.V[vOff:], dst.VStride,
					&scratch.Residual, row, col, cfg) {
					b.Fatalf("ReconstructSplitMVInterMacroblock failed at (%d,%d)", row, col)
				}
			}
		}
	}
	b.ReportMetric(float64(rows*cols), "mb/op")
}

func BenchmarkInterMBBuilderSplitMVGrid(b *testing.B) {
	cases := []struct {
		name string
		mode MacroblockMode
		cfg  InterPredictionConfig
	}{
		{
			name: "partition0_sixTap8x8",
			mode: splitMVBenchMode(0, []MotionVector{
				{Row: 3, Col: 5},
				{Row: 7, Col: 1},
			}),
		},
		{
			name: "partition3_equalPairs8x4",
			mode: splitMVBenchModePartition3(true),
		},
		{
			name: "partition3_mixedPairs4x4",
			mode: splitMVBenchModePartition3(false),
		},
		{
			name: "partition3_equalPairs8x4_bilinear",
			mode: splitMVBenchModePartition3(true),
			cfg:  InterPredictionConfig{UseBilinear: true},
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkInterMBBuilderSplitMVGrid(b, tc.mode, tc.cfg)
		})
	}
}

func benchmarkInterMBBuilderSplitMVGrid(b *testing.B, mode MacroblockMode, cfg InterPredictionConfig) {
	const width, height = 1280, 720
	const cols = width / 16
	const rows = height / 16
	srcFB, dstFB := makeInterMBBenchScene(width, height)
	dst := &dstFB.Img
	src := &srcFB.Img

	modes := make([]MacroblockMode, rows*cols)
	tokens := make([]MacroblockTokens, rows*cols)
	for i := range modes {
		modes[i] = mode
	}
	dequants := testMacroblockDequants()
	var scratch IntraReconstructionScratch

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ReconstructInterFrameGridWithConfig(dst, src, src, src, rows, cols, modes, tokens, &dequants, &scratch, cfg); err != nil {
			b.Fatalf("ReconstructInterFrameGridWithConfig: %v", err)
		}
	}
	b.ReportMetric(float64(rows*cols), "mb/op")
}

func splitMVBenchMode(partition uint8, subsetMVs []MotionVector) MacroblockMode {
	mode := MacroblockMode{
		Mode:        common.SplitMV,
		RefFrame:    common.LastFrame,
		Is4x4:       true,
		MBSkipCoeff: true,
		Partition:   partition,
	}
	if partition >= tables.NumMBSplits || len(subsetMVs) == 0 {
		return mode
	}
	partitions := int(tables.MBSplitCount[partition])
	fillCount := int(tables.MBSplitFillCount[partition])
	for subset := range partitions {
		mv := subsetMVs[subset%len(subsetMVs)]
		fillStart := subset * fillCount
		for i := range fillCount {
			mode.BlockMV[tables.MBSplitFillOffset[partition][fillStart+i]] = mv
		}
	}
	mode.MV = mode.BlockMV[15]
	return mode
}

func splitMVBenchModePartition3(equalPairs bool) MacroblockMode {
	mode := MacroblockMode{
		Mode:        common.SplitMV,
		RefFrame:    common.LastFrame,
		Is4x4:       true,
		MBSkipCoeff: true,
		Partition:   3,
	}
	for block := 0; block < 16; block += 2 {
		mv := MotionVector{
			Row: int16(1 + (block&6)*2),
			Col: int16(3 + (block & 4)),
		}
		mode.BlockMV[block] = mv
		if equalPairs {
			mode.BlockMV[block+1] = mv
		} else {
			mode.BlockMV[block+1] = MotionVector{
				Row: mv.Row + 2,
				Col: mv.Col + 2,
			}
		}
	}
	mode.MV = mode.BlockMV[15]
	return mode
}

// BenchmarkInterMBBuilderAlternatingZeroMV720p keeps every skipped ZeroMV
// macroblock as a single-MB run by alternating references across the row.
func BenchmarkInterMBBuilderAlternatingZeroMV720p(b *testing.B) {
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
			ref := common.LastFrame
			if c&1 != 0 {
				ref = common.GoldenFrame
			}
			modes[r*cols+c] = MacroblockMode{
				Mode:        common.ZeroMV,
				RefFrame:    ref,
				MBSkipCoeff: true,
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
