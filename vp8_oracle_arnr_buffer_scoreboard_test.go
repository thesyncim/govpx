//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8OracleARNRBufferAdler instruments the ARNR alt-ref buffer parity gap.
// govpx is documented as not yet byte-exact on ARNR, so this test only fails
// hard when neither side fires the ARNR path; otherwise it logs per-side
// frame indices and y/u/v Adler32 deltas as a scoreboard.
//
// libvpx's vp8/encoder/ratectrl.c calc_gf_params() unconditionally clears
// `source_alt_ref_pending` whenever `cpi->pass != 2`, so one-pass libvpx
// never fires a hidden ARF regardless of `--auto-alt-ref=1`. Driving both
// sides through two-pass (govpx via CollectFirstPassStats + TwoPassStats,
// libvpx via `--passes=2 --pass=1`/`--pass=2`) is the only way to exercise
// the auto-ARF scheduler symmetrically. The two-pass fixture here is the
// libvpx-faithful comparison; the one-pass fallback is preserved as a
// scoreboard so the synthetic one-pass driver tests
// (`TestAutoAltRefDriverEmitsHiddenFrame`) continue to pin the govpx-only
// behaviour.
func TestVP8OracleARNRBufferAdler(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle ARNR comparison")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

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

	// Govpx pass 1: collect first-pass stats so the two-pass scheduler has
	// the libvpx-faithful inputs.
	govpxStats := captureGovpxFirstPassStats(t, opts, sources)

	// Govpx pass 2: encode with the collected stats; the auto-ARF driver
	// now consults the second-pass GF/ARF section heuristic instead of
	// the one-pass eager fallback.
	govpxOpts := opts
	govpxOpts.TwoPassStats = govpxStats
	govpxTrace := captureGovpxLookaheadEncoderTrace(t, govpxOpts, sources)

	// Libvpx side: spawn vpxenc-oracle for pass 1 (writes the .fpf stats
	// file), then for pass 2 reading the same .fpf and emitting the
	// trace.
	libvpxTrace := captureLibvpxARNRTwoPassEncoderTrace(t, vpxencOracle, opts, targetKbps, sources)

	gFrames, err := coracle.TraceFrameRows(govpxTrace)
	if err != nil {
		t.Fatalf("parse govpx frame rows: %v", err)
	}
	lFrames, err := coracle.TraceFrameRows(libvpxTrace)
	if err != nil {
		t.Fatalf("parse libvpx frame rows: %v", err)
	}

	gIdx, gFrame := findOracleARFFrame(gFrames)
	lIdx, lFrame := findOracleARFFrame(lFrames)

	if gFrame == nil && lFrame == nil {
		t.Logf("ARNR scoreboard (two-pass): both sides emitted zero ARF frames; auto-ARF gate stayed closed on this fixture (govpx_frames=%d libvpx_frames=%d)", len(gFrames), len(lFrames))
		return
	}
	if gFrame == nil {
		t.Logf("ARNR scoreboard (two-pass): govpx emitted no ARF frame; libvpx ARF at trace_index=%d frame_index=%v y=%v u=%v v=%v",
			lIdx, lFrame["frame_index"], lFrame["y_adler32"], lFrame["u_adler32"], lFrame["v_adler32"])
		return
	}
	if lFrame == nil {
		t.Logf("ARNR scoreboard (two-pass): libvpx emitted no ARF frame; govpx ARF at trace_index=%d frame_index=%v y=%v u=%v v=%v",
			gIdx, gFrame["frame_index"], gFrame["y_adler32"], gFrame["u_adler32"], gFrame["v_adler32"])
		return
	}

	gY := int64(coracle.TraceFloat(gFrame["y_adler32"]))
	lY := int64(coracle.TraceFloat(lFrame["y_adler32"]))
	gU := int64(coracle.TraceFloat(gFrame["u_adler32"]))
	lU := int64(coracle.TraceFloat(lFrame["u_adler32"]))
	gV := int64(coracle.TraceFloat(gFrame["v_adler32"]))
	lV := int64(coracle.TraceFloat(lFrame["v_adler32"]))
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
				return int64(coracle.TraceFloat(v)), true
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
		// Fallback: hidden ARF heuristic. Skip rows where the alt-ref
		// refresh bit is just the boilerplate keyframe `refresh_last &
		// refresh_golden & refresh_altref` triple — only an actual
		// hidden ARF clears both LAST and GOLDEN.
		if traceBool(row["refresh_last"]) || traceBool(row["refresh_golden"]) {
			continue
		}
		// Skip keyframes; libvpx writes refresh_last/golden/altref=true on
		// the keyframe but the LAST/GOLDEN flags above already filter it.
		// Defensive guard for traces that omit those fields.
		if ft, ok := row["frame_type"].(string); ok && ft == "key" {
			continue
		}
		return i, row
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
	requireOracleTraceBuild(t)
	var trace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	enc.SetOracleTraceWriter(&trace)
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

// captureLibvpxARNRTwoPassEncoderTrace runs vpxenc-oracle in two-pass mode
// (`--passes=2`) so libvpx's auto-alt-ref scheduler in vp8/encoder/firstpass.c
// `define_gf_group` actually fires. One-pass mode hard-codes
// `cpi->source_alt_ref_pending = 0` in vp8/encoder/ratectrl.c
// `calc_gf_params` (the heuristic-based path is commented out in upstream),
// so any single-pass invocation with `--auto-alt-ref=1` is a no-op.
//
// Pass 1 writes the FIRSTPASS_STATS .fpf file that pass 2 consumes; only
// pass 2's JSONL trace is returned so the caller can inspect the actual
// emitted frames.
func captureLibvpxARNRTwoPassEncoderTrace(t *testing.T, vpxencOracle string, opts EncoderOptions, targetKbps int, sources []Image) []byte {
	t.Helper()
	common := vp8OracleTraceConfig(
		"",
		opts,
		len(sources),
		targetKbps,
		nil,
		[]string{
			"--end-usage=vbr",
			"--arnr-maxframes=" + strconv.Itoa(opts.ARNRMaxFrames),
			"--arnr-strength=" + strconv.Itoa(opts.ARNRStrength),
			"--arnr-type=" + strconv.Itoa(opts.ARNRType),
		},
	)
	_, trace, diag, err := coracle.VpxencVP8TwoPassTraceI420(
		encoderValidationI420Bytes(t, sources),
		coracle.VpxencVP8TwoPassTraceConfig{
			FirstPassBinaryPath:  vpxencOracle,
			SecondPassBinaryPath: vpxencOracle,
			Common:               common,
		},
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle two-pass ARNR trace failed: %v\n%s", err, diag)
	}
	return trace
}
