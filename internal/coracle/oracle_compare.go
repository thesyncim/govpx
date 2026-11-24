// Package coracle hosts the libvpx oracle helpers and trace comparator. This
// file provides a stream-based comparator that walks the per-frame and per-MB
// JSON Lines emitted by govpx's encoder oracle (see
// ../../vp8_encoder_oracle_trace.go) alongside the equivalent stream produced by
// the patched libvpx vpxenc (see build_vpxenc.sh) and reports field-level
// divergences. The comparator is read-only and pure Go; it is independent of
// cgo and the libvpx build, so it can be unit-tested against synthetic JSONL
// inputs.
package coracle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DefaultMaxDivergences caps the number of divergences CompareOracleTraces
// returns by default; callers can override via CompareOptions.MaxDivergences.
// The cap exists so a single mismatched field does not produce gigabytes of
// downstream noise when both streams are fundamentally desynchronised.
const DefaultMaxDivergences = 64

// Divergence describes a single field-level mismatch between the govpx and
// libvpx oracle traces. Field naming matches the JSON keys emitted by both
// sides (see oracleTraceFrameRow / oracleTraceMBRow / oracleTraceRateRow /
// oracleTraceRecodeRow). MBRow and MBCol are only meaningful when
// RowKind == "mb" or "inter_candidate"; they are -1 for per-frame rows
// ("frame", "rate", "recode") or rows that lack MB coordinates.
type Divergence struct {
	// RowIndex is the zero-based ordinal of the row within whichever stream
	// the comparator was reading when the mismatch was detected. govpx and
	// libvpx are expected to emit rows in the same order so a shared index
	// is sufficient.
	RowIndex int
	// RowKind is "frame", "mb", "inter_candidate", "rate", or "recode"
	// for paired rows; "missing_govpx" or "missing_libvpx" when one stream
	// ended early; "type_mismatch" when the same row index has different
	// "type" values.
	RowKind string
	// FrameIndex is the per-frame counter copied from the row when known.
	FrameIndex int64
	// MBRow / MBCol are the macroblock coordinates for "mb" rows; -1 for
	// per-frame rows or stream-level errors.
	MBRow int
	MBCol int
	// Field is the JSON key whose value differs (or "" for a row-level
	// mismatch like "type_mismatch" / "missing_*").
	Field string
	// Govpx and Libvpx hold the field values as decoded from the JSON. For
	// stream-level mismatches the row-kind values are stored verbatim.
	Govpx  any
	Libvpx any
}

// CompareOptions tunes the comparator. Zero value is safe; defaults are
// applied lazily.
type CompareOptions struct {
	// MaxDivergences caps the number of divergences the comparator
	// collects before short-circuiting. <= 0 selects DefaultMaxDivergences.
	MaxDivergences int
	// IgnoreFields is a set of JSON keys the comparator skips entirely.
	// Used to silence fields that are intentionally not yet matched (for
	// example, plane Adler32 checksums while one side hashes the
	// post-loopfilter reference and the other hashes the unfiltered
	// reconstruction).
	IgnoreFields map[string]bool
	// NumericFieldTolerances allows selected numeric fields to differ by an
	// absolute amount without reporting a divergence. Fields omitted from
	// this map remain exact.
	NumericFieldTolerances map[string]float64
}

// CompareOracleTraces walks the two JSON Lines streams in lockstep and
// reports the first MaxDivergences mismatches. The streams are expected to
// follow the schema documented in ../../vp8_encoder_oracle_trace.go: per
// encoded frame both sides emit a {"type":"rate", ...} row, optionally a
// {"type":"recode", ...} row when the recode loop ran more than once,
// then a {"type":"frame", ...} row, and finally zero or more
// {"type":"inter_candidate", ...} and {"type":"mb", ...} rows for inter
// frames. The comparator is
// order-sensitive because both encoders emit rows deterministically;
// mismatched ordering is itself reported as a divergence so the caller can
// tell apart "wrong field value" from "stream desynchronised". The
// per-(row,field) diff logic is generic over the row type, so adding new
// fields to "rate" or "recode" rows on either side requires no comparator
// changes.
//
// Errors are returned only for I/O or JSON decode failures; semantic
// divergences are surfaced through the returned slice. A nil error with an
// empty slice means the streams matched within the configured tolerances.
func CompareOracleTraces(govpxJSONL io.Reader, libvpxJSONL io.Reader, opts CompareOptions) ([]Divergence, error) {
	if govpxJSONL == nil || libvpxJSONL == nil {
		return nil, errors.New("coracle: both readers must be non-nil")
	}
	maxDiv := opts.MaxDivergences
	if maxDiv <= 0 {
		maxDiv = DefaultMaxDivergences
	}
	gScan := bufio.NewScanner(govpxJSONL)
	lScan := bufio.NewScanner(libvpxJSONL)
	// Oracle rows are small (well under a few KB) but allow generous
	// buffers so we don't trip on padded debug fields added later.
	const maxLineBytes = 1 << 20
	gBuf := make([]byte, 0, 64*1024)
	lBuf := make([]byte, 0, 64*1024)
	gScan.Buffer(gBuf, maxLineBytes)
	lScan.Buffer(lBuf, maxLineBytes)

	var divergences []Divergence
	rowIndex := 0
	for {
		if len(divergences) >= maxDiv {
			break
		}
		gOK := gScan.Scan()
		lOK := lScan.Scan()
		if !gOK && !lOK {
			break
		}
		if !gOK {
			divergences = append(divergences, Divergence{
				RowIndex: rowIndex,
				RowKind:  "missing_govpx",
				MBRow:    -1,
				MBCol:    -1,
				Libvpx:   string(lScan.Bytes()),
			})
			// Continue draining libvpx so the caller sees how many
			// extra rows it has, up to MaxDivergences.
			for len(divergences) < maxDiv && lScan.Scan() {
				rowIndex++
				divergences = append(divergences, Divergence{
					RowIndex: rowIndex,
					RowKind:  "missing_govpx",
					MBRow:    -1,
					MBCol:    -1,
					Libvpx:   string(lScan.Bytes()),
				})
			}
			break
		}
		if !lOK {
			divergences = append(divergences, Divergence{
				RowIndex: rowIndex,
				RowKind:  "missing_libvpx",
				MBRow:    -1,
				MBCol:    -1,
				Govpx:    string(gScan.Bytes()),
			})
			for len(divergences) < maxDiv && gScan.Scan() {
				rowIndex++
				divergences = append(divergences, Divergence{
					RowIndex: rowIndex,
					RowKind:  "missing_libvpx",
					MBRow:    -1,
					MBCol:    -1,
					Govpx:    string(gScan.Bytes()),
				})
			}
			break
		}

		var gRow, lRow map[string]any
		if err := json.Unmarshal(gScan.Bytes(), &gRow); err != nil {
			return divergences, fmt.Errorf("coracle: govpx row %d: %w", rowIndex, err)
		}
		if err := json.Unmarshal(lScan.Bytes(), &lRow); err != nil {
			return divergences, fmt.Errorf("coracle: libvpx row %d: %w", rowIndex, err)
		}

		gType, _ := gRow["type"].(string)
		lType, _ := lRow["type"].(string)
		frameIdx := frameIndexOf(gRow, lRow)
		mbRow, mbCol := mbCoordsOf(gRow, lRow)

		if gType != lType {
			divergences = append(divergences, Divergence{
				RowIndex:   rowIndex,
				RowKind:    "type_mismatch",
				FrameIndex: frameIdx,
				MBRow:      mbRow,
				MBCol:      mbCol,
				Field:      "type",
				Govpx:      gType,
				Libvpx:     lType,
			})
			rowIndex++
			continue
		}

		// Compare every key the union of both rows exposes; this catches
		// fields that one side stopped emitting as well as fields that
		// shifted value.
		seen := make(map[string]struct{}, len(gRow)+len(lRow))
		for k := range gRow {
			seen[k] = struct{}{}
		}
		for k := range lRow {
			seen[k] = struct{}{}
		}
		for field := range seen {
			if opts.IgnoreFields[field] {
				continue
			}
			gv, gHas := gRow[field]
			lv, lHas := lRow[field]
			if !gHas {
				divergences = append(divergences, Divergence{
					RowIndex:   rowIndex,
					RowKind:    gType,
					FrameIndex: frameIdx,
					MBRow:      mbRow,
					MBCol:      mbCol,
					Field:      field,
					Govpx:      nil,
					Libvpx:     lv,
				})
				if len(divergences) >= maxDiv {
					break
				}
				continue
			}
			if !lHas {
				divergences = append(divergences, Divergence{
					RowIndex:   rowIndex,
					RowKind:    gType,
					FrameIndex: frameIdx,
					MBRow:      mbRow,
					MBCol:      mbCol,
					Field:      field,
					Govpx:      gv,
					Libvpx:     nil,
				})
				if len(divergences) >= maxDiv {
					break
				}
				continue
			}
			if !valuesEqual(gv, lv) {
				if tolerance, ok := opts.NumericFieldTolerances[field]; ok && numericValuesWithinTolerance(gv, lv, tolerance) {
					continue
				}
				divergences = append(divergences, Divergence{
					RowIndex:   rowIndex,
					RowKind:    gType,
					FrameIndex: frameIdx,
					MBRow:      mbRow,
					MBCol:      mbCol,
					Field:      field,
					Govpx:      gv,
					Libvpx:     lv,
				})
				if len(divergences) >= maxDiv {
					break
				}
			}
		}
		rowIndex++
	}
	if err := gScan.Err(); err != nil {
		return divergences, fmt.Errorf("coracle: govpx scanner: %w", err)
	}
	if err := lScan.Err(); err != nil {
		return divergences, fmt.Errorf("coracle: libvpx scanner: %w", err)
	}
	return divergences, nil
}

func numericValuesWithinTolerance(a any, b any, tolerance float64) bool {
	if tolerance < 0 {
		return false
	}
	af, ok := numericValue(a)
	if !ok {
		return false
	}
	bf, ok := numericValue(b)
	if !ok {
		return false
	}
	diff := af - bf
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// valuesEqual reports whether two JSON-decoded values are equal. JSON numbers
// decode into float64 in both rows, so direct comparison is sufficient for
// integer-valued fields. Slices (the per-MB EOB array decodes as []any) are
// compared element-wise. Nested maps are compared recursively for forward
// compatibility with future schema additions.
func valuesEqual(a any, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, va := range av {
			vb, has := bv[k]
			if !has || !valuesEqual(va, vb) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func frameIndexOf(rows ...map[string]any) int64 {
	for _, row := range rows {
		v, ok := row["frame_index"]
		if !ok {
			continue
		}
		switch fv := v.(type) {
		case float64:
			return int64(fv)
		case int64:
			return fv
		case int:
			return int64(fv)
		}
	}
	return -1
}

func mbCoordsOf(rows ...map[string]any) (int, int) {
	row := -1
	col := -1
	for _, r := range rows {
		if v, ok := r["mb_row"]; ok {
			if fv, ok := v.(float64); ok {
				row = int(fv)
			}
		}
		if v, ok := r["mb_col"]; ok {
			if fv, ok := v.(float64); ok {
				col = int(fv)
			}
		}
		if row >= 0 || col >= 0 {
			return row, col
		}
	}
	return row, col
}
