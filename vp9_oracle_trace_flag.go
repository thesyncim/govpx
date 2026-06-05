//go:build !govpx_oracle_trace

package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

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
