package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// Keyframes use the fixed default coefficient probabilities. Inter frames
// already provide a per-frame table; retain the same prepared representation.
var vp8KeyFrameCoefCosts = func() (costs vp8enc.CoefficientTokenCostTable) {
	vp8enc.FillCoefficientTokenCostTable(&vp8tables.DefaultCoefProbs, &costs)
	return
}()
