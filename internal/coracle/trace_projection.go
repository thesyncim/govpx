package coracle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

var vp8EncoderDecisionFields = map[string]map[string]bool{
	"rate": {
		"type":                 true,
		"frame_index":          true,
		"frame_type":           true,
		"q_index":              true,
		"active_worst_quality": true,
		"active_best_quality":  true,
		"projected_frame_size": true,
		"this_frame_target":    true,
		"zbin_over_quant":      true,
	},
	"recode": {
		"type":        true,
		"frame_index": true,
		"loop_count":  true,
		"final_q":     true,
		"reason":      true,
	},
	"frame": {
		"type":                  true,
		"frame_index":           true,
		"frame_type":            true,
		"q_index":               true,
		"base_q_index":          true,
		"loop_filter_level":     true,
		"refresh_last":          true,
		"refresh_golden":        true,
		"refresh_altref":        true,
		"sign_bias_golden":      true,
		"sign_bias_altref":      true,
		"refresh_entropy_probs": true,
		"default_coef_reset":    true,
	},
}

var vp8InterCandidateFields = map[string]bool{
	"type":        true,
	"frame_index": true,
	"mb_row":      true,
	"mb_col":      true,
	"picker":      true,
	"mode_index":  true,
	"mode":        true,
	"ref_slot":    true,
	"ref_frame":   true,
	"outcome":     true,
	"became_best": true,
	"loop_break":  true,
	"mv_row":      true,
	"mv_col":      true,
}

var vp8InterCandidateThresholdFields = map[string]bool{
	"type":        true,
	"frame_index": true,
	"mb_row":      true,
	"mb_col":      true,
	"picker":      true,
	"mode_index":  true,
	"mode":        true,
	"ref_slot":    true,
	"threshold":   true,
}

// ProjectVP8EncoderDecisionTrace keeps the VP8 encoder trace rows that
// describe rate-control, recode, and frame-header decisions.
func ProjectVP8EncoderDecisionTrace(trace []byte) ([]byte, error) {
	return projectTrace(trace, func(row map[string]any) (map[string]bool, bool) {
		typ, _ := row["type"].(string)
		fields := vp8EncoderDecisionFields[typ]
		return fields, len(fields) != 0
	})
}

// ProjectVP8InterCandidateTrace keeps tested VP8 inter-candidate rows and the
// row identity fields needed for libvpx comparisons.
func ProjectVP8InterCandidateTrace(trace []byte) ([]byte, error) {
	return projectTrace(trace, func(row map[string]any) (map[string]bool, bool) {
		if typ, _ := row["type"].(string); typ != "inter_candidate" {
			return nil, false
		}
		if outcome, _ := row["outcome"].(string); outcome != "tested" {
			return nil, false
		}
		return vp8InterCandidateFields, true
	})
}

// ProjectVP8InterCandidateThresholdTrace keeps the VP8 inter-candidate row
// identity plus rd_threshes[] threshold values.
func ProjectVP8InterCandidateThresholdTrace(trace []byte) ([]byte, error) {
	return projectTrace(trace, func(row map[string]any) (map[string]bool, bool) {
		if typ, _ := row["type"].(string); typ != "inter_candidate" {
			return nil, false
		}
		return vp8InterCandidateThresholdFields, true
	})
}

// FirstTraceRows formats up to limit non-empty trace rows with their row index.
func FirstTraceRows(trace []byte, limit int) string {
	var buf bytes.Buffer
	lines := TraceLines(trace)
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		buf.WriteString(strconv.Itoa(i))
		buf.WriteString(": ")
		buf.Write(lines[i])
		buf.WriteByte('\n')
	}
	return buf.String()
}

// TraceLines returns non-empty JSONL rows without their trailing line endings.
func TraceLines(trace []byte) [][]byte {
	lines := bytes.Split(trace, []byte{'\n'})
	out := lines[:0]
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

func projectTrace(trace []byte, project func(map[string]any) (map[string]bool, bool)) ([]byte, error) {
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("coracle: trace row is not valid JSON: %w", err)
		}
		fields, ok := project(row)
		if !ok {
			continue
		}
		projected := make(map[string]any, len(fields))
		for field := range fields {
			if v, ok := row[field]; ok {
				projected[field] = v
			}
		}
		encoded, err := json.Marshal(projected)
		if err != nil {
			return nil, fmt.Errorf("coracle: marshal projected trace row: %w", err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("coracle: scan trace: %w", err)
	}
	return out.Bytes(), nil
}
