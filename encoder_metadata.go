package govpx

// LastQuantizer returns the public 0..63 quantizer and internal VP8 qindex for
// the most recently committed encoded frame.
func (e *VP8Encoder) LastQuantizer() (public int, internal int, ok bool) {
	if e == nil || e.closed || !e.lastQuantizerValid {
		return 0, 0, false
	}
	return e.lastQuantizerPublic, e.lastQuantizerInternal, true
}

func (e *VP8Encoder) setEncodeResultQuantizer(result *EncodeResult, qIndex int) {
	public := libvpxQIndexToPublicQuantizer(qIndex)
	result.Quantizer = public
	result.InternalQuantizer = qIndex
	e.lastQuantizerPublic = public
	e.lastQuantizerInternal = qIndex
	e.lastQuantizerValid = true
}
