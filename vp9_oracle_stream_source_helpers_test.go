//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

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
) ([]vp9test.RateScoreboardRow, [][]byte) {
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
	rows := vp9test.ParseRateScoreboardRows(t, trace.Bytes())
	if len(rows) != len(packets) {
		t.Fatalf("govpx auto-alt-ref trace rows = %d, packets = %d",
			len(rows), len(packets))
	}
	for i := range rows {
		vp9test.EnrichRateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLibvpxVP9AutoAltRefPacketRowsForOracleTest(t *testing.T,
	sources []*image.YCbCr, extraArgs ...string,
) ([]vp9test.RateScoreboardRow, [][]byte) {
	t.Helper()
	rows, packets := vp9test.VpxencFrameFlagTracePackets(t, sources, nil,
		extraArgs...)
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		vp9test.EnrichRateScoreboardRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
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

func vp9OracleFlushIndexSet(indexes []int) map[int]bool {
	set := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		set[index] = true
	}
	return set
}
