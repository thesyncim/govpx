//go:build govpx_oracle_trace

package govpx_test

import (
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleFrameFlagTransitionsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 frame-flag transitions")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 8
	opts := vp9oracle.CBROptions(width, height, 600)
	extraArgs := vp9oracle.CBRArgs(600, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []govpx.EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9oracle.FlagAt(frames, 3, govpx.EncodeForceKeyFrame),
		},
		{
			name:  "force-kf-frame1",
			flags: vp9oracle.FlagAt(frames, 1, govpx.EncodeForceKeyFrame),
		},
		{
			name:  "force-kf-every-frame",
			flags: vp9oracle.RepeatAllFramesFlag(frames, govpx.EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-last",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoUpdateLast),
		},
		{
			name:  "repeat-no-update-golden",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoUpdateGolden),
		},
		{
			name:  "repeat-no-update-altref",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-golden-altref",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoUpdateGolden|govpx.EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9oracle.RepeatInterFlag(frames, vp9oracle.NoUpdateRefFlags),
		},
		{
			name:  "repeat-no-reference-golden",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoReferenceGolden),
		},
		{
			name:  "repeat-no-reference-altref",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoReferenceAltRef),
		},
		{
			name:  "repeat-no-reference-golden-altref",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9oracle.RepeatInterFlag(frames,
				govpx.EncodeNoReferenceLast|govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9oracle.RefRefreshTransitions(frames),
		},
		{
			name:  "repeat-no-update-entropy",
			flags: vp9oracle.RepeatInterFlag(frames, govpx.EncodeNoUpdateEntropy),
		},
		{
			name:  "alternating-reference-controls",
			flags: vp9oracle.AlternatingReferenceControls(frames),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, tc.flags)
			libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			t.Logf("VP9 frame-flag transitions %s: %s",
				tc.name, stats)
			t.Logf("VP9 frame-flag transition rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_TRANSITION_SCOREBOARD_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 frame-flag transition mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleFrameFlagReferenceUpdateMatrixMatchesLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 reference/update matrix")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 6
	opts := vp9oracle.CBROptions(width, height, 650)
	extraArgs := vp9oracle.CBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name string
		flag govpx.EncodeFlags
	}{
		{name: "no-update-last", flag: govpx.EncodeNoUpdateLast},
		{name: "no-update-golden", flag: govpx.EncodeNoUpdateGolden},
		{name: "no-update-altref", flag: govpx.EncodeNoUpdateAltRef},
		{name: "no-update-last-golden", flag: govpx.EncodeNoUpdateLast | govpx.EncodeNoUpdateGolden},
		{name: "no-update-all", flag: vp9oracle.NoUpdateRefFlags},
		{name: "no-reference-last", flag: govpx.EncodeNoReferenceLast},
		{name: "no-reference-golden", flag: govpx.EncodeNoReferenceGolden},
		{name: "no-reference-altref", flag: govpx.EncodeNoReferenceAltRef},
		{name: "no-reference-golden-altref", flag: govpx.EncodeNoReferenceGolden | govpx.EncodeNoReferenceAltRef},
		{name: "no-reference-all", flag: govpx.EncodeNoReferenceLast | govpx.EncodeNoReferenceGolden | govpx.EncodeNoReferenceAltRef},
		{name: "force-golden-no-update-last", flag: govpx.EncodeForceGoldenFrame | govpx.EncodeNoUpdateLast},
		{name: "force-altref-no-update-golden", flag: govpx.EncodeForceAltRefFrame | govpx.EncodeNoUpdateGolden},
		{name: "force-golden-altref-no-update-last", flag: govpx.EncodeForceGoldenFrame | govpx.EncodeForceAltRefFrame | govpx.EncodeNoUpdateLast},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			flags := vp9oracle.RepeatInterFlag(frames, tc.flag)
			govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, flags)
			libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height,
				sources, flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			t.Logf("VP9 reference/update matrix %s: %s",
				tc.name, stats)
			t.Logf("VP9 reference/update matrix rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_FLAG_MATRIX_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 reference/update matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleOddSizeFrameFlagTransitionsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 odd-size transitions")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 65, 63, 7
	opts := vp9oracle.CBROptions(width, height, 650)
	extraArgs := vp9oracle.CBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []govpx.EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9oracle.FlagAt(frames, 3, govpx.EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9oracle.RepeatInterFlag(frames, vp9oracle.NoUpdateRefFlags),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9oracle.RepeatInterFlag(frames,
				govpx.EncodeNoReferenceLast|govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9oracle.RefRefreshTransitions(frames),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, tc.flags)
			libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			t.Logf("VP9 odd-size transitions %s: %s", tc.name, stats)
			t.Logf("VP9 odd-size transition rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if vp9test.StrictEnv("GOVPX_VP9_ODD_TRANSITION_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 odd-size transition mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}
