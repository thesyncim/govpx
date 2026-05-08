package govpx

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOracleARNRBufferAdler instruments the ARNR alt-ref buffer parity gap.
// govpx is documented as not yet byte-exact on ARNR, so this test only fails
// hard when neither side fires the ARNR path; otherwise it logs per-side
// frame indices and y/u/v Adler32 deltas as a scoreboard.
func TestOracleARNRBufferAdler(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle ARNR comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 12
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
		LookaheadFrames:   8,
		AutoAltRef:        true,
		ARNRMaxFrames:     5,
		ARNRStrength:      3,
		ARNRType:          3,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxLookaheadEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxARNREncoderTrace(t, vpxencOracle, "arnr-vbr", opts, targetKbps, sources)

	gFrames := oracleTraceFrameRows(t, govpxTrace)
	lFrames := oracleTraceFrameRows(t, libvpxTrace)

	gIdx, gFrame := findOracleARFFrame(gFrames)
	lIdx, lFrame := findOracleARFFrame(lFrames)

	if gFrame == nil && lFrame == nil {
		t.Errorf("no ARF frame appeared on either side; ARNR config did not fire (govpx_frames=%d libvpx_frames=%d)", len(gFrames), len(lFrames))
		return
	}
	if gFrame == nil {
		t.Logf("ARNR scoreboard: govpx emitted no ARF frame; libvpx ARF at trace_index=%d frame_index=%v y=%v u=%v v=%v",
			lIdx, lFrame["frame_index"], lFrame["y_adler32"], lFrame["u_adler32"], lFrame["v_adler32"])
		return
	}
	if lFrame == nil {
		t.Logf("ARNR scoreboard: libvpx emitted no ARF frame; govpx ARF at trace_index=%d frame_index=%v y=%v u=%v v=%v",
			gIdx, gFrame["frame_index"], gFrame["y_adler32"], gFrame["u_adler32"], gFrame["v_adler32"])
		return
	}

	gY := int64(traceFloat(gFrame["y_adler32"]))
	lY := int64(traceFloat(lFrame["y_adler32"]))
	gU := int64(traceFloat(gFrame["u_adler32"]))
	lU := int64(traceFloat(lFrame["u_adler32"]))
	gV := int64(traceFloat(gFrame["v_adler32"]))
	lV := int64(traceFloat(lFrame["v_adler32"]))
	t.Logf("ARNR frame: govpx_trace_index=%d libvpx_trace_index=%d", gIdx, lIdx)
	t.Logf("ARNR frame: govpx_frame_index=%v libvpx_frame_index=%v", gFrame["frame_index"], lFrame["frame_index"])
	t.Logf("ARNR frame: govpx_y=%d libvpx_y=%d delta=%d match=%v", gY, lY, gY-lY, gY == lY)
	t.Logf("ARNR frame: govpx_u=%d libvpx_u=%d delta=%d match=%v", gU, lU, gU-lU, gU == lU)
	t.Logf("ARNR frame: govpx_v=%d libvpx_v=%d delta=%d match=%v", gV, lV, gV-lV, gV == lV)
}

// findOracleARFFrame returns the (trace-order index, row) of the first frame
// whose refresh_altref is true and which is followed by another frame with
// the same source PTS. The libvpx oracle emits the hidden ARF frame back-to-
// back with the showed inter frame at the same source timestamp.
//
// If a "pts" field is not present we fall back to: any frame with
// refresh_altref=true and refresh_last=false and refresh_golden=false (the
// classic hidden ARF refresh pattern).
func findOracleARFFrame(rows []map[string]any) (int, map[string]any) {
	getPTS := func(row map[string]any) (int64, bool) {
		for _, key := range []string{"pts", "source_pts", "src_pts", "timestamp"} {
			if v, ok := row[key]; ok {
				return int64(traceFloat(v)), true
			}
		}
		return 0, false
	}
	for i, row := range rows {
		if !traceBool(row["refresh_altref"]) {
			continue
		}
		ptsI, hasI := getPTS(row)
		if hasI && i+1 < len(rows) {
			ptsNext, hasNext := getPTS(rows[i+1])
			if hasNext && ptsI == ptsNext {
				return i, row
			}
		}
		// Fallback: hidden ARF heuristic.
		if !traceBool(row["refresh_last"]) && !traceBool(row["refresh_golden"]) {
			return i, row
		}
	}
	return -1, nil
}

func traceBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case string:
		return x == "true" || x == "1"
	default:
		return false
	}
}

// captureGovpxLookaheadEncoderTrace drives the govpx encoder for ARNR fixtures.
// It tolerates ErrFrameNotReady (returned while the lookahead queue fills) and
// flushes at the end so all hidden ARF frames are emitted into the trace.
func captureGovpxLookaheadEncoderTrace(t *testing.T, opts EncoderOptions, sources []Image) []byte {
	t.Helper()
	var trace bytes.Buffer
	opts.OracleTraceWriter = &trace
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, opts.Width*opts.Height*3)
	for i, source := range sources {
		_, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
	}
	for {
		_, err := enc.FlushInto(packet)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			t.Fatalf("FlushInto returned error: %v", err)
		}
	}
	return append([]byte(nil), trace.Bytes()...)
}

// captureLibvpxARNREncoderTrace is a sibling of captureLibvpxEncoderTrace that
// allows enabling lookahead and auto-alt-ref. The shared helper bakes in
// `--lag-in-frames=0 --auto-alt-ref=0`, which would block the ARNR path even
// if vpxenc honoured the override of repeated flags.
func captureLibvpxARNREncoderTrace(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image) []byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	tracePath := filepath.Join(dir, name+".jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--lag-in-frames=" + strconv.Itoa(opts.LookaheadFrames),
		"--auto-alt-ref=1",
		"--arnr-maxframes=" + strconv.Itoa(opts.ARNRMaxFrames),
		"--arnr-strength=" + strconv.Itoa(opts.ARNRStrength),
		"--arnr-type=" + strconv.Itoa(opts.ARNRType),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc-oracle (ARNR) failed: %v\n%s", err, out)
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", tracePath, err)
	}
	return trace
}
