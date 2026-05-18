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

// TestVP8Noise0PerMBProbeTask206 is the task #206 investigation
// infrastructure for the deterministic 640x360 noise:0 fuzz seed
// (regression_640x360_serial_noise0_inter_diverge). It runs the failing
// fixture end-to-end on both govpx and the combined
// `vpxenc-frameflags-oracle` driver with GOVPX_ORACLE_TRACE_OUT set,
// then reports:
//
//  1. The first per-MB JSONL row in frame 1 whose canonical fields
//     (mode, ref_frame, mv_row, mv_col, skip, eob_sum, ...) diverge
//     between govpx and libvpx.
//  2. The frame-level cpi/sf field divergences for frame 1 (as dumped
//     by the libvpx oracle's emit_frame hook). govpx side reports nil
//     since its frame trace lacks the matching cpi/sf fields; the
//     libvpx values are visible standalone.
//  3. Per-frame byte parity (first_byte_diff, size_delta) so the
//     bitstream-level mismatch and the per-MB tracer findings can be
//     read off the same row.
//
// The probe runs two scenarios:
//
//   - "with-noise": frame 1 receives SetNoiseSensitivity(0) on the
//     govpx side and `noise:0` on the libvpx control-script side.
//     This is the failing case. Today (task #206) it still produces
//     1541 vs 1301 bytes (240B gap).
//   - "no-noise": no runtime control fires. Today this is the working
//     baseline (both sides 1534 bytes byte-identical).
//
// Gated behind GOVPX_TASK206_PROBE=1 so it never adds CI cost. It
// always passes (logging only) so it stays runnable as a long-lived
// audit aid as the gap is closed.
func TestVP8Noise0PerMBProbeTask206(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	if os.Getenv("GOVPX_TASK206_PROBE") != "1" {
		t.Skip("set GOVPX_TASK206_PROBE=1 to run the noise:0 per-MB investigation probe")
	}
	driver := findVpxencFrameFlagsOracle(t)

	scripts := []struct {
		name        string
		script      []string
		applyNoise0 bool
	}{
		{"with-noise", []string{"-", "noise:0"}, true},
		{"no-noise", []string{"-", "-"}, false},
	}

	for _, sc := range scripts {
		t.Run(sc.name, func(t *testing.T) {
			runTask206Probe(t, driver, sc.script, sc.applyNoise0)
		})
	}
}

func runTask206Probe(t *testing.T, driver string, script []string, applyNoise0 bool) {
	t.Helper()
	const (
		w          = 640
		h          = 360
		cpuUsed    = 0
		targetKbps = 300
	)
	opts := oracleRuntimeBaseFuzzOptions(w, h, targetKbps, cpuUsed)
	opts.Threads = 0
	sources := oracleRuntimeFuzzSources(w, h, 2, 0)

	requireOracleTraceBuild(t)
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	govpxFrames := make([][]byte, 0, len(sources))
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		if i == 1 && applyNoise0 {
			mustRuntime(t, "SetNoiseSensitivity(0)", enc.SetNoiseSensitivity(0))
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
	t.Logf("[%s] govpx frames=%d sizes=%v", t.Name(), len(govpxFrames), task206FrameSizes(govpxFrames))

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "noise.yuv")
	ivfPath := filepath.Join(dir, "noise.ivf")
	libvpxTracePath := filepath.Join(dir, "libvpx.jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)
	args := []string{
		"--infile=" + yuvPath,
		"--outfile=" + ivfPath,
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--fps-num=" + strconv.Itoa(opts.FPS),
		"--fps-den=1",
		"--frames=" + strconv.Itoa(len(sources)),
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--deadline=rt",
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--end-usage=cbr",
		"--auto-alt-ref=0",
		"--token-parts=" + strconv.Itoa(opts.TokenPartitions),
		"--frame-flags=0,0",
		"--control-script=" + strings.Join(script, ","),
	}
	cmd := exec.Command(driver, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+libvpxTracePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-frameflags-oracle failed: %v\n%s", err, out)
	}
	ivfData, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read libvpx ivf: %v", err)
	}
	libvpxFrames := parseIVFFramePayloads(t, ivfData)
	t.Logf("[%s] libvpx frames=%d sizes=%v", t.Name(), len(libvpxFrames), task206FrameSizes(libvpxFrames))

	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}
	// Dump traces to stable /tmp paths for offline inspection.
	tag := strings.ReplaceAll(t.Name(), "/", "_")
	_ = os.WriteFile("/tmp/govpx_task206_"+tag+".jsonl", govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile("/tmp/libvpx_task206_"+tag+".jsonl", libvpxTrace, 0o644)

	for fi := 0; fi < len(govpxFrames) && fi < len(libvpxFrames); fi++ {
		gf, lf := govpxFrames[fi], libvpxFrames[fi]
		firstByteDiff := -1
		minLen := len(gf)
		if len(lf) < minLen {
			minLen = len(lf)
		}
		for i := 0; i < minLen; i++ {
			if gf[i] != lf[i] {
				firstByteDiff = i
				break
			}
		}
		if firstByteDiff == -1 && len(gf) != len(lf) {
			firstByteDiff = minLen
		}
		t.Logf("[%s] frame%d byte-parity: govpx_len=%d libvpx_len=%d size_delta=%d first_byte_diff=%d",
			t.Name(), fi, len(gf), len(lf), len(gf)-len(lf), firstByteDiff)
	}

	// First diverging MB row in frame 1.
	gRows := task206ParseMBRowsForFrame(govpxTraceBuf.Bytes(), 1)
	lRows := task206ParseMBRowsForFrame(libvpxTrace, 1)
	t.Logf("[%s] frame1 govpx_mb_rows=%d libvpx_mb_rows=%d", t.Name(), len(gRows), len(lRows))
	minRows := len(gRows)
	if len(lRows) < minRows {
		minRows = len(lRows)
	}
	firstDivIdx := -1
	for i := 0; i < minRows; i++ {
		g, l := gRows[i], lRows[i]
		if !task206RowsEqual(g, l) {
			firstDivIdx = i
			break
		}
	}
	if firstDivIdx >= 0 {
		g, l := gRows[firstDivIdx], lRows[firstDivIdx]
		t.Logf("[%s] first MB divergence idx=%d mb_row=%v mb_col=%v",
			t.Name(), firstDivIdx, g["mb_row"], g["mb_col"])
		for _, field := range task206MBFields {
			gv, gok := g[field]
			lv, lok := l[field]
			if !gok && !lok {
				continue
			}
			gvs := task206JSONStr(gv)
			lvs := task206JSONStr(lv)
			if gvs != lvs {
				t.Logf("[%s]   field=%-32s govpx=%s libvpx=%s", t.Name(), field, gvs, lvs)
			}
		}
	} else {
		t.Logf("[%s] no per-MB divergence on frame 1 (rows match on canonical keys)", t.Name())
	}
}

var task206MBFields = []string{
	"mode", "ref_frame", "mv_row", "mv_col", "skip", "uv_mode",
	"partition", "rate", "rate_y", "rate_uv", "distortion", "distortion_uv",
	"sse", "score", "yrd",
	"eob_sum",
	"q_index", "active_worst_quality", "active_best_quality",
}

func task206FrameSizes(frames [][]byte) []int {
	out := make([]int, len(frames))
	for i, f := range frames {
		out[i] = len(f)
	}
	return out
}

func task206ParseMBRowsForFrame(data []byte, frameIdx int) []map[string]any {
	var rows []map[string]any
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 0, 64*1024), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if int(fi) != frameIdx {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func task206RowsEqual(g, l map[string]any) bool {
	for _, k := range task206MBFields {
		gv, gok := g[k]
		lv, lok := l[k]
		if !gok && !lok {
			continue
		}
		if task206JSONStr(gv) != task206JSONStr(lv) {
			return false
		}
	}
	return true
}

func task206JSONStr(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<err:%v>", err)
	}
	return string(b)
}
