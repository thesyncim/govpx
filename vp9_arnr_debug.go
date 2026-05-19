//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"os"
	"sync"
)

const vp9ARNRDebugBuild = true

var (
	vp9ARNRDebugOnce sync.Once
	vp9ARNRDebugFlag bool
)

func vp9ARNRDebugEnabled() bool {
	vp9ARNRDebugOnce.Do(func() {
		vp9ARNRDebugFlag = os.Getenv("GOVPX_VP9_ARNR_DEBUG") == "1"
	})
	return vp9ARNRDebugFlag
}

func vp9ARNRDebugf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
