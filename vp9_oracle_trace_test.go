//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
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
	trace.Reset()
	if _, err := e.EncodeIntoWithResult(
		newVP9YCbCrForTest(width, height, 160, 128, 128), dst); err != nil {
		t.Fatalf("EncodeIntoWithResult after disabling trace: %v", err)
	}
	if trace.Len() != 0 {
		t.Fatalf("trace emitted after disabling writer: %q", trace.String())
	}
}
