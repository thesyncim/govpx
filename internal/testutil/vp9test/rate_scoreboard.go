package vp9test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

type RateScoreboardRow struct {
	FrameIndex           int     `json:"frame_index"`
	Flags                uint32  `json:"flags"`
	Dropped              bool    `json:"dropped"`
	DropReason           string  `json:"drop_reason"`
	KeyFrame             bool    `json:"key_frame"`
	ShowFrame            bool    `json:"show_frame"`
	CodedWidth           int     `json:"coded_width"`
	CodedHeight          int     `json:"coded_height"`
	BaseQIndex           int     `json:"base_qindex"`
	PublicQuantizer      int     `json:"public_quantizer"`
	SizeBytes            int     `json:"size_bytes"`
	SizeBits             int     `json:"size_bits"`
	FirstPartitionSize   int     `json:"first_partition_size"`
	TargetBitrateKbps    int     `json:"target_bitrate_kbps"`
	FrameTargetBits      int     `json:"frame_target_bits"`
	BufferLevelBits      int     `json:"buffer_level_bits"`
	BufferOptimalBits    int     `json:"buffer_optimal_bits"`
	RefreshFrameFlags    uint8   `json:"refresh_frame_flags"`
	RefreshFrameContext  bool    `json:"refresh_frame_context"`
	ErrorResilient       bool    `json:"error_resilient"`
	FrameParallel        bool    `json:"frame_parallel"`
	FrameContextIdx      int     `json:"frame_context_idx"`
	TxMode               int     `json:"tx_mode"`
	InterpFilter         int     `json:"interp_filter"`
	ReferenceMode        int     `json:"reference_mode"`
	CompoundAllowed      bool    `json:"compound_allowed"`
	ReferenceMask        uint8   `json:"reference_mask"`
	LoopFilterLevel      int     `json:"loop_filter_level"`
	TemporalLayerID      int     `json:"temporal_layer_id"`
	TemporalLayerCount   int     `json:"temporal_layer_count"`
	TemporalLayerSync    bool    `json:"temporal_layer_sync"`
	TL0PICIDX            uint8   `json:"tl0_pic_idx"`
	RecodeAllowed        bool    `json:"recode_allowed"`
	RecodeLoopCount      int     `json:"recode_loop_count"`
	ActiveBestQ          int     `json:"active_best_q"`
	ActiveWorstQ         int     `json:"active_worst_q"`
	RateCorrectionFactor float64 `json:"rate_correction_factor"`
	TileLog2Cols         int     `json:"tile_log2_cols"`
	TileLog2Rows         int     `json:"tile_log2_rows"`
}

func ParseRateScoreboardRows(t testing.TB, trace []byte) []RateScoreboardRow {
	t.Helper()
	rows := make([]RateScoreboardRow, 0, bytes.Count(trace, []byte("\n")))
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var raw struct {
			Row string `json:"row"`
			RateScoreboardRow
		}
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("VP9 rate trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if raw.Row != "vp9_frame" {
			continue
		}
		row := raw.RateScoreboardRow
		if row.SizeBits == 0 && row.SizeBytes != 0 {
			row.SizeBits = row.SizeBytes * 8
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan VP9 rate trace: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("VP9 rate trace has no vp9_frame rows:\n%s", trace)
	}
	return rows
}

func PctDelta(got int, want int) float64 {
	den := math.Max(1, math.Abs(float64(want)))
	return math.Abs(float64(got-want)) * 100 / den
}

func FormatRateScoreboardRows(govpxRows, libvpxRows []RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,govpx_flags,libvpx_flags,govpx_drop,libvpx_drop,govpx_key,libvpx_key,govpx_show,libvpx_show,govpx_width,libvpx_width,govpx_height,libvpx_height,govpx_q,libvpx_q,govpx_public_q,libvpx_public_q,govpx_active_best_q,libvpx_active_best_q,govpx_active_worst_q,libvpx_active_worst_q,govpx_rate_correction,libvpx_rate_correction,govpx_recode_allowed,libvpx_recode_allowed,govpx_recode_loops,libvpx_recode_loops,govpx_bytes,libvpx_bytes,govpx_bits,libvpx_bits,govpx_first_part,libvpx_first_part,govpx_target,libvpx_target,govpx_frame_target,libvpx_frame_target,govpx_buffer,libvpx_buffer,govpx_buffer_opt,libvpx_buffer_opt,govpx_refresh,libvpx_refresh,govpx_refresh_ctx,libvpx_refresh_ctx,govpx_tx,libvpx_tx,govpx_filter,libvpx_filter,govpx_refmode,libvpx_refmode,govpx_refmask,libvpx_refmask,govpx_lf,libvpx_lf,govpx_tile_cols,libvpx_tile_cols,govpx_tid,libvpx_tid,govpx_tlayers,libvpx_tlayers,govpx_tl0,libvpx_tl0,govpx_tsync,libvpx_tsync")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		fmt.Fprintf(&b, "%d,%#x,%#x,%t,%t,%t,%t,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6g,%.6g,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%t,%t,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%t,%t\n",
			g.FrameIndex, g.Flags, l.Flags, g.Dropped, l.Dropped, g.KeyFrame,
			l.KeyFrame, g.ShowFrame, l.ShowFrame, g.CodedWidth, l.CodedWidth,
			g.CodedHeight, l.CodedHeight, g.BaseQIndex, l.BaseQIndex,
			g.PublicQuantizer, l.PublicQuantizer, g.ActiveBestQ, l.ActiveBestQ,
			g.ActiveWorstQ, l.ActiveWorstQ, g.RateCorrectionFactor,
			l.RateCorrectionFactor, g.RecodeAllowed, l.RecodeAllowed,
			g.RecodeLoopCount, l.RecodeLoopCount, g.SizeBytes, l.SizeBytes,
			g.SizeBits, l.SizeBits, g.FirstPartitionSize, l.FirstPartitionSize,
			g.TargetBitrateKbps, l.TargetBitrateKbps, g.FrameTargetBits,
			l.FrameTargetBits, g.BufferLevelBits, l.BufferLevelBits,
			g.BufferOptimalBits, l.BufferOptimalBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.RefreshFrameContext, l.RefreshFrameContext,
			g.TxMode, l.TxMode, g.InterpFilter, l.InterpFilter,
			g.ReferenceMode, l.ReferenceMode, g.ReferenceMask, l.ReferenceMask,
			g.LoopFilterLevel, l.LoopFilterLevel, g.TileLog2Cols,
			l.TileLog2Cols, g.TemporalLayerID, l.TemporalLayerID,
			g.TemporalLayerCount, l.TemporalLayerCount, g.TL0PICIDX,
			l.TL0PICIDX, g.TemporalLayerSync, l.TemporalLayerSync)
	}
	return b.String()
}

func DroppedFrameIndices(rows []RateScoreboardRow) []int {
	out := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Dropped {
			out = append(out, row.FrameIndex)
		}
	}
	return out
}

func DropReasonCount(rows []RateScoreboardRow, reason string) int {
	count := 0
	for _, row := range rows {
		if row.DropReason == reason {
			count++
		}
	}
	return count
}

func CountHiddenRows(rows []RateScoreboardRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.ShowFrame {
			count++
		}
	}
	return count
}

func CountAltRefRefreshRows(rows []RateScoreboardRow, altRefMask uint8) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.KeyFrame && row.RefreshFrameFlags&altRefMask != 0 {
			count++
		}
	}
	return count
}

func SameIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func QHistogram(rows []RateScoreboardRow) [256]int {
	var hist [256]int
	for _, row := range rows {
		if row.Dropped {
			continue
		}
		if uint(row.BaseQIndex) < uint(len(hist)) {
			hist[row.BaseQIndex]++
		}
	}
	return hist
}

func HistogramDistance(a, b [256]int) (distance, mismatchedBins int) {
	for i := range a {
		d := a[i] - b[i]
		if d != 0 {
			mismatchedBins++
			if d < 0 {
				d = -d
			}
			distance += d
		}
	}
	return distance, mismatchedBins
}

func FormatQHistogram(hist [256]int) string {
	var b bytes.Buffer
	first := true
	for q, count := range hist {
		if count == 0 {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d:%d", q, count)
		first = false
	}
	if first {
		return "empty"
	}
	return b.String()
}

func FormatAutoAltRefVisibilityRows(govpxRows, libvpxRows []RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "packet,govpx_frame,libvpx_frame,govpx_show,libvpx_show,govpx_key,libvpx_key,govpx_refresh,libvpx_refresh,govpx_q,libvpx_q,govpx_bytes,libvpx_bytes,govpx_first_part,libvpx_first_part")
	limit := len(govpxRows)
	if len(libvpxRows) > limit {
		limit = len(libvpxRows)
	}
	for i := 0; i < limit; i++ {
		g, gok := rateScoreboardRowAt(govpxRows, i)
		l, lok := rateScoreboardRowAt(libvpxRows, i)
		fmt.Fprintf(&b, "%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			i,
			optionalInt(gok, g.FrameIndex),
			optionalInt(lok, l.FrameIndex),
			optionalBool(gok, g.ShowFrame),
			optionalBool(lok, l.ShowFrame),
			optionalBool(gok, g.KeyFrame),
			optionalBool(lok, l.KeyFrame),
			optionalHex(gok, g.RefreshFrameFlags),
			optionalHex(lok, l.RefreshFrameFlags),
			optionalInt(gok, g.BaseQIndex),
			optionalInt(lok, l.BaseQIndex),
			optionalInt(gok, g.SizeBytes),
			optionalInt(lok, l.SizeBytes),
			optionalInt(gok, g.FirstPartitionSize),
			optionalInt(lok, l.FirstPartitionSize))
	}
	return b.String()
}

func rateScoreboardRowAt(rows []RateScoreboardRow, i int) (RateScoreboardRow, bool) {
	if i < 0 || i >= len(rows) {
		return RateScoreboardRow{}, false
	}
	return rows[i], true
}

func optionalInt(ok bool, v int) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}

func optionalBool(ok bool, v bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%t", v)
}

func optionalHex(ok bool, v uint8) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%#x", v)
}
