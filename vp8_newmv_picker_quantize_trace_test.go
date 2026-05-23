//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8NewMVPickerQuantizeTraceRows compares the libvpx-side NEWMV picker
// quantize trace contract for the 1280x720 SSIM BestQuality cohort. The oracle
// must emit per-block rows for frame 1 MB(0,0), including the quant path,
// zbin state, pre-quant coefficients, post-quant coefficients, and EOB.
func TestVP8NewMVPickerQuantizeTraceRows(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the NEWMV picker quantize trace comparison")
	}
	vpxencOracle := vp8test.VpxencOracle(t)

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

	trace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8BestARNRPickerOracleConfig(
			vpxencOracle,
			opts,
			len(sources),
			[]string{"GOVPX_ORACLE_NEWMV_PICKER=1"},
		),
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}

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
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	totalRows := 0
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"newmv_picker_quantize"`)) {
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

	t.Logf("newmv picker quantize rows = %d", totalRows)
	t.Logf("frame 1 MB(0,0) NEWMV rows = %d", len(capturedMB00Frame1NEWMV))
	if len(capturedMB00Frame1NEWMV) == 0 {
		t.Fatalf("no NEWMV picker rows captured at MB(0,0) frame 1")
	}

	for _, r := range capturedMB00Frame1NEWMV {
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
		acNonZero := 0
		for i, v := range r.Pre.Coeff {
			if i == 0 {
				continue
			}
			if v != 0 {
				acNonZero++
			}
		}
		t.Logf("block=%2d path=%s zbin_extra=%d zbin_oq=%d eob=%d pre.coeff[0]=%d pre.AC_nonzero=%d post.qcoeff_nonzero=%d post.last_nonzero_idx=%d mv=%v",
			r.Block, r.QuantPath, r.ZbinExtra, r.ZbinOQ, r.EOB,
			r.Pre.Coeff[0], acNonZero, nonZero, lastIdx, r.MV)
	}

	for _, r := range capturedMB00Frame1NEWMV {
		if r.Block != 0 {
			continue
		}
		t.Logf("block=0 mv=%v ref=%d path=%s zbin_extra=%d", r.MV, r.RefFrame, r.QuantPath, r.ZbinExtra)
		t.Logf("block=0 pre.coeff=%v", r.Pre.Coeff)
		t.Logf("block=0 pre.zbin=%v", r.Pre.Zbin)
		t.Logf("block=0 pre.round=%v", r.Pre.Round)
		t.Logf("block=0 pre.quant=%v", r.Pre.Quant)
		t.Logf("block=0 pre.quant_shift=%v", r.Pre.QuantShift)
		t.Logf("block=0 pre.zrun_zbin_boost=%v", r.Pre.ZrunZbinBoost)
		t.Logf("block=0 pre.dequant=%v", r.Pre.Dequant)
		t.Logf("block=0 post.qcoeff=%v", r.Post.QCoeff)
		t.Logf("block=0 post.dqcoeff=%v", r.Post.DqCoeff)
		break
	}
}
