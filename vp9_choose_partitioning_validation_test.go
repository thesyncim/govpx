//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9ChoosePartitioningGateDeferredSeeds drives the formerly-deferred
// FuzzVP9EncoderReferenceControlSequences seeds end-to-end under the same
// pipeline the fuzz uses, so we can see per-seed PASS/FAIL without going
// through go test fuzz seed-name mangling.
//
// This is a manual validation harness for the libvpx choose_partitioning port.
// It remains gated behind GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE=1 because it
// requires the external libvpx oracle, but the encoder path itself now runs by
// default through sf.PartitionSearchType == VAR_BASED_PARTITION.
func TestVP9ChoosePartitioningGateDeferredSeeds(t *testing.T) {
	if os.Getenv("GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE") != "1" {
		t.Skip("set GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE=1 to run the Phase C validation harness")
	}
	coracletest.SkipWithoutOracle(t, "VP9 choose_partitioning validation")
	coracletest.VpxencVP9FrameFlags(t)
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
			label := fmt.Sprintf("phaseC-validate-%s-%s", s.name, hex.EncodeToString(sum[:4]))
			govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
			libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
			vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
		})
	}
}
