//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzEncoderReferenceControlSequences closes plan-§3 F8 / G10 from
// the VP8 byte-exactness tracker: arbitrary sequences of
// SetReferenceFrame / CopyReferenceFrame / EncodeFlags ref-update bits
// are scheduled per-frame and the encoded bytes must match the
// libvpx vpxenc-frameflags driver driven through the same script.
//
// The existing vp8_oracle_encoder_copy_reference_parity_test.go covers
// hand-picked sequences; FuzzOracleEncoderRuntimeControlTransitions
// covers ref-control as one action among many. This fuzzer is
// focused exclusively on the SET / COPY / ref-update permutation
// space, so its seed corpus stays tightly scoped to that surface
// without competing with the broader runtime-control fuzzer for
// regression-coverage attention.
func FuzzEncoderReferenceControlSequences(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run ref-control sequence fuzz")
	}
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},                 // all-default
		{0, 1, 0, 2, 0, 3, 0, 0},                 // SET last/golden/altref
		{4, 0, 5, 0, 6, 0, 0, 0},                 // COPY last/golden/altref
		{1, 2, 3, 4, 5, 6, 0, 0},                 // mixed SET+COPY
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, // long mixed sequence
		{0, 7, 0, 8, 0, 9, 0, 10},                // EFLAG NoUpdate variants
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := coracletest.VpxencFrameFlags(t)
		tc := newRefControlsFuzzCase(data)
		sum := sha256.Sum256(data)
		label := "fuzz-refctrl-" + hex.EncodeToString(sum[:4])
		t.Logf("%s script=%s", label, strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := []string{
			"--copy-ref-log=" + filepath.Join(t.TempDir(), "copy-reference.log"),
			"--control-script=" + strings.Join(tc.script, ","),
		}
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// newRefControlsFuzzCase generates a per-frame schedule that mixes
// SET, COPY, and EncodeFlags ref-update operations. Each fuzz byte
// past the prefix picks one operation kind from the focused pool.
func newRefControlsFuzzCase(data []byte) oracleRuntimeControlFuzzCase {
	r := testutil.NewByteCursor(data)
	const (
		width   = 64
		height  = 64
		bitrate = 700
	)
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.Pick(len(framesPool))]
	opts := oracleRuntimeBaseFuzzOptions(width, height, bitrate, 0)
	sources := oracleRuntimeFuzzSources(width, height, frames, 0)
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)

	refNames := [...]string{"last", "golden", "altref"}
	refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}

	for frame := 1; frame < frames; frame++ {
		switch r.Pick(11) {
		case 0:
			// No-op frame.
		case 1, 2, 3:
			// SetReferenceFrame.
			idx := r.Pick(len(refs))
			imageIndex := 8 + r.Pick(8)
			script[frame] = "setref:" + refNames[idx] + ":panning:" + strconv.Itoa(imageIndex)
			apply[frame] = setReferencePanningApply(refs[idx], imageIndex, refNames[idx])
		case 4, 5, 6:
			// CopyReferenceFrame.
			idx := r.Pick(len(refs))
			script[frame] = "copyref:" + refNames[idx]
			capturedIdx := idx
			capturedName := refNames[idx]
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dst := newTestImage(e.opts.Width, e.opts.Height)
				mustRuntime(t, "CopyReferenceFrame("+capturedName+")", e.CopyReferenceFrame(refs[capturedIdx], &dst))
			}
		case 7:
			flags[frame] |= EncodeNoUpdateLast
		case 8:
			flags[frame] |= EncodeNoUpdateGolden
		case 9:
			flags[frame] |= EncodeNoUpdateAltRef
		case 10:
			flags[frame] |= EncodeNoReferenceLast | EncodeNoUpdateLast
		}
	}

	return oracleRuntimeControlFuzzCase{
		name:       "refctrl",
		opts:       opts,
		targetKbps: bitrate,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: true,
	}
}
