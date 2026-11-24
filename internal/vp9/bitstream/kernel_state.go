package bitstream

// KernelState exposes the writer's arithmetic-coder state for batch pack
// kernels that run many boolean writes with the state held in registers.
// ok is false when the writer is discarding or already in the error state;
// kernels must then fall back to the per-call path.
func (w *Writer) KernelState() (lowValue, rng uint32, count int32, buf []byte, pos uint32, ok bool) {
	if w.discard || w.err {
		return 0, 0, 0, nil, 0, false
	}
	return w.lowValue, w.rng, w.count, w.buf, w.pos, true
}

// SetKernelState stores the arithmetic-coder state a batch kernel produced.
// The kernel must have respected the same emit semantics as Write, and the
// caller must have prechecked buffer capacity for the whole batch.
func (w *Writer) SetKernelState(lowValue, rng uint32, count int32, pos uint32) {
	w.lowValue = lowValue
	w.rng = rng
	w.count = count
	w.pos = pos
}
