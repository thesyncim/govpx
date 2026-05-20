package govpx

import "github.com/thesyncim/govpx/internal/vp9/encoder"

type vp9RCPickQAndBoundsTwoPassInputs = encoder.RCPickQAndBoundsTwoPassInputs
type vp9RCPickQAndBoundsTwoPassResult = encoder.RCPickQAndBoundsTwoPassResult

func vp9RCPickQAndBoundsTwoPass(in vp9RCPickQAndBoundsTwoPassInputs, regulatedQ int) vp9RCPickQAndBoundsTwoPassResult {
	return encoder.RCPickQAndBoundsTwoPass(in, regulatedQ)
}
