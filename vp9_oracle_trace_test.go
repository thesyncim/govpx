//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleTraceWriterEmitsFrameRows(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}

	var trace bytes.Buffer
	e.SetOracleTraceWriter(&trace)
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 128, 128, 128), dst)
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

	e.SetOracleTraceWriter(nil)
	trace.Reset()
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 160, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult after disabling trace: %v", err)
	}
	if trace.Len() != 0 {
		t.Fatalf("trace emitted after disabling writer: %q", trace.String())
	}
}

func TestVP9OracleTraceWriterEmitsCBRRateFields(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}

	var trace bytes.Buffer
	e.SetOracleTraceWriter(&trace)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 128, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	var row struct {
		TargetBitrateKbps   int `json:"target_bitrate_kbps"`
		EffectiveTargetKbps int `json:"effective_target_bitrate_kbps"`
		FrameTargetBits     int `json:"frame_target_bits"`

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
	if row.TargetBitrateKbps != 700 || row.EffectiveTargetKbps != 700 ||
		row.FrameTargetBits == 0 {
		t.Fatalf("rate target trace fields = target:%d effective:%d frame:%d",
			row.TargetBitrateKbps, row.EffectiveTargetKbps, row.FrameTargetBits)
	}
	if row.RecodeAllowed || row.RecodeLoopCount != 0 {
		t.Fatalf("recode trace = allowed:%t loops:%d, want disabled one-pass VP9",
			row.RecodeAllowed, row.RecodeLoopCount)
	}
}

func TestVP9OracleTraceWriterReportsTargetLevelClamp(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   10_000,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TargetLevel:         10,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}

	var trace bytes.Buffer
	e.SetOracleTraceWriter(&trace)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 96, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	var row struct {
		TargetBitrateKbps   int `json:"target_bitrate_kbps"`
		EffectiveTargetKbps int `json:"effective_target_bitrate_kbps"`
		FrameTargetBits     int `json:"frame_target_bits"`
		BufferOptimalBits   int `json:"buffer_optimal_bits"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(trace.Bytes()), &row); err != nil {
		t.Fatalf("trace row is not valid JSON: %v\n%s", err, trace.String())
	}
	if row.TargetBitrateKbps != 10_000 || row.EffectiveTargetKbps != 160 {
		t.Fatalf("target-level trace bitrate fields = target:%d effective:%d, want 10000 and 160",
			row.TargetBitrateKbps, row.EffectiveTargetKbps)
	}
	if row.FrameTargetBits != 32000 || row.BufferOptimalBits != 80000 {
		t.Fatalf("target-level trace rc fields = frame:%d optimal:%d, want 32000 and 80000",
			row.FrameTargetBits, row.BufferOptimalBits)
	}
}
