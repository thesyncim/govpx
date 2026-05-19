package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func (e *VP9Encoder) ensureVP9LeafKeyframeDecisionCache(miRows, miCols int) {
	n := miRows * miCols
	if cap(e.vp9LeafKeyframeDecisions) < n {
		e.vp9LeafKeyframeDecisions = make([]vp9LeafKeyframeDecisionEntry, n)
	} else {
		e.vp9LeafKeyframeDecisions = e.vp9LeafKeyframeDecisions[:n]
	}
	e.vp9LeafKeyframeDecisionsRows = miRows
	e.vp9LeafKeyframeDecisionsCols = miCols
	e.resetVP9LeafKeyframeDecisionCache()
}

func (e *VP9Encoder) resetVP9LeafKeyframeDecisionCache() {
	e.vp9LeafKeyframeDecisionsVer++
	if e.vp9LeafKeyframeDecisionsVer == 0 {
		for i := range e.vp9LeafKeyframeDecisions {
			e.vp9LeafKeyframeDecisions[i] = vp9LeafKeyframeDecisionEntry{}
		}
		e.vp9LeafKeyframeDecisionsVer = 1
	}
}

func (e *VP9Encoder) ensureVP9KeyframePartitionDecisionCache(miRows, miCols int) {
	n := miRows * miCols * int(common.BlockSizes)
	if cap(e.vp9KeyframePartitionDecisions) < n {
		e.vp9KeyframePartitionDecisions = make([]vp9KeyframePartitionDecisionEntry, n)
	} else {
		e.vp9KeyframePartitionDecisions = e.vp9KeyframePartitionDecisions[:n]
	}
	e.vp9KeyframePartitionDecisionsRows = miRows
	e.vp9KeyframePartitionDecisionsCols = miCols
	e.vp9KeyframePartitionDecisionsVer++
	if e.vp9KeyframePartitionDecisionsVer == 0 {
		for i := range e.vp9KeyframePartitionDecisions {
			e.vp9KeyframePartitionDecisions[i] = vp9KeyframePartitionDecisionEntry{}
		}
		e.vp9KeyframePartitionDecisionsVer = 1
	}
}

func (e *VP9Encoder) lookupVP9KeyframePartitionDecision(miRow, miCol int,
	root common.BlockSize,
) (common.BlockSize, bool) {
	if e.vp9KeyframePartitionDecisionsCols <= 0 ||
		root < 0 || root >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9KeyframePartitionDecisionsRows ||
		miCol >= e.vp9KeyframePartitionDecisionsCols {
		return common.BlockInvalid, false
	}
	off := (miRow*e.vp9KeyframePartitionDecisionsCols+miCol)*int(common.BlockSizes) + int(root)
	if off < 0 || off >= len(e.vp9KeyframePartitionDecisions) {
		return common.BlockInvalid, false
	}
	entry := &e.vp9KeyframePartitionDecisions[off]
	if !entry.valid || entry.version != e.vp9KeyframePartitionDecisionsVer ||
		entry.root != root {
		return common.BlockInvalid, false
	}
	return entry.target, true
}

func (e *VP9Encoder) storeVP9KeyframePartitionDecision(miRow, miCol int,
	root, target common.BlockSize,
) {
	if e.vp9KeyframePartitionDecisionsCols <= 0 ||
		root < 0 || root >= common.BlockSizes {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9KeyframePartitionDecisionsRows ||
		miCol >= e.vp9KeyframePartitionDecisionsCols {
		return
	}
	off := (miRow*e.vp9KeyframePartitionDecisionsCols+miCol)*int(common.BlockSizes) + int(root)
	if off < 0 || off >= len(e.vp9KeyframePartitionDecisions) {
		return
	}
	e.vp9KeyframePartitionDecisions[off] = vp9KeyframePartitionDecisionEntry{
		version: e.vp9KeyframePartitionDecisionsVer,
		root:    root,
		target:  target,
		valid:   true,
	}
}

func (e *VP9Encoder) lookupVP9LeafKeyframeDecision(miRow, miCol int,
	bsize common.BlockSize,
) (vp9KeyframeModeDecision, bool) {
	if e.vp9LeafKeyframeDecisionsCols <= 0 {
		return vp9KeyframeModeDecision{}, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafKeyframeDecisionsRows ||
		miCol >= e.vp9LeafKeyframeDecisionsCols {
		return vp9KeyframeModeDecision{}, false
	}
	off := miRow*e.vp9LeafKeyframeDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafKeyframeDecisions) {
		return vp9KeyframeModeDecision{}, false
	}
	entry := &e.vp9LeafKeyframeDecisions[off]
	if !entry.valid || entry.version != e.vp9LeafKeyframeDecisionsVer ||
		entry.bsize != bsize {
		return vp9KeyframeModeDecision{}, false
	}
	return entry.decision, true
}

func (e *VP9Encoder) storeVP9LeafKeyframeDecision(miRow, miCol int,
	bsize common.BlockSize, decision vp9KeyframeModeDecision,
) {
	if e.vp9LeafKeyframeDecisionsCols <= 0 {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafKeyframeDecisionsRows ||
		miCol >= e.vp9LeafKeyframeDecisionsCols {
		return
	}
	off := miRow*e.vp9LeafKeyframeDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafKeyframeDecisions) {
		return
	}
	e.vp9LeafKeyframeDecisions[off] = vp9LeafKeyframeDecisionEntry{
		version:  e.vp9LeafKeyframeDecisionsVer,
		bsize:    bsize,
		decision: decision,
		valid:    true,
	}
}

// ensureVP9LeafInterDecisionCache sizes the per-frame leaf-write picker
// decision cache to the current miGrid extent. Called from
// ensureVP9EncoderModeBuffers so the cache always tracks the active frame.
// The version stamp is bumped to invalidate any stale entries left from the
// prior frame (avoids the O(N) zeroing every frame).
//
// libvpx: vp9/encoder/vp9_encodeframe.c::set_offsets resizes cpi->td.mb
// per-frame; the per-block mbmi decision survives within the frame but is
// reset at frame boundaries via vp9_zero(cm->mip).
func (e *VP9Encoder) ensureVP9LeafInterDecisionCache(miRows, miCols int) {
	n := miRows * miCols
	if cap(e.vp9LeafInterDecisions) < n {
		e.vp9LeafInterDecisions = make([]vp9LeafInterDecisionEntry, n)
	} else {
		e.vp9LeafInterDecisions = e.vp9LeafInterDecisions[:n]
	}
	e.vp9LeafInterDecisionsRows = miRows
	e.vp9LeafInterDecisionsCols = miCols
	e.vp9LeafInterDecisionsVer++
	// On version wraparound (extremely unlikely; uint32 covers 4B frames)
	// zero the cache so a stale version stamp can't masquerade as fresh.
	if e.vp9LeafInterDecisionsVer == 0 {
		for i := range e.vp9LeafInterDecisions {
			e.vp9LeafInterDecisions[i] = vp9LeafInterDecisionEntry{}
		}
		e.vp9LeafInterDecisionsVer = 1
	}
}

// lookupVP9LeafInterDecision returns a previously stored leaf-write inter
// picker decision for (miRow, miCol, bsize) if one was committed in the
// current frame. The first leaf-write visit (count pre-pass) populates;
// the second visit (bitstream write pass) consumes. A miss returns false.
func (e *VP9Encoder) lookupVP9LeafInterDecision(miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if e.vp9LeafInterDecisionsCols <= 0 {
		return vp9InterModeDecision{}, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterDecisionsRows ||
		miCol >= e.vp9LeafInterDecisionsCols {
		return vp9InterModeDecision{}, false
	}
	off := miRow*e.vp9LeafInterDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterDecisions) {
		return vp9InterModeDecision{}, false
	}
	entry := &e.vp9LeafInterDecisions[off]
	if !entry.valid || entry.version != e.vp9LeafInterDecisionsVer ||
		entry.bsize != bsize {
		return vp9InterModeDecision{}, false
	}
	return entry.decision, true
}

// storeVP9LeafInterDecision commits the picker decision for (miRow, miCol,
// bsize) to the per-frame leaf cache. Subsequent same-frame lookups at the
// same key return the stored decision, allowing the bitstream write pass to
// skip pickVP9InterReferenceMode after the count pre-pass populated the
// entry.
func (e *VP9Encoder) storeVP9LeafInterDecision(miRow, miCol int,
	bsize common.BlockSize, decision vp9InterModeDecision,
) {
	if e.vp9LeafInterDecisionsCols <= 0 {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterDecisionsRows ||
		miCol >= e.vp9LeafInterDecisionsCols {
		return
	}
	off := miRow*e.vp9LeafInterDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterDecisions) {
		return
	}
	e.vp9LeafInterDecisions[off] = vp9LeafInterDecisionEntry{
		version:  e.vp9LeafInterDecisionsVer,
		bsize:    bsize,
		decision: decision,
		valid:    true,
	}
}

func (e *VP9Encoder) fillVP9MiGrid(miRows, miCols, r, c int, bsize common.BlockSize, mi vp9dec.NeighborMi) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := e.miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}
