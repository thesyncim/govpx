package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func (e *VP9Encoder) ensureVP9VarPartCopyState(miRows, miCols int) bool {
	if e == nil || miRows <= 0 || miCols <= 0 {
		return false
	}
	miStride := encoder.CalcMiSize(miCols)
	partitionLen := miStride * miRows
	sbCount := encoder.ContentStateBufferSize(miStride, miRows)
	if partitionLen <= 0 || sbCount <= 0 {
		return false
	}
	sameDims := e.varPartPrevPartitionMiRows == miRows &&
		e.varPartPrevPartitionMiCols == miCols &&
		e.varPartPrevPartitionMiStride == miStride
	e.varPartPrevPartition = buffers.EnsureLen(e.varPartPrevPartition, partitionLen)
	e.varPartPrevPartitionValid = buffers.EnsureLen(e.varPartPrevPartitionValid, sbCount)
	e.varPartPrevSegmentID = buffers.EnsureLen(e.varPartPrevSegmentID, sbCount)
	e.varPartPrevVarianceLow = buffers.EnsureLen(e.varPartPrevVarianceLow, sbCount)
	e.varPartCopiedFrameCnt = buffers.EnsureLen(e.varPartCopiedFrameCnt, sbCount)
	if !sameDims {
		clear(e.varPartPrevPartition)
		clear(e.varPartPrevPartitionValid)
		clear(e.varPartPrevSegmentID)
		clear(e.varPartPrevVarianceLow)
		clear(e.varPartCopiedFrameCnt)
		e.varPartPrevPartitionMiRows = miRows
		e.varPartPrevPartitionMiCols = miCols
		e.varPartPrevPartitionMiStride = miStride
	}
	return true
}

func (e *VP9Encoder) vp9VarPartCopySBOffset(miRow, miCol int) int {
	if e == nil || e.varPartPrevPartitionMiStride <= 0 ||
		miRow < 0 || miCol < 0 {
		return -1
	}
	return (e.varPartPrevPartitionMiStride>>3)*(miRow>>3) + (miCol >> 3)
}

func (e *VP9Encoder) vp9CommitVarPartSBPartitionState(miRows, miCols, miRow, miCol int,
	inter *vp9InterEncodeState,
) {
	if e == nil || inter == nil || inter.counts != nil ||
		e.varPartCopyCommitDisabled || e.sf.CopyPartitionFlag == 0 ||
		!e.varPartFrameValid {
		return
	}
	sbMiRow := (miRow >> 3) << 3
	sbMiCol := (miCol >> 3) << 3
	sbIdx := e.vp9ChoosePartitioningSBIndex(miCols, sbMiRow, sbMiCol)
	if sbIdx < 0 || sbIdx >= len(e.varPartSBComputed) ||
		!e.varPartSBComputed[sbIdx] {
		return
	}
	if !e.ensureVP9VarPartCopyState(miRows, miCols) {
		return
	}
	sbOffset := e.vp9VarPartCopySBOffset(sbMiRow, sbMiCol)
	if sbOffset < 0 || sbOffset >= len(e.varPartCopiedFrameCnt) ||
		sbOffset >= len(e.varPartPrevPartitionValid) ||
		sbOffset >= len(e.varPartPrevSegmentID) ||
		sbOffset >= len(e.varPartPrevVarianceLow) {
		return
	}
	if sbIdx < len(e.varPartSBCopiedPartition) && e.varPartSBCopiedPartition[sbIdx] {
		if e.varPartCopiedFrameCnt[sbOffset] < 255 {
			e.varPartCopiedFrameCnt[sbOffset]++
		}
		return
	}
	if !encoder.UpdatePrevPartitionFromGrid(e.varPartPrevPartition,
		e.varPartPrevPartitionMiStride, e.varPartGrid, miRows, miCols,
		sbMiRow, sbMiCol) {
		return
	}
	if sbIdx < len(e.varPartSBSegmentID) {
		e.varPartPrevSegmentID[sbOffset] = e.varPartSBSegmentID[sbIdx]
	} else {
		e.varPartPrevSegmentID[sbOffset] = encoder.CyclicRefreshSegmentBase
	}
	if sbIdx < len(e.varPartSBVarLow) {
		e.varPartPrevVarianceLow[sbOffset] = e.varPartSBVarLow[sbIdx]
	} else {
		e.varPartPrevVarianceLow[sbOffset] = [25]uint8{}
	}
	e.varPartPrevPartitionValid[sbOffset] = true
	e.varPartCopiedFrameCnt[sbOffset] = 0
}

func (e *VP9Encoder) saveVP9VarPartCopyStateForPostDrop() bool {
	if e == nil || len(e.varPartPrevPartition) == 0 {
		return false
	}
	e.varPartPrevPartitionSnap = buffers.EnsureLen(e.varPartPrevPartitionSnap,
		len(e.varPartPrevPartition))
	copy(e.varPartPrevPartitionSnap, e.varPartPrevPartition)
	e.varPartPrevPartitionValidSnap = buffers.EnsureLen(e.varPartPrevPartitionValidSnap,
		len(e.varPartPrevPartitionValid))
	copy(e.varPartPrevPartitionValidSnap, e.varPartPrevPartitionValid)
	e.varPartPrevSegmentIDSnap = buffers.EnsureLen(e.varPartPrevSegmentIDSnap,
		len(e.varPartPrevSegmentID))
	copy(e.varPartPrevSegmentIDSnap, e.varPartPrevSegmentID)
	e.varPartPrevVarianceLowSnap = buffers.EnsureLen(e.varPartPrevVarianceLowSnap,
		len(e.varPartPrevVarianceLow))
	copy(e.varPartPrevVarianceLowSnap, e.varPartPrevVarianceLow)
	e.varPartCopiedFrameCntSnap = buffers.EnsureLen(e.varPartCopiedFrameCntSnap,
		len(e.varPartCopiedFrameCnt))
	copy(e.varPartCopiedFrameCntSnap, e.varPartCopiedFrameCnt)
	return true
}

func (e *VP9Encoder) restoreVP9VarPartCopyStateAfterPostDrop(saved bool) {
	if e == nil || !saved {
		return
	}
	e.varPartPrevPartition = buffers.EnsureLen(e.varPartPrevPartition,
		len(e.varPartPrevPartitionSnap))
	copy(e.varPartPrevPartition, e.varPartPrevPartitionSnap)
	e.varPartPrevPartitionValid = buffers.EnsureLen(e.varPartPrevPartitionValid,
		len(e.varPartPrevPartitionValidSnap))
	copy(e.varPartPrevPartitionValid, e.varPartPrevPartitionValidSnap)
	e.varPartPrevSegmentID = buffers.EnsureLen(e.varPartPrevSegmentID,
		len(e.varPartPrevSegmentIDSnap))
	copy(e.varPartPrevSegmentID, e.varPartPrevSegmentIDSnap)
	e.varPartPrevVarianceLow = buffers.EnsureLen(e.varPartPrevVarianceLow,
		len(e.varPartPrevVarianceLowSnap))
	copy(e.varPartPrevVarianceLow, e.varPartPrevVarianceLowSnap)
	e.varPartCopiedFrameCnt = buffers.EnsureLen(e.varPartCopiedFrameCnt,
		len(e.varPartCopiedFrameCntSnap))
	copy(e.varPartCopiedFrameCnt, e.varPartCopiedFrameCntSnap)
}
