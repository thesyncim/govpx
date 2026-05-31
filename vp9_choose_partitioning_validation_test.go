//go:build govpx_oracle_trace

package govpx_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

// TestVP9ChoosePartitioningReferenceControlSeedsMatchLibvpx checks the
// reference-control seed set against the libvpx frame-flags oracle with
// choose_partitioning active.
func TestVP9ChoosePartitioningReferenceControlSeedsMatchLibvpx(t *testing.T) {
	vp9test.RequireEnvFlag(t, "GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE", "choose_partitioning seed parity")
	vp9test.RequireOracle(t, "VP9 choose_partitioning validation")
	vp9test.RequireVpxencFrameFlags(t)
	type seedSpec struct {
		name string
		data []byte
	}
	seeds := []seedSpec{
		{"baseline_zero", []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{"baseline_0to3", []byte{0, 1, 0, 2, 0, 3, 0, 0}},
		{"baseline_1to6", []byte{1, 2, 3, 4, 5, 6, 0, 0}},
		{"baseline_4to7", []byte{0, 4, 0, 5, 0, 6, 0, 7}},
		{"baseline_ff_seq", []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"baseline_7to10", []byte{0, 7, 0, 8, 0, 9, 0, 10}},
		{"regression_582528dd", []byte("0")},
		{"regression_916d1b27", []byte("1")},
		{"regression_2fde656d", []byte("2")},
		{"regression_6573b9b5", []byte("7")},
	}
	for _, s := range seeds {
		s := s
		t.Run(s.name, func(t *testing.T) {
			tc := newVP9RefControlsFuzzCase(s.data)
			sum := sha256.Sum256(s.data)
			label := fmt.Sprintf("choose-partitioning-%s-%s", s.name, hex.EncodeToString(sum[:4]))
			govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
			libvpxFrames := vp9test.VpxencFrameFlagPackets(t, tc.sources,
				vp9oracle.LibvpxFrameFlags(tc.flags), tc.extraArgs...)
			vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
		})
	}
}
