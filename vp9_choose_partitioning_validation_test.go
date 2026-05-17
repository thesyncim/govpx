//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

// TestVP9ChoosePartitioningGateDeferredSeeds drives each of the deferred
// FuzzVP9EncoderReferenceControlSequences seeds end-to-end under the same
// pipeline the fuzz uses, so we can see per-seed PASS/FAIL under the gate
// without going through go test fuzz seed-name mangling.
//
// This is a manual validation harness for Phase C of the libvpx
// choose_partitioning port — gated behind GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE=1
// so it never runs in the default CI surface (the picker is still opt-in
// behind GOVPX_VP9_LIBVPX_CHOOSE_PARTITIONING=1 and currently produces a
// divergent partition tree). Re-enable when the wiring is byte-aligned with
// libvpx's nonrd_use_partition (vp9_encodeframe.c:5470).
func TestVP9ChoosePartitioningGateDeferredSeeds(t *testing.T) {
	if os.Getenv("GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE") != "1" {
		t.Skip("set GOVPX_VP9_CHOOSE_PARTITIONING_VALIDATE=1 to run the Phase C validation harness")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 choose_partitioning validation")
	}
	requireVP9VpxencFrameFlagsOracle(t)
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
	}
	for _, s := range seeds {
		s := s
		t.Run(s.name, func(t *testing.T) {
			tc := newVP9RefControlsFuzzCase(s.data)
			sum := sha256.Sum256(s.data)
			label := fmt.Sprintf("phaseC-validate-%s-%s", s.name, hex.EncodeToString(sum[:4]))
			govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
			libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
			assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
		})
	}
}
