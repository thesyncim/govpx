package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestBuildKeyFrameCoefficientProbabilityUpdates(t *testing.T) {
	const rows, cols = 16, 16
	modes := make([]KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	above := make([]TokenContextPlanes, cols)

	frameProbs, updates, err := BuildKeyFrameCoefficientProbabilityUpdates(rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs)
	if err != nil {
		t.Fatalf("BuildKeyFrameCoefficientProbabilityUpdates returned error: %v", err)
	}

	if updates.UpdateCount == 0 {
		t.Fatalf("UpdateCount = 0, want EOB-heavy coefficient grid to update probabilities")
	}
	if frameProbs == tables.DefaultCoefProbs {
		t.Fatalf("frame probabilities equal defaults, want updated probabilities")
	}
}

func TestWriteCoefficientKeyFrameEmitsCoefficientProbabilityUpdates(t *testing.T) {
	const rows, cols = 16, 16
	modes := make([]KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
	}
	above := make([]TokenContextPlanes, cols)
	packet := make([]byte, 65536)

	n, err := WriteCoefficientKeyFrame(packet, cols*16, rows*16, KeyFrameStateConfig{BaseQIndex: 20}, modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
	}

	coefProbs := tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Probability.UpdateCount == 0 {
		t.Fatalf("state coefficient update count = 0, want emitted updates")
	}
	if coefProbs == tables.DefaultCoefProbs {
		t.Fatalf("parsed coefficient probabilities equal defaults, want updates applied")
	}
}
