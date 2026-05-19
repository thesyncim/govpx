//go:build !govpx_oracle_trace

package govpx

const vp9ARNRDebugBuild = false

func vp9ARNRDebugEnabled() bool { return false }

func vp9ARNRDebugf(string, ...any) {}
