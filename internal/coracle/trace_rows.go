package coracle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// TraceRows parses oracle JSON Lines into row maps in stream order.
func TraceRows(trace []byte) ([]map[string]any, error) {
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	var rows []map[string]any
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("coracle: trace row is not valid JSON: %w", err)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("coracle: scan trace: %w", err)
	}
	return rows, nil
}

// TraceRowsOfType returns rows whose "type" field equals rowType.
func TraceRowsOfType(trace []byte, rowType string) ([]map[string]any, error) {
	rows, err := TraceRows(trace)
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, row := range rows {
		if typ, _ := row["type"].(string); typ == rowType {
			out = append(out, row)
		}
	}
	return out, nil
}

// TraceFrameRows returns trace rows with type "frame".
func TraceFrameRows(trace []byte) ([]map[string]any, error) {
	return TraceRowsOfType(trace, "frame")
}

// TraceRowsByFrame indexes rows of rowType by frame_index. If multiple rows
// for the same frame exist, the last row wins.
func TraceRowsByFrame(trace []byte, rowType string) (map[int64]map[string]any, error) {
	rows, err := TraceRowsOfType(trace, rowType)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]map[string]any, len(rows))
	for _, row := range rows {
		out[int64(TraceFloat(row["frame_index"]))] = row
	}
	return out, nil
}

// TraceFloat converts the numeric shapes produced by JSON trace decoders and
// hand-built rows into float64. Non-numeric values return 0.
func TraceFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}
