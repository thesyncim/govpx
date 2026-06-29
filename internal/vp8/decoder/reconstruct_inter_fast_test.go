package decoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// TestReconstructInterFrameGridFastMatchesSlow verifies that the
// frame-state fast path produces byte-identical output to the
// per-MB slow path for a representative inter-frame layout that
// exercises ZeroMV copies, sub-pel sixtap, and sub-pel bilinear.
func TestReconstructInterFrameGridFastMatchesSlow(t *testing.T) {
	const width, height = 96, 48
	const cols = width / 16
	const rows = height / 16

	srcFB, err := common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer ref: %v", err)
	}
	fillImage(&srcFB.Img, testImage(width, height))
	srcFB.ExtendBorders()

	modes := make([]MacroblockMode, rows*cols)
	tokens := make([]MacroblockTokens, rows*cols)
	for r := range rows {
		for c := range cols {
			i := r*cols + c
			switch (r*cols + c) % 4 {
			case 0:
				modes[i] = MacroblockMode{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true}
			case 1:
				modes[i] = MacroblockMode{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 3, Col: 5}, MBSkipCoeff: true}
			case 2:
				modes[i] = MacroblockMode{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: -2, Col: 7}, MBSkipCoeff: true}
			default:
				modes[i] = MacroblockMode{Mode: common.NewMV, RefFrame: common.LastFrame, MV: MotionVector{Row: 16, Col: 16}, MBSkipCoeff: true}
			}
		}
	}
	dequants := testMacroblockDequants()

	// SixTap config (default).
	for _, useBilinear := range []bool{false, true} {
		t.Run(map[bool]string{false: "sixtap", true: "bilinear"}[useBilinear], func(t *testing.T) {
			cfg := InterPredictionConfig{UseBilinear: useBilinear}

			imgFast := blankImage(width, height)
			var scratchFast IntraReconstructionScratch
			if err := ReconstructInterFrameGridWithConfig(&imgFast, &srcFB.Img, &srcFB.Img, &srcFB.Img, rows, cols, modes, tokens, &dequants, &scratchFast, cfg); err != nil {
				t.Fatalf("fast grid: %v", err)
			}

			// Reference image computed via the slow per-MB API.
			imgSlow := blankImage(width, height)
			var scratchSlow IntraReconstructionScratch
			for row := range rows {
				yRow := row * 16 * imgSlow.YStride
				uRow := row * 8 * imgSlow.UStride
				vRow := row * 8 * imgSlow.VStride
				for col := range cols {
					index := row*cols + col
					mode := &modes[index]
					yOff := yRow + col*16
					uOff := uRow + col*8
					vOff := vRow + col*8
					if !ReconstructWholeMVInterMacroblock(mode, &tokens[index], &dequants[mode.SegmentID], &srcFB.Img,
						imgSlow.Y[yOff:], imgSlow.YStride,
						imgSlow.U[uOff:], imgSlow.UStride,
						imgSlow.V[vOff:], imgSlow.VStride,
						&scratchSlow.Residual, row, col, cfg) {
						t.Fatalf("slow per-MB at (%d,%d) failed", row, col)
					}
				}
			}

			if !bytes.Equal(imgFast.Y, imgSlow.Y) {
				for i := 0; i < len(imgFast.Y); i++ {
					if imgFast.Y[i] != imgSlow.Y[i] {
						t.Fatalf("Y mismatch at byte %d: fast=%d slow=%d (row=%d col=%d)", i, imgFast.Y[i], imgSlow.Y[i], i/imgFast.YStride, i%imgFast.YStride)
					}
				}
			}
			if !bytes.Equal(imgFast.U, imgSlow.U) {
				t.Fatalf("U plane mismatch")
			}
			if !bytes.Equal(imgFast.V, imgSlow.V) {
				t.Fatalf("V plane mismatch")
			}
		})
	}
}

func TestReconstructInterFrameGridZeroMVRunFastMatchesSlow(t *testing.T) {
	const width, height = 128, 32
	const cols = width / 16
	const rows = height / 16

	last := testImage(width, height)
	golden := testImage(width, height)
	alt := testImage(width, height)
	addImageDelta(&golden, 37)
	addImageDelta(&alt, 83)

	modes := make([]MacroblockMode, rows*cols)
	tokens := make([]MacroblockTokens, rows*cols)
	for i := range modes {
		modes[i] = MacroblockMode{Mode: common.ZeroMV, RefFrame: common.LastFrame, MBSkipCoeff: true}
	}

	modes[3].RefFrame = common.GoldenFrame
	modes[4].RefFrame = common.GoldenFrame
	modes[5].RefFrame = common.AltRefFrame
	modes[6].MBSkipCoeff = false
	tokens[6].EOB[0] = 1
	tokens[6].QCoeff[0][0] = 8
	modes[8].RefFrame = common.GoldenFrame
	modes[9].RefFrame = common.AltRefFrame
	modes[10].RefFrame = common.GoldenFrame
	modes[11].RefFrame = common.GoldenFrame
	modes[12].RefFrame = common.AltRefFrame
	modes[13].RefFrame = common.AltRefFrame
	modes[14].RefFrame = common.LastFrame
	modes[15].RefFrame = common.LastFrame

	dequants := testMacroblockDequants()
	imgFast := blankImage(width, height)
	var scratchFast IntraReconstructionScratch
	if err := ReconstructInterFrameGridWithConfig(&imgFast, &last, &golden, &alt, rows, cols, modes, tokens, &dequants, &scratchFast, InterPredictionConfig{}); err != nil {
		t.Fatalf("fast grid: %v", err)
	}

	imgSlow := blankImage(width, height)
	var scratchSlow IntraReconstructionScratch
	for row := range rows {
		yRow := row * 16 * imgSlow.YStride
		uRow := row * 8 * imgSlow.UStride
		vRow := row * 8 * imgSlow.VStride
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			yOff := yRow + col*16
			uOff := uRow + col*8
			vOff := vRow + col*8
			ref := &last
			switch mode.RefFrame {
			case common.GoldenFrame:
				ref = &golden
			case common.AltRefFrame:
				ref = &alt
			}
			if !ReconstructWholeMVInterMacroblock(mode, &tokens[index], &dequants[mode.SegmentID], ref,
				imgSlow.Y[yOff:], imgSlow.YStride,
				imgSlow.U[uOff:], imgSlow.UStride,
				imgSlow.V[vOff:], imgSlow.VStride,
				&scratchSlow.Residual, row, col, InterPredictionConfig{}) {
				t.Fatalf("slow per-MB at (%d,%d) failed", row, col)
			}
		}
	}

	assertPlaneEqual(t, "Y", imgFast.Y, imgSlow.Y)
	assertPlaneEqual(t, "U", imgFast.U, imgSlow.U)
	assertPlaneEqual(t, "V", imgFast.V, imgSlow.V)
}

func addImageDelta(img *common.Image, delta byte) {
	for i := range img.Y {
		img.Y[i] += delta
	}
	for i := range img.U {
		img.U[i] += delta
	}
	for i := range img.V {
		img.V[i] += delta
	}
}
