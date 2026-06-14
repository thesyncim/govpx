//go:build !govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const vp9OracleTraceBuild = false

type vp9OracleTraceState struct{}

type vp9OracleTraceHolder struct{}

type vp9OracleFrameSummary struct{}

func (e *VP9Encoder) resetVP9OracleTraceState() {}

func (e *VP9Encoder) vp9OracleTraceEnabled() bool { return false }

func (e *VP9Encoder) resetVP9OracleRateSelectionTrace() {}

func (e *VP9Encoder) recordVP9FullRDFirstInterMv(int, int, int, int8, int, int) {}

func (e *VP9Encoder) vp9FullRDFirstInterMv() (int, int, bool) { return 0, 0, false }

func (e *VP9Encoder) recordVP9FullRDFirstInterSubpelMv(int, int, int, int8, int, int) {}

func (e *VP9Encoder) vp9FullRDFirstInterSubpelMv() (int, int, bool) { return 0, 0, false }

func (e *VP9Encoder) recordVP9FullRDInterYRD(int, int, int, vp9FullRDInterYRDResult) {}

func (e *VP9Encoder) vp9FullRDInterYRD() (vp9FullRDInterYRDResult, bool) {
	return vp9FullRDInterYRDResult{}, false
}

func (e *VP9Encoder) recordVP9FullRDInterThisRD(int, int, int, vp9FullRDInterThisRDResult) {
}

func (e *VP9Encoder) recordVP9FullRDSub8x8(vp9Sub8x8Capture) {}

func (e *VP9Encoder) vp9CapturedFullRDSub8x8() (vp9Sub8x8Capture, bool) {
	return vp9Sub8x8Capture{}, false
}

func (e *VP9Encoder) recordVP9Sub8x8WrapperCommit(int, int, int, vp9dec.InterpFilter) {}

func (e *VP9Encoder) vp9CapturedSub8x8WrapperCommit() (int, vp9dec.InterpFilter, bool) {
	return 0, 0, false
}

func (e *VP9Encoder) recordVP9Sub8x8InterCommit(vp9Sub8x8InterCapture) {}

func (e *VP9Encoder) vp9CapturedSub8x8InterMi72Commit() (vp9Sub8x8InterCapture, bool) {
	return vp9Sub8x8InterCapture{}, false
}

func (e *VP9Encoder) recordVP9Sub8x8IntraCommit(vp9Sub8x8IntraCapture) {}

func (e *VP9Encoder) vp9CapturedSub8x8IntraCommit() (vp9Sub8x8IntraCapture, bool) {
	return vp9Sub8x8IntraCapture{}, false
}

func (e *VP9Encoder) vp9CapturedFullRDInterThisRD() (vp9FullRDInterThisRDResult, bool) {
	return vp9FullRDInterThisRDResult{}, false
}

func (e *VP9Encoder) recordVP9OracleRateSelectionTrace(int, int, float64, bool, int) {
}

func (e *VP9Encoder) vp9OracleRateSelectionTrace() (int, int, float64, bool, int) {
	return 0, 0, 0, false, 0
}

func (e *VP9Encoder) emitVP9OracleDroppedFrameTrace(EncodeFlags, uint32, uint32, temporalFrame, vp9DropReason) {
}

func (e *VP9Encoder) emitVP9OracleEncodedFrameTrace(int, EncodeFlags, *vp9dec.UncompressedHeader, int, int, bool, VP9EncodeResult, int) {
}

func (e *VP9Encoder) emitVP9OracleFrameTrace(vp9OracleFrameSummary) {}

func (e *VP9Encoder) vp9TraceCommitBlock(int, int, int, *vp9dec.NeighborMi, common.PredictionMode) {
}

func (e *VP9Encoder) vp9TraceCommitBlockPre(int, int, int, *vp9dec.NeighborMi, common.PredictionMode) {
}
