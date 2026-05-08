package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestDiagMBRateAggregator is a transient diagnostic that walks per-MB
// `aggregated_rate` rows from both encoders and prints the per-frame total
// drift, along with the first divergent MB. Used to localize the
// projected_frame_size aggregator gap behind the 64-bit tolerance in
// TestOracleEncoderTraceDecisionCompare.
//
// Gated on GOVPX_DIAG_MB_RATE=1 so it doesn't run in CI; the body is the
// same VBR/cpu3 panning fixture as TestOracleEncoderTraceDecisionCompare.
func TestDiagMBRateAggregator(t *testing.T) {
	if os.Getenv("GOVPX_DIAG_MB_RATE") != "1" {
		t.Skip("set GOVPX_DIAG_MB_RATE=1 to run per-MB rate aggregator diagnostic")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 6
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
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	gov := captureGovpxEncoderTrace(t, opts, sources)
	libvpx := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-mb-rate", opts, targetKbps, sources, []string{"--end-usage=vbr"})
	govRows := parseDiagMBRows(t, gov)
	libRows := parseDiagMBRows(t, libvpx)
	t.Logf("govpx mb rows=%d libvpx mb rows=%d", len(govRows), len(libRows))

	// Also dump projected_frame_size + entropy savings breakdown per frame.
	govProj := parseDiagProjectedWithSavings(t, gov)
	libProj := parseDiagProjectedWithSavings(t, libvpx)
	for fi := 0; fi < len(govProj) && fi < len(libProj); fi++ {
		t.Logf("frame %d gov{proj=%d coef=%d ref=%d} lib{proj=%d coef=%d ref=%d} delta_proj=%d delta_coef=%d delta_ref=%d",
			fi,
			govProj[fi].Projected, govProj[fi].CoefSavings, govProj[fi].RefFrameSavings,
			libProj[fi].Projected, libProj[fi].CoefSavings, libProj[fi].RefFrameSavings,
			govProj[fi].Projected-libProj[fi].Projected,
			govProj[fi].CoefSavings-libProj[fi].CoefSavings,
			govProj[fi].RefFrameSavings-libProj[fi].RefFrameSavings)
	}

	type frameSummary struct {
		FrameIndex int
		GovTotal   int
		LibTotal   int
		Delta      int
		FirstDivAt string
	}
	frames2 := make(map[int]*frameSummary)
	frameOrder := make([]int, 0)
	addFrame := func(fi int) *frameSummary {
		if fs, ok := frames2[fi]; ok {
			return fs
		}
		fs := &frameSummary{FrameIndex: fi}
		frames2[fi] = fs
		frameOrder = append(frameOrder, fi)
		return fs
	}
	govByKey := make(map[string]diagMBRow)
	for _, r := range govRows {
		govByKey[fmt.Sprintf("%d/%d/%d", r.FrameIndex, r.MBRow, r.MBCol)] = r
	}
	for _, r := range libRows {
		key := fmt.Sprintf("%d/%d/%d", r.FrameIndex, r.MBRow, r.MBCol)
		fs := addFrame(r.FrameIndex)
		fs.LibTotal = r.AggregatedRate
		gv, ok := govByKey[key]
		if !ok {
			continue
		}
		// The chosen-mode rate per MB
		rateDelta := gv.MBRate - r.MBRate
		aggDelta := gv.AggregatedRate - r.AggregatedRate
		if fs.FirstDivAt == "" && (rateDelta != 0 || aggDelta != 0) {
			fs.FirstDivAt = fmt.Sprintf("MB(%d,%d) gov_rate=%d lib_rate=%d gov_agg=%d lib_agg=%d ratedelta=%d aggdelta=%d", r.MBRow, r.MBCol, gv.MBRate, r.MBRate, gv.AggregatedRate, r.AggregatedRate, rateDelta, aggDelta)
		}
	}
	for _, r := range govRows {
		fs := addFrame(r.FrameIndex)
		fs.GovTotal = r.AggregatedRate
	}
	sort.Ints(frameOrder)
	for _, fi := range frameOrder {
		fs := frames2[fi]
		fs.Delta = fs.GovTotal - fs.LibTotal
		t.Logf("frame %d gov_totalrate=%d lib_totalrate=%d delta=%d (delta_bits=%d) first_div=%s",
			fs.FrameIndex, fs.GovTotal, fs.LibTotal, fs.Delta, fs.Delta>>8, fs.FirstDivAt)
	}
}

type diagMBRow struct {
	FrameIndex     int
	MBRow          int
	MBCol          int
	MBRate         int
	AggregatedRate int
}

func parseDiagMBRows(t *testing.T, trace []byte) []diagMBRow {
	t.Helper()
	var out []diagMBRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		mbr, _ := row["mb_rate"].(float64)
		agg, _ := row["aggregated_rate"].(float64)
		// Track the LAST occurrence of (frame_index, mb_row, mb_col) since
		// recodes overwrite the slot in govpx and libvpx flushes per-frame.
		out = append(out, diagMBRow{
			FrameIndex:     int(fi),
			MBRow:          int(mr),
			MBCol:          int(mc),
			MBRate:         int(mbr),
			AggregatedRate: int(agg),
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	// Keep only the last entry per (frame, row, col).
	dedup := make(map[string]diagMBRow)
	for _, r := range out {
		key := fmt.Sprintf("%d/%d/%d", r.FrameIndex, r.MBRow, r.MBCol)
		dedup[key] = r
	}
	deduped := make([]diagMBRow, 0, len(dedup))
	for _, r := range dedup {
		deduped = append(deduped, r)
	}
	sort.Slice(deduped, func(i, j int) bool {
		if deduped[i].FrameIndex != deduped[j].FrameIndex {
			return deduped[i].FrameIndex < deduped[j].FrameIndex
		}
		if deduped[i].MBRow != deduped[j].MBRow {
			return deduped[i].MBRow < deduped[j].MBRow
		}
		return deduped[i].MBCol < deduped[j].MBCol
	})
	return deduped
}

type diagProjectedRow struct {
	Frame           int
	Projected       int
	CoefSavings     int
	RefFrameSavings int
}

func parseDiagProjectedWithSavings(t *testing.T, trace []byte) []diagProjectedRow {
	t.Helper()
	var recs []diagProjectedRow
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ != "rate" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		pj, _ := row["projected_frame_size"].(float64)
		coef, _ := row["coef_savings_bits"].(float64)
		ref, _ := row["ref_frame_savings_bits"].(float64)
		recs = append(recs, diagProjectedRow{Frame: int(fi), Projected: int(pj), CoefSavings: int(coef), RefFrameSavings: int(ref)})
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Frame < recs[j].Frame })
	return recs
}
