//go:build govpx_oracle_trace

package govpx

import (
	"sync/atomic"
)

// Task #160 build-tag-gated probe counters: confirm vp9SearchFilterRef
// invocation reachability and flip-rate on the {0x32} RuntimeControls seed.
// In non-trace builds these symbols don't exist; the helper below compiles
// to a no-op via the alternate file vp9_search_filter_ref_probe_noop.go.
//
// Reset to 0 by test setup; ProbeVP9SearchFilterRefFires() reports current
// counts.
var (
	vp9SearchFilterRefFiresCount atomic.Uint64
	vp9SearchFilterRefFlipsCount atomic.Uint64
)

func vp9SearchFilterRefProbeFire() {
	vp9SearchFilterRefFiresCount.Add(1)
}

func vp9SearchFilterRefProbeFlip() {
	vp9SearchFilterRefFlipsCount.Add(1)
}

// ProbeVP9SearchFilterRefFires returns (fires, flips) since reset. Used by
// the seed #8 diagnostic test.
func ProbeVP9SearchFilterRefFires() (uint64, uint64) {
	return vp9SearchFilterRefFiresCount.Load(),
		vp9SearchFilterRefFlipsCount.Load()
}

// ResetVP9SearchFilterRefProbes zeroes the counters. Used by tests for
// per-encode isolation.
func ResetVP9SearchFilterRefProbes() {
	vp9SearchFilterRefFiresCount.Store(0)
	vp9SearchFilterRefFlipsCount.Store(0)
}
