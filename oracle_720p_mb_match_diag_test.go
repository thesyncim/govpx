package govpx

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestOracle720pRealtimeMBMatchDiag compares the per-MB decision rows for
// the 1280x720 realtime CBR bench fixture. It is debug-only and is meant
// to pinpoint whether the remaining divergence is in mode selection,
// reference choice, motion vectors, skips, or coefficient energy.
func TestOracle720pRealtimeMBMatchDiag(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the 720p realtime MB match diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1200
		frames     = 30
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
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-720p-mb-match", opts, targetKbps, sources, []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
	})

	gov := indexInterFrameMBs(t, govpxTrace)
	lib := indexInterFrameMBs(t, libvpxTrace)

	type fieldCounts struct {
		mode, ref, mv, skip, seg, eob int
		total                         int
	}
	var fc fieldCounts
	for frameIdx, gMBs := range gov {
		lMBs, ok := lib[frameIdx]
		if !ok {
			continue
		}
		for key, gRow := range gMBs {
			lRow, ok := lMBs[key]
			if !ok {
				continue
			}
			fc.total++
			if gRow.Mode == lRow.Mode {
				fc.mode++
			}
			if gRow.RefFrame == lRow.RefFrame {
				fc.ref++
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
	}

	if fc.total == 0 {
		t.Fatalf("no inter MB rows compared")
	}
	pct := func(n int) float64 { return 100.0 * float64(n) / float64(fc.total) }
	t.Logf("720p bench-noise MB match-rate: total=%d mode=%.2f ref=%.2f mv=%.2f skip=%.2f seg=%.2f eob=%.2f",
		fc.total, pct(fc.mode), pct(fc.ref), pct(fc.mv), pct(fc.skip), pct(fc.seg), pct(fc.eob))

	// Print the first few mismatches so the residual gap can be localized.
	keys := make([]struct {
		frame int64
		row   int
		col   int
	}, 0)
	for frameIdx, gMBs := range gov {
		lMBs, ok := lib[frameIdx]
		if !ok {
			continue
		}
		for key, gRow := range gMBs {
			lRow, ok := lMBs[key]
			if !ok {
				continue
			}
			if gRow.Mode != lRow.Mode || gRow.RefFrame != lRow.RefFrame || gRow.MVRow != lRow.MVRow || gRow.MVCol != lRow.MVCol || gRow.Skip != lRow.Skip || gRow.SegmentID != lRow.SegmentID || gRow.EOBSum != lRow.EOBSum {
				keys = append(keys, struct {
					frame int64
					row   int
					col   int
				}{frameIdx, int(key >> 16), int(key & 0xffff)})
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].frame != keys[j].frame {
			return keys[i].frame < keys[j].frame
		}
		if keys[i].row != keys[j].row {
			return keys[i].row < keys[j].row
		}
		return keys[i].col < keys[j].col
	})
	var buf bytes.Buffer
	limit := min(len(keys), 12)
	for i := 0; i < limit; i++ {
		k := keys[i]
		g := gov[k.frame][int64(k.row)<<16|int64(k.col)]
		l := lib[k.frame][int64(k.row)<<16|int64(k.col)]
		fmt.Fprintf(&buf, "frame=%d mb=(%d,%d) gov[mode=%s ref=%s mv=(%d,%d) skip=%v seg=%d eob=%d] lib[mode=%s ref=%s mv=(%d,%d) skip=%v seg=%d eob=%d]\n",
			k.frame, k.row, k.col,
			g.Mode, g.RefFrame, g.MVRow, g.MVCol, g.Skip, g.SegmentID, g.EOBSum,
			l.Mode, l.RefFrame, l.MVRow, l.MVCol, l.Skip, l.SegmentID, l.EOBSum)
	}
	t.Logf("\nfirst mismatches:\n%s", buf.String())
}
