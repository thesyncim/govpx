package govpx

import "github.com/thesyncim/govpx/internal/vpx/buffers"

// vp9CyclicResizePending reports whether the next cyclic-refresh setup /
// postencode should treat this frame as following a coded-size change.
func (e *VP9Encoder) vp9CyclicResizePending() bool {
	if e == nil {
		return false
	}
	return e.cyclicResizePending
}

func (e *VP9Encoder) vp9ClearCyclicResizePending() {
	if e == nil {
		return
	}
	e.cyclicResizePending = false
}

// vp9LatchCyclicResizeForFrame copies resize_pending into the per-frame
// latch on the first post-resize inter frame. Key/intra-only frames after
// resize keep cyclicResizePending set until an inter show frame consumes it.
func (e *VP9Encoder) vp9LatchCyclicResizeForFrame(isKey, intraOnly bool) {
	if e == nil {
		return
	}
	e.cyclicResizeFramePending = false
	if e.cyclicResizePending && !isKey && !intraOnly {
		e.cyclicResizeFramePending = true
		e.cyclicResizePending = false
	}
}

// saveVP9CyclicRefreshMapsForCounts snapshots cyclic refresh maps after
// vp9_cyclic_refresh_setup so the count pass can mutate SegMap/RefreshMap
// the same way the wire encode pass does, then restore Setup state before
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
