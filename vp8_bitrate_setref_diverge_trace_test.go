//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestBitrateSetRefDivergeTrace pins task #208's diagnostic for the
// `regression_640x360_threads1_bitrate_setref_diverge` fuzz seed.
// Reproduces the seed exactly, captures govpx and libvpx per-frame rate
// rows side-by-side, and dumps them for inspection. Skipped unless
// GOVPX_BITRATE_SETREF_TRACE=1 to avoid blocking CI.
func TestBitrateSetRefDivergeTrace(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run this trace")
	}
	if os.Getenv("GOVPX_BITRATE_SETREF_TRACE") != "1" {
		t.Skip("set GOVPX_BITRATE_SETREF_TRACE=1 to run this trace")
	}
	driver := findVpxencFrameFlagsOracle(t)
	const (
		width  = 640
		height = 360
		fps    = 30
	)
	frames := 3
	threads := 1
	targetKbps := 700
	cpuUsed := 0

	opts := oracleRuntimeBaseFuzzOptions(width, height, targetKbps, cpuUsed)
	opts.Threads = threads
	sources := oracleRuntimeFuzzSources(width, height, frames, 0)

	scriptEntries := []string{
		"-",
		"bitrate:300+fps:15+minq:4+maxq:52+drop:60+setref:last:panning:9",
		"bitrate:300+fps:15+minq:4+maxq:52+drop:60",
	}
	apply := map[int]func(*testing.T, *VP8Encoder){
		1: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			cfg := oracleRuntimeCurrentRateControlConfig(e)
			cfg.TargetBitrateKbps = 300
			cfg.MinQuantizer = 4
			cfg.MaxQuantizer = 52
			cfg.DropFrameWaterMark = 60
			cfg.DropFrameAllowed = true
			mustRuntime(t, "SetRateControl", e.SetRateControl(cfg))
			e.opts.FPS = 15
			e.opts.TimebaseNum = 1
			e.opts.TimebaseDen = 15
			e.timing = timingFromEncoderOptions(e.opts)
			img := encoderValidationPanningFrame(width, height, 9)
			mustRuntime(t, "SetReferenceFrame", e.SetReferenceFrame(ReferenceLast, img))
		},
		2: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			cfg := oracleRuntimeCurrentRateControlConfig(e)
			cfg.TargetBitrateKbps = 300
			cfg.MinQuantizer = 4
			cfg.MaxQuantizer = 52
			cfg.DropFrameWaterMark = 60
			cfg.DropFrameAllowed = true
			mustRuntime(t, "SetRateControl", e.SetRateControl(cfg))
			e.opts.FPS = 15
			e.opts.TimebaseNum = 1
			e.opts.TimebaseDen = 15
			e.timing = timingFromEncoderOptions(e.opts)
		},
	}

	requireOracleTraceBuild(t)
	var govTrace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(&govTrace)
	govpxFrames := make([][]byte, 0, frames)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		result, err := enc.EncodeInto(packet, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if !result.Dropped {
			govpxFrames = append(govpxFrames, append([]byte(nil), result.Data...))
		}
	}
	enc.Close()

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "frames.yuv")
	ivfPath := filepath.Join(dir, "frames.ivf")
	tracePath := filepath.Join(dir, "frames.jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)
	args := []string{
		"--infile=" + yuvPath,
		"--outfile=" + ivfPath,
		"--width=" + strconv.Itoa(width),
		"--height=" + strconv.Itoa(height),
		"--fps-num=" + strconv.Itoa(fps),
		"--fps-den=1",
		"--frames=" + strconv.Itoa(frames),
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=4",
		"--max-q=56",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--deadline=rt",
		"--cpu-used=" + strconv.Itoa(cpuUsed),
		"--end-usage=cbr",
		"--auto-alt-ref=0",
		"--token-parts=0",
		"--threads=" + strconv.Itoa(threads),
		"--control-script=" + strings.Join(scriptEntries, ","),
		"--copy-ref-log=" + filepath.Join(dir, "copy.log"),
	}
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", tracePath)
	cmd := exec.Command(driver, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-frameflags-oracle failed: %v\n%s", err, out)
	}
	libTrace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	t.Logf("\n=== GOVPX RATE ROWS ===\n%s", formatTaskRateRows(govTrace.Bytes()))
	t.Logf("\n=== LIBVPX RATE ROWS ===\n%s", formatTaskRateRows(libTrace))
	t.Logf("\n=== LIBVPX RAW FRAME 1 RATE JSONL ===")
	scan := bufio.NewScanner(bytes.NewReader(libTrace))
	scan.Buffer(make([]byte, 0, 64*1024), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "rate" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if int(fi) <= 2 {
			t.Logf("LIBVPX f=%v: %s", row["frame_index"], scan.Bytes())
		}
	}

	t.Logf("\n=== GOVPX FIRST 3 MB ROWS FRAME 1 ===\n%s", formatTaskMBRows(govTrace.Bytes(), 1, 3))
	t.Logf("\n=== LIBVPX FIRST 3 MB ROWS FRAME 1 ===\n%s", formatTaskMBRows(libTrace, 1, 3))
}

func formatTaskRateRows(trace []byte) string {
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 64*1024), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "rate" {
			continue
		}
		fmt.Fprintf(&out, "rate  f=%v q=%v awq=%v abq=%v target=%v projected=%v buffer=%v speed=%v ni_av_qi=%v ni_frames=%v avg_enc=%v avg_pick=%v\n",
			row["frame_index"], row["q_index"], row["active_worst_quality"],
			row["active_best_quality"], row["this_frame_target"],
			row["projected_frame_size"], row["buffer_level"],
			row["cpi_speed"], row["ni_av_qi"], row["ni_frames"], row["avg_encode_time"], row["avg_pick_mode_time"])
	}
	return out.String()
}

func formatTaskMBRows(trace []byte, frame int, maxCount int) string {
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 64*1024), 1<<22)
	count := 0
	for scan.Scan() {
		if count >= maxCount {
			break
		}
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "mb" {
			continue
		}
		fIdx, _ := row["frame_index"].(float64)
		if int(fIdx) != frame {
			continue
		}
		fmt.Fprintf(&out, "mb r=%v c=%v mode=%v ref=%v mv=(%v,%v) skip=%v eob=%v rate=%v\n",
			row["mb_row"], row["mb_col"], row["mode"],
			row["ref_frame"], row["mv_row"], row["mv_col"], row["skip"],
			row["eob_sum"], row["mb_rate"])
		count++
	}
	return out.String()
}
