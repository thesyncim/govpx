//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Task298SPLITMVRDBisect re-runs the BestARNR audit cohort
// (seed 19981bff, 1280x720 BestQuality/cpu0/VBR/screen-content=1/SSIM/
// ARNR=1/1/2) with the per-MB inter_candidate oracle trace enabled on
// BOTH sides (govpx via SetOracleTraceWriter; libvpx via the vpxenc-oracle
// trace path), then extracts the per-mode RD-candidate scores at MB(0,0)
// frame 1 and surfaces the SPLITMV vs NEWMV picker delta.
//
// Task #297 already pinned the upstream cause of the -5 byte ARNR pin-hold
// to the inter mode picker (govpx picks NEWMV, libvpx picks SPLITMV at
// MB(0,0) frame 1; the resulting zbin_mode_boost gap (4 vs 0) propagates
// through zbin_extra to UV qcoeff at scan_pos 0 of block 16). This test
// dumps the candidate RD scoreboard so the SPLITMV-vs-NEWMV picker
// divergence can be localized to the RATE or DISTORTION component of the
// SPLITMV RD score.
//
// Task #298 finding: govpx's NEWMV picker RD score at MB(0,0) frame 1 is
// score=102349 / yrd=73707 / rate=20474 / rate_y=7519 / dist=58282.
// libvpx's NEWMV picker RD score at MB(0,0) frame 1 is
// score=160686 / yrd=129509 / rate=48796 / rate_y=34799 / dist=55660.
// The dominant gap is rate_y (govpx 7519 vs libvpx 34799, delta=-27280;
// distortion delta is only +2622). The picker's NEWMV rate_y in govpx
// is 5x SMALLER than libvpx for the same MV=(8,16), same ref (frame 0
// reconstruction which is byte-identical), same source frame. govpx's
// picker quantize for NEWMV is producing all-zero Y qcoeff (see
// {"type":"mb","frame_index":1,"mb_row":0,"mb_col":0,"mode":"NEWMV"...,
// "qcoeff":[[0,0,...0],...,[0,0,...0]],"eob_sum":1}) while libvpx's
// picker quantize for NEWMV is producing enough non-zero Y coefficients
// to yield rate_y=34799 (libvpx ultimately accepts SPLITMV with the same
// resulting all-zero qcoeff). The Y rate disparity floods downstream:
// govpx's NEWMV yrd=73707 caps SPLITMV's segment_yrd commit in
// vp8_rd_pick_best_mbsegmentation (libvpx rdopt.c:1996 `if (tmp_rd <
// best_mode.yrd)` and govpx selectInterFrameSplitModeRDScore line 155
// `if shape.SegmentYRD < bestSegmentYRD`). With the cap at 73707
// (vs 129509 in libvpx), SPLITMV's per-label search at MB(0,0) cannot
// commit any partition and selectInterFrameSplitModeRDScore returns
// ok=false (outcome="splitmv_rd_dropout" surfaced by the new trace
// probes), so govpx never tests SPLITMV/LAST. The root cause is in
// govpx's picker-side Y quantize for NEWMV: see encoder_inter_rd.go:132
// (estimateInterResidualRDAccountingWithModeContext call to
// buildPredictedMacroblockCoefficientsInternal) vs libvpx rdopt.c:1647
// (macro_block_yrd). The downstream divergence is correctly localized
// but the actual cause of the picker quantize disparity (same residual
// + same zbin → different qcoeff) is still upstream of static
// inspection — it requires a per-block picker-side Y qcoeff oracle
// trace similar to the task #296 pre-trellis UV hook, which is the
// next step.
func TestVP8Task298SPLITMVRDBisect(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #298 SPLITMV RD bisect")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := findVpxencOracle(t)

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

	// libvpx side.
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "task298.yuv")
	ivfPath := filepath.Join(dir, "task298.ivf")
	libvpxTracePath := filepath.Join(dir, "task298.jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--best"
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		deadlineArg,
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+libvpxTracePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("vpxenc-oracle args: %v", args)
		t.Logf("vpxenc-oracle output:\n%s", out)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	t.Logf("govpx trace bytes=%d libvpx trace bytes=%d", govpxTrace.Len(), len(libvpxTrace))
	if err := os.WriteFile("/tmp/298-govpx-best.jsonl", govpxTrace.Bytes(), 0o644); err == nil {
		t.Logf("wrote /tmp/298-govpx-best.jsonl")
	}
	if err := os.WriteFile("/tmp/298-libvpx-best.jsonl", libvpxTrace, 0o644); err == nil {
		t.Logf("wrote /tmp/298-libvpx-best.jsonl")
	}

	gRows := task298ParseInterCandidateRows(govpxTrace.Bytes(), 1, 0, 0)
	lRows := task298ParseInterCandidateRows(libvpxTrace, 1, 0, 0)
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

type task298InterCandidateRow struct {
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

func task298ParseInterCandidateRows(buf []byte, wantFrameIndex uint64, wantMBRow int, wantMBCol int) []task298InterCandidateRow {
	out := []task298InterCandidateRow{}
	for _, line := range bytes.Split(buf, []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"inter_candidate"`)) {
			continue
		}
		var r task298InterCandidateRow
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

func pickRow(rows []task298InterCandidateRow, mode, ref string) *task298InterCandidateRow {
	for i := range rows {
		if rows[i].Mode == mode && rows[i].RefFrame == ref {
			return &rows[i]
		}
	}
	return nil
}
