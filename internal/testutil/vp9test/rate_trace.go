package vp9test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

type RateTraceRow struct {
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
	EffectiveTargetKbps  int     `json:"effective_target_bitrate_kbps"`
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

func ParseRateTraceRows(t testing.TB, trace []byte) []RateTraceRow {
	t.Helper()
	rows := make([]RateTraceRow, 0, bytes.Count(trace, []byte("\n")))
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var raw struct {
			Row string `json:"row"`
			RateTraceRow
		}
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("VP9 rate trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if raw.Row != "vp9_frame" {
			continue
		}
		row := raw.RateTraceRow
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

func FormatRateTraceRows(govpxRows, libvpxRows []RateTraceRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,govpx_flags,libvpx_flags,govpx_drop,libvpx_drop,govpx_key,libvpx_key,govpx_show,libvpx_show,govpx_width,libvpx_width,govpx_height,libvpx_height,govpx_q,libvpx_q,govpx_public_q,libvpx_public_q,govpx_active_best_q,libvpx_active_best_q,govpx_active_worst_q,libvpx_active_worst_q,govpx_rate_correction,libvpx_rate_correction,govpx_recode_allowed,libvpx_recode_allowed,govpx_recode_loops,libvpx_recode_loops,govpx_bytes,libvpx_bytes,govpx_bits,libvpx_bits,govpx_first_part,libvpx_first_part,govpx_target,libvpx_target,govpx_effective_target,libvpx_effective_target,govpx_frame_target,libvpx_frame_target,govpx_buffer,libvpx_buffer,govpx_buffer_opt,libvpx_buffer_opt,govpx_refresh,libvpx_refresh,govpx_refresh_ctx,libvpx_refresh_ctx,govpx_tx,libvpx_tx,govpx_filter,libvpx_filter,govpx_refmode,libvpx_refmode,govpx_refmask,libvpx_refmask,govpx_lf,libvpx_lf,govpx_tile_cols,libvpx_tile_cols,govpx_tid,libvpx_tid,govpx_tlayers,libvpx_tlayers,govpx_tl0,libvpx_tl0,govpx_tsync,libvpx_tsync")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		fmt.Fprintf(&b, "%d,%#x,%#x,%t,%t,%t,%t,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6g,%.6g,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%t,%t,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%t,%t\n",
			g.FrameIndex, g.Flags, l.Flags, g.Dropped, l.Dropped, g.KeyFrame,
			l.KeyFrame, g.ShowFrame, l.ShowFrame, g.CodedWidth, l.CodedWidth,
			g.CodedHeight, l.CodedHeight, g.BaseQIndex, l.BaseQIndex,
			g.PublicQuantizer, l.PublicQuantizer, g.ActiveBestQ, l.ActiveBestQ,
			g.ActiveWorstQ, l.ActiveWorstQ, g.RateCorrectionFactor,
			l.RateCorrectionFactor, g.RecodeAllowed, l.RecodeAllowed,
			g.RecodeLoopCount, l.RecodeLoopCount, g.SizeBytes, l.SizeBytes,
			g.SizeBits, l.SizeBits, g.FirstPartitionSize, l.FirstPartitionSize,
			g.TargetBitrateKbps, l.TargetBitrateKbps, g.EffectiveTargetKbps,
			l.EffectiveTargetKbps, g.FrameTargetBits, l.FrameTargetBits,
			g.BufferLevelBits, l.BufferLevelBits,
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

func FormatSingleRateTraceRows(rows []RateTraceRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,flags,drop,reason,key,show,width,height,q,public_q,bytes,bits,first_part,target,effective_target,frame_target,buffer,refresh,refresh_ctx,tx,filter,refmode,refmask,lf,tile_cols,tid,tlayers,tl0,tsync")
	for _, row := range rows {
		fmt.Fprintf(&b, "%d,%#x,%t,%s,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%t,%d,%d,%d,%#x,%d,%d,%d,%d,%d,%t\n",
			row.FrameIndex, row.Flags, row.Dropped, row.DropReason, row.KeyFrame,
			row.ShowFrame, row.CodedWidth, row.CodedHeight, row.BaseQIndex,
			row.PublicQuantizer, row.SizeBytes, row.SizeBits,
			row.FirstPartitionSize, row.TargetBitrateKbps,
			row.EffectiveTargetKbps, row.FrameTargetBits, row.BufferLevelBits,
			row.RefreshFrameFlags, row.RefreshFrameContext, row.TxMode, row.InterpFilter,
			row.ReferenceMode, row.ReferenceMask, row.LoopFilterLevel,
			row.TileLog2Cols, row.TemporalLayerID, row.TemporalLayerCount,
			row.TL0PICIDX, row.TemporalLayerSync)
	}
	return b.String()
}

type RateTraceFlagMapper func(uint32) uint32

type TransitionStats struct {
	Rows                     int
	FlagMismatches           int
	DropMismatches           int
	KeyMismatches            int
	ShowMismatches           int
	CodedSizeMismatches      int
	QMismatches              int
	PublicQMismatches        int
	SizeMismatches           int
	FirstPartitionMismatches int
	TargetMismatches         int
	BufferMismatches         int
	BufferOptimalMismatches  int
	RefreshMismatches        int
	HeaderMismatches         int
	ModeHeaderMismatches     int
	LoopFilterMismatches     int
	TileMismatches           int
	TemporalMismatches       int
	TL0Mismatches            int
	MaxQDrift                int
	MaxSizeDeltaPct          float64
	MaxBufferDeltaPct        float64
	MaxBufferOptimalDeltaPct float64
}

func (s TransitionStats) HasMismatch() bool {
	return s.FlagMismatches != 0 || s.DropMismatches != 0 ||
		s.KeyMismatches != 0 || s.ShowMismatches != 0 ||
		s.CodedSizeMismatches != 0 ||
		s.QMismatches != 0 || s.PublicQMismatches != 0 ||
		s.SizeMismatches != 0 || s.FirstPartitionMismatches != 0 ||
		s.TargetMismatches != 0 || s.BufferMismatches != 0 ||
		s.BufferOptimalMismatches != 0 || s.RefreshMismatches != 0 ||
		s.HeaderMismatches != 0 || s.ModeHeaderMismatches != 0 ||
		s.LoopFilterMismatches != 0 || s.TileMismatches != 0 ||
		s.TemporalMismatches != 0 || s.TL0Mismatches != 0
}

func (s TransitionStats) String() string {
	return fmt.Sprintf("rows=%d flag=%d drop=%d key=%d show=%d coded_size=%d q=%d public_q=%d size=%d first_part=%d target=%d buffer=%d buffer_opt=%d refresh=%d header=%d mode_header=%d lf=%d tile=%d temporal=%d tl0=%d max_q_drift=%d max_size_delta_pct=%.2f max_buffer_delta_pct=%.2f max_buffer_opt_delta_pct=%.2f",
		s.Rows, s.FlagMismatches, s.DropMismatches, s.KeyMismatches,
		s.ShowMismatches, s.CodedSizeMismatches, s.QMismatches, s.PublicQMismatches,
		s.SizeMismatches, s.FirstPartitionMismatches, s.TargetMismatches,
		s.BufferMismatches, s.BufferOptimalMismatches, s.RefreshMismatches,
		s.HeaderMismatches, s.ModeHeaderMismatches, s.LoopFilterMismatches,
		s.TileMismatches, s.TemporalMismatches, s.TL0Mismatches,
		s.MaxQDrift, s.MaxSizeDeltaPct, s.MaxBufferDeltaPct,
		s.MaxBufferOptimalDeltaPct)
}

func CompareTransitionRows(t testing.TB, govpxRows, libvpxRows []RateTraceRow,
	libvpxFlags RateTraceFlagMapper,
) TransitionStats {
	t.Helper()
	if len(govpxRows) == 0 || len(libvpxRows) == 0 {
		t.Fatalf("empty VP9 transition rows: govpx=%d libvpx=%d",
			len(govpxRows), len(libvpxRows))
	}
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("VP9 transition row count: govpx=%d libvpx=%d",
			len(govpxRows), len(libvpxRows))
	}
	stats := TransitionStats{Rows: len(govpxRows)}
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.FrameIndex != l.FrameIndex {
			t.Fatalf("row %d frame_index: govpx=%d libvpx=%d",
				i, g.FrameIndex, l.FrameIndex)
		}
		if g.RecodeAllowed || l.RecodeAllowed ||
			g.RecodeLoopCount != 0 || l.RecodeLoopCount != 0 {
			t.Fatalf("row %d recode: govpx allowed=%t loops=%d libvpx allowed=%t loops=%d, want one-pass VP9 no-recode",
				i, g.RecodeAllowed, g.RecodeLoopCount, l.RecodeAllowed,
				l.RecodeLoopCount)
		}
		if libvpxFlags(g.Flags) != l.Flags {
			stats.FlagMismatches++
		}
		if g.Dropped != l.Dropped {
			stats.DropMismatches++
		}
		if g.KeyFrame != l.KeyFrame {
			stats.KeyMismatches++
		}
		if g.ShowFrame != l.ShowFrame {
			stats.ShowMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.CodedWidth != l.CodedWidth || g.CodedHeight != l.CodedHeight) {
			stats.CodedSizeMismatches++
		}
		if g.BaseQIndex != l.BaseQIndex {
			stats.QMismatches++
			drift := g.BaseQIndex - l.BaseQIndex
			if drift < 0 {
				drift = -drift
			}
			if drift > stats.MaxQDrift {
				stats.MaxQDrift = drift
			}
		}
		if !g.Dropped && !l.Dropped && g.PublicQuantizer != l.PublicQuantizer {
			stats.PublicQMismatches++
		}
		if g.SizeBits != l.SizeBits {
			stats.SizeMismatches++
			if delta := PctDelta(g.SizeBits, l.SizeBits); delta > stats.MaxSizeDeltaPct {
				stats.MaxSizeDeltaPct = delta
			}
		}
		if !g.Dropped && !l.Dropped &&
			g.FirstPartitionSize != l.FirstPartitionSize {
			stats.FirstPartitionMismatches++
		}
		if g.TargetBitrateKbps != l.TargetBitrateKbps ||
			g.EffectiveTargetKbps != l.EffectiveTargetKbps ||
			g.FrameTargetBits != l.FrameTargetBits {
			stats.TargetMismatches++
		}
		if g.BufferLevelBits != l.BufferLevelBits {
			stats.BufferMismatches++
			if delta := PctDelta(g.BufferLevelBits, l.BufferLevelBits); delta > stats.MaxBufferDeltaPct {
				stats.MaxBufferDeltaPct = delta
			}
		}
		if g.BufferOptimalBits != l.BufferOptimalBits {
			stats.BufferOptimalMismatches++
			if delta := PctDelta(g.BufferOptimalBits, l.BufferOptimalBits); delta > stats.MaxBufferOptimalDeltaPct {
				stats.MaxBufferOptimalDeltaPct = delta
			}
		}
		if g.RefreshFrameFlags != l.RefreshFrameFlags {
			stats.RefreshMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.RefreshFrameContext != l.RefreshFrameContext ||
				g.ErrorResilient != l.ErrorResilient ||
				g.FrameParallel != l.FrameParallel ||
				g.FrameContextIdx != l.FrameContextIdx) {
			stats.HeaderMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.TxMode != l.TxMode ||
				g.InterpFilter != l.InterpFilter ||
				g.ReferenceMode != l.ReferenceMode ||
				g.CompoundAllowed != l.CompoundAllowed ||
				g.ReferenceMask != l.ReferenceMask) {
			stats.ModeHeaderMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			g.LoopFilterLevel != l.LoopFilterLevel {
			stats.LoopFilterMismatches++
		}
		if !g.Dropped && !l.Dropped &&
			(g.TileLog2Cols != l.TileLog2Cols ||
				g.TileLog2Rows != l.TileLog2Rows) {
			stats.TileMismatches++
		}
		if g.TemporalLayerID != l.TemporalLayerID ||
			g.TemporalLayerCount != l.TemporalLayerCount ||
			g.TemporalLayerSync != l.TemporalLayerSync {
			stats.TemporalMismatches++
		}
		if g.TL0PICIDX != l.TL0PICIDX {
			stats.TL0Mismatches++
		}
	}
	return stats
}

func DroppedFrameIndices(rows []RateTraceRow) []int {
	out := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Dropped {
			out = append(out, row.FrameIndex)
		}
	}
	return out
}

func DropReasonCount(rows []RateTraceRow, reason string) int {
	count := 0
	for _, row := range rows {
		if row.DropReason == reason {
			count++
		}
	}
	return count
}

func CountHiddenRows(rows []RateTraceRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.ShowFrame {
			count++
		}
	}
	return count
}

func CountAltRefRefreshRows(rows []RateTraceRow, altRefMask uint8) int {
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

func QHistogram(rows []RateTraceRow) [256]int {
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

func FormatAutoAltRefVisibilityRows(govpxRows, libvpxRows []RateTraceRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "packet,govpx_frame,libvpx_frame,govpx_show,libvpx_show,govpx_key,libvpx_key,govpx_refresh,libvpx_refresh,govpx_q,libvpx_q,govpx_bytes,libvpx_bytes,govpx_first_part,libvpx_first_part")
	limit := len(govpxRows)
	if len(libvpxRows) > limit {
		limit = len(libvpxRows)
	}
	for i := 0; i < limit; i++ {
		g, gok := rateTraceRowAt(govpxRows, i)
		l, lok := rateTraceRowAt(libvpxRows, i)
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

func rateTraceRowAt(rows []RateTraceRow, i int) (RateTraceRow, bool) {
	if i < 0 || i >= len(rows) {
		return RateTraceRow{}, false
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

func CountByteParityMatchesWithDrops(t testing.TB,
	govpxRows []RateTraceRow, govpxPackets [][]byte,
	libvpxRows []RateTraceRow, libvpxPackets [][]byte,
) (matches int, packetMatches int, dropMatches int, firstMismatch int) {
	t.Helper()
	if len(govpxRows) != len(libvpxRows) ||
		len(govpxPackets) != len(govpxRows) ||
		len(libvpxPackets) != len(libvpxRows) {
		t.Fatalf("VP9 drop-aware parity row/packet count mismatch: govpx_rows=%d govpx_packets=%d libvpx_rows=%d libvpx_packets=%d",
			len(govpxRows), len(govpxPackets), len(libvpxRows),
			len(libvpxPackets))
	}
	firstMismatch = -1
	for i := range govpxRows {
		gDrop := govpxRows[i].Dropped
		lDrop := libvpxRows[i].Dropped
		switch {
		case gDrop && lDrop:
			matches++
			dropMatches++
		case gDrop || lDrop:
			if firstMismatch < 0 {
				firstMismatch = i
			}
		case len(govpxPackets[i]) != 0 && bytes.Equal(govpxPackets[i], libvpxPackets[i]):
			matches++
			packetMatches++
		default:
			if firstMismatch < 0 {
				firstMismatch = i
			}
		}
	}
	return matches, packetMatches, dropMatches, firstMismatch
}

func FormatDropAwareStreamParityRows(t testing.TB,
	govpxRows []RateTraceRow, govpxPackets [][]byte,
	libvpxRows []RateTraceRow, libvpxPackets [][]byte,
) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,row_match,packet_match,first_diff,govpx_drop,libvpx_drop,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_target,libvpx_target,govpx_buffer,libvpx_buffer,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		packetMatch := false
		if g.Dropped && l.Dropped {
			packetMatch = true
		} else if !g.Dropped && !l.Dropped {
			packetMatch = bytes.Equal(govpxPackets[i], libvpxPackets[i])
		}
		rowMatch := g.Dropped == l.Dropped &&
			g.BaseQIndex == l.BaseQIndex &&
			g.FrameTargetBits == l.FrameTargetBits &&
			g.BufferLevelBits == l.BufferLevelBits &&
			g.RefreshFrameFlags == l.RefreshFrameFlags
		fmt.Fprintf(&b, "%d,%t,%t,%d,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d\n",
			g.FrameIndex, rowMatch, packetMatch,
			testutil.FirstByteDiff(govpxPackets[i], libvpxPackets[i]),
			g.Dropped, l.Dropped,
			len(govpxPackets[i]), len(libvpxPackets[i]), g.BaseQIndex,
			l.BaseQIndex, g.FrameTargetBits, l.FrameTargetBits,
			g.BufferLevelBits, l.BufferLevelBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.FirstPartitionSize, l.FirstPartitionSize)
	}
	return b.String()
}
