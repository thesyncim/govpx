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

// TestOracle720pKeyFrameMBMatchDiag compares the per-MB decision rows for the
// 1280x720 realtime CBR key frame on the bench-noise fixture. This isolates
// the remaining projected-size gap behind the frame-0 rate row.
func TestOracle720pKeyFrameMBMatchDiag(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the 720p keyframe MB match diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1200
		frames     = 2
	)
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    999,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = scoreboardBenchNoiseFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-720p-keyframe-mb-match", opts, targetKbps, sources, []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
	})

	gov := indexKeyFrameMBs(t, govpxTrace)
	lib := indexKeyFrameMBs(t, libvpxTrace)

	type fieldCounts struct {
		mode, uv, mv, skip, seg, eob int
		total                        int
	}
	var fc fieldCounts
	for key, gRow := range gov {
		lRow, ok := lib[key]
		if !ok {
			continue
		}
		fc.total++
		if gRow.Mode == lRow.Mode {
			fc.mode++
		}
		if gRow.UVMode == lRow.UVMode {
			fc.uv++
		}
		if gRow.MVRow == lRow.MVRow && gRow.MVCol == lRow.MVCol {
			fc.mv++
		}
		if gRow.Skip == lRow.Skip {
			fc.skip++
		}
		if gRow.SegmentID == lRow.SegmentID {
			fc.seg++
		}
		if gRow.EOBSum == lRow.EOBSum {
			fc.eob++
		}
	}
	if fc.total == 0 {
		t.Fatalf("no keyframe MB rows compared")
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(fc.total) }
	t.Logf("720p keyframe MB match-rate: total=%d mode=%.2f uv=%.2f mv=%.2f skip=%.2f seg=%.2f eob=%.2f",
		fc.total, pct(fc.mode), pct(fc.uv), pct(fc.mv), pct(fc.skip), pct(fc.seg), pct(fc.eob))

	keys := make([]struct {
		row int
		col int
	}, 0)
	for key := range gov {
		if _, ok := lib[key]; ok {
			keys = append(keys, struct {
				row int
				col int
			}{row: int(key >> 16), col: int(key & 0xffff)})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].row != keys[j].row {
			return keys[i].row < keys[j].row
		}
		return keys[i].col < keys[j].col
	})
	var buf bytes.Buffer
	limit := min(len(keys), 12)
	for i := 0; i < limit; i++ {
		k := keys[i]
		key := int64(k.row)<<16 | int64(k.col)
		g := gov[key]
		l := lib[key]
		fmt.Fprintf(&buf, "mb=(%d,%d) gov[mode=%s uv=%s mv=(%d,%d) skip=%v seg=%d eob=%d] lib[mode=%s uv=%s mv=(%d,%d) skip=%v seg=%d eob=%d]\n",
			k.row, k.col,
			g.Mode, g.UVMode, g.MVRow, g.MVCol, g.Skip, g.SegmentID, g.EOBSum,
			l.Mode, l.UVMode, l.MVRow, l.MVCol, l.Skip, l.SegmentID, l.EOBSum)
	}
	for _, k := range keys {
		key := int64(k.row)<<16 | int64(k.col)
		g := gov[key]
		l := lib[key]
		if g.EOBSum != l.EOBSum {
			fmt.Fprintf(&buf, "first mismatch mb=(%d,%d) eob gov=%v lib=%v\n", k.row, k.col, g.EOB, l.EOB)
			break
		}
	}
	t.Logf("\nfirst keyframe mismatches:\n%s", buf.String())
}

func indexKeyFrameMBs(t *testing.T, trace []byte) map[int64]oracleTraceMBRow {
	t.Helper()
	out := make(map[int64]oracleTraceMBRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row oracleTraceMBRow
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if row.Type == "mb" && row.FrameIndex == 0 {
			out[int64(row.MBRow)<<16|int64(row.MBCol)] = row
		}
	}
	return out
}
