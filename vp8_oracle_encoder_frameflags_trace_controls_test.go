//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
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
//   - the oracle trace side channel enabled through the coracle runner
//     (so we can prove the per-MB JSONL trace path is wired in the same
//     encode pass).
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

	ivfData, traceData, diag, err := coracle.VpxencVP8FrameFlagsEncodeTraceI420(
		encoderValidationI420Bytes(t, sources),
		coracle.VpxencVP8FrameFlagsConfig{
			BinaryPath:        driver,
			Width:             width,
			Height:            height,
			Frames:            frames,
			FPSNum:            fps,
			FPSDen:            1,
			TargetBitrateKbps: targetKbps,
			MinQ:              4,
			MaxQ:              56,
			KeyFrameMinDist:   999,
			KeyFrameMaxDist:   999,
			Deadline:          "good",
			CPUUsed:           0,
			EndUsage:          "cbr",
			AutoAltRef:        false,
			TokenPartitions:   0,
			ExtraArgs:         []string{"--control-script=" + strings.Join(scriptEntries, ",")},
		},
	)
	if err != nil {
		t.Fatalf("vpxenc-frameflags-oracle failed: %v\n%s", err, diag)
	}

	// (a) IVF produced and non-empty.
	payloads, err := testutil.IVFFramePayloads(ivfData)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatalf("vpxenc-frameflags-oracle produced no frames (ivf=%d bytes)", len(ivfData))
	}

	// (b) Trace file produced and non-empty.
	if len(traceData) == 0 {
		t.Fatalf("trace file is empty (combined binary may be missing oracle_trace.c TU)")
	}

	// (c) Trace contains both per-frame and per-MB rows.
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
		t.Fatalf("no per-frame trace rows in returned trace (control-script path may have suppressed the oracle emit hook)")
	}
	if mbRows == 0 {
		t.Fatalf("no per-MB trace rows in returned trace (oracle_trace.c TU likely not linked into libvpx.a)")
	}
	t.Logf("vpxenc-frameflags-oracle trace-control coverage ok: frames=%d ivf=%d trace_frame_rows=%d trace_mb_rows=%d",
		len(payloads), len(ivfData), frameRows, mbRows)
}
