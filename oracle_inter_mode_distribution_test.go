package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestOracleInterModeDistributionScoreboard captures per-fixture inter-frame
// mode/ref/skip distribution for govpx and libvpx, emits a side-by-side
// scoreboard, and gates regression against
// testdata/inter_mode_distribution_baseline.json. Each fixture's mode pp
// (percentage-points) deltas from libvpx must stay within +/- 4pp of the
// recorded baseline; the L1 mode-distribution distance must not grow by
// more than 6pp; and the EOB-sum-ratio must not drift more than 0.10.
//
// The scoreboard targets the +74% inter-frame-size divergence diagnosed
// in r7-b: at speed 8 RT CBR with both encoders pinned at Q max, govpx
// over-picks NEAR/NEW vs libvpx's NEAREST/ZEROMV. Tracking the per-mode
// percentage-point gap lets future patches confirm the gap closes
// without regressing existing scoreboards.
//
// Bootstrap with GOVPX_UPDATE_BASELINES=1 to seed the file.
func TestOracleInterModeDistributionScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle inter-mode distribution scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

	type fixtureKind int
	const (
		fixturePanning fixtureKind = iota
		fixtureBenchNoise
	)
	type fixtureSpec struct {
		Width      int
		Height     int
		Deadline   Deadline
		CpuUsed    int
		FPS        int
		TargetKbps int
		Frames     int
		KFInterval int
		Kind       fixtureKind
		Name       string
	}
	specs := []fixtureSpec{
		// Panning fixtures sweep small/medium/large with realistic motion.
		{64, 64, DeadlineRealtime, 8, 30, 700, 8, 999, fixturePanning, "rt-cpu8-64x64-panning"},
		{128, 128, DeadlineRealtime, 8, 30, 700, 8, 999, fixturePanning, "rt-cpu8-128x128-panning"},
		{256, 256, DeadlineRealtime, 8, 30, 1000, 8, 999, fixturePanning, "rt-cpu8-256x256-panning"},
		// Bench-style noise fixtures track the +74% inter-frame gap (no real
		// motion -- only pseudo-random pixel deltas per frame, the worst case
		// for ZEROMV-vs-NEW divergence).
		{128, 128, DeadlineRealtime, 8, 30, 1200, 8, 30, fixtureBenchNoise, "rt-cpu8-128x128-bench-noise"},
		{256, 256, DeadlineRealtime, 8, 30, 1200, 8, 30, fixtureBenchNoise, "rt-cpu8-256x256-bench-noise"},
	}

	type modeBreakdown struct {
		ZEROMV    float64 `json:"zeromv"`
		NEARESTMV float64 `json:"nearestmv"`
		NEARMV    float64 `json:"nearmv"`
		NEWMV     float64 `json:"newmv"`
		Intra     float64 `json:"intra"`
	}
	type fixtureReport struct {
		Name          string        `json:"name"`
		Width         int           `json:"width"`
		Height        int           `json:"height"`
		Deadline      string        `json:"deadline"`
		CpuUsed       int           `json:"cpu_used"`
		Govpx         modeBreakdown `json:"govpx"`
		Libvpx        modeBreakdown `json:"libvpx"`
		Diff          modeBreakdown `json:"diff_pp"`
		L1Pp          float64       `json:"l1_pp"`
		GovpxSkipPct  float64       `json:"govpx_skip_pct"`
		LibvpxSkipPct float64       `json:"libvpx_skip_pct"`
		GovpxLastPct  float64       `json:"govpx_last_pct"`
		LibvpxLastPct float64       `json:"libvpx_last_pct"`
		GovpxEOBSum   int           `json:"govpx_eob_sum"`
		LibvpxEOBSum  int           `json:"libvpx_eob_sum"`
		EOBSumRatio   float64       `json:"eob_sum_ratio"`
	}

	type baselineEntry struct {
		Diff        modeBreakdown `json:"diff_pp"`
		L1Pp        float64       `json:"l1_pp"`
		EOBSumRatio float64       `json:"eob_sum_ratio"`
	}
	type baselineFile struct {
		Fixtures map[string]baselineEntry `json:"fixtures"`
	}

	baselinePath := filepath.Join("testdata", "inter_mode_distribution_baseline.json")
	updateBaselines := os.Getenv("GOVPX_UPDATE_BASELINES") == "1"

	var baseline baselineFile
	baselineExists := false
	if !updateBaselines {
		raw, err := os.ReadFile(baselinePath)
		if err == nil {
			if err := json.Unmarshal(raw, &baseline); err != nil {
				t.Fatalf("baseline %s: %v", baselinePath, err)
			}
			baselineExists = true
		} else if !os.IsNotExist(err) {
			t.Fatalf("read baseline %s: %v", baselinePath, err)
		}
	}

	currentBaseline := baselineFile{Fixtures: make(map[string]baselineEntry, len(specs))}
	reports := make([]fixtureReport, 0, len(specs))

	for _, spec := range specs {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:               spec.Width,
				Height:              spec.Height,
				FPS:                 spec.FPS,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   spec.TargetKbps,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				Deadline:            spec.Deadline,
				CpuUsed:             spec.CpuUsed,
				KeyFrameInterval:    spec.KFInterval,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
			}
			sources := make([]Image, spec.Frames)
			for i := range sources {
				if spec.Kind == fixtureBenchNoise {
					sources[i] = scoreboardBenchNoiseFrame(spec.Width, spec.Height, i)
				} else {
					sources[i] = encoderValidationPanningFrame(spec.Width, spec.Height, i)
				}
			}
			govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
			extra := []string{
				"--end-usage=cbr",
				"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
				"--undershoot-pct=100", "--overshoot-pct=15",
				"--threads=1", "--noise-sensitivity=0",
			}
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "interdist-"+spec.Name, opts, spec.TargetKbps, sources, extra)

			gMode, gRefLast, gSkip, gEOB, gTotal := scoreboardInterMacroblockHistogram(t, govpxTrace)
			lMode, lRefLast, lSkip, lEOB, lTotal := scoreboardInterMacroblockHistogram(t, libvpxTrace)

			breakdown := func(m map[string]int, total int) modeBreakdown {
				if total <= 0 {
					return modeBreakdown{}
				}
				return modeBreakdown{
					ZEROMV:    100.0 * float64(m["ZEROMV"]) / float64(total),
					NEARESTMV: 100.0 * float64(m["NEARESTMV"]) / float64(total),
					NEARMV:    100.0 * float64(m["NEARMV"]) / float64(total),
					NEWMV:     100.0 * float64(m["NEWMV"]) / float64(total),
					Intra:     100.0 * float64(m["INTRA"]) / float64(total),
				}
			}
			govpxBreakdown := breakdown(gMode, gTotal)
			libvpxBreakdown := breakdown(lMode, lTotal)
			diff := modeBreakdown{
				ZEROMV:    govpxBreakdown.ZEROMV - libvpxBreakdown.ZEROMV,
				NEARESTMV: govpxBreakdown.NEARESTMV - libvpxBreakdown.NEARESTMV,
				NEARMV:    govpxBreakdown.NEARMV - libvpxBreakdown.NEARMV,
				NEWMV:     govpxBreakdown.NEWMV - libvpxBreakdown.NEWMV,
				Intra:     govpxBreakdown.Intra - libvpxBreakdown.Intra,
			}
			l1 := math.Abs(diff.ZEROMV) + math.Abs(diff.NEARESTMV) + math.Abs(diff.NEARMV) + math.Abs(diff.NEWMV) + math.Abs(diff.Intra)
			eobRatio := 0.0
			if lEOB > 0 {
				eobRatio = float64(gEOB) / float64(lEOB)
			}
			pct := func(num, den int) float64 {
				if den <= 0 {
					return 0
				}
				return 100.0 * float64(num) / float64(den)
			}
			report := fixtureReport{
				Name:          spec.Name,
				Width:         spec.Width,
				Height:        spec.Height,
				Deadline:      deadlineString(spec.Deadline),
				CpuUsed:       spec.CpuUsed,
				Govpx:         govpxBreakdown,
				Libvpx:        libvpxBreakdown,
				Diff:          diff,
				L1Pp:          l1,
				GovpxSkipPct:  pct(gSkip, gTotal),
				LibvpxSkipPct: pct(lSkip, lTotal),
				GovpxLastPct:  pct(gRefLast, gTotal),
				LibvpxLastPct: pct(lRefLast, lTotal),
				GovpxEOBSum:   gEOB,
				LibvpxEOBSum:  lEOB,
				EOBSumRatio:   eobRatio,
			}
			if data, err := json.MarshalIndent(report, "", "  "); err == nil {
				t.Logf("scoreboard %s:\n%s", spec.Name, data)
			}
			currentBaseline.Fixtures[spec.Name] = baselineEntry{
				Diff:        diff,
				L1Pp:        l1,
				EOBSumRatio: eobRatio,
			}
			reports = append(reports, report)

			if !updateBaselines && baselineExists {
				prev, ok := baseline.Fixtures[spec.Name]
				if !ok {
					t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, spec.Name)
					return
				}
				const ppTol = 4.0
				const l1Tol = 6.0
				const eobTol = 0.10
				if math.Abs(diff.NEARESTMV-prev.Diff.NEARESTMV) > ppTol {
					t.Errorf("NEARESTMV pp drift %s: cur=%+.2fpp baseline=%+.2fpp drift=%+.2fpp > %.1f",
						spec.Name, diff.NEARESTMV, prev.Diff.NEARESTMV, diff.NEARESTMV-prev.Diff.NEARESTMV, ppTol)
				}
				if math.Abs(diff.NEARMV-prev.Diff.NEARMV) > ppTol {
					t.Errorf("NEARMV pp drift %s: cur=%+.2fpp baseline=%+.2fpp drift=%+.2fpp > %.1f",
						spec.Name, diff.NEARMV, prev.Diff.NEARMV, diff.NEARMV-prev.Diff.NEARMV, ppTol)
				}
				if math.Abs(diff.NEWMV-prev.Diff.NEWMV) > ppTol {
					t.Errorf("NEWMV pp drift %s: cur=%+.2fpp baseline=%+.2fpp drift=%+.2fpp > %.1f",
						spec.Name, diff.NEWMV, prev.Diff.NEWMV, diff.NEWMV-prev.Diff.NEWMV, ppTol)
				}
				if math.Abs(diff.ZEROMV-prev.Diff.ZEROMV) > ppTol {
					t.Errorf("ZEROMV pp drift %s: cur=%+.2fpp baseline=%+.2fpp drift=%+.2fpp > %.1f",
						spec.Name, diff.ZEROMV, prev.Diff.ZEROMV, diff.ZEROMV-prev.Diff.ZEROMV, ppTol)
				}
				if l1-prev.L1Pp > l1Tol {
					t.Errorf("L1 pp regression %s: cur=%.2fpp baseline=%.2fpp drift=%.2fpp > %.1f",
						spec.Name, l1, prev.L1Pp, l1-prev.L1Pp, l1Tol)
				}
				if math.Abs(eobRatio-prev.EOBSumRatio) > eobTol {
					t.Errorf("EOB ratio drift %s: cur=%.3f baseline=%.3f drift=%.3f > %.2f",
						spec.Name, eobRatio, prev.EOBSumRatio, eobRatio-prev.EOBSumRatio, eobTol)
				}
			}
		})
	}

	if updateBaselines || !baselineExists {
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(baselinePath), err)
		}
		data, err := json.MarshalIndent(currentBaseline, "", "  ")
		if err != nil {
			t.Fatalf("Marshal baseline: %v", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(baselinePath, data, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", baselinePath, err)
		}
		t.Logf("wrote baseline %s with %d fixtures", baselinePath, len(currentBaseline.Fixtures))
	}

	// Stable order summary for human readability.
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	var summary bytes.Buffer
	fmt.Fprintln(&summary, "fixture,govpx_zeromv,govpx_nearest,govpx_near,govpx_new,libvpx_zeromv,libvpx_nearest,libvpx_near,libvpx_new,l1_pp,eob_sum_ratio")
	for _, r := range reports {
		fmt.Fprintf(&summary, "%s,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.3f\n",
			r.Name,
			r.Govpx.ZEROMV, r.Govpx.NEARESTMV, r.Govpx.NEARMV, r.Govpx.NEWMV,
			r.Libvpx.ZEROMV, r.Libvpx.NEARESTMV, r.Libvpx.NEARMV, r.Libvpx.NEWMV,
			r.L1Pp, r.EOBSumRatio)
	}
	t.Logf("inter-mode distribution scoreboard summary:\n%s", summary.String())
}

// scoreboardBenchNoiseFrame mirrors the cmd/govpx-bench makeBenchmarkFrame
// pseudo-random pattern. This is the worst-case fixture for ZEROMV-vs-NEW
// divergence -- the source has no real motion, only per-frame pixel deltas.
func scoreboardBenchNoiseFrame(width, height, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

// scoreboardInterMacroblockHistogram counts MB-level mode/ref/skip from the
// trace's per-MB rows, returning {mode counts, last-frame count, skip count,
// eob sum, total inter MBs}. Frame 0 (keyframe) MBs are excluded.
func scoreboardInterMacroblockHistogram(t *testing.T, trace []byte) (mode map[string]int, lastFrame int, skip int, eobSum int, total int) {
	t.Helper()
	mode = map[string]int{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if int(fi) <= 0 {
			continue
		}
		m, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		if ref == "INTRA_FRAME" {
			mode["INTRA"]++
		} else {
			mode[m]++
			if ref == "LAST_FRAME" {
				lastFrame++
			}
		}
		var s bool
		if sv, ok := row["skip"].(bool); ok {
			s = sv
		} else if sv, ok := row["skip"].(float64); ok {
			s = sv != 0
		}
		if s {
			skip++
		}
		if eob, ok := row["eob_sum"].(float64); ok {
			eobSum += int(eob)
		}
		total++
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scoreboard scan trace: %v", err)
	}
	return mode, lastFrame, skip, eobSum, total
}
