//go:build !govpx_oracle_trace

package govpx

// In production builds the search_filter_ref probe helpers compile to no-ops.
// The trace-build counterpart in vp9_search_filter_ref_probe.go is gated
// behind govpx_oracle_trace and used only by the seed #8 diagnostic test.

const vp9SearchFilterRefProbeBuild = false

func vp9SearchFilterRefProbeFire() {}

func vp9SearchFilterRefProbeFlip() {}
