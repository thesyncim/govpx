//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9OracleFrameFlagTransitionsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 frame-flag transitions")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 8
	opts := vp9OracleCBROptions(width, height, 600)
	extraArgs := vp9OracleCBRArgs(600, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
		},
		{
			name:  "force-kf-frame1",
			flags: vp9OracleFlagAt(frames, 1, EncodeForceKeyFrame),
		},
		{
			name:  "force-kf-every-frame",
			flags: vp9OracleRepeatAllFramesFlag(frames, EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-last",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateLast),
		},
		{
			name:  "repeat-no-update-golden",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden),
		},
		{
			name:  "repeat-no-update-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateGolden|EncodeNoUpdateAltRef),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
		},
		{
			name:  "repeat-no-reference-golden",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceGolden),
		},
		{
			name:  "repeat-no-reference-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceAltRef),
		},
		{
			name:  "repeat-no-reference-golden-altref",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9OracleRefRefreshTransitions(frames),
		},
		{
			name:  "repeat-no-update-entropy",
			flags: vp9OracleRepeatInterFlag(frames, EncodeNoUpdateEntropy),
		},
		{
			name:  "alternating-reference-controls",
			flags: vp9OracleAlternatingReferenceControls(frames),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateTraceRows(t, opts, sources, tc.flags)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
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
	opts := vp9OracleCBROptions(width, height, 650)
	extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name string
		flag EncodeFlags
	}{
		{name: "no-update-last", flag: EncodeNoUpdateLast},
		{name: "no-update-golden", flag: EncodeNoUpdateGolden},
		{name: "no-update-altref", flag: EncodeNoUpdateAltRef},
		{name: "no-update-last-golden", flag: EncodeNoUpdateLast | EncodeNoUpdateGolden},
		{name: "no-update-all", flag: vp9NoUpdateRefFlags},
		{name: "no-reference-last", flag: EncodeNoReferenceLast},
		{name: "no-reference-golden", flag: EncodeNoReferenceGolden},
		{name: "no-reference-altref", flag: EncodeNoReferenceAltRef},
		{name: "no-reference-golden-altref", flag: EncodeNoReferenceGolden | EncodeNoReferenceAltRef},
		{name: "no-reference-all", flag: EncodeNoReferenceLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef},
		{name: "force-golden-no-update-last", flag: EncodeForceGoldenFrame | EncodeNoUpdateLast},
		{name: "force-altref-no-update-golden", flag: EncodeForceAltRefFrame | EncodeNoUpdateGolden},
		{name: "force-golden-altref-no-update-last", flag: EncodeForceGoldenFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			flags := vp9OracleRepeatInterFlag(frames, tc.flag)
			govpxRows := captureVP9RateTraceRows(t, opts, sources, flags)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
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
	opts := vp9OracleCBROptions(width, height, 650)
	extraArgs := vp9OracleCBRArgs(650, 600, 400, 500, 0)
	cases := []struct {
		name  string
		flags []EncodeFlags
	}{
		{
			name:  "force-kf-frame3",
			flags: vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame),
		},
		{
			name:  "repeat-no-update-all",
			flags: vp9OracleRepeatInterFlag(frames, vp9NoUpdateRefFlags),
		},
		{
			name: "repeat-no-reference-all",
			flags: vp9OracleRepeatInterFlag(frames,
				EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
		},
		{
			name:  "force-golden-altref-transitions",
			flags: vp9OracleRefRefreshTransitions(frames),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateTraceRows(t, opts, sources, tc.flags)
			libvpxRows := captureLibvpxVP9RateTraceRows(t, width, height,
				sources, tc.flags, extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
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
