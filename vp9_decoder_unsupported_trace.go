//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
)

func (d *VP9Decoder) traceVP9Unsupported(reason string) {
	if os.Getenv("GOVPX_TRACE_UNSUPPORTED") != "" {
		fmt.Fprintf(os.Stderr, "vp9 unsupported: %s\n", reason)
	}
}
