package encoder

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

// Zbin mode boosts mirror libvpx v1.16.0 VP8 vp8/encoder/encodeframe.c
// zbin_mode_boost setup for inter-frame macroblock coding.
const (
	LastFrameZeroMVZbinBoost  = 6
	GoldenAltZeroMVZbinBoost  = 12
	NonZeroInterModeZbinBoost = 4
	SplitInterModeZbinBoost   = 0
	IntraInterFrameZbinBoost  = 0
)

// InterZbinModeBoost returns libvpx VP8's zbin mode boost for an inter-frame
// macroblock mode.
func InterZbinModeBoost(mode *InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return IntraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return LastFrameZeroMVZbinBoost
		}
		return GoldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return SplitInterModeZbinBoost
	default:
		return NonZeroInterModeZbinBoost
	}
}
