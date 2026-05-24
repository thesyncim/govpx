package vp9test

import (
	"strings"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestParseRateScoreboardRows(t *testing.T) {
	trace := []byte(strings.Join([]string{
		`{"row":"build_info","frame_index":99}`,
		`{"row":"vp9_frame","frame_index":0,"flags":17,"key_frame":true,"show_frame":true,"coded_width":64,"coded_height":32,"base_qindex":22,"public_quantizer":8,"size_bytes":11,"first_partition_size":7,"target_bitrate_kbps":500,"frame_target_bits":900,"buffer_level_bits":1000,"buffer_optimal_bits":1100,"refresh_frame_flags":7,"refresh_frame_context":true,"tx_mode":1,"interp_filter":2,"reference_mode":3,"reference_mask":5,"loop_filter_level":4,"temporal_layer_id":1,"temporal_layer_count":2,"temporal_layer_sync":true,"tl0_pic_idx":9,"recode_allowed":true,"recode_loop_count":2,"active_best_q":18,"active_worst_q":40,"rate_correction_factor":1.25,"tile_log2_cols":1,"tile_log2_rows":0}`,
		`{"row":"vp9_frame","frame_index":1,"dropped":true,"drop_reason":"watermark_decimation","size_bytes":0,"size_bits":32,"base_qindex":300}`,
		"",
	}, "\n"))

	rows := ParseRateScoreboardRows(t, trace)
	if len(rows) != 2 {
		t.Fatalf("len(ParseRateScoreboardRows) = %d, want 2", len(rows))
	}
	first := rows[0]
	if first.FrameIndex != 0 || first.Flags != 17 || !first.KeyFrame ||
		!first.ShowFrame || first.CodedWidth != 64 || first.CodedHeight != 32 {
		t.Fatalf("first row basic fields = %+v", first)
	}
	if first.SizeBits != 88 {
		t.Fatalf("first row SizeBits = %d, want fallback from bytes", first.SizeBits)
	}
	if !first.RefreshFrameContext || first.RefreshFrameFlags != 7 ||
		first.TemporalLayerID != 1 || first.TL0PICIDX != 9 ||
		!first.RecodeAllowed || first.RecodeLoopCount != 2 {
		t.Fatalf("first row control fields = %+v", first)
	}
	second := rows[1]
	if !second.Dropped || second.DropReason != "watermark_decimation" ||
		second.SizeBits != 32 {
		t.Fatalf("second row = %+v", second)
	}
}

func TestRateScoreboardHelpers(t *testing.T) {
	rows := []RateScoreboardRow{
		{FrameIndex: 0, BaseQIndex: 22},
		{FrameIndex: 1, Dropped: true, DropReason: "watermark_decimation", BaseQIndex: 23},
		{FrameIndex: 2, BaseQIndex: 22},
		{FrameIndex: 3, BaseQIndex: -1},
		{FrameIndex: 4, BaseQIndex: 256},
	}

	if got, want := DroppedFrameIndices(rows), []int{1}; !SameIntSlice(got, want) {
		t.Fatalf("DroppedFrameIndices = %v, want %v", got, want)
	}
	if got := DropReasonCount(rows, "watermark_decimation"); got != 1 {
		t.Fatalf("DropReasonCount = %d, want 1", got)
	}
	if got := CountHiddenRows([]RateScoreboardRow{
		{FrameIndex: 0, ShowFrame: true},
		{FrameIndex: 1, ShowFrame: false},
		{FrameIndex: 2, ShowFrame: false, Dropped: true},
	}); got != 1 {
		t.Fatalf("CountHiddenRows = %d, want 1", got)
	}
	if got := CountAltRefRefreshRows([]RateScoreboardRow{
		{FrameIndex: 0, KeyFrame: true, RefreshFrameFlags: 4},
		{FrameIndex: 1, RefreshFrameFlags: 4},
		{FrameIndex: 2, Dropped: true, RefreshFrameFlags: 4},
		{FrameIndex: 3, RefreshFrameFlags: 2},
	}, 4); got != 1 {
		t.Fatalf("CountAltRefRefreshRows = %d, want 1", got)
	}
	hist := QHistogram(rows)
	if hist[22] != 2 || hist[23] != 0 {
		t.Fatalf("QHistogram[22]=%d [23]=%d, want 2 and 0", hist[22], hist[23])
	}
	if got := FormatQHistogram(hist); got != "22:2" {
		t.Fatalf("FormatQHistogram = %q, want %q", got, "22:2")
	}
	other := hist
	other[22]--
	other[24]++
	distance, bins := HistogramDistance(hist, other)
	if distance != 2 || bins != 2 {
		t.Fatalf("HistogramDistance = (%d, %d), want (2, 2)", distance, bins)
	}
	if !SameIntSlice([]int{1, 2, 3}, []int{1, 2, 3}) ||
		SameIntSlice([]int{1, 2, 3}, []int{1, 3, 2}) {
		t.Fatal("SameIntSlice comparison failed")
	}
	if got := FormatQHistogram([256]int{}); got != "empty" {
		t.Fatalf("FormatQHistogram(empty) = %q, want empty", got)
	}
}

func TestAutoAltRefVisibilityFormatting(t *testing.T) {
	govpxRows := []RateScoreboardRow{{
		FrameIndex:         0,
		ShowFrame:          true,
		KeyFrame:           true,
		RefreshFrameFlags:  7,
		BaseQIndex:         20,
		SizeBytes:          10,
		FirstPartitionSize: 6,
	}, {
		FrameIndex:         1,
		ShowFrame:          false,
		RefreshFrameFlags:  4,
		BaseQIndex:         30,
		SizeBytes:          8,
		FirstPartitionSize: 4,
	}}
	libvpxRows := []RateScoreboardRow{{
		FrameIndex:         0,
		ShowFrame:          true,
		KeyFrame:           true,
		RefreshFrameFlags:  7,
		BaseQIndex:         21,
		SizeBytes:          11,
		FirstPartitionSize: 7,
	}}

	out := FormatAutoAltRefVisibilityRows(govpxRows, libvpxRows)
	for _, want := range []string{
		"packet,govpx_frame,libvpx_frame,govpx_show,libvpx_show",
		"0,0,0,true,true,true,true,0x7,0x7,20,21,10,11,6,7",
		"1,1,-,false,-,false,-,0x4,-,30,-,8,-,4,-",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatAutoAltRefVisibilityRows missing %q in:\n%s",
				want, out)
		}
	}
}

func TestReferenceMaskFromLibvpxFrameFlags(t *testing.T) {
	const (
		libvpxNoRefLast = 1 << 16
		libvpxNoRefGF   = 1 << 17
		libvpxNoRefARF  = 1 << 21
	)
	cases := []struct {
		name  string
		flags uint32
		want  uint8
	}{
		{name: "all", want: 1<<uint(vp9dec.LastFrame) |
			1<<uint(vp9dec.GoldenFrame) | 1<<uint(vp9dec.AltrefFrame)},
		{name: "golden-altref", flags: libvpxNoRefLast,
			want: 1<<uint(vp9dec.GoldenFrame) | 1<<uint(vp9dec.AltrefFrame)},
		{name: "last", flags: libvpxNoRefGF | libvpxNoRefARF,
			want: 1 << uint(vp9dec.LastFrame)},
		{name: "none", flags: libvpxNoRefLast | libvpxNoRefGF | libvpxNoRefARF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReferenceMaskFromLibvpxFrameFlags(tc.flags); got != tc.want {
				t.Fatalf("mask = %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestRateScoreboardFormatting(t *testing.T) {
	govpxRows := []RateScoreboardRow{{
		FrameIndex:           0,
		Flags:                1,
		KeyFrame:             true,
		ShowFrame:            true,
		CodedWidth:           64,
		CodedHeight:          64,
		BaseQIndex:           20,
		PublicQuantizer:      8,
		ActiveBestQ:          16,
		ActiveWorstQ:         32,
		RateCorrectionFactor: 1.5,
		SizeBytes:            10,
		SizeBits:             80,
		FirstPartitionSize:   7,
		TargetBitrateKbps:    400,
		FrameTargetBits:      800,
		BufferLevelBits:      900,
		BufferOptimalBits:    1000,
		RefreshFrameFlags:    7,
		RefreshFrameContext:  true,
		TxMode:               1,
		InterpFilter:         2,
		ReferenceMode:        3,
		ReferenceMask:        5,
		LoopFilterLevel:      11,
		TileLog2Cols:         1,
		TemporalLayerID:      1,
		TemporalLayerCount:   2,
		TL0PICIDX:            9,
		TemporalLayerSync:    true,
	}}
	libvpxRows := []RateScoreboardRow{{
		FrameIndex:           0,
		Flags:                2,
		KeyFrame:             true,
		ShowFrame:            true,
		CodedWidth:           64,
		CodedHeight:          64,
		BaseQIndex:           21,
		PublicQuantizer:      9,
		ActiveBestQ:          17,
		ActiveWorstQ:         33,
		RateCorrectionFactor: 1.25,
		SizeBytes:            11,
		SizeBits:             88,
		FirstPartitionSize:   8,
		TargetBitrateKbps:    400,
		FrameTargetBits:      810,
		BufferLevelBits:      910,
		BufferOptimalBits:    1010,
		RefreshFrameFlags:    3,
		TxMode:               2,
		InterpFilter:         1,
		ReferenceMode:        2,
		ReferenceMask:        6,
		LoopFilterLevel:      12,
		TemporalLayerCount:   2,
		TL0PICIDX:            9,
	}}

	out := FormatRateScoreboardRows(govpxRows, libvpxRows)
	for _, want := range []string{
		"frame,govpx_flags,libvpx_flags",
		"0,0x1,0x2,false,false,true,true,true,true,64,64,64,64,20,21",
		",1.5,1.25,false,false,0,0,10,11,80,88",
		",0x7,0x3,true,false,1,2,2,1,3,2,0x5,0x6",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatRateScoreboardRows missing %q in:\n%s", want, out)
		}
	}
}

func TestSingleRateScoreboardFormatting(t *testing.T) {
	rows := []RateScoreboardRow{{
		FrameIndex:           3,
		Flags:                5,
		Dropped:              true,
		DropReason:           "watermark_decimation",
		KeyFrame:             false,
		ShowFrame:            false,
		CodedWidth:           64,
		CodedHeight:          32,
		BaseQIndex:           44,
		PublicQuantizer:      18,
		SizeBytes:            0,
		SizeBits:             0,
		FirstPartitionSize:   0,
		TargetBitrateKbps:    300,
		FrameTargetBits:      700,
		BufferLevelBits:      800,
		RefreshFrameFlags:    4,
		RefreshFrameContext:  true,
		TxMode:               1,
		InterpFilter:         2,
		ReferenceMode:        3,
		ReferenceMask:        5,
		LoopFilterLevel:      11,
		TileLog2Cols:         1,
		TemporalLayerID:      2,
		TemporalLayerCount:   3,
		TL0PICIDX:            9,
		TemporalLayerSync:    true,
		RateCorrectionFactor: 1.2,
	}}

	out := FormatSingleRateScoreboardRows(rows)
	for _, want := range []string{
		"frame,flags,drop,reason,key,show,width,height",
		"3,0x5,true,watermark_decimation,false,false,64,32,44,18",
		",300,700,800,0x4,true,1,2,3,0x5,11,1,2,3,9,true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatSingleRateScoreboardRows missing %q in:\n%s",
				want, out)
		}
	}
}

func TestCompareTransitionRows(t *testing.T) {
	govpxRows := []RateScoreboardRow{{
		FrameIndex:           0,
		Flags:                1,
		KeyFrame:             true,
		ShowFrame:            true,
		CodedWidth:           64,
		CodedHeight:          64,
		BaseQIndex:           20,
		PublicQuantizer:      8,
		SizeBits:             80,
		FirstPartitionSize:   7,
		TargetBitrateKbps:    300,
		FrameTargetBits:      600,
		BufferLevelBits:      900,
		BufferOptimalBits:    1000,
		RefreshFrameFlags:    7,
		RefreshFrameContext:  true,
		FrameContextIdx:      1,
		TxMode:               1,
		InterpFilter:         2,
		ReferenceMode:        3,
		CompoundAllowed:      true,
		ReferenceMask:        5,
		LoopFilterLevel:      11,
		TileLog2Cols:         1,
		TemporalLayerID:      1,
		TemporalLayerCount:   2,
		TemporalLayerSync:    true,
		TL0PICIDX:            9,
		RateCorrectionFactor: 1.1,
	}}
	libvpxRows := []RateScoreboardRow{{
		FrameIndex:           0,
		Flags:                9,
		KeyFrame:             false,
		ShowFrame:            false,
		CodedWidth:           32,
		CodedHeight:          64,
		BaseQIndex:           25,
		PublicQuantizer:      9,
		SizeBits:             100,
		FirstPartitionSize:   9,
		TargetBitrateKbps:    400,
		FrameTargetBits:      650,
		BufferLevelBits:      1000,
		BufferOptimalBits:    1100,
		RefreshFrameFlags:    3,
		ErrorResilient:       true,
		FrameParallel:        true,
		FrameContextIdx:      2,
		TxMode:               2,
		InterpFilter:         3,
		ReferenceMode:        4,
		ReferenceMask:        7,
		LoopFilterLevel:      12,
		TileLog2Rows:         1,
		TemporalLayerCount:   2,
		TemporalLayerSync:    false,
		TL0PICIDX:            10,
		RateCorrectionFactor: 1.1,
	}}

	stats := CompareTransitionRows(t, govpxRows, libvpxRows, func(flags uint32) uint32 {
		return flags + 8
	})
	if !stats.HasMismatch() {
		t.Fatal("CompareTransitionRows reported no mismatches")
	}
	if stats.FlagMismatches != 0 || stats.KeyMismatches != 1 ||
		stats.ShowMismatches != 1 || stats.CodedSizeMismatches != 1 ||
		stats.QMismatches != 1 || stats.PublicQMismatches != 1 ||
		stats.SizeMismatches != 1 || stats.FirstPartitionMismatches != 1 ||
		stats.TargetMismatches != 1 || stats.BufferMismatches != 1 ||
		stats.BufferOptimalMismatches != 1 || stats.RefreshMismatches != 1 ||
		stats.HeaderMismatches != 1 || stats.ModeHeaderMismatches != 1 ||
		stats.LoopFilterMismatches != 1 || stats.TileMismatches != 1 ||
		stats.TemporalMismatches != 1 || stats.TL0Mismatches != 1 {
		t.Fatalf("CompareTransitionRows stats = %+v", stats)
	}
	if stats.MaxQDrift != 5 || stats.MaxSizeDeltaPct != 20 ||
		stats.MaxBufferDeltaPct != 10 ||
		stats.MaxBufferOptimalDeltaPct != PctDelta(1000, 1100) {
		t.Fatalf("CompareTransitionRows deltas = %+v", stats)
	}
	if got := stats.String(); !strings.Contains(got, "rows=1 flag=0") ||
		!strings.Contains(got, "max_q_drift=5") {
		t.Fatalf("TransitionStats.String = %q", got)
	}
}

func TestDropAwareStreamParityRows(t *testing.T) {
	govpxRows := []RateScoreboardRow{
		{FrameIndex: 0, BaseQIndex: 20, FrameTargetBits: 100, BufferLevelBits: 200, RefreshFrameFlags: 7, FirstPartitionSize: 5},
		{FrameIndex: 1, Dropped: true},
		{FrameIndex: 2, BaseQIndex: 30, FrameTargetBits: 110, BufferLevelBits: 210, RefreshFrameFlags: 3, FirstPartitionSize: 4},
	}
	libvpxRows := []RateScoreboardRow{
		{FrameIndex: 0, BaseQIndex: 20, FrameTargetBits: 100, BufferLevelBits: 200, RefreshFrameFlags: 7, FirstPartitionSize: 5},
		{FrameIndex: 1, Dropped: true},
		{FrameIndex: 2, BaseQIndex: 31, FrameTargetBits: 111, BufferLevelBits: 211, RefreshFrameFlags: 1, FirstPartitionSize: 6},
	}
	govpxPackets := [][]byte{{1, 2, 3}, nil, {7, 8, 9}}
	libvpxPackets := [][]byte{{1, 2, 3}, nil, {7, 0, 9}}

	matches, packetMatches, dropMatches, firstMismatch :=
		CountByteParityMatchesWithDrops(t, govpxRows, govpxPackets,
			libvpxRows, libvpxPackets)
	if matches != 2 || packetMatches != 1 || dropMatches != 1 ||
		firstMismatch != 2 {
		t.Fatalf("CountByteParityMatchesWithDrops = (%d, %d, %d, %d)",
			matches, packetMatches, dropMatches, firstMismatch)
	}

	out := FormatDropAwareStreamParityRows(t, govpxRows, govpxPackets,
		libvpxRows, libvpxPackets)
	for _, want := range []string{
		"frame,row_match,packet_match,first_diff",
		"0,true,true,-1,false,false,3,3,20,20,100,100,200,200,0x7,0x7,5,5",
		"1,true,true,-1,true,true,0,0,0,0,0,0,0,0,0x0,0x0,0,0",
		"2,false,false,1,false,false,3,3,30,31,110,111,210,211,0x3,0x1,4,6",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatDropAwareStreamParityRows missing %q in:\n%s",
				want, out)
		}
	}
}

func TestPctDelta(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
		pct  float64
	}{
		{name: "same", got: 10, want: 10, pct: 0},
		{name: "positive", got: 12, want: 10, pct: 20},
		{name: "negative want", got: -8, want: -10, pct: 20},
		{name: "zero denominator", got: 2, want: 0, pct: 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PctDelta(tc.got, tc.want); got != tc.pct {
				t.Fatalf("PctDelta(%d, %d) = %v, want %v",
					tc.got, tc.want, got, tc.pct)
			}
		})
	}
}
