//go:build govpx_oracle_trace

package govpx

import (
	"io"
	"sync"
)

const oracleTraceBuild = true

type oracleTraceState struct {
	writer               io.Writer
	predictorDump        bool
	predictorDumpAllRows bool
	// pretrellisUVDump, when true, enables emission of
	// {"type":"pretrellis_uv_qcoeff",...} rows from the per-MB UV
	// quantize loop on the accepted-path encode. Mirrors the libvpx-side
	// GOVPX_ORACLE_PRETRELLIS_UV environment-variable gate; both sides
	// are off by default so the per-frame trace size stays bounded.
	pretrellisUVDump bool

	// chromaOptimizeBDump, when true, enables emission of
	// {"type":"chroma_optimize_b",...} rows from the per-MB UV optimize
	// trellis on the accepted-path encode (one row per UV 4x4 block
	// 16..23). Mirrors GOVPX_ORACLE_CHROMA_OPTIMIZE_B on the libvpx side;
	// used to bisect post-trellis ±1 DC keep/drop divergences identified
	// by task #314 between govpx and libvpx.
	chromaOptimizeBDump bool

	mbBuffer             []oracleTraceMBRow
	interCandidateBuffer []oracleTraceInterCandidateRow
	recodeLoopCount      int
	recodeReason         string
	totalByteCount       int64
}

var oracleTraceStates sync.Map

// SetOracleTraceWriter enables oracle trace emission for this encoder. It is
// available only in govpx_oracle_trace builds.
func (e *VP8Encoder) SetOracleTraceWriter(w io.Writer) {
	if e == nil {
		return
	}
	if w == nil {
		oracleTraceStates.Delete(e)
		return
	}
	state := e.oracleTraceStateCreate()
	state.writer = w
}

// SetOracleTracePredictorDump enables predictor-plane rows in oracle traces.
// It is available only in govpx_oracle_trace builds.
func (e *VP8Encoder) SetOracleTracePredictorDump(enabled bool, allRows bool) {
	if e == nil {
		return
	}
	state := e.oracleTraceStateCreate()
	state.predictorDump = enabled
	state.predictorDumpAllRows = allRows
}

// SetOracleTracePretrellisUVDump enables per-UV-block pre-trellis
// qcoeff/dqcoeff/coeff rows in oracle traces. Mirrors the libvpx-side
// GOVPX_ORACLE_PRETRELLIS_UV environment-variable gate. Available only in
// govpx_oracle_trace builds. Each enabled MB emits 8 rows (one per UV 4x4
// block 16..23), so callers should restrict the recipient stream when
// running on larger fixtures.
func (e *VP8Encoder) SetOracleTracePretrellisUVDump(enabled bool) {
	if e == nil {
		return
	}
	state := e.oracleTraceStateCreate()
	state.pretrellisUVDump = enabled
}

// SetOracleTraceChromaOptimizeBDump enables per-UV-block post-trellis
// qcoeff/dqcoeff/dequant/coeff rows in oracle traces. Mirrors the
// libvpx-side GOVPX_ORACLE_CHROMA_OPTIMIZE_B environment-variable gate.
// Available only in govpx_oracle_trace builds. Each enabled MB emits 8
// rows (one per UV 4x4 block 16..23); callers should restrict the
// recipient stream when running on larger fixtures.
func (e *VP8Encoder) SetOracleTraceChromaOptimizeBDump(enabled bool) {
	if e == nil {
		return
	}
	state := e.oracleTraceStateCreate()
	state.chromaOptimizeBDump = enabled
}

func (e *VP8Encoder) oracleTraceState() *oracleTraceState {
	if e == nil {
		return nil
	}
	if state, ok := oracleTraceStates.Load(e); ok {
		return state.(*oracleTraceState)
	}
	return nil
}

func (e *VP8Encoder) oracleTraceStateCreate() *oracleTraceState {
	if state, ok := oracleTraceStates.Load(e); ok {
		return state.(*oracleTraceState)
	}
	state := &oracleTraceState{}
	actual, _ := oracleTraceStates.LoadOrStore(e, state)
	return actual.(*oracleTraceState)
}

func (e *VP8Encoder) resetOracleTraceState() {
	oracleTraceStates.Delete(e)
}

func (e *VP8Encoder) resetOracleTraceRecode() {
	if state := e.oracleTraceState(); state != nil {
		state.recodeLoopCount = 0
		state.recodeReason = ""
	}
}

func (e *VP8Encoder) incrementOracleTraceRecodeLoop() {
	if state := e.oracleTraceState(); state != nil {
		state.recodeLoopCount++
	}
}

func (e *VP8Encoder) setOracleTraceRecodeReason(reason string) {
	if state := e.oracleTraceState(); state != nil {
		state.recodeReason = reason
	}
}

func (e *VP8Encoder) oracleTraceRecodeLoopCountForTest() int {
	if state := e.oracleTraceState(); state != nil {
		return state.recodeLoopCount
	}
	return 0
}

func (e *VP8Encoder) oracleTraceMBBufferLenForTest() int {
	if state := e.oracleTraceState(); state != nil {
		return len(state.mbBuffer)
	}
	return 0
}
