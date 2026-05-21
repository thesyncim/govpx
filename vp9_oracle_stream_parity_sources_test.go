//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func countVP9ByteParityMatchesWithDrops(t *testing.T,
	govpxRows []vp9RateScoreboardRow, govpxPackets [][]byte,
	libvpxRows []vp9RateScoreboardRow, libvpxPackets [][]byte,
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

func captureVP9LookaheadPacketsWithFlushesForOracleTest(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr, flushAfter []int,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 lookahead flush source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	opts.Width = width
	opts.Height = height
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	flushSet := vp9OracleFlushIndexSet(flushAfter)
	packets := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			// Keep filling the lookahead queue.
		} else if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		} else {
			if result.Dropped {
				t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
			}
			packets = append(packets, append([]byte(nil), result.Data...))
		}
		if flushSet[i] {
			packets = append(packets,
				drainVP9LookaheadFlushForOracleTest(t, enc, dst)...)
		}
	}
	packets = append(packets, drainVP9LookaheadFlushForOracleTest(t, enc, dst)...)
	if len(packets) != len(sources) {
		t.Fatalf("VP9 lookahead flush packets = %d, want %d",
			len(packets), len(sources))
	}
	return packets
}

func drainVP9LookaheadFlushForOracleTest(t *testing.T, enc *VP9Encoder, dst []byte) [][]byte {
	t.Helper()
	var packets [][]byte
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		if result.Dropped {
			t.Fatal("FlushIntoWithResult unexpectedly dropped")
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	return packets
}

func captureGovpxVP9AutoAltRefPacketRowsForOracleTest(t *testing.T,
	opts VP9EncoderOptions, sources []*image.YCbCr,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 auto-alt-ref source")
	}
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.setVP9OracleTraceWriter(&trace)
	dstSize, err := vp9AllocatingEncodeBufferSize(opts.Width, opts.Height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, 0, len(sources)+1)
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		if result.Dropped {
			t.Fatal("FlushIntoWithResult unexpectedly dropped")
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	rows := parseVP9RateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(packets) {
		t.Fatalf("govpx auto-alt-ref trace rows = %d, packets = %d",
			len(rows), len(packets))
	}
	for i := range rows {
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t *testing.T,
	sources []*image.YCbCr, extraArgs ...string,
) ([]vp9RateScoreboardRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx auto-alt-ref source")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	var raw []byte
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
		raw = vp9test.AppendI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), nil, extraArgs...)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	rows := parseVP9RateScoreboardRows(t, trace)
	wantPackets := 0
	for _, row := range rows {
		if !row.Dropped {
			wantPackets++
		}
	}
	gotPackets, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if gotPackets != wantPackets {
		t.Fatalf("libvpx auto-alt-ref IVF packets = %d, want %d",
			gotPackets, wantPackets)
	}
	packets := make([][]byte, len(rows))
	if wantPackets == 0 {
		return rows, packets
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	packetIndex := 0
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, packetIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", packetIndex, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], packets[i])
		packetIndex++
	}
	return rows, packets
}

func countVP9HiddenRows(rows []vp9RateScoreboardRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.ShowFrame {
			count++
		}
	}
	return count
}

func vp9OracleROIMap(width int, height int, pattern string) *ROIMap {
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	roi := &ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			switch pattern {
			case "checker":
				roi.SegmentID[idx] = uint8((row + col) & 1)
			case "left1":
				if col < (cols+1)/2 {
					roi.SegmentID[idx] = 1
				}
			case "quadrants":
				roi.SegmentID[idx] = uint8(0)
				if row >= rows/2 {
					roi.SegmentID[idx] += 2
				}
				if col >= cols/2 {
					roi.SegmentID[idx]++
				}
			case "border1":
				if row == 0 || col == 0 || row == rows-1 || col == cols-1 {
					roi.SegmentID[idx] = 1
				}
			default:
				panic("unknown VP9 ROI pattern")
			}
		}
	}
	switch pattern {
	case "checker", "left1":
		roi.DeltaQuantizer[1] = -10
		roi.DeltaLoopFilter[1] = -3
	case "quadrants":
		roi.DeltaQuantizer[1] = -8
		roi.DeltaQuantizer[2] = 8
		roi.DeltaLoopFilter[3] = 4
	case "border1":
		roi.DeltaQuantizer[1] = -6
	}
	return roi
}

func vp9OracleActiveMap(width int, height int, pattern string) ([]uint8, int, int) {
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			switch pattern {
			case "all":
				activeMap[idx] = 1
			case "checker":
				if (row+col)&1 == 0 {
					activeMap[idx] = 1
				}
			case "left-off":
				if col != 0 {
					activeMap[idx] = 1
				}
			case "right-off":
				if col != cols-1 {
					activeMap[idx] = 1
				}
			case "border-off":
				if row != 0 && col != 0 && row != rows-1 && col != cols-1 {
					activeMap[idx] = 1
				}
			default:
				panic("unknown VP9 active-map pattern")
			}
		}
	}
	return activeMap, rows, cols
}

func countVP9AltRefRefreshRows(rows []vp9RateScoreboardRow) int {
	count := 0
	for _, row := range rows {
		if !row.Dropped && !row.KeyFrame &&
			row.RefreshFrameFlags&(1<<vp9AltRefSlot) != 0 {
			count++
		}
	}
	return count
}

func formatVP9AutoAltRefVisibilityRows(govpxRows, libvpxRows []vp9RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "packet,govpx_frame,libvpx_frame,govpx_show,libvpx_show,govpx_key,libvpx_key,govpx_refresh,libvpx_refresh,govpx_q,libvpx_q,govpx_bytes,libvpx_bytes,govpx_first_part,libvpx_first_part")
	limit := len(govpxRows)
	if len(libvpxRows) > limit {
		limit = len(libvpxRows)
	}
	for i := 0; i < limit; i++ {
		g, gok := vp9ScoreboardRowAt(govpxRows, i)
		l, lok := vp9ScoreboardRowAt(libvpxRows, i)
		fmt.Fprintf(&b, "%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			i,
			vp9OptionalInt(gok, g.FrameIndex),
			vp9OptionalInt(lok, l.FrameIndex),
			vp9OptionalBool(gok, g.ShowFrame),
			vp9OptionalBool(lok, l.ShowFrame),
			vp9OptionalBool(gok, g.KeyFrame),
			vp9OptionalBool(lok, l.KeyFrame),
			vp9OptionalHex(gok, g.RefreshFrameFlags),
			vp9OptionalHex(lok, l.RefreshFrameFlags),
			vp9OptionalInt(gok, g.BaseQIndex),
			vp9OptionalInt(lok, l.BaseQIndex),
			vp9OptionalInt(gok, g.SizeBytes),
			vp9OptionalInt(lok, l.SizeBytes),
			vp9OptionalInt(gok, g.FirstPartitionSize),
			vp9OptionalInt(lok, l.FirstPartitionSize))
	}
	return b.String()
}

func vp9ScoreboardRowAt(rows []vp9RateScoreboardRow, i int) (vp9RateScoreboardRow, bool) {
	if i < 0 || i >= len(rows) {
		return vp9RateScoreboardRow{}, false
	}
	return rows[i], true
}

func vp9OptionalInt(ok bool, v int) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}

func vp9OptionalBool(ok bool, v bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%t", v)
}

func vp9OptionalHex(ok bool, v uint8) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%#x", v)
}

func vp9OracleFlushIndexSet(indexes []int) map[int]bool {
	set := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		set[index] = true
	}
	return set
}
