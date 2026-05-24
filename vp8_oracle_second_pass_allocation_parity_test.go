//go:build govpx_oracle_trace

package govpx

// Second-pass allocation oracle compare.
//
// TestVP8OracleSecondPassAllocationParity drives a libvpx pass-1 run against
// each fixture's source to produce a libvpx FIRSTPASS_STATS file (.fpf),
// then runs both encoders on a matched pass-2 configuration:
//   - libvpx: vpxenc-oracle --pass=2 --fpf=<file> with the trace env var,
//     emitting a per-frame "rate" row carrying q_index / this_frame_target.
//   - govpx: a fresh VP8Encoder configured with the same first-pass stats
//     parsed from the fpf and fed through EncoderOptions.TwoPassStats.
//
// We then compare per-frame q_index (within +/- 2 qindex) and
// this_frame_target (within 5% relative). Tallies are written / compared
// against testdata/second_pass_alloc_baseline.json under the standard
// GOVPX_UPDATE_BASELINES=1 bootstrap pattern.
//
// Fixtures:
//   - good-quality-cpu0-vbr: synthetic 32x32 ramp, 8 frames, 400 kbps
//     (tripwire pinned at 100% target_match by R2-H — DO NOT regress).
//   - park-joy-90p-vbr: external Y4M, 160x90 @ 50fps, 12 frames, 350 kbps.
//   - desktopqvga-vbr: external raw I420, 320x240 @ 30fps, 12 frames,
//     600 kbps.
//
// External fixtures get an additional hard 90% target_match floor enforced
// against the baseline: once a fixture has cleared 90% it must stay there.
// Below 90% the test surfaces a t.Logf diagnosis so the scoreboard makes
// the gap visible without silently widening the per-frame tolerance.
//
// All work is gated behind GOVPX_WITH_ORACLE=1. External fixtures skip
// individually when their corpus file is not present so plain `go test`
// without the encoder corpus still runs the synthetic tripwire.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

const secondPassAllocBaselinePath = "testdata/second_pass_alloc_baseline.json"

type secondPassRateRow struct {
	FrameIndex      int64 `json:"frame_index"`
	QIndex          int   `json:"q_index"`
	ThisFrameTarget int   `json:"this_frame_target"`
}

type secondPassFrameDiff struct {
	FrameIndex     int64
	QIndexGovpx    int
	QIndexLibvpx   int
	QIndexDelta    int
	TargetGovpx    int
	TargetLibvpx   int
	TargetRelDelta float64
}

type FixtureSecondPassReport struct {
	Name              string
	FrameTotal        int
	QWithinTol        int
	TargetWithinTol   int
	QMatchPct         float64
	TargetMatchPct    float64
	MaxQIndexDelta    int
	MaxTargetRelDelta float64
}

type secondPassBaselineEntry struct {
	QMatchPct         float64 `json:"q_match_pct"`
	TargetMatchPct    float64 `json:"target_match_pct"`
	MaxQIndexDelta    int     `json:"max_qindex_delta"`
	MaxTargetRelDelta float64 `json:"max_target_rel_delta"`
}

type secondPassBaselineFile struct {
	Fixtures map[string]secondPassBaselineEntry `json:"fixtures"`
}

func TestVP8OracleSecondPassAllocationParity(t *testing.T) {
	vp8test.RequireOracle(t, "second-pass allocation oracle compare")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		rampWidth      = 32
		rampHeight     = 32
		rampFPS        = 30
		rampTargetKbps = 400
		rampFrames     = 8
	)
	rampSources := make([]Image, rampFrames)
	for i := range rampSources {
		rampSources[i] = firstPassOracleRampFrame(rampWidth, rampHeight, i)
	}

	fixtures := []secondPassFixture{
		{
			name: "good-quality-cpu0-vbr",
			opts: EncoderOptions{
				Width:             rampWidth,
				Height:            rampHeight,
				FPS:               rampFPS,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: rampTargetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  60,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
			},
			targetKbps: rampTargetKbps,
			sources:    rampSources,
		},
	}

	fixtures = append(fixtures, loadExternalSecondPassFixtures(t)...)

	reports := make([]FixtureSecondPassReport, 0, len(fixtures))
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			fpfData, libvpxTrace, diag, err := vp8test.VpxencVP8TwoPassTraceI420(
				encoderValidationI420Bytes(t, f.sources),
				vp8test.VpxencVP8TwoPassConfig{
					FirstPassBinaryPath:  vpxenc,
					SecondPassBinaryPath: vpxencOracle,
					Common: vp8OracleTraceConfig(
						"",
						f.opts,
						len(f.sources),
						f.targetKbps,
						nil,
						[]string{"--end-usage=vbr"},
					),
				},
			)
			if err != nil {
				t.Fatalf("vpxenc two-pass trace failed: %v\n%s", err, diag)
			}
			parsed := parseLibvpxFirstPassStats(t, fpfData)

			// Drive govpx pass 2 with the libvpx-emitted stats; capture trace.
			govpxOpts := f.opts
			govpxOpts.TwoPassStats = parsed
			govpxTrace := captureGovpxEncoderTrace(t, govpxOpts, f.sources)

			govpxRows := secondPassRateRowsFromTrace(t, govpxTrace)
			libvpxRows := secondPassRateRowsFromTrace(t, libvpxTrace)
			report, diffs := scoreSecondPassAlloc(f.name, govpxRows, libvpxRows)
			reports = append(reports, report)
			t.Logf("second-pass scoreboard: %s frames=%d q_match=%.2f%% target_match=%.2f%% maxQΔ=%d maxTargetRelΔ=%.4f",
				report.Name, report.FrameTotal,
				report.QMatchPct, report.TargetMatchPct,
				report.MaxQIndexDelta, report.MaxTargetRelDelta)
			for _, d := range diffs {
				if absInt(d.QIndexDelta) > 2 || math.Abs(d.TargetRelDelta) > 0.05 {
					t.Logf("  frame %d qΔ=%d (govpx=%d libvpx=%d) targetΔrel=%.4f (govpx=%d libvpx=%d)",
						d.FrameIndex, d.QIndexDelta, d.QIndexGovpx, d.QIndexLibvpx,
						d.TargetRelDelta, d.TargetGovpx, d.TargetLibvpx)
				}
			}
		})
	}

	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })

	if vp8test.UpdateBaselines() {
		writeSecondPassBaseline(t, reports)
		return
	}
	enforceSecondPassBaseline(t, reports)
}

func secondPassRateRowsFromTrace(t *testing.T, trace []byte) []secondPassRateRow {
	t.Helper()
	var rows []secondPassRateRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("trace row not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typeRaw, ok := raw["type"]
		if !ok {
			continue
		}
		var rowType string
		if err := json.Unmarshal(typeRaw, &rowType); err != nil {
			continue
		}
		if rowType != "rate" {
			continue
		}
		var row secondPassRateRow
		if v, ok := raw["frame_index"]; ok {
			_ = json.Unmarshal(v, &row.FrameIndex)
		}
		if v, ok := raw["q_index"]; ok {
			_ = json.Unmarshal(v, &row.QIndex)
		}
		if v, ok := raw["this_frame_target"]; ok {
			_ = json.Unmarshal(v, &row.ThisFrameTarget)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func scoreSecondPassAlloc(name string, govpxRows []secondPassRateRow, libvpxRows []secondPassRateRow) (FixtureSecondPassReport, []secondPassFrameDiff) {
	total := min(len(libvpxRows), len(govpxRows))
	report := FixtureSecondPassReport{Name: name, FrameTotal: total}
	if total == 0 {
		return report, nil
	}
	diffs := make([]secondPassFrameDiff, total)
	for i := range total {
		g := govpxRows[i]
		l := libvpxRows[i]
		d := secondPassFrameDiff{
			FrameIndex:   g.FrameIndex,
			QIndexGovpx:  g.QIndex,
			QIndexLibvpx: l.QIndex,
			QIndexDelta:  g.QIndex - l.QIndex,
			TargetGovpx:  g.ThisFrameTarget,
			TargetLibvpx: l.ThisFrameTarget,
		}
		// Relative delta is referenced to libvpx's target; sentinel zero
		// means we report absolute delta as ratio over max(1, target).
		denom := math.Abs(float64(l.ThisFrameTarget))
		if denom < 1.0 {
			denom = 1.0
		}
		d.TargetRelDelta = float64(g.ThisFrameTarget-l.ThisFrameTarget) / denom
		diffs[i] = d

		if absInt(d.QIndexDelta) <= 2 {
			report.QWithinTol++
		}
		if math.Abs(d.TargetRelDelta) <= 0.05 {
			report.TargetWithinTol++
		}
		if absInt(d.QIndexDelta) > absInt(report.MaxQIndexDelta) {
			report.MaxQIndexDelta = d.QIndexDelta
		}
		if math.Abs(d.TargetRelDelta) > math.Abs(report.MaxTargetRelDelta) {
			report.MaxTargetRelDelta = d.TargetRelDelta
		}
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(total) }
	report.QMatchPct = pct(report.QWithinTol)
	report.TargetMatchPct = pct(report.TargetWithinTol)
	return report, diffs
}

func writeSecondPassBaseline(t *testing.T, reports []FixtureSecondPassReport) {
	t.Helper()
	file := secondPassBaselineFile{Fixtures: map[string]secondPassBaselineEntry{}}
	for _, r := range reports {
		file.Fixtures[r.Name] = secondPassBaselineEntry{
			QMatchPct:         r.QMatchPct,
			TargetMatchPct:    r.TargetMatchPct,
			MaxQIndexDelta:    r.MaxQIndexDelta,
			MaxTargetRelDelta: r.MaxTargetRelDelta,
		}
	}
	vp8test.WriteJSONBaseline(t, secondPassAllocBaselinePath, file)
	t.Logf("wrote %s", secondPassAllocBaselinePath)
}

func enforceSecondPassBaseline(t *testing.T, reports []FixtureSecondPassReport) {
	t.Helper()
	file, ok := vp8test.ReadOptionalJSONBaseline[secondPassBaselineFile](t, secondPassAllocBaselinePath)
	if !ok {
		t.Fatalf("baseline %s is missing; run with GOVPX_UPDATE_BASELINES=1 to bootstrap", secondPassAllocBaselinePath)
	}
	const tol = 2.0
	// External corpus fixtures get a stricter floor enforcement once their
	// baseline is at or above 90% target_match_pct. Below that, the
	// baseline-relative `tol` regression check still applies and the
	// shortfall is surfaced via a t.Logf diagnosis so the scoreboard makes
	// the gap visible without silently widening tolerances.
	const externalFloorPct = 90.0
	for _, r := range reports {
		baseline, ok := file.Fixtures[r.Name]
		if !ok {
			t.Errorf("second-pass baseline missing fixture %q (run with GOVPX_UPDATE_BASELINES=1)", r.Name)
			continue
		}
		if r.QMatchPct < baseline.QMatchPct-tol {
			t.Errorf("%s q_match_pct = %.4f, baseline %.4f, regression > %.2f",
				r.Name, r.QMatchPct, baseline.QMatchPct, tol)
		}
		if r.TargetMatchPct < baseline.TargetMatchPct-tol {
			t.Errorf("%s target_match_pct = %.4f, baseline %.4f, regression > %.2f",
				r.Name, r.TargetMatchPct, baseline.TargetMatchPct, tol)
		}
		if isExternalSecondPassFixture(r.Name) {
			// Once the external fixture clears 90% match, hold it
			// there as a hard floor. If a future run drops below
			// 90% (and the previous baseline ALSO held above 90%),
			// fail loudly rather than allow silent regression.
			if baseline.TargetMatchPct >= externalFloorPct && r.TargetMatchPct < externalFloorPct {
				t.Errorf("%s external fixture target_match_pct = %.4f below hard floor %.2f%% (baseline %.4f); pass-2 allocation regressed (qΔmax=%d targetRelΔmax=%.4f). Diagnose the divergence — do not widen the tolerance.",
					r.Name, r.TargetMatchPct, externalFloorPct,
					baseline.TargetMatchPct, r.MaxQIndexDelta, r.MaxTargetRelDelta)
			}
			if baseline.QMatchPct >= externalFloorPct && r.QMatchPct < externalFloorPct {
				t.Errorf("%s external fixture q_match_pct = %.4f below hard floor %.2f%% (baseline %.4f); pass-2 q-index allocation regressed (qΔmax=%d). Diagnose the divergence — do not widen the tolerance.",
					r.Name, r.QMatchPct, externalFloorPct,
					baseline.QMatchPct, r.MaxQIndexDelta)
			}
			if r.TargetMatchPct < externalFloorPct {
				t.Logf("DIAGNOSIS %s: target_match_pct = %.4f%% below 90%% floor; pass-2 govpx target consistently undershoots libvpx (qΔmax=%d targetRelΔmax=%.4f). Root cause TBD — see commit message / scoreboard diagnostic logs.",
					r.Name, r.TargetMatchPct, r.MaxQIndexDelta, r.MaxTargetRelDelta)
			}
		}
	}
}

// isExternalSecondPassFixture reports whether the named fixture comes from the
// external Y4M / raw I420 corpus (as opposed to the synthetic ramp). Only
// external fixtures get the hard 90% floor: the ramp is a tripwire pinned to
// 100% by other assertions and external corpora may legitimately exhibit
// frame-level allocation noise that the ramp does not.
func isExternalSecondPassFixture(name string) bool {
	switch name {
	case "park-joy-90p-vbr", "desktopqvga-vbr":
		return true
	}
	return false
}

// secondPassFixture pairs a corpus clip with the EncoderOptions used to drive
// libvpx pass 1, libvpx pass 2 (with trace), and govpx pass 2.
type secondPassFixture struct {
	name       string
	opts       EncoderOptions
	targetKbps int
	sources    []Image
}

// loadExternalSecondPassFixtures returns external Y4M / raw-I420 fixtures
// keyed off the standard encoder corpus directory. They are skipped when the
// corpus is not present so the test still runs in CI configurations that
// lack the test-data submodule (e.g. plain `go test`).
func loadExternalSecondPassFixtures(t *testing.T) []secondPassFixture {
	t.Helper()
	type spec struct {
		fixtureName string
		path        string
		maxFrames   int
		targetKbps  int
		baseOpts    func(width int, height int, fps int) EncoderOptions
	}
	specs := []spec{
		{
			fixtureName: "park-joy-90p-vbr",
			path:        filepath.Join("internal", "coracle", "build", "test-data", "encoder", "park_joy_90p_8_420.y4m"),
			maxFrames:   12,
			targetKbps:  350,
			baseOpts: func(width int, height int, fps int) EncoderOptions {
				return EncoderOptions{
					Width:             width,
					Height:            height,
					FPS:               fps,
					RateControlMode:   RateControlVBR,
					TargetBitrateKbps: 350,
					MinQuantizer:      4,
					MaxQuantizer:      56,
					KeyFrameInterval:  60,
					Deadline:          DeadlineGoodQuality,
					CpuUsed:           0,
				}
			},
		},
		{
			fixtureName: "desktopqvga-vbr",
			path:        filepath.Join("internal", "coracle", "build", "test-data", "encoder", "desktopqvga.320_240.yuv"),
			maxFrames:   12,
			targetKbps:  600,
			baseOpts: func(width int, height int, fps int) EncoderOptions {
				return EncoderOptions{
					Width:             width,
					Height:            height,
					FPS:               fps,
					RateControlMode:   RateControlVBR,
					TargetBitrateKbps: 600,
					MinQuantizer:      4,
					MaxQuantizer:      56,
					KeyFrameInterval:  60,
					Deadline:          DeadlineGoodQuality,
					CpuUsed:           0,
				}
			},
		},
	}
	var out []secondPassFixture
	for _, s := range specs {
		if _, err := os.Stat(s.path); err != nil {
			t.Logf("external second-pass fixture %s not present at %s; skipping", s.fixtureName, s.path)
			continue
		}
		clip, ok := vp8corpus.ReadSourceClip(t, s.path, s.maxFrames)
		if !ok {
			t.Logf("external second-pass fixture %s not a supported source clip; skipping", s.fixtureName)
			continue
		}
		frames := vp8SourceClipImages(clip)
		out = append(out, secondPassFixture{
			name:       s.fixtureName,
			opts:       s.baseOpts(clip.Width, clip.Height, clip.FPS),
			targetKbps: s.targetKbps,
			sources:    frames,
		})
	}
	return out
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
