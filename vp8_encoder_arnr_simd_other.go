package govpx

import "github.com/thesyncim/govpx/internal/pixel"

func applyTemporalFilterSIMD(src []byte, ss int, pred []byte, ps, w, strength, weight int, acc, count []uint32) bool {
	if !pixel.Enabled || (w != 8 && w != 16) || strength < 0 || strength > 6 {
		return false
	}
	pixel.Temporal(src, ss, pred, ps, w, strength, weight, acc, count)
	return true
}
