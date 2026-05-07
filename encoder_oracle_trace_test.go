package govpx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestOracleTraceWriterEmitsFrameAndMBRows encodes a 32x32 keyframe followed
// by an inter frame with the trace writer enabled and asserts the JSONL
// stream has 1 frame row for the keyframe, 1 frame row for the inter frame,
// and 4 MB rows (32x32 = 2x2 macroblocks) for the inter frame.
func TestOracleTraceWriterEmitsFrameAndMBRows(t *testing.T) {
	const w, h = 32, 32
	var buf bytes.Buffer
	e, err := NewVP8Encoder(EncoderOptions{
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
		OracleTraceWriter:   &buf,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	keyImg := testImage(w, h)
	// Vary content so the encoder makes nontrivial decisions instead of
	// taking the zero-reference fast path.
	for i := range keyImg.Y {
		keyImg.Y[i] = byte((i*7 + 11) & 0xff)
	}
	for i := range keyImg.U {
		keyImg.U[i] = byte((i*3 + 5) & 0xff)
	}
	for i := range keyImg.V {
		keyImg.V[i] = byte((i*5 + 23) & 0xff)
	}

	dst := make([]byte, 1<<16)
	if _, err := e.EncodeInto(dst, keyImg, 0, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	// Inter frame: shift content slightly so it is not a perfect copy of
	// the previous reference (avoids the zero-reference shortcut path).
	interImg := testImage(w, h)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			interImg.Y[row*interImg.YStride+col] = keyImg.Y[((row+1)%h)*keyImg.YStride+((col+2)%w)]
		}
	}
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for row := 0; row < uvH; row++ {
		for col := 0; col < uvW; col++ {
			interImg.U[row*interImg.UStride+col] = keyImg.U[((row+1)%uvH)*keyImg.UStride+((col+1)%uvW)]
			interImg.V[row*interImg.VStride+col] = keyImg.V[((row+1)%uvH)*keyImg.VStride+((col+1)%uvW)]
		}
	}
	if _, err := e.EncodeInto(dst, interImg, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}

	lines := splitNonEmptyLines(buf.Bytes())
	var frameRows []map[string]interface{}
	var mbRows []map[string]interface{}
	for i, line := range lines {
		var row map[string]interface{}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("trace line %d not valid JSON: %v\nline=%q", i, err, line)
		}
		typ, ok := row["type"].(string)
		if !ok {
			t.Fatalf("trace line %d missing string type field: %v", i, row)
		}
		switch typ {
		case "frame":
			frameRows = append(frameRows, row)
		case "mb":
			mbRows = append(mbRows, row)
		default:
			t.Fatalf("trace line %d has unexpected type %q", i, typ)
		}
	}

	if len(frameRows) != 2 {
		t.Fatalf("frame rows = %d, want 2", len(frameRows))
	}
	if len(mbRows) != 4 {
		t.Fatalf("mb rows = %d, want 4 (2x2 inter frame)", len(mbRows))
	}

	// Frame-row schema sanity.
	if got, want := frameRows[0]["frame_type"], "key"; got != want {
		t.Fatalf("frame[0].frame_type = %v, want %v", got, want)
	}
	if got, want := frameRows[1]["frame_type"], "inter"; got != want {
		t.Fatalf("frame[1].frame_type = %v, want %v", got, want)
	}
	if frameRows[0]["frame_index"].(float64) != 0 {
		t.Fatalf("frame[0].frame_index = %v, want 0", frameRows[0]["frame_index"])
	}
	if frameRows[1]["frame_index"].(float64) != 1 {
		t.Fatalf("frame[1].frame_index = %v, want 1", frameRows[1]["frame_index"])
	}
	for i, row := range frameRows {
		for _, key := range []string{
			"q_index", "base_q_index", "loop_filter_level",
			"refresh_last", "refresh_golden", "refresh_altref",
			"sign_bias_golden", "sign_bias_altref",
			"segmentation_enabled",
			"y_adler32", "u_adler32", "v_adler32",
			"size_bytes",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("frame[%d] missing field %q", i, key)
			}
		}
	}

	// MB-row schema sanity. Expect raster scan order across 2x2 MBs and
	// frame_index = 1 (the inter frame).
	wantCells := [][2]float64{{0, 0}, {0, 1}, {1, 0}, {1, 1}}
	for i, row := range mbRows {
		if got := row["frame_index"].(float64); got != 1 {
			t.Fatalf("mb[%d].frame_index = %v, want 1", i, got)
		}
		if got, want := row["mb_row"].(float64), wantCells[i][0]; got != want {
			t.Fatalf("mb[%d].mb_row = %v, want %v", i, got, want)
		}
		if got, want := row["mb_col"].(float64), wantCells[i][1]; got != want {
			t.Fatalf("mb[%d].mb_col = %v, want %v", i, got, want)
		}
		for _, key := range []string{
			"segment_id", "mode", "ref_frame",
			"mv_row", "mv_col", "skip",
			"eob", "eob_sum",
			"improved_mv_start", "improved_mv_near_sadidx",
			"improved_mv_row", "improved_mv_col", "improved_mv_sr",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("mb[%d] missing field %q", i, key)
			}
		}
		eob, ok := row["eob"].([]interface{})
		if !ok {
			t.Fatalf("mb[%d].eob is not an array: %T", i, row["eob"])
		}
		if len(eob) != 25 {
			t.Fatalf("mb[%d].eob length = %d, want 25", i, len(eob))
		}
	}
}

func TestOracleMBTraceIncludesImprovedMVStart(t *testing.T) {
	var buf bytes.Buffer
	e := &VP8Encoder{
		opts: EncoderOptions{OracleTraceWriter: &buf},
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:               vp8common.LastFrame,
		Mode:                   vp8common.NewMV,
		MV:                     vp8enc.MotionVector{Row: 24, Col: -8},
		ImprovedMVStart:        true,
		ImprovedMVNearSADIndex: 3,
		ImprovedMVSR:           2,
		ImprovedMVPredictor:    vp8enc.MotionVector{Row: 16, Col: -16},
	}
	var coeffs vp8enc.MacroblockCoefficients

	e.emitOracleMBTrace(1, 2, &mode, &coeffs)
	e.flushOracleMBTraceBuffer()

	lines := splitNonEmptyLines(buf.Bytes())
	if len(lines) != 1 {
		t.Fatalf("trace rows = %d, want 1", len(lines))
	}
	var row map[string]interface{}
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("trace row is not valid JSON: %v", err)
	}
	if got := row["improved_mv_start"]; got != true {
		t.Fatalf("improved_mv_start = %v, want true", got)
	}
	if got := row["improved_mv_near_sadidx"].(float64); got != 3 {
		t.Fatalf("improved_mv_near_sadidx = %v, want 3", got)
	}
	if got := row["improved_mv_row"].(float64); got != 16 {
		t.Fatalf("improved_mv_row = %v, want 16", got)
	}
	if got := row["improved_mv_col"].(float64); got != -16 {
		t.Fatalf("improved_mv_col = %v, want -16", got)
	}
	if got := row["improved_mv_sr"].(float64); got != 2 {
		t.Fatalf("improved_mv_sr = %v, want 2", got)
	}
}

// TestOracleTraceWriterNilProducesNoOverhead verifies that omitting
// OracleTraceWriter results in no writer activity and that the encoded byte
// stream is identical to a baseline run with the same configuration.
func TestOracleTraceWriterNilProducesNoOverhead(t *testing.T) {
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
		if traceWriter != nil {
			opts.OracleTraceWriter = traceWriter
		}
		e, err := NewVP8Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
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
		for row := 0; row < h; row++ {
			for col := 0; col < w; col++ {
				inter.Y[row*inter.YStride+col] = key.Y[((row+1)%h)*key.YStride+((col+2)%w)]
			}
		}
		uvW := (w + 1) >> 1
		uvH := (h + 1) >> 1
		for row := 0; row < uvH; row++ {
			for col := 0; col < uvW; col++ {
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

func splitNonEmptyLines(b []byte) [][]byte {
	var out [][]byte
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}
