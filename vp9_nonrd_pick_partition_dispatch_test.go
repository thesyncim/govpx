package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestVP9NonrdPickPartitionEnvGate confirms the env-gated dispatch wired
// into pickVP9InterPartitionBlockSize matches the documented contract:
// vp9NonrdPickPartitionEnabled() reflects the cached
// GOVPX_VP9_NONRD_PICK_PARTITION value resolved at package init.
//
// libvpx: vp9/encoder/vp9_encodeframe.c:4598-4855 nonrd_pick_partition body
// with use_ml_based_partitioning=1 (libvpx vp9_encodeframe.c:4627-4628).
func TestVP9NonrdPickPartitionEnvGate(t *testing.T) {
	// The env value is resolved at init time so we can only observe the
	// resolved cache here. Tests that need to flip the gate at runtime
	// would have to thread an explicit override into
	// vp9NonrdPickPartitionEnabled.
	got := vp9NonrdPickPartitionEnabled()
	if got != vp9NonrdPickPartitionOptIn {
		t.Errorf("vp9NonrdPickPartitionEnabled() = %v, want cached %v",
			got, vp9NonrdPickPartitionOptIn)
	}
}

// TestVP9NonrdPickPartitionSplitSize confirms vp9MLSplitSize maps each
// ML-eligible parent bsize to its libvpx subsize_lookup
// (vp9/common/vp9_common_data.c subsize_lookup[PARTITION_SPLIT]).
func TestVP9NonrdPickPartitionSplitSize(t *testing.T) {
	cases := []struct {
		in   common.BlockSize
		want common.BlockSize
		ok   bool
	}{
		{common.Block64x64, common.Block32x32, true},
		{common.Block32x32, common.Block16x16, true},
		{common.Block16x16, common.Block8x8, true},
		{common.Block8x8, common.BlockInvalid, false},
	}
	for _, tc := range cases {
		got, ok := vp9MLSplitSize(tc.in)
		if ok != tc.ok {
			t.Errorf("vp9MLSplitSize(%v) ok = %v, want %v", tc.in, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("vp9MLSplitSize(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
