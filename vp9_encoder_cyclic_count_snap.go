package govpx

import (
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// saveVP9CyclicRefreshMapsForCounts snapshots cyclic refresh maps after
// vp9_cyclic_refresh_setup so the count pass can mutate SegMap/RefreshMap
// the same way the wire encode pass does. libvpx runs
// vp9_choose_segmap_coding_method on the realized mi_grid after encode,
// which sees cumulative update_segment mutations; govpx's count pass must
// replay those mutations for the chooser, then restore Setup state before
// the wire pass.
func (e *VP9Encoder) saveVP9CyclicRefreshMapsForCounts() bool {
	if e == nil || !e.cyclicAQ.Enabled || !e.cyclicAQ.Apply ||
		len(e.cyclicAQ.SegMap) == 0 || len(e.cyclicAQ.RefreshMap) == 0 {
		return false
	}
	n := len(e.cyclicAQ.SegMap)
	e.cyclicCountSegMapSnap = buffers.EnsureLen(e.cyclicCountSegMapSnap, n)
	e.cyclicCountRefreshMapSnap = buffers.EnsureLen(e.cyclicCountRefreshMapSnap, n)
	copy(e.cyclicCountSegMapSnap, e.cyclicAQ.SegMap)
	copy(e.cyclicCountRefreshMapSnap, e.cyclicAQ.RefreshMap)
	return true
}

func (e *VP9Encoder) restoreVP9CyclicRefreshMapsAfterCounts(saved bool) {
	if e == nil || !saved ||
		len(e.cyclicCountSegMapSnap) == 0 ||
		len(e.cyclicCountRefreshMapSnap) == 0 {
		return
	}
	if len(e.cyclicAQ.SegMap) != len(e.cyclicCountSegMapSnap) ||
		len(e.cyclicAQ.RefreshMap) != len(e.cyclicCountRefreshMapSnap) {
		return
	}
	copy(e.cyclicAQ.SegMap, e.cyclicCountSegMapSnap)
	copy(e.cyclicAQ.RefreshMap, e.cyclicCountRefreshMapSnap)
}
