//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// chromaSubpelBaselineCase records per-fixture Adler32 / size drift between
// govpx and the libvpx oracle on the inter-frame reconstruction path that the
// chroma sub-pel filter rounding gap dominates at sizes >64x64.
type chromaSubpelBaselineCase struct {
	Name           string  `json:"name"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Frames         int     `json:"frames"`
	YAdlerMismatch int     `json:"y_adler_mismatch_inter_frames"`
	UAdlerMismatch int     `json:"u_adler_mismatch_inter_frames"`
	VAdlerMismatch int     `json:"v_adler_mismatch_inter_frames"`
	KeyframeMatch  bool    `json:"keyframe_y_u_v_adler_match"`
	KeyframeQMatch bool    `json:"keyframe_q_match"`
	InterQMatch    bool    `json:"inter_q_match_all_frames"`
	MaxSizePctAbs  float64 `json:"max_inter_size_delta_pct_abs"`
	MeanSizePctAbs float64 `json:"mean_inter_size_delta_pct_abs"`
}

// chromaSubpelBaseline is the baseline shape for the
// TestOracleChromaSubpelScoreboard scoreboard. Each entry records the
// per-fixture summary scalars; tightening the assertion is a matter of
// re-running scoreboard-update once a fix lands.
type chromaSubpelBaseline struct {
	Cases []chromaSubpelBaselineCase `json:"cases"`
}

// TestOracleChromaSubpelScoreboard tracks the residual Adler32 byte-identity
// gap that lives on inter frames at sizes >64x64. The 64x64 byte-identity gate
// stays in TestOracleReconstructionAdler32Match; this scoreboard pins the
// 96x96 / 128x128 / 160x96 panning realtime CBR cpu8 cases where keyframes
// remain byte-identical and Q matches every frame, but inter-frame y/u/v
// Adler32 still differ. Subagent localized the residual to the chroma
// sub-pel filter rounding (govpx 137/118 vs libvpx 139/117 at MB(0,0) row 0
// col 7/3); decoded |delta| peaks at 4 (Y) / 3 (U) / 1 (V) with mean
// magnitude < 0.04 (PSNR vs libvpx > 60 dB). The scalar sixTapPredict matches
// the libvpx C reference numerically; the residual is a rounding edge case
// the larger fixture corpus uncovers, most likely from the sub-pel filter
// state at the right frame edge. Closing it requires per-pixel libvpx-side
// xd->predictor instrumentation to localize.
func TestOracleChromaSubpelScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle chroma sub-pel scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "96x96-realtime-cbr-cpu8", width: 96, height: 96},
		{name: "128x128-realtime-cbr-cpu8", width: 128, height: 128},
		{name: "160x96-realtime-cbr-cpu8", width: 160, height: 96},
	}
	updateBaselines := os.Getenv("GOVPX_UPDATE_BASELINES") == "1"

	current := chromaSubpelBaseline{Cases: make([]chromaSubpelBaselineCase, 0, len(cases))}
	for _, cfg := range cases {
		opts := EncoderOptions{
			Width:             cfg.width,
			Height:            cfg.height,
			FPS:               fps,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			Deadline:          DeadlineRealtime,
			CpuUsed:           8,
			KeyFrameInterval:  999,
		}
		sources := make([]Image, frames)
		for i := range sources {
			sources[i] = encoderValidationPanningFrame(cfg.width, cfg.height, i)
		}
		govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
		libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "chroma-subpel-"+cfg.name, opts, targetKbps, sources, []string{"--end-usage=cbr"})
		gFrames := oracleTraceFrameRows(t, govpxTrace)
		lFrames := oracleTraceFrameRows(t, libvpxTrace)
		if len(gFrames) != frames || len(lFrames) != frames {
			t.Fatalf("[%s] frame rows govpx=%d libvpx=%d want %d", cfg.name, len(gFrames), len(lFrames), frames)
		}

		var summary chromaSubpelBaselineCase
		summary.Name = cfg.name
		summary.Width = cfg.width
		summary.Height = cfg.height
		summary.Frames = frames
		summary.KeyframeMatch = true
		summary.KeyframeQMatch = true
		summary.InterQMatch = true
		var interSizePcts []float64
		// Per-frame markdown table.
		var tbl bytes.Buffer
		fmt.Fprintf(&tbl, "[%s] per-frame Adler32/size/Q diff\n", cfg.name)
		fmt.Fprintln(&tbl, "| frame | type | y_match | u_match | v_match | q_govpx | q_libvpx | size_govpx | size_libvpx | size_delta_pct |")
		fmt.Fprintln(&tbl, "|---|---|---|---|---|---|---|---|---|---|")
		for i := range frames {
			g := gFrames[i]
			l := lFrames[i]
			ftype, _ := g["frame_type"].(string)
			yMatch := g["y_adler32"] == l["y_adler32"]
			uMatch := g["u_adler32"] == l["u_adler32"]
			vMatch := g["v_adler32"] == l["v_adler32"]
			gQ, lQ := traceFloat(g["q_index"]), traceFloat(l["q_index"])
			gSize, lSize := traceFloat(g["size_bytes"]), traceFloat(l["size_bytes"])
			var sizePct float64
			if lSize > 0 {
				sizePct = 100.0 * (gSize - lSize) / lSize
			}
			if i == 0 || ftype == "key" {
				if !yMatch || !uMatch || !vMatch {
					summary.KeyframeMatch = false
				}
				if gQ != lQ {
					summary.KeyframeQMatch = false
				}
			} else {
				if !yMatch {
					summary.YAdlerMismatch++
				}
				if !uMatch {
					summary.UAdlerMismatch++
				}
				if !vMatch {
					summary.VAdlerMismatch++
				}
				if gQ != lQ {
					summary.InterQMatch = false
				}
				interSizePcts = append(interSizePcts, math.Abs(sizePct))
			}
			fmt.Fprintf(&tbl, "| %d | %s | %v | %v | %v | %d | %d | %d | %d | %+.3f |\n",
				i, ftype, yMatch, uMatch, vMatch, int(gQ), int(lQ), int(gSize), int(lSize), sizePct)
		}
		t.Logf("\n%s", tbl.String())

		var maxAbs, sumAbs float64
		for _, p := range interSizePcts {
			if p > maxAbs {
				maxAbs = p
			}
			sumAbs += p
		}
		summary.MaxSizePctAbs = maxAbs
		if len(interSizePcts) > 0 {
			summary.MeanSizePctAbs = sumAbs / float64(len(interSizePcts))
		}
		t.Logf("[%s] summary: keyframe_match=%v keyframe_q_match=%v inter_q_match=%v y_mismatch=%d/%d u_mismatch=%d/%d v_mismatch=%d/%d max_inter_size_pct_abs=%.3f mean_inter_size_pct_abs=%.3f",
			cfg.name, summary.KeyframeMatch, summary.KeyframeQMatch, summary.InterQMatch,
			summary.YAdlerMismatch, frames-1, summary.UAdlerMismatch, frames-1, summary.VAdlerMismatch, frames-1,
			summary.MaxSizePctAbs, summary.MeanSizePctAbs)

		current.Cases = append(current.Cases, summary)
	}

	// Stable case order in the baseline file.
	sort.Slice(current.Cases, func(i, j int) bool { return current.Cases[i].Name < current.Cases[j].Name })

	baselinePath := filepath.Join("testdata", "chroma_subpel_scoreboard_baseline.json")
	_, statErr := os.Stat(baselinePath)
	if updateBaselines || os.IsNotExist(statErr) {
		buf, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			t.Fatalf("Marshal chroma subpel scoreboard baseline: %v", err)
		}
		buf = append(buf, '\n')
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(baselinePath), err)
		}
		if err := os.WriteFile(baselinePath, buf, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", baselinePath, err)
		}
		t.Logf("wrote baseline %s", baselinePath)
		return
	}
	if statErr != nil {
		t.Fatalf("stat %s: %v", baselinePath, statErr)
	}
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", baselinePath, err)
	}
	var base chromaSubpelBaseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("Unmarshal baseline: %v", err)
	}
	baseByName := make(map[string]chromaSubpelBaselineCase, len(base.Cases))
	for _, c := range base.Cases {
		baseByName[c.Name] = c
	}
	for _, cur := range current.Cases {
		b, ok := baseByName[cur.Name]
		if !ok {
			t.Errorf("baseline missing entry for %q; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name)
			continue
		}
		// Tightening direction: counts must not grow, keyframe match must not regress
		// from true to false, max size pct must not grow beyond a small slack.
		if cur.YAdlerMismatch > b.YAdlerMismatch {
			t.Errorf("[%s] y_adler_mismatch_inter_frames=%d exceeds baseline %d; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name, cur.YAdlerMismatch, b.YAdlerMismatch)
		}
		if cur.UAdlerMismatch > b.UAdlerMismatch {
			t.Errorf("[%s] u_adler_mismatch_inter_frames=%d exceeds baseline %d; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name, cur.UAdlerMismatch, b.UAdlerMismatch)
		}
		if cur.VAdlerMismatch > b.VAdlerMismatch {
			t.Errorf("[%s] v_adler_mismatch_inter_frames=%d exceeds baseline %d; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name, cur.VAdlerMismatch, b.VAdlerMismatch)
		}
		if b.KeyframeMatch && !cur.KeyframeMatch {
			t.Errorf("[%s] keyframe_y_u_v_adler_match regressed from true to false; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name)
		}
		if b.KeyframeQMatch && !cur.KeyframeQMatch {
			t.Errorf("[%s] keyframe_q_match regressed from true to false; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name)
		}
		if b.InterQMatch && !cur.InterQMatch {
			t.Errorf("[%s] inter_q_match_all_frames regressed from true to false; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name)
		}
		if cur.MaxSizePctAbs > b.MaxSizePctAbs+0.5 {
			t.Errorf("[%s] max_inter_size_delta_pct_abs=%.3f exceeds baseline %.3f + 0.5; rerun with GOVPX_UPDATE_BASELINES=1 if intended", cur.Name, cur.MaxSizePctAbs, b.MaxSizePctAbs)
		}
	}
	// Render a compact tightening progress log, ordered by name for determinism.
	for _, cur := range current.Cases {
		b := baseByName[cur.Name]
		t.Logf("[%s] tightening: y_mismatch %d→%d (%+d) u %d→%d (%+d) v %d→%d (%+d) max_size_pct %.3f→%.3f (%+.3f)",
			cur.Name,
			b.YAdlerMismatch, cur.YAdlerMismatch, cur.YAdlerMismatch-b.YAdlerMismatch,
			b.UAdlerMismatch, cur.UAdlerMismatch, cur.UAdlerMismatch-b.UAdlerMismatch,
			b.VAdlerMismatch, cur.VAdlerMismatch, cur.VAdlerMismatch-b.VAdlerMismatch,
			b.MaxSizePctAbs, cur.MaxSizePctAbs, cur.MaxSizePctAbs-b.MaxSizePctAbs)
	}
}
