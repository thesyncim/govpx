package govpx

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
