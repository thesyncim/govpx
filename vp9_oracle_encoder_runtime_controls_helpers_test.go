//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strings"
	"testing"
)

// vp9RuntimeControlCase describes one runtime-control scenario for the
// byte-parity gate above.
type vp9RuntimeControlCase struct {
	name      string
	applyAt   int      // frame index at which apply runs (govpx side)
	scriptTok string   // libvpx-side control-script token applied at the same frame
	extraArgs []string // optional extra libvpx CLI args
	apply     func(*testing.T, *VP9Encoder)
}

// runVP9RuntimeControlCase encodes `frames` frames with the govpx VP9 encoder
// while applying tc.apply at frame tc.applyAt, then runs the libvpx oracle
// with the matching --control-script= entry and compares both trace rows
// and raw packet bytes. The byte-parity assertion mirrors the VP8 runtime-
// controls gate: every visible packet must match libvpx byte-for-byte. Test
// failure logs the row-level trace so regressions are easy to triage.
func runVP9RuntimeControlCase(t *testing.T, opts VP9EncoderOptions,
	extraArgs []string, width, height, frames int, tc vp9RuntimeControlCase,
) {
	t.Helper()
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	flags := make([]EncodeFlags, frames)

	before := func(enc *VP9Encoder, frame int) {
		if frame == tc.applyAt && tc.apply != nil {
			tc.apply(t, enc)
		}
	}

	govpxRows, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
		opts, sources, flags, before)

	libvpxArgs := append([]string(nil), extraArgs...)
	libvpxArgs = append(libvpxArgs, tc.extraArgs...)
	script := vp9RuntimeControlScript(frames, map[int]string{tc.applyAt: tc.scriptTok})
	libvpxArgs = append(libvpxArgs, "--control-script="+strings.Join(script, ","))

	libvpxRows, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
		sources, flags, libvpxArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 runtime control %s: matches=%d/%d first_mismatch=%d stats=%s",
		tc.name, matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 runtime control %s rows:\n%s",
		tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_CONTROLS_STRICT") {
		assertVP9RuntimeControlByteParity(t, tc.name, govpxPackets, libvpxPackets)
	}
}

// vp9RuntimeControlScript builds the per-frame --control-script CSV that the
// libvpx vpxenc-vp9-frameflags driver consumes. Frames not listed in updates
// emit "-" so the driver leaves the live config alone.
func vp9RuntimeControlScript(frames int, updates map[int]string) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	for frame, update := range updates {
		if frame >= 0 && frame < frames && update != "" {
			script[frame] = update
		}
	}
	return script
}

// assertVP9RuntimeControlByteParity asserts every visible packet matches
// libvpx byte-for-byte and that drop classifications agree. It is reached
// only under GOVPX_VP9_RUNTIME_CONTROLS_STRICT=1 because the broader VP9
// runtime-control surface is still being pinned.
func assertVP9RuntimeControlByteParity(t *testing.T, label string,
	got, want [][]byte,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("VP9 runtime control %s packet count: got=%d want=%d",
			label, len(got), len(want))
	}
	for i := range got {
		gotEmpty := len(got[i]) == 0
		wantEmpty := len(want[i]) == 0
		if gotEmpty != wantEmpty {
			t.Errorf("VP9 runtime control %s frame %d drop mismatch: got_empty=%t want_empty=%t",
				label, i, gotEmpty, wantEmpty)
			continue
		}
		if gotEmpty {
			continue
		}
		if !bytes.Equal(got[i], want[i]) {
			diff := testutil.FirstByteDiff(got[i], want[i])
			t.Errorf("VP9 runtime control %s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d",
				label, i, len(got[i]), len(want[i]), diff)
		}
	}
}
