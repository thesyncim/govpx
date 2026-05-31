//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"image"
	"strings"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

type RuntimeControlCase struct {
	Name        string
	ApplyAt     int
	ScriptToken string
	ExtraArgs   []string
	Apply       func(testing.TB, *govpx.VP9Encoder)
}

func RunRuntimeControlCase(t testing.TB, opts govpx.VP9EncoderOptions,
	extraArgs []string, width, height, frames int, tc RuntimeControlCase,
) {
	t.Helper()
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	flags := make([]govpx.EncodeFlags, frames)

	before := func(enc *govpx.VP9Encoder, frame int) {
		if frame == tc.ApplyAt && tc.Apply != nil {
			tc.Apply(t, enc)
		}
	}

	govpxRows, govpxPackets := CaptureGovpxStreamParityPacketRowsWithHooks(t,
		opts, sources, flags, before)

	libvpxArgs := append([]string(nil), extraArgs...)
	libvpxArgs = append(libvpxArgs, tc.ExtraArgs...)
	script := RuntimeControlScript(frames, map[int]string{tc.ApplyAt: tc.ScriptToken})
	libvpxArgs = append(libvpxArgs, "--control-script="+strings.Join(script, ","))

	libvpxRows, libvpxPackets := CaptureLibvpxStreamParityPacketRows(t,
		sources, flags, libvpxArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows,
		RateTraceFlagMapper)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 runtime control %s: matches=%d/%d first_mismatch=%d stats=%s",
		tc.Name, matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 runtime control %s rows:\n%s",
		tc.Name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	if vp9test.StrictEnv("GOVPX_VP9_RUNTIME_CONTROLS_STRICT") {
		AssertRuntimeControlByteParity(t, tc.Name, govpxPackets, libvpxPackets)
	}
}

func RuntimeControlScript(frames int, updates map[int]string) []string {
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

func AssertRuntimeControlByteParity(t testing.TB, label string, got, want [][]byte) {
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
