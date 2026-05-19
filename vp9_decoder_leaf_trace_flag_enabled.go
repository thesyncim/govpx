//go:build govpx_oracle_trace

package govpx

const vp9DecodedLeafTraceBuild = true

type vp9DecodedLeafTraceState struct {
	rows []vp9DecodedLeafTrace
}

func (d *VP9Decoder) enableVP9DecodedLeafTrace() {
	if d == nil {
		return
	}
	if d.leafTrace == nil {
		d.leafTrace = &vp9DecodedLeafTraceState{}
	}
}

func (d *VP9Decoder) disableVP9DecodedLeafTrace() {
	if d == nil {
		return
	}
	d.leafTrace = nil
}

func (d *VP9Decoder) resetVP9DecodedLeafTrace() {
	if d == nil || d.leafTrace == nil {
		return
	}
	d.leafTrace.rows = d.leafTrace.rows[:0]
}

func (d *VP9Decoder) vp9DecodedLeafTraceActive() bool {
	if d == nil {
		return false
	}
	return d.leafTrace != nil
}

func (d *VP9Decoder) vp9DecodedLeafTraceRows() []vp9DecodedLeafTrace {
	if d == nil || d.leafTrace == nil {
		return nil
	}
	rows := d.leafTrace.rows
	return append([]vp9DecodedLeafTrace(nil), rows...)
}

func (d *VP9Decoder) emitVP9DecodedLeafTrace(row vp9DecodedLeafTrace) {
	if d == nil || d.leafTrace == nil {
		return
	}
	d.leafTrace.rows = append(d.leafTrace.rows, row)
}
