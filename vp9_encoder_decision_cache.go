package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func (e *VP9Encoder) ensureVP9LeafKeyframeDecisionCache(miRows, miCols int) {
	n := miRows * miCols
	e.vp9LeafKeyframeDecisions = buffers.EnsureLen(e.vp9LeafKeyframeDecisions, n)
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
	e.vp9KeyframePartitionDecisions = buffers.EnsureLen(e.vp9KeyframePartitionDecisions, n)
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

const vp9KeyframeDecisionSnapshotMaxCells = 64
const vp9KeyframeDecisionSnapshotMaxPartitions = vp9KeyframeDecisionSnapshotMaxCells * int(common.BlockSizes)

type vp9KeyframeDecisionRegionSnapshot struct {
	miRow, miCol int
	rows, cols   int
	leaf         [vp9KeyframeDecisionSnapshotMaxCells]vp9LeafKeyframeDecisionEntry
	partition    [vp9KeyframeDecisionSnapshotMaxPartitions]vp9KeyframePartitionDecisionEntry
	ok           bool
}

func (e *VP9Encoder) snapshotVP9KeyframeDecisionRegion(miRows, miCols, miRow, miCol int,
	root common.BlockSize, snap *vp9KeyframeDecisionRegionSnapshot,
) bool {
	if snap == nil {
		return false
	}
	*snap = vp9KeyframeDecisionRegionSnapshot{}
	if root < 0 || root >= common.BlockSizes || miRow < 0 || miCol < 0 ||
		miRow >= miRows || miCol >= miCols ||
		e.vp9LeafKeyframeDecisionsCols <= 0 ||
		e.vp9KeyframePartitionDecisionsCols <= 0 {
		return false
	}
	rows := min(int(common.Num8x8BlocksHighLookup[root]), miRows-miRow)
	cols := min(int(common.Num8x8BlocksWideLookup[root]), miCols-miCol)
	if rows <= 0 || cols <= 0 || rows*cols > len(snap.leaf) {
		return false
	}
	const blockSizes = int(common.BlockSizes)
	if rows*cols*blockSizes > len(snap.partition) {
		return false
	}
	for r := range rows {
		for c := range cols {
			cell := r*cols + c
			row := miRow + r
			col := miCol + c
			leafOff := row*e.vp9LeafKeyframeDecisionsCols + col
			if leafOff < 0 || leafOff >= len(e.vp9LeafKeyframeDecisions) {
				return false
			}
			partOff := (row*e.vp9KeyframePartitionDecisionsCols + col) * blockSizes
			if partOff < 0 || partOff+blockSizes > len(e.vp9KeyframePartitionDecisions) {
				return false
			}
			snap.leaf[cell] = e.vp9LeafKeyframeDecisions[leafOff]
			copy(snap.partition[cell*blockSizes:(cell+1)*blockSizes],
				e.vp9KeyframePartitionDecisions[partOff:partOff+blockSizes])
		}
	}
	snap.miRow = miRow
	snap.miCol = miCol
	snap.rows = rows
	snap.cols = cols
	snap.ok = true
	return true
}

func (e *VP9Encoder) restoreVP9KeyframeDecisionRegion(snap vp9KeyframeDecisionRegionSnapshot) {
	if !snap.ok || snap.rows <= 0 || snap.cols <= 0 ||
		e.vp9LeafKeyframeDecisionsCols <= 0 ||
		e.vp9KeyframePartitionDecisionsCols <= 0 {
		return
	}
	const blockSizes = int(common.BlockSizes)
	for r := 0; r < snap.rows; r++ {
		for c := 0; c < snap.cols; c++ {
			cell := r*snap.cols + c
			row := snap.miRow + r
			col := snap.miCol + c
			leafOff := row*e.vp9LeafKeyframeDecisionsCols + col
			if leafOff >= 0 && leafOff < len(e.vp9LeafKeyframeDecisions) {
				e.vp9LeafKeyframeDecisions[leafOff] = snap.leaf[cell]
			}
			partOff := (row*e.vp9KeyframePartitionDecisionsCols + col) * blockSizes
			if partOff >= 0 && partOff+blockSizes <= len(e.vp9KeyframePartitionDecisions) {
				copy(e.vp9KeyframePartitionDecisions[partOff:partOff+blockSizes],
					snap.partition[cell*blockSizes:(cell+1)*blockSizes])
			}
		}
	}
}

// clampVP9LeafDecisionTxSizes mirrors libvpx reset_skip_tx_size after
// frame-level tx_mode demotion. The source function walks the committed
// mi_grid_visible entries and lowers any tx_size above the new ceiling. The
// fallback write/count replay surface remains the leaf decision cache; normal
// packed writes consume the final count walk's miGrid directly.
func (e *VP9Encoder) clampVP9LeafDecisionTxSizes(maxTxSize common.TxSize) {
	if maxTxSize >= common.TxSizes {
		return
	}
	for i := range e.vp9LeafInterDecisions {
		entry := &e.vp9LeafInterDecisions[i]
		if !entry.valid || entry.version != e.vp9LeafInterDecisionsVer {
			continue
		}
		if entry.decision.txSize > maxTxSize {
			entry.decision.txSize = maxTxSize
		}
	}
	for i := range e.vp9LeafKeyframeDecisions {
		entry := &e.vp9LeafKeyframeDecisions[i]
		if !entry.valid || entry.version != e.vp9LeafKeyframeDecisionsVer {
			continue
		}
		if entry.decision.txSize > maxTxSize {
			entry.decision.txSize = maxTxSize
		}
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
	e.vp9LeafInterDecisions = buffers.EnsureLen(e.vp9LeafInterDecisions, n)
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
// bsize) to the per-frame fallback cache. Subsequent same-frame lookups at the
// same key avoid re-running pickVP9InterReferenceMode when packed count-state
// replay is unavailable or a tx-mode demotion reruns the count walk.
func (e *VP9Encoder) storeVP9LeafInterDecision(miRow, miCol int,
	bsize common.BlockSize, decision vp9InterModeDecision,
) {
	decision.lumaPredReady = false
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

// ensureVP9LeafInterRDDecisionCache sizes the depth-first full-RD inter
// SEARCH->WRITE replay cache to the current miGrid extent and bumps the version
// stamp so stale prior-frame entries can't masquerade as fresh. It mirrors
// ensureVP9LeafInterDecisionCache; the two caches are kept separate so the
// flag-off production path (which only uses vp9LeafInterDecisions) is never
// perturbed by the deep recursion's commits.
//
// libvpx: rd_pick_partition runs once per SB and stores the committed per-leaf
// mbmi into mi_grid_visible (vp9/encoder/vp9_encodeframe.c); write_modes_b reads
// it back without recomputation (vp9/encoder/vp9_bitstream.c).
func (e *VP9Encoder) ensureVP9LeafInterRDDecisionCache(miRows, miCols int) {
	n := miRows * miCols
	e.vp9LeafInterRDDecisions = buffers.EnsureLen(e.vp9LeafInterRDDecisions, n)
	e.vp9LeafInterRDDecisionsRows = miRows
	e.vp9LeafInterRDDecisionsCols = miCols
	e.vp9LeafInterRDDecisionsVer++
	if e.vp9LeafInterRDDecisionsVer == 0 {
		for i := range e.vp9LeafInterRDDecisions {
			e.vp9LeafInterRDDecisions[i] = vp9LeafInterRDDecisionEntry{}
		}
		e.vp9LeafInterRDDecisionsVer = 1
	}
}

// lookupVP9LeafInterRDDecision returns the committed deep-RD search decision for
// (miRow, miCol, bsize) if pickVP9InterPartitionRD committed one in the current
// frame's search pass. The bitstream write descent consumes it to replay the
// search's leaf choice without re-picking. A miss returns false (caller falls
// back to the normal pick path). Only called while vp9InterUseDeepRDPartition is
// active.
func (e *VP9Encoder) lookupVP9LeafInterRDDecision(miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if e.vp9LeafInterRDDecisionsCols <= 0 {
		return vp9InterModeDecision{}, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterRDDecisionsRows ||
		miCol >= e.vp9LeafInterRDDecisionsCols {
		return vp9InterModeDecision{}, false
	}
	off := miRow*e.vp9LeafInterRDDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterRDDecisions) {
		return vp9InterModeDecision{}, false
	}
	entry := &e.vp9LeafInterRDDecisions[off]
	if !entry.valid || entry.version != e.vp9LeafInterRDDecisionsVer ||
		entry.bsize != bsize {
		return vp9InterModeDecision{}, false
	}
	return entry.decision, true
}

// storeVP9LeafInterRDDecision commits the deep-RD search's chosen leaf decision
// for (miRow, miCol, bsize). Called from scoreVP9InterPartitionLeaf (gated on
// vp9InterUseDeepRDPartition) as it fills the mi grid. The depth-first search
// re-runs the winning partition arm last, so the final store at each key the
// writer actually descends is the committed (winning) leaf's decision; losing
// trial arms leave entries at block sizes the writer never reads back.
func (e *VP9Encoder) storeVP9LeafInterRDDecision(miRow, miCol int,
	bsize common.BlockSize, decision vp9InterModeDecision,
) {
	decision.lumaPredReady = false
	if e.vp9LeafInterRDDecisionsCols <= 0 {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterRDDecisionsRows ||
		miCol >= e.vp9LeafInterRDDecisionsCols {
		return
	}
	off := miRow*e.vp9LeafInterRDDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterRDDecisions) {
		return
	}
	e.vp9LeafInterRDDecisions[off] = vp9LeafInterRDDecisionEntry{
		version:  e.vp9LeafInterRDDecisionsVer,
		bsize:    bsize,
		decision: decision,
		valid:    true,
	}
}

// ensureVP9InterPartitionRDDecisionCache sizes the deep full-RD inter
// SEARCH->WRITE partition-tree cache to (miRows*miCols*BlockSizes) and bumps the
// version stamp to invalidate stale prior-frame entries. Mirrors
// ensureVP9KeyframePartitionDecisionCache; allocated only under
// vp9InterUseDeepRDPartition.
func (e *VP9Encoder) ensureVP9InterPartitionRDDecisionCache(miRows, miCols int) {
	n := miRows * miCols * int(common.BlockSizes)
	e.vp9InterPartitionRDDecisions = buffers.EnsureLen(e.vp9InterPartitionRDDecisions, n)
	e.vp9InterPartitionRDDecisionsRows = miRows
	e.vp9InterPartitionRDDecisionsCols = miCols
	e.vp9InterPartitionRDDecisionsVer++
	if e.vp9InterPartitionRDDecisionsVer == 0 {
		for i := range e.vp9InterPartitionRDDecisions {
			e.vp9InterPartitionRDDecisions[i] = vp9InterPartitionRDDecisionEntry{}
		}
		e.vp9InterPartitionRDDecisionsVer = 1
	}
}

// lookupVP9InterPartitionRDDecision returns the deep-RD search's committed child
// block size for node (miRow, miCol, root) if pickVP9InterPartitionRD committed
// one this frame. The writer's region picker reads it to descend the search's
// partition tree without re-deciding the node. Mirrors
// lookupVP9KeyframePartitionDecision.
func (e *VP9Encoder) lookupVP9InterPartitionRDDecision(miRow, miCol int,
	root common.BlockSize,
) (common.BlockSize, bool) {
	if e.vp9InterPartitionRDDecisionsCols <= 0 ||
		root < 0 || root >= common.BlockSizes {
		return common.BlockInvalid, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9InterPartitionRDDecisionsRows ||
		miCol >= e.vp9InterPartitionRDDecisionsCols {
		return common.BlockInvalid, false
	}
	off := (miRow*e.vp9InterPartitionRDDecisionsCols+miCol)*int(common.BlockSizes) + int(root)
	if off < 0 || off >= len(e.vp9InterPartitionRDDecisions) {
		return common.BlockInvalid, false
	}
	entry := &e.vp9InterPartitionRDDecisions[off]
	if !entry.valid || entry.version != e.vp9InterPartitionRDDecisionsVer ||
		entry.root != root {
		return common.BlockInvalid, false
	}
	return entry.target, true
}

// storeVP9InterPartitionRDDecision commits the deep-RD search's chosen child
// block size for node (miRow, miCol, root). Called at each pickVP9InterPartitionRD
// node as it returns its committed decision; the depth-first commit pass re-runs
// the winning arm last, so for every node the writer actually descends the final
// store is the committed (winning) partition. Mirrors
// storeVP9KeyframePartitionDecision.
func (e *VP9Encoder) storeVP9InterPartitionRDDecision(miRow, miCol int,
	root, target common.BlockSize,
) {
	if e.vp9InterPartitionRDDecisionsCols <= 0 ||
		root < 0 || root >= common.BlockSizes {
		return
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9InterPartitionRDDecisionsRows ||
		miCol >= e.vp9InterPartitionRDDecisionsCols {
		return
	}
	off := (miRow*e.vp9InterPartitionRDDecisionsCols+miCol)*int(common.BlockSizes) + int(root)
	if off < 0 || off >= len(e.vp9InterPartitionRDDecisions) {
		return
	}
	e.vp9InterPartitionRDDecisions[off] = vp9InterPartitionRDDecisionEntry{
		version: e.vp9InterPartitionRDDecisionsVer,
		root:    root,
		target:  target,
		valid:   true,
	}
}

// vp9LookupDeepInterRDDecision is the flag-gated read entry the bitstream write
// descent uses to replay a committed deep-RD leaf decision. When
// vp9InterUseDeepRDPartition is off it returns a miss unconditionally, so the
// write path falls through to its normal pick/lookup chain and production stays
// byte-identical (the deep cache is never even allocated in that case).
func (e *VP9Encoder) vp9LookupDeepInterRDDecision(miRow, miCol int,
	bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if !vp9InterUseDeepRDPartition || !vp9InterDeepRDReplayWrites {
		return vp9InterModeDecision{}, false
	}
	return e.lookupVP9LeafInterRDDecision(miRow, miCol, bsize)
}

// vp9LookupDeepInterRDDecisionForWrite is the write-descent replay entry that
// reconciles the sub-8x8 key mismatch. The deep recursion stores each leaf at
// its actual bsize, but the writer's prepareVP9InterBlockResidue runs at
// reconBsize == ModeInfoDecodeBSize(bsize), which folds every sub-8x8 leaf
// (BLOCK_4X4/8X4/4X8) to BLOCK_8X8. When the BLOCK_8X8 lookup misses and the
// committed mi grid records a sub-8x8 SbType at this position (the deep
// recursion's fillVP9MiGrid leaf commit), retry the lookup at that real
// sub-8x8 bsize so the wrapper's committed bmi quartet is replayed verbatim
// (else the sub-8x8 model re-fill collapses the segment NEWMV/NEAREST MVs to
// ZEROMV). Off / replay-disabled returns a miss.
func (e *VP9Encoder) vp9LookupDeepInterRDDecisionForWrite(miRows, miCols,
	miRow, miCol int, bsize common.BlockSize,
) (vp9InterModeDecision, bool) {
	if !vp9InterUseDeepRDPartition || !vp9InterDeepRDReplayWrites {
		return vp9InterModeDecision{}, false
	}
	if d, ok := e.lookupVP9LeafInterRDDecision(miRow, miCol, bsize); ok {
		return d, true
	}
	if vp9InterUseDeepRDSub8x8 && bsize == common.Block8x8 {
		// The deep recursion committed exactly one leaf per (miRow, miCol): the
		// winning partition arm re-runs last, so the stored entry IS the committed
		// sub-8x8 leaf. The mode-info-footprint lookup above used BLOCK_8X8 (the
		// recon bsize that folds all sub-8x8 shapes), so it misses whenever the
		// committed shape is BLOCK_8X4 / BLOCK_4X8 (and even BLOCK_4X4 once the mi
		// grid is wiped between the count pre-pass and the write pass —
		// resetVP9EncoderCodingState zeroes e.miGrid, so reading mi.SbType is
		// unreliable). Recover the committed leaf at its OWN stored sub-8x8 bsize
		// directly from the entry instead of trusting the wiped mi grid.
		if d, sub, ok := e.peekVP9LeafInterRDDecisionSub8x8(miRow, miCol); ok &&
			sub < common.Block8x8 {
			return d, true
		}
	}
	return vp9InterModeDecision{}, false
}

// peekVP9LeafInterRDDecisionSub8x8 returns the committed leaf decision and its
// stored bsize for (miRow, miCol) regardless of a bsize-key match, used by the
// write descent to recover a sub-8x8 leaf when the BLOCK_8X8 mode-info lookup
// misses and the mi grid SbType is unreliable (wiped between passes). Only the
// current frame's version is honoured.
func (e *VP9Encoder) peekVP9LeafInterRDDecisionSub8x8(miRow, miCol int,
) (vp9InterModeDecision, common.BlockSize, bool) {
	if e.vp9LeafInterRDDecisionsCols <= 0 {
		return vp9InterModeDecision{}, common.BlockInvalid, false
	}
	if miRow < 0 || miCol < 0 ||
		miRow >= e.vp9LeafInterRDDecisionsRows ||
		miCol >= e.vp9LeafInterRDDecisionsCols {
		return vp9InterModeDecision{}, common.BlockInvalid, false
	}
	off := miRow*e.vp9LeafInterRDDecisionsCols + miCol
	if off < 0 || off >= len(e.vp9LeafInterRDDecisions) {
		return vp9InterModeDecision{}, common.BlockInvalid, false
	}
	entry := &e.vp9LeafInterRDDecisions[off]
	if !entry.valid || entry.version != e.vp9LeafInterRDDecisionsVer {
		return vp9InterModeDecision{}, common.BlockInvalid, false
	}
	return entry.decision, entry.bsize, true
}

// vp9LookupDeepInterPartition is the flag-gated read entry the writer's region
// picker uses to descend the deep-RD search's committed partition tree. A hit
// means pickVP9InterPartitionRD already decided this node; the writer returns the
// cached child size verbatim rather than re-deciding via
// pickVP9InterPartitionBlockSize (whose early-exits can diverge). The first
// visit per SB (the root, count pre-pass) misses, so the caller falls through
// and runs the search, which populates the whole subtree; every later visit
// hits. Off / replay-disabled returns a miss, restoring the re-decide path.
func (e *VP9Encoder) vp9LookupDeepInterPartition(miRow, miCol int,
	root common.BlockSize,
) (common.BlockSize, bool) {
	if !vp9InterUseDeepRDPartition || !vp9InterDeepRDReplayWrites {
		return common.BlockInvalid, false
	}
	return e.lookupVP9InterPartitionRDDecision(miRow, miCol, root)
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
