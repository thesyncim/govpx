package benchcmd

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
)

// LibvpxAbsoluteGate bundles the thresholds for the absolute-reference
// govpx-vs-libvpx assertion that accompanies each per-feature BD-rate
// gate. The Skip mode allows the gate test to t.Skip the libvpx
// assertion when the helper binary is missing and the build was not
// requested; this keeps normal local test runs usable until the libvpx
// oracle has been built.
type LibvpxAbsoluteGate struct {
	// MaxBDRateOverLibvpxPct is the cap on govpx-vs-libvpx BD-rate.
	// Govpx is "OK" when it stays at or below this percentage worse
	// than libvpx at equal PSNR. Negative values mean govpx must beat
	// libvpx.
	MaxBDRateOverLibvpxPct float64
	// MinBDPSNRdB is the floor on govpx-vs-libvpx BD-PSNR. Govpx is
	// "OK" when it stays above this many dB below libvpx at equal
	// rate. Negative values express "govpx may sit up to N dB below
	// libvpx".
	MinBDPSNRdB float64
}

// FeatureLibvpxObservation is the one-row summary printed by
// reportFeatureGateLibvpxRow into the per-feature scoreboard. Captures
// govpx feature-on-vs-off BD-rate plus the absolute govpx-vs-libvpx
// gap so a reviewer can see at-a-glance which features close the
// libvpx parity gap (and which still trail).
type FeatureLibvpxObservation struct {
	Feature string
	// GovpxBDRatePct is the feature-on-vs-off BD-rate measured
	// entirely within govpx. Negative is better (govpx saves bitrate
	// when the feature is enabled).
	GovpxBDRatePct float64
	// LibvpxBDRatePct is the matching libvpx-vp9 feature-on-vs-off
	// BD-rate, derived from the libvpx helper's feature-on
	// (kbps, PSNR-proxy) curve vs govpx's feature-off curve. NaN
	// when no libvpx reference was produced. (We hold the libvpx
	// feature-off curve out of the test loop to keep scoreboard
	// cost low; the absolute govpx-vs-libvpx BD-rate is the
	// substantive number.)
	LibvpxBDRatePct float64
	// GovpxVsLibvpxBDRatePct is the absolute govpx-vs-libvpx BD-rate
	// at the on-feature operating point. Negative means govpx
	// outperforms libvpx; positive means govpx trails.
	GovpxVsLibvpxBDRatePct float64
	// GovpxVsLibvpxBDPSNRdB is the absolute govpx-vs-libvpx BD-PSNR
	// at the on-feature operating point. Positive means govpx has
	// more dB at equal rate; negative means govpx has less.
	GovpxVsLibvpxBDPSNRdB float64
	// LibvpxErr captures the reason no libvpx curve was produced
	// (binary missing, mapping unsupported, subprocess failed). Nil
	// when LibvpxBDRatePct / GovpxVsLibvpxBDRatePct are populated.
	LibvpxErr error
}

// FormatFeatureScoreboard renders a per-feature markdown-ish scoreboard
// table from the observations.
//
// Column layout (matches the task spec):
//
//	Feature        | govpx BD-rate | libvpx BD-rate | govpx-vs-libvpx
//
// Cells render NaN entries as "—" so a missing libvpx oracle is
// visually obvious rather than poisoning the column-alignment math
// with floating-point garbage.
func FormatFeatureScoreboard(rows []FeatureLibvpxObservation) string {
	header := [4]string{"Feature", "govpx BD-rate", "libvpx BD-rate", "govpx-vs-libvpx (BD-rate / BD-PSNR)"}
	out := make([][4]string, 0, len(rows)+1)
	out = append(out, header)
	for _, r := range rows {
		govpxCell := fmt.Sprintf("%+0.3f%%", r.GovpxBDRatePct)
		libvpxCell := "—"
		crossCell := "—"
		if !math.IsNaN(r.LibvpxBDRatePct) {
			libvpxCell = fmt.Sprintf("%+0.3f%%", r.LibvpxBDRatePct)
		}
		if r.LibvpxErr != nil {
			crossCell = "libvpx err: " + r.LibvpxErr.Error()
		} else if !math.IsNaN(r.GovpxVsLibvpxBDRatePct) {
			crossCell = fmt.Sprintf("%+0.3f%% / %+0.3f dB",
				r.GovpxVsLibvpxBDRatePct, r.GovpxVsLibvpxBDPSNRdB)
		}
		out = append(out, [4]string{r.Feature, govpxCell, libvpxCell, crossCell})
	}
	widths := [4]int{}
	for _, r := range out {
		for i, c := range r {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	var sb strings.Builder
	for _, r := range out {
		fmt.Fprintf(&sb, "  %-*s | %-*s | %-*s | %s\n",
			widths[0], r[0], widths[1], r[1], widths[2], r[2], r[3])
	}
	return sb.String()
}

// LibvpxBuildRequested reports whether the BD-rate gates should
// proactively build the libvpx vpxenc-vp9-frameflags helper when it
// is missing. Off by default so `make verify-bd-rate` stays fast on
// a clean checkout; CI / `make verify-bd-rate-with-libvpx` can opt
// in via GOVPX_BD_RATE_BUILD_LIBVPX=1.
func LibvpxBuildRequested() bool {
	return os.Getenv("GOVPX_BD_RATE_BUILD_LIBVPX") == "1"
}

// LibvpxRequired reports whether a missing libvpx helper should hard-
// fail (t.Fatal) instead of soft-skip the absolute-reference
// assertion. Off by default. Set GOVPX_BD_RATE_LIBVPX_REQUIRED=1 (or
// pass through `make verify-bd-rate`) when the gate must always
// observe the libvpx oracle — e.g. CI runs that build the oracle
// up-front.
func LibvpxRequired() bool {
	return os.Getenv("GOVPX_BD_RATE_LIBVPX_REQUIRED") == "1"
}

// featureScoreboardMu guards the shared scoreboard rows so per-feature
// gates can record their numbers concurrently.
var (
	featureScoreboardMu   sync.Mutex
	featureScoreboardRows []FeatureLibvpxObservation
)

// AppendFeatureScoreboardRow records one row for the per-feature
// scoreboard. The summary diagnostic test prints the table at the end
// of the BD-rate run.
func AppendFeatureScoreboardRow(row FeatureLibvpxObservation) {
	featureScoreboardMu.Lock()
	defer featureScoreboardMu.Unlock()
	featureScoreboardRows = append(featureScoreboardRows, row)
}

// FeatureScoreboardRows returns a defensive copy of the rows recorded
// so far.
func FeatureScoreboardRows() []FeatureLibvpxObservation {
	featureScoreboardMu.Lock()
	defer featureScoreboardMu.Unlock()
	out := make([]FeatureLibvpxObservation, len(featureScoreboardRows))
	copy(out, featureScoreboardRows)
	return out
}
