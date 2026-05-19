//go:build govpx_oracle_trace

package govpx

import "sync"

const vp9DecodedLeafTraceBuild = true

type vp9DecodedLeafTraceState struct {
	rows []vp9DecodedLeafTrace
}

var vp9DecodedLeafTraceStates sync.Map

func (d *VP9Decoder) enableVP9DecodedLeafTrace() {
	if d == nil {
		return
	}
	vp9DecodedLeafTraceStates.LoadOrStore(d, &vp9DecodedLeafTraceState{})
}

func (d *VP9Decoder) disableVP9DecodedLeafTrace() {
	if d == nil {
		return
	}
	vp9DecodedLeafTraceStates.Delete(d)
}

func (d *VP9Decoder) resetVP9DecodedLeafTrace() {
	if d == nil {
		return
	}
	state, ok := vp9DecodedLeafTraceStates.Load(d)
	if !ok {
		return
	}
	trace := state.(*vp9DecodedLeafTraceState)
	trace.rows = trace.rows[:0]
}

func (d *VP9Decoder) vp9DecodedLeafTraceActive() bool {
	if d == nil {
		return false
	}
	_, ok := vp9DecodedLeafTraceStates.Load(d)
	return ok
}

func (d *VP9Decoder) vp9DecodedLeafTraceRows() []vp9DecodedLeafTrace {
	if d == nil {
		return nil
	}
	state, ok := vp9DecodedLeafTraceStates.Load(d)
	if !ok {
		return nil
	}
	rows := state.(*vp9DecodedLeafTraceState).rows
	return append([]vp9DecodedLeafTrace(nil), rows...)
}

func (d *VP9Decoder) emitVP9DecodedLeafTrace(row vp9DecodedLeafTrace) {
	if d == nil {
		return
	}
	state, ok := vp9DecodedLeafTraceStates.Load(d)
	if !ok {
		return
	}
	trace := state.(*vp9DecodedLeafTraceState)
	trace.rows = append(trace.rows, row)
}
