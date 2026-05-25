//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzVP8OracleEncoderRuntimeControlTransitions compares generated runtime-control
// schedules against the libvpx frame-flags driver. Go writes failing fuzz inputs
// to testdata/fuzz/FuzzVP8OracleEncoderRuntimeControlTransitions, and those corpus
// files are replayed by ordinary go test runs as regression tests.
func FuzzVP8OracleEncoderRuntimeControlTransitions(f *testing.F) {
	vp8test.RequireOracleF(f, "runtime-control fuzz parity")
	seeds := [][]byte{
		{0, 0, 1, 2, 3, 4, 5, 6, 7, 8},
		{0, 2, 7, 7, 7, 3, 5, 1, 4, 6, 8},
		{0, 4, 3, 4, 8, 2, 2, 5, 6, 7, 1},
		{1, 0, 0, 0},
		{2, 0, 1, 2, 3, 4, 5, 6},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := vp8test.VpxencFrameFlags(t)
		tc := vp8OracleRuntimeControlFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s script=%s", label, strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := append([]string(nil), tc.extraArgs...)
		if tc.copyRefLog {
			extraArgs = append(extraArgs, "--copy-ref-log="+filepath.Join(t.TempDir(), "copy-reference.log"))
		}
		extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames,
			vp8OracleRuntimeControlFuzzMatchLimit(t.Name()))
	})
}
