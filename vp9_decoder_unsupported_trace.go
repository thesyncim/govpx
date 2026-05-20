//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
)

var vp9TraceUnsupportedEnabled = os.Getenv("GOVPX_TRACE_UNSUPPORTED") != ""

func (d *VP9Decoder) traceVP9Unsupported(reason string) {
	if vp9TraceUnsupportedEnabled {
		fmt.Fprintf(os.Stderr, "vp9 unsupported: %s\n", reason)
	}
}
