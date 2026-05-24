package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// ResetFrameContexts resets every VP9 entropy context and returns the active
// frame context selected by libvpx after a full reset.
func ResetFrameContexts(frameContexts *[common.FrameContexts]vp9dec.FrameContext) vp9dec.FrameContext {
	for i := range frameContexts {
		vp9dec.ResetFrameContext(&frameContexts[i])
	}
	return frameContexts[0]
}

// PrepareFrameContext applies the reset_frame_context header policy before a
// frame is coded and returns the slot index and active context selected for the
// frame.
func PrepareFrameContext(frameContexts *[common.FrameContexts]vp9dec.FrameContext,
	hdr *vp9dec.UncompressedHeader,
) (int, vp9dec.FrameContext) {
	idx := int(hdr.FrameContextIdx)
	if idx >= common.FrameContexts {
		idx = 0
	}
	if hdr.FrameType == common.KeyFrame ||
		hdr.ErrorResilientMode || hdr.ResetFrameContext == 3 {
		return 0, ResetFrameContexts(frameContexts)
	}
	if hdr.IntraOnly && hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&frameContexts[idx])
		return 0, frameContexts[0]
	}
	if hdr.IntraOnly {
		return 0, frameContexts[0]
	}
	if hdr.ResetFrameContext == 2 {
		vp9dec.ResetFrameContext(&frameContexts[idx])
	}
	return idx, frameContexts[idx]
}

// CommitFrameContext stores the adapted active context when the VP9 header asks
// the encoder to refresh its selected context slot.
func CommitFrameContext(frameContexts *[common.FrameContexts]vp9dec.FrameContext,
	active vp9dec.FrameContext, hdr *vp9dec.UncompressedHeader, idx int,
) {
	if idx < 0 || idx >= common.FrameContexts || !hdr.RefreshFrameContext {
		return
	}
	frameContexts[idx] = active
}

// AdaptFrameContext updates the active encoder context from frame counts unless
// the header selected one of VP9's context-adaptation bypass modes.
func AdaptFrameContext(active *vp9dec.FrameContext,
	frameContexts *[common.FrameContexts]vp9dec.FrameContext,
	hdr *vp9dec.UncompressedHeader, idx int, counts *FrameCounts,
	txMode common.TxMode, previousFrameWasKey bool,
) {
	if hdr == nil || counts == nil ||
		idx < 0 || idx >= common.FrameContexts ||
		hdr.ErrorResilientMode || hdr.FrameParallelDecoding {
		return
	}
	pre := &frameContexts[idx]
	bridge := FrameCountsForDecoder(counts)
	vp9dec.AdaptFrameContextWithCounts(active, pre, &bridge, hdr, txMode,
		previousFrameWasKey)
}
