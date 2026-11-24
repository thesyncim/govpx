package decoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestVP8Algorithm2PredictWholeMVInterLumaWithState(t *testing.T) {
	for _, size := range [][2]int{{17, 19}, {64, 64}} {
		var ref common.FrameBuffer
		if err := ref.Resize(size[0], size[1], 32, 32); err != nil {
			t.Fatal(err)
		}
		fillImage(&ref.Img, testImage(size[0], size[1]))
		ref.ExtendBorders()
		for _, cfg := range []InterPredictionConfig{{}, {UseBilinear: true}, {FullPixel: true}, {UseBilinear: true, FullPixel: true}} {
			state := PrepareInterFrameRefState(&ref.Img, cfg)
			for _, pos := range [][2]int{{0, 0}, {state.mbRows - 1, state.mbCols - 1}} {
				for _, mv := range []MotionVector{{}, {Row: 3, Col: 5}, {Row: -7, Col: 9}, {Row: 8}, {Row: -16, Col: 24}, {Row: -32768, Col: 32767}} {
					for _, kind := range []common.MBPredictionMode{common.ZeroMV, common.NewMV, common.NearMV, common.SplitMV, common.DCPred} {
						mode := MacroblockMode{RefFrame: common.LastFrame, Mode: kind, MV: mv, MBSkipCoeff: true, Is4x4: kind == common.SplitMV}
						gy, wy := bytes.Repeat([]byte{211}, 16*23), bytes.Repeat([]byte{211}, 16*23)
						var u, v [64]byte
						var tokens MacroblockTokens
						var dq common.MacroblockDequant
						var scratch MacroblockResidual
						got := PredictWholeMVInterLumaWithState(&state, &mode, gy, 23, pos[0], pos[1])
						want := ReconstructWholeMVInterMacroblockWithState(&state, &mode, &tokens, &dq, wy, 23, u[:], 8, v[:], 8, &scratch, pos[0], pos[1])
						if got != want || !bytes.Equal(gy, wy) {
							t.Fatalf("size%v cfg%+v pos%v mv%v mode%d: %t/%t or luma mismatch", size, cfg, pos, mv, kind, got, want)
						}
					}
				}
			}
		}
	}
}

func TestVP8Algorithm2PredictWholeMVInterLumaValidation(t *testing.T) {
	img := testImage(32, 32)
	state := PrepareInterFrameRefState(&img, InterPredictionConfig{})
	mode := MacroblockMode{RefFrame: common.LastFrame, Mode: common.ZeroMV, MBSkipCoeff: true}
	var y [256]byte
	for _, bad := range []*InterFrameRefState{nil, {}, {ready: true}} {
		if PredictWholeMVInterLumaWithState(bad, &mode, y[:], 16, 0, 0) {
			t.Fatal("invalid state accepted")
		}
	}
	if PredictWholeMVInterLumaWithState(&state, nil, y[:], 16, 0, 0) {
		t.Fatal("nil mode accepted")
	}
	if PredictWholeMVInterLumaWithState(&state, &mode, y[:255], 16, 0, 0) {
		t.Fatal("short output accepted")
	}
	if PredictWholeMVInterLumaWithState(&state, &mode, y[:], 16, -1, 0) {
		t.Fatal("invalid macroblock accepted")
	}
	state.uPlane = nil
	if PredictWholeMVInterLumaWithState(&state, &mode, y[:], 16, 0, 0) {
		t.Fatal("unaddressable chroma accepted")
	}
}
