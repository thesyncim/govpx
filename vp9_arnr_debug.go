//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
)

const vp9ARNRDebugBuild = true

var vp9ARNRDebugFlag = os.Getenv("GOVPX_VP9_ARNR_DEBUG") == "1"

func vp9ARNRDebugEnabled() bool {
	return vp9ARNRDebugFlag
}

func vp9ARNRDebugf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
