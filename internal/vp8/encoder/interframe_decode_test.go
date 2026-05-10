package encoder_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteCoefficientInterFrameScratchDecodesWithPublicDecoder(t *testing.T) {
	const (
		width  = 32
		height = 16
		cols   = 2
	)

	key := make([]byte, 4096)
	keyModes := []vp8enc.KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
	}
	keyN, err := vp8enc.WriteZeroKeyFrame(key, width, height, vp8enc.KeyFrameStateConfig{BaseQIndex: 20}, keyModes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	cfg := vp8enc.DefaultInterFrameStateConfig(20)
	modes := []vp8enc.InterFrameMacroblockMode{
		{Mode: common.ZeroMV, MBSkipCoeff: false},
		{Mode: common.ZeroMV, MBSkipCoeff: false},
	}
	coeffs := make([]vp8enc.MacroblockCoefficients, len(modes))
	coeffs[0].QCoeff[24][0] = 8
	coeffs[0].QCoeff[0][1] = -3
	coeffs[1].QCoeff[16][0] = 2
	setAllMacroblockEOBs(&coeffs[0], false)
	setAllMacroblockEOBs(&coeffs[1], false)
	above := make([]vp8enc.TokenContextPlanes, cols)
	inter := make([]byte, 8192)
	var scratch vp8enc.PartitionScratch
	interN, _, _, _, _, _, err := vp8enc.WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings(inter, width, height, cfg, modes, coeffs, above, &tables.DefaultCoefProbs, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, tables.DefaultMVContext, &scratch)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings returned error: %v", err)
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key[:keyN]); err != nil {
		t.Fatalf("Decode key frame returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame after key frame returned no frame")
	}
	if err := d.Decode(inter[:interN]); err != nil {
		t.Fatalf("Decode scratch inter frame returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame after scratch inter frame returned no frame")
	}
	if frame.Width != width || frame.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d", frame.Width, frame.Height, width, height)
	}
}
