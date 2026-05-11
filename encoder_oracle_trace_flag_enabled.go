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

	mbBuffer             []oracleTraceMBRow
	interCandidateBuffer []oracleTraceInterCandidateRow
	recodeLoopCount      int
	recodeReason         string
	totalByteCount       int64
}

var oracleTraceStates sync.Map

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

func (e *VP8Encoder) SetOracleTracePredictorDump(enabled bool, allRows bool) {
	if e == nil {
		return
	}
	state := e.oracleTraceStateCreate()
	state.predictorDump = enabled
	state.predictorDumpAllRows = allRows
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
