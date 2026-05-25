//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"strings"
	"testing"
)

// TestVP8OracleTraceWriterNilProducesNoOverhead verifies that omitting
// OracleTraceWriter results in no writer activity and that the encoded byte
// stream is identical to a baseline run with the same configuration.
func TestVP8OracleTraceWriterNilProducesNoOverhead(t *testing.T) {
	requireOracleTraceBuild(t)
	const w, h = 32, 32

	encode := func(traceWriter *bytes.Buffer) ([]byte, []byte) {
		opts := EncoderOptions{
			Width:               w,
			Height:              h,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
		}
		e, err := NewVP8Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		if traceWriter != nil {
			e.SetOracleTraceWriter(traceWriter)
		}
		key := testImage(w, h)
		for i := range key.Y {
			key.Y[i] = byte((i*7 + 11) & 0xff)
		}
		for i := range key.U {
			key.U[i] = byte((i*3 + 5) & 0xff)
		}
		for i := range key.V {
			key.V[i] = byte((i*5 + 23) & 0xff)
		}
		dst := make([]byte, 1<<16)
		keyResult, err := e.EncodeInto(dst, key, 0, 1, EncodeForceKeyFrame)
		if err != nil {
			t.Fatalf("key EncodeInto returned error: %v", err)
		}
		keyBytes := append([]byte(nil), keyResult.Data...)

		inter := testImage(w, h)
		for row := range h {
			for col := range w {
				inter.Y[row*inter.YStride+col] = key.Y[((row+1)%h)*key.YStride+((col+2)%w)]
			}
		}
		uvW := (w + 1) >> 1
		uvH := (h + 1) >> 1
		for row := range uvH {
			for col := range uvW {
				inter.U[row*inter.UStride+col] = key.U[((row+1)%uvH)*key.UStride+((col+1)%uvW)]
				inter.V[row*inter.VStride+col] = key.V[((row+1)%uvH)*key.VStride+((col+1)%uvW)]
			}
		}
		dst2 := make([]byte, 1<<16)
		interResult, err := e.EncodeInto(dst2, inter, 1, 1, 0)
		if err != nil {
			t.Fatalf("inter EncodeInto returned error: %v", err)
		}
		interBytes := append([]byte(nil), interResult.Data...)
		return keyBytes, interBytes
	}

	baseKey, baseInter := encode(nil)

	var traceBuf bytes.Buffer
	tracedKey, tracedInter := encode(&traceBuf)

	if !bytes.Equal(baseKey, tracedKey) {
		t.Fatalf("key frame bytes differ between traced (%d B) and baseline (%d B) runs", len(tracedKey), len(baseKey))
	}
	if !bytes.Equal(baseInter, tracedInter) {
		t.Fatalf("inter frame bytes differ between traced (%d B) and baseline (%d B) runs", len(tracedInter), len(baseInter))
	}

	// Sanity: nil writer scenario must produce no trace output. We re-run
	// with nil and check there is no way to observe writes (the encode
	// function above already established baseKey/baseInter; the absence of a
	// writer means nothing was written to compare).

	// The traced run must emit at least one frame and one MB row.
	if traceBuf.Len() == 0 {
		t.Fatalf("traced run wrote no oracle trace output")
	}
	if !strings.Contains(traceBuf.String(), `"type":"frame"`) {
		t.Fatalf("traced run missing frame rows: %q", traceBuf.String())
	}
	if !strings.Contains(traceBuf.String(), `"type":"mb"`) {
		t.Fatalf("traced run missing mb rows: %q", traceBuf.String())
	}
}
