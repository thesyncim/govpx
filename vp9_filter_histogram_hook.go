package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

const (
	vp9SwitchableInterpHistogramContexts = 4
	vp9SwitchableInterpHistogramFilters  = 3
)

// vp9SwitchableInterpHistogramHook is a build-time diagnostic hook used by
// task #156's byte-4 investigation. The hook is nil in production builds.
var vp9SwitchableInterpHistogramHook func(
	frameIdx int,
	hist [vp9SwitchableInterpHistogramContexts][vp9SwitchableInterpHistogramFilters]uint32,
	total [vp9SwitchableInterpHistogramFilters]uint32,
	c int,
)

// vp9CallSwitchableInterpHistogramHook is a no-op when no diagnostic hook is
// installed. Test code installs vp9SwitchableInterpHistogramHook to read
// counts.SwitchableInterp before fix_interp_filter runs (libvpx
// vp9_bitstream.c:864-885) so the per-frame c value driving the demotion
// can be inspected without rebuilding the encoder.
func vp9CallSwitchableInterpHistogramHook(frameIdx int, counts *encoder.FrameCounts) {
	if vp9SwitchableInterpHistogramHook == nil || counts == nil {
		return
	}
	var hist [vp9SwitchableInterpHistogramContexts][vp9SwitchableInterpHistogramFilters]uint32
	var total [vp9SwitchableInterpHistogramFilters]uint32
	c := 0
	for i := range vp9SwitchableInterpHistogramFilters {
		for j := range vp9SwitchableInterpHistogramContexts {
			hist[j][i] = counts.SwitchableInterp[j][i]
			total[i] += counts.SwitchableInterp[j][i]
		}
		if total[i] > 0 {
			c++
		}
	}
	vp9SwitchableInterpHistogramHook(frameIdx, hist, total, c)
}
