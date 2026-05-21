package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

// LastQuantizer returns the public 0..63 quantizer and the internal VP8
// qindex for the most recently committed encoded frame. ok is false on a
// nil or closed encoder, and before any frame has been committed (which
// includes dropped or buffered-by-lookahead inputs).
func (e *VP8Encoder) LastQuantizer() (public int, internal int, ok bool) {
	if e == nil || e.closed || !e.lastQuantizerValid {
		return 0, 0, false
	}
	return e.lastQuantizerPublic, e.lastQuantizerInternal, true
}

func (e *VP8Encoder) setEncodeResultQuantizer(result *EncodeResult, qIndex int) {
	public := vp8common.QIndexToPublicQuantizer(qIndex)
	result.Quantizer = public
	result.InternalQuantizer = qIndex
	e.lastQuantizerPublic = public
	e.lastQuantizerInternal = qIndex
	e.lastQuantizerValid = true
}
