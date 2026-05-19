//go:build govpx_oracle_trace

// Task #310 Phase 2: capture and analyze libvpx-side NEWMV picker quantize
// trace for the BestARNR seed at MB(0,0) frame 1 NEWMV.
//
// This test drives vpxenc-oracle with GOVPX_ORACLE_NEWMV_PICKER=1 against
// the same 1280x720 SSIM BestQuality/cpu0/VBR/screen-content=1 fixture
// task #304 used (its TestVP8Task304PickerYResidualAudit panning-frame
// pair, ARNRMaxFrames=1/Strength=1/Type=2), then parses the emitted
// {"type":"newmv_picker_quantize",...} rows and prints the MB(0,0)
// frame 1 Y-block-0 NEWMV slice. The captured row shows the libvpx
// quantize-path label ("regular" or "fast"), b->zbin_extra, the
// pre-quantize coeff array (so the row-15 predictor delta can be
// ruled in/out), and the post-quantize qcoeff + eob.
//
// Gating: GOVPX_WITH_ORACLE=1 must be set so the test only runs when
// the libvpx oracle binary is available. The test always Logf()'s the
// summary; failures are reserved for the "no NEWMV rows captured at
// MB(0,0)" case (oracle binary built without the task #310 hook).
package govpx

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestVP8Task310NewMVPickerQuantizeCapture(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #310 NEWMV picker quantize capture")
	}
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
		Threads:           1,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "task310.yuv")
	ivfPath := filepath.Join(dir, "task310.ivf")
	tracePath := filepath.Join(dir, "task310.jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--best",
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--target-bitrate=" + strconv.Itoa(opts.TargetBitrateKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--end-usage=vbr",
		"--screen-content-mode=1",
		"--token-parts=1",
		"--threads=1",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=1",
		"--arnr-type=2",
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(),
		"GOVPX_ORACLE_TRACE_OUT="+tracePath,
		"GOVPX_ORACLE_NEWMV_PICKER=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, out)
	}

	// Persist for offline analysis.
	persist := "/tmp/task310_libvpx_newmv_picker.jsonl"
	if data, err := os.ReadFile(tracePath); err == nil {
		_ = os.WriteFile(persist, data, 0o644)
		t.Logf("task310: persisted libvpx trace to %s (%d bytes)", persist, len(data))
	}

	f, err := os.Open(tracePath)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer f.Close()

	type pre struct {
		Coeff         []int `json:"coeff"`
		Zbin          []int `json:"zbin"`
		Round         []int `json:"round"`
		Quant         []int `json:"quant"`
		QuantShift    []int `json:"quant_shift"`
		ZrunZbinBoost []int `json:"zrun_zbin_boost"`
		Dequant       []int `json:"dequant"`
	}
	type post struct {
		QCoeff  []int `json:"qcoeff"`
		DqCoeff []int `json:"dqcoeff"`
	}
	type row struct {
		Type       string `json:"type"`
		FrameIndex int    `json:"frame_index"`
		MBRow      int    `json:"mb_row"`
		MBCol      int    `json:"mb_col"`
		Block      int    `json:"block"`
		Mode       string `json:"mode"`
		RefFrame   int    `json:"ref_frame"`
		MV         []int  `json:"mv"`
		QuantPath  string `json:"quant_path"`
		ZbinExtra  int    `json:"zbin_extra"`
		ZbinOQ     int    `json:"zbin_oq"`
		EOB        int    `json:"eob"`
		Pre        pre    `json:"pre"`
		Post       post   `json:"post"`
	}

	var capturedMB00Frame1NEWMV []row
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	totalRows := 0
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		// Cheap typename check before unmarshaling.
		if !bytesContains(line, []byte(`"type":"newmv_picker_quantize"`)) {
			continue
		}
		totalRows++
		var r row
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != 1 || r.MBRow != 0 || r.MBCol != 0 || r.Mode != "NEWMV" {
			continue
		}
		capturedMB00Frame1NEWMV = append(capturedMB00Frame1NEWMV, r)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}

	t.Logf("task310: total newmv_picker_quantize rows = %d", totalRows)
	t.Logf("task310: MB(0,0) frame 1 NEWMV rows = %d", len(capturedMB00Frame1NEWMV))
	if len(capturedMB00Frame1NEWMV) == 0 {
		t.Fatalf("no NEWMV picker rows captured at MB(0,0) frame 1 — task #310 hook may not be live")
	}

	// govpx-side reference (from task #304 final report):
	//   pre.coeff (Y block 0 for MV=(8,16) NEWMV/LAST_FRAME): all FDCT |AC|
	//     values < zbin so regular_quantize_b emits all-zero qcoeff.
	//   zbin_extra (govpx, fresh compute): 8
	//   post.eob: 0 (Y0..15 all-zero)
	//   rate_y (govpx, all 17 EOB tokens): 7519
	// libvpx reports rate_y=34799 → NEWMV at MB(0,0) MUST have non-zero eob
	// somewhere. The captured row pins (path, zbin_extra, coeff, qcoeff, eob).
	for _, r := range capturedMB00Frame1NEWMV {
		// Determine number of non-zero qcoeff and last non-zero index.
		nonZero := 0
		lastIdx := -1
		for i, v := range r.Post.QCoeff {
			if v != 0 {
				nonZero++
				if i > lastIdx {
					lastIdx = i
				}
			}
		}
		// Determine number of non-zero pre-quant AC coeff.
		acNonZero := 0
		for i, v := range r.Pre.Coeff {
			if i == 0 {
				continue
			}
			if v != 0 {
				acNonZero++
			}
		}
		t.Logf("task310: block=%2d path=%s zbin_extra=%d zbin_oq=%d eob=%d pre.coeff[0]=%d pre.AC_nonzero=%d post.qcoeff_nonzero=%d post.last_nonzero_idx=%d mv=%v",
			r.Block, r.QuantPath, r.ZbinExtra, r.ZbinOQ, r.EOB,
			r.Pre.Coeff[0], acNonZero, nonZero, lastIdx, r.MV)
	}

	// Print full pre/post for block 0 specifically (the focus of task #304).
	for _, r := range capturedMB00Frame1NEWMV {
		if r.Block != 0 {
			continue
		}
		t.Logf("task310: block=0 mv=%v ref=%d path=%s zbin_extra=%d", r.MV, r.RefFrame, r.QuantPath, r.ZbinExtra)
		t.Logf("task310: block=0 pre.coeff=%v", r.Pre.Coeff)
		t.Logf("task310: block=0 pre.zbin=%v", r.Pre.Zbin)
		t.Logf("task310: block=0 pre.round=%v", r.Pre.Round)
		t.Logf("task310: block=0 pre.quant=%v", r.Pre.Quant)
		t.Logf("task310: block=0 pre.quant_shift=%v", r.Pre.QuantShift)
		t.Logf("task310: block=0 pre.zrun_zbin_boost=%v", r.Pre.ZrunZbinBoost)
		t.Logf("task310: block=0 pre.dequant=%v", r.Pre.Dequant)
		t.Logf("task310: block=0 post.qcoeff=%v", r.Post.QCoeff)
		t.Logf("task310: block=0 post.dqcoeff=%v", r.Post.DqCoeff)
		break
	}
}

func bytesContains(b, sub []byte) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
