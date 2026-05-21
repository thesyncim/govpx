//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP8OracleVpxencFrameFlagsWritesTraceAndRuntimeControls is the infrastructure smoke
// test for the combined VP8 reference driver produced by
// internal/coracle/build_vpxenc_frameflags_oracle.sh. It encodes a
// short 64x64 panning fixture through `vpxenc-frameflags-oracle`
// with BOTH:
//
//   - a non-trivial --control-script that flips the libvpx bitrate
//     mid-stream (so we can prove the per-frame runtime-control path
//     is wired), and
//   - GOVPX_ORACLE_TRACE_OUT pointing at a temp file (so we can prove
//     the per-MB JSONL trace path is wired in the same encode pass).
//
// The test asserts that the resulting IVF has at least one frame,
// that the JSONL trace contains both `"type":"frame"` and
// `"type":"mb"` rows, and that no run of the combined surface drops
// either capability silently. It is intentionally cheap: 4 frames,
// 64x64, single-threaded — so it can run on every govpx_oracle_trace
// build without slowing down the parity gate.
func TestVP8OracleVpxencFrameFlagsWritesTraceAndRuntimeControls(t *testing.T) {
	driver := coracletest.VpxencFrameFlagsOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		frames     = 4
		targetKbps = 200
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "trace-controls.yuv")
	ivfPath := filepath.Join(dir, "trace-controls.ivf")
	tracePath := filepath.Join(dir, "trace-controls.jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)

	// Per-frame control-script: frame 0 keeps the starting bitrate
	// (which is itself --target-bitrate=200), frames 1..3 raise it
	// mid-stream. Empty entries ("-") are a no-op in vpxenc_frameflags.c
	// (see for_each_control_token), so the encode stays well-defined
	// even on the no-op frames.
	scriptEntries := []string{
		"-",
		"bitrate:" + strconv.Itoa(targetKbps*2),
		"-",
		"-",
	}

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
		"--deadline=good",
		"--cpu-used=0",
		"--end-usage=cbr",
		"--auto-alt-ref=0",
		"--token-parts=0",
		"--control-script=" + strings.Join(scriptEntries, ","),
	}

	// Run with GOVPX_ORACLE_TRACE_OUT pointing at the trace file so
	// the oracle TU inside the linked libvpx.a opens its writer.
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", tracePath)
	cmd := exec.Command(driver, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-frameflags-oracle failed: %v\n%s", err, out)
	}

	// (a) IVF produced and non-empty.
	ivfData, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read %s: %v", ivfPath, err)
	}
	payloads, err := testutil.IVFFramePayloads(ivfData)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatalf("vpxenc-frameflags-oracle produced no frames (ivf=%d bytes)", len(ivfData))
	}

	// (b) Trace file produced and non-empty.
	traceInfo, err := os.Stat(tracePath)
	if err != nil {
		t.Fatalf("trace file missing: %v", err)
	}
	if traceInfo.Size() == 0 {
		t.Fatalf("trace file is empty (combined binary may be missing oracle_trace.c TU)")
	}

	// (c) Trace contains both per-frame and per-MB rows.
	traceData, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var frameRows, mbRows int
	scanner := bufio.NewScanner(bytes.NewReader(traceData))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "frame":
			frameRows++
		case "mb":
			mbRows++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if frameRows == 0 {
		t.Fatalf("no per-frame trace rows in %s (control-script path may have suppressed the oracle emit hook)", tracePath)
	}
	if mbRows == 0 {
		t.Fatalf("no per-MB trace rows in %s (oracle_trace.c TU likely not linked into libvpx.a)", tracePath)
	}
	t.Logf("vpxenc-frameflags-oracle trace-control coverage ok: frames=%d ivf=%d trace_frame_rows=%d trace_mb_rows=%d",
		len(payloads), len(ivfData), frameRows, mbRows)
}
