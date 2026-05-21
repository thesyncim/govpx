//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8SplitMVRDCandidateTrace compares govpx and libvpx inter-candidate
// rows for the BestQuality ARNR cohort whose frame 1 MB(0,0) mode decision
// distinguishes SPLITMV from NEWMV. It keeps the trace context in test logs and
// asserts that the expected candidate rows are present on both sides.
func TestVP8SplitMVRDCandidateTrace(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run SPLITMV RD parity")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := coracletest.VpxencOracle(t)

	opts := EncoderOptions{
		Width:             1280,
		Height:            720,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		Tuning:            TuneSSIM,
		ScreenContentMode: 1,
		TokenPartitions:   1,
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=vbr",
		"--screen-content-mode=1",
		"--token-parts=1",
		"--threads=4",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=1",
		"--arnr-type=2",
	})

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	// govpx side: capture inter_candidate trace.
	govpxTrace := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTrace)
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		_, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(vpxencOracle, opts, len(sources), 700, nil, extraArgs),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	t.Logf("govpx trace bytes=%d libvpx trace bytes=%d", govpxTrace.Len(), len(libvpxTrace))
	gRows := parseSplitMVInterCandidateRows(govpxTrace.Bytes(), 1, 0, 0)
	lRows := parseSplitMVInterCandidateRows(libvpxTrace, 1, 0, 0)
	t.Logf("MB(0,0) frame 1 candidates: govpx=%d libvpx=%d", len(gRows), len(lRows))

	// Pull the SPLITMV/LAST candidate from each side, including
	// "skipped_threshold" rows (govpx may early-exit at the threshold gate).
	for _, r := range gRows {
		if r.Mode == "SPLITMV" {
			t.Logf("govpx SPLITMV row: idx=%d ref=%s outcome=%s threshold=%d score=%d best_score_before=%d",
				r.ModeIndex, r.RefFrame, r.Outcome, r.Threshold, r.Score, 0)
		}
	}
	for _, r := range lRows {
		if r.Mode == "SPLITMV" {
			t.Logf("libvpx SPLITMV row: idx=%d ref=%s outcome=%s threshold=%d score=%d best_score_before=%d",
				r.ModeIndex, r.RefFrame, r.Outcome, r.Threshold, r.Score, 0)
		}
	}

	// Sort both by ModeIndex
	sort.Slice(gRows, func(i, j int) bool { return gRows[i].ModeIndex < gRows[j].ModeIndex })
	sort.Slice(lRows, func(i, j int) bool { return lRows[i].ModeIndex < lRows[j].ModeIndex })

	t.Logf("govpx MB(0,0) frame 1 candidates (sorted by mode_index):")
	for _, r := range gRows {
		t.Logf("  idx=%2d mode=%-10s ref=%-12s score=%-12d yrd=%-12d rate=%-7d rate_y=%-7d rate_uv=%-7d dist=%-9d dist_uv=%-7d skip=%v became_best=%v",
			r.ModeIndex, r.Mode, r.RefFrame, r.Score, r.YRD, r.Rate, r.RateY, r.RateUV, r.Distortion, r.DistortionUV, r.Skip, r.BecameBest)
	}
	t.Logf("libvpx MB(0,0) frame 1 candidates (sorted by mode_index):")
	for _, r := range lRows {
		t.Logf("  idx=%2d mode=%-10s ref=%-12s score=%-12d yrd=%-12d rate=%-7d rate_y=%-7d rate_uv=%-7d dist=%-9d dist_uv=%-7d skip=%v became_best=%v",
			r.ModeIndex, r.Mode, r.RefFrame, r.Score, r.YRD, r.Rate, r.RateY, r.RateUV, r.Distortion, r.DistortionUV, r.Skip, r.BecameBest)
	}

	// Locate the SPLITMV (LAST ref) and NEWMV (LAST ref) candidates by mode+ref on both sides.
	splitGov, splitLib := pickRow(gRows, "SPLITMV", "LAST_FRAME"), pickRow(lRows, "SPLITMV", "LAST_FRAME")
	newGov, newLib := pickRow(gRows, "NEWMV", "LAST_FRAME"), pickRow(lRows, "NEWMV", "LAST_FRAME")
	if splitGov == nil || splitLib == nil || newGov == nil || newLib == nil {
		t.Fatalf("missing MB(0,0) frame 1 LAST_FRAME candidates: govpx split=%v new=%v libvpx split=%v new=%v",
			splitGov != nil, newGov != nil, splitLib != nil, newLib != nil)
	}

	if splitGov != nil && newGov != nil {
		t.Logf("govpx MB(0,0) frame 1 SPLITMV vs NEWMV: split.score=%d new.score=%d delta=%d (split-new=%d)",
			splitGov.Score, newGov.Score, splitGov.Score-newGov.Score, splitGov.Score-newGov.Score)
		t.Logf("govpx SPLITMV: rate=%d (rate_y=%d rate_uv=%d) dist=%d (dist_uv=%d)",
			splitGov.Rate, splitGov.RateY, splitGov.RateUV, splitGov.Distortion, splitGov.DistortionUV)
		t.Logf("govpx NEWMV  : rate=%d (rate_y=%d rate_uv=%d) dist=%d (dist_uv=%d)",
			newGov.Rate, newGov.RateY, newGov.RateUV, newGov.Distortion, newGov.DistortionUV)
	}
	if splitLib != nil && newLib != nil {
		t.Logf("libvpx MB(0,0) frame 1 SPLITMV vs NEWMV: split.score=%d new.score=%d delta=%d (split-new=%d)",
			splitLib.Score, newLib.Score, splitLib.Score-newLib.Score, splitLib.Score-newLib.Score)
		t.Logf("libvpx SPLITMV: rate=%d (rate_y=%d rate_uv=%d) dist=%d (dist_uv=%d)",
			splitLib.Rate, splitLib.RateY, splitLib.RateUV, splitLib.Distortion, splitLib.DistortionUV)
		t.Logf("libvpx NEWMV  : rate=%d (rate_y=%d rate_uv=%d) dist=%d (dist_uv=%d)",
			newLib.Rate, newLib.RateY, newLib.RateUV, newLib.Distortion, newLib.DistortionUV)
	}
	if splitGov != nil && splitLib != nil {
		t.Logf("SPLITMV (LAST) gov-vs-lib: dscore=%d drate=%d (drate_y=%d drate_uv=%d) ddist=%d (ddist_uv=%d)",
			splitGov.Score-splitLib.Score, splitGov.Rate-splitLib.Rate, splitGov.RateY-splitLib.RateY, splitGov.RateUV-splitLib.RateUV,
			splitGov.Distortion-splitLib.Distortion, splitGov.DistortionUV-splitLib.DistortionUV)
	}
	if newGov != nil && newLib != nil {
		t.Logf("NEWMV (LAST) gov-vs-lib: dscore=%d drate=%d (drate_y=%d drate_uv=%d) ddist=%d (ddist_uv=%d)",
			newGov.Score-newLib.Score, newGov.Rate-newLib.Rate, newGov.RateY-newLib.RateY, newGov.RateUV-newLib.RateUV,
			newGov.Distortion-newLib.Distortion, newGov.DistortionUV-newLib.DistortionUV)
	}
}

type splitMVInterCandidateRow struct {
	FrameIndex   uint64 `json:"frame_index"`
	MBRow        int    `json:"mb_row"`
	MBCol        int    `json:"mb_col"`
	ModeIndex    int    `json:"mode_index"`
	Mode         string `json:"mode"`
	RefSlot      int    `json:"ref_slot"`
	RefFrame     string `json:"ref_frame"`
	Threshold    int    `json:"threshold"`
	Score        int    `json:"score"`
	YRD          int    `json:"yrd"`
	Rate         int    `json:"rate"`
	RateY        int    `json:"rate_y"`
	RateUV       int    `json:"rate_uv"`
	Distortion   int    `json:"distortion"`
	DistortionUV int    `json:"distortion_uv"`
	SSE          int    `json:"sse"`
	Skip         bool   `json:"skip"`
	BecameBest   bool   `json:"became_best"`
	LoopBreak    bool   `json:"loop_break"`
	Outcome      string `json:"outcome"`
}

func parseSplitMVInterCandidateRows(buf []byte, wantFrameIndex uint64, wantMBRow int, wantMBCol int) []splitMVInterCandidateRow {
	out := []splitMVInterCandidateRow{}
	for _, line := range bytes.Split(buf, []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"inter_candidate"`)) {
			continue
		}
		var r splitMVInterCandidateRow
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != wantFrameIndex || r.MBRow != wantMBRow || r.MBCol != wantMBCol {
			continue
		}
		out = append(out, r)
	}
	return out
}

func pickRow(rows []splitMVInterCandidateRow, mode, ref string) *splitMVInterCandidateRow {
	for i := range rows {
		if rows[i].Mode == mode && rows[i].RefFrame == ref {
			return &rows[i]
		}
	}
	return nil
}
