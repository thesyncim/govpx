//go:build !govpx_oracle_trace

package govpx

const vp9DecodedLeafTraceBuild = false

type vp9DecodedLeafTraceState struct{}

func (d *VP9Decoder) disableVP9DecodedLeafTrace() {}

func (d *VP9Decoder) resetVP9DecodedLeafTrace() {}

func (d *VP9Decoder) vp9DecodedLeafTraceActive() bool { return false }

func (d *VP9Decoder) emitVP9DecodedLeafTrace(vp9DecodedLeafTrace) {}
