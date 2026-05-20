//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVP9OracleTraceWriterEmitsFrameRows(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	var trace bytes.Buffer
	e.SetVP9OracleTraceWriter(&trace)
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("EncodeIntoWithResult returned empty packet")
	}
	row := trace.String()
	for _, want := range []string{
		`"row":"vp9_frame"`,
		`"frame_index":0`,
		`"key_frame":true`,
		`"refresh_frame_flags":255`,
	} {
		if !strings.Contains(row, want) {
			t.Fatalf("trace row %q missing %s", row, want)
		}
	}

	e.SetVP9OracleTraceWriter(nil)
	if e.vp9OracleTraceEnabled() {
		t.Fatal("trace active after disabling writer")
	}
	trace.Reset()
	if _, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 160, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult after disabling trace: %v", err)
	}
	if trace.Len() != 0 {
		t.Fatalf("trace emitted after disabling writer: %q", trace.String())
	}
}

func TestVP9OracleTraceWriterEmitsCBRRateFields(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}

	var trace bytes.Buffer
	e.SetVP9OracleTraceWriter(&trace)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 128, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	var row struct {
		ActiveBestQ          int     `json:"active_best_q"`
		ActiveWorstQ         int     `json:"active_worst_q"`
		RateCorrectionFactor float64 `json:"rate_correction_factor"`
		RecodeAllowed        bool    `json:"recode_allowed"`
		RecodeLoopCount      int     `json:"recode_loop_count"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(trace.Bytes()), &row); err != nil {
		t.Fatalf("trace row is not valid JSON: %v\n%s", err, trace.String())
	}
	if row.ActiveBestQ == 0 || row.ActiveWorstQ == 0 || row.RateCorrectionFactor <= 0 {
		t.Fatalf("rate trace fields = best:%d worst:%d correction:%f",
			row.ActiveBestQ, row.ActiveWorstQ, row.RateCorrectionFactor)
	}
	if row.RecodeAllowed || row.RecodeLoopCount != 0 {
		t.Fatalf("recode trace = allowed:%t loops:%d, want disabled one-pass VP9",
			row.RecodeAllowed, row.RecodeLoopCount)
	}
}
