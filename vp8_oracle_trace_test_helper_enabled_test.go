//go:build govpx_oracle_trace

package govpx

import "io"

func enableOracleTraceForTest(e *VP8Encoder) {
	e.SetOracleTraceWriter(io.Discard)
}
