package govpx

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
