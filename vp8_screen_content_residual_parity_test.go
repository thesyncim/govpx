//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"os"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8ScreenContentResidualParity compares govpx and libvpx per-MB traces
// for the 12-frame screen-content CBR fixture. The test is logging-only: it
// reports the first mode/ref/MV divergence and dumps the matching inter-mode
// candidate scoreboard so parity work has an objective, reproducible target.
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8ScreenContentResidualParity -v
func TestVP8ScreenContentResidualParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run screen-content residual parity")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		frameCount = 12
		targetKbps = 2000
	)

	govpxSources := make([]Image, frameCount)
	for i := range govpxSources {
		yc := makeScreenTextWindowFrame(width, height, i)
		govpxSources[i] = Image{
			Width:   width,
			Height:  height,
			Y:       yc.Y,
			U:       yc.Cb,
			V:       yc.Cr,
			YStride: yc.YStride,
			UStride: yc.CStride,
			VStride: yc.CStride,
		}
	}

	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      63,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		ScreenContentMode: 1,
		Threads:           1,
	}

	// govpx side.
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range govpxSources {
		if _, err := enc.EncodeInto(packet, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, govpxSources),
		vp8OracleTraceConfig(
			vpxencOracle,
			opts,
			len(govpxSources),
			targetKbps,
			nil,
			[]string{
				"--end-usage=cbr",
				"--screen-content-mode=1",
				"--threads=1",
			},
		),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}
	t.Logf("screen_content_residual govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		govpxTraceBuf.Len(), len(libvpxTrace))

	// Walk all 12 frames; emit per-frame divergence summary.
	for frameIdx := uint64(0); frameIdx < frameCount; frameIdx++ {
		gRows := parseMBActivityRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := parseMBActivityRowsForFrame(libvpxTrace, frameIdx)

		gByKey := map[[2]int]map[string]any{}
		lByKey := map[[2]int]map[string]any{}
		keys := [][2]int{}
		for _, r := range gRows {
			row, _ := r["mb_row"].(float64)
			col, _ := r["mb_col"].(float64)
			k := [2]int{int(row), int(col)}
			gByKey[k] = r
			keys = append(keys, k)
		}
		for _, r := range lRows {
			row, _ := r["mb_row"].(float64)
			col, _ := r["mb_col"].(float64)
			k := [2]int{int(row), int(col)}
			lByKey[k] = r
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})

		modePairs := map[string]int{}
		refPairs := map[string]int{}
		modeMismatches := 0
		refMismatches := 0
		mvMismatches := 0
		firstDiv := [2]int{-1, -1}
		var firstGov, firstLib map[string]any
		for _, k := range keys {
			g, gok := gByKey[k]
			l, lok := lByKey[k]
			if !gok || !lok {
				continue
			}
			gm, _ := g["mode"].(string)
			lm, _ := l["mode"].(string)
			gref, _ := g["ref_frame"].(string)
			lref, _ := l["ref_frame"].(string)
			if gm != lm {
				modeMismatches++
				modePairs[gm+"|"+lm]++
			}
			if gref != lref {
				refMismatches++
				refPairs[gref+"|"+lref]++
			}
			grow, _ := g["mv_row"].(float64)
			gcol, _ := g["mv_col"].(float64)
			lrow, _ := l["mv_row"].(float64)
			lcol, _ := l["mv_col"].(float64)
			if grow != lrow || gcol != lcol {
				mvMismatches++
			}
			if firstDiv[0] < 0 && (gm != lm || gref != lref || grow != lrow || gcol != lcol) {
				firstDiv = k
				firstGov = g
				firstLib = l
			}
		}
		t.Logf("screen_content_residual frame%d mbs=%d mode_mm=%d ref_mm=%d mv_mm=%d",
			frameIdx, len(keys), modeMismatches, refMismatches, mvMismatches)

		if modeMismatches == 0 && refMismatches == 0 && mvMismatches == 0 {
			continue
		}

		// Sort histograms for stability.
		type histEntry struct {
			pair  string
			count int
		}
		var modeHist []histEntry
		for p, c := range modePairs {
			modeHist = append(modeHist, histEntry{p, c})
		}
		sort.Slice(modeHist, func(i, j int) bool { return modeHist[i].count > modeHist[j].count })
		var refHist []histEntry
		for p, c := range refPairs {
			refHist = append(refHist, histEntry{p, c})
		}
		sort.Slice(refHist, func(i, j int) bool { return refHist[i].count > refHist[j].count })
		for _, e := range modeHist {
			t.Logf("screen_content_residual frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			t.Logf("screen_content_residual frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			t.Logf("screen_content_residual frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
			logScreenContentInterCandidateScoreboardAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		}

		// Once we've found the first divergent frame, stop dumping
		// scoreboards (the downstream frames inherit the divergence).
		t.Logf("screen_content_residual FIRST_DIVERGENT_FRAME=%d", frameIdx)
		break
	}
}
