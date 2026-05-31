//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8Panning360pMBParity compares govpx and libvpx per-MB mode/ref/MV
// traces for the 360p panning CBR fixture at the 1200 kbps rung.
func TestVP8Panning360pMBParity(t *testing.T) {
	vp8test.RequireOracle(t, "360p panning CBR MB parity")
	requireOracleTraceBuild(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 640
		height     = 360
		frameCount = 16
	)
	targetKbps := 1200

	// Same source the BD-rate fixture uses.
	ycbcrSources := make([]*image.YCbCr, frameCount)
	govpxSources := make([]Image, frameCount)
	for i := range ycbcrSources {
		yc := testutil.NewTexturedPanningYCbCr(width, height, i)
		ycbcrSources[i] = yc
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

	// Match TestVP8BDRate360pPanningCBR encoder options:
	// QLadder/RateLadder ([16,28,40,52] / [300,600,1200,2400]) under
	// the default GoodQuality/CpuUsed=0 path with CBR rate control.
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Threads:           1,
	}

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

	libvpxTrace, diag, err := vp8test.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, govpxSources),
		vp8OracleTraceConfig(
			vpxencOracle,
			opts,
			len(govpxSources),
			targetKbps,
			nil,
			[]string{
				"--end-usage=cbr",
				"--threads=1",
			},
		),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	t.Logf("panning_360p_cbr target_kbps=%d govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		targetKbps, govpxTraceBuf.Len(), len(libvpxTrace))

	frameProbeList := []uint64{0, 1, 2, 3, 7, 15}
	for _, frameIdx := range frameProbeList {
		gRows := parseMBActivityRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := parseMBActivityRowsForFrame(libvpxTrace, frameIdx)
		t.Logf("panning_360p_cbr frame%d govpx_mb_rows=%d libvpx_mb_rows=%d", frameIdx, len(gRows), len(lRows))

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
		t.Logf("panning_360p_cbr frame%d mode_mismatches=%d ref_mismatches=%d mv_mismatches=%d total_mbs=%d",
			frameIdx, modeMismatches, refMismatches, mvMismatches, len(keys))

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
			t.Logf("panning_360p_cbr frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			t.Logf("panning_360p_cbr frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			t.Logf("panning_360p_cbr frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
			logScreenContentInterCandidateTraceAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		} else {
			t.Logf("panning_360p_cbr frame%d NO_DIV; all MBs match (mode, ref, mv)", frameIdx)
		}

		// Frame-level rate/Q probe (realtime_cpu8-style: surfaces autoSpeed,
		// rate-control, and q_index divergence between encoders).
		gFrame := parseRealtimeCPU8FrameRow(govpxTraceBuf.Bytes(), frameIdx)
		lFrame := parseRealtimeCPU8FrameRow(libvpxTrace, frameIdx)
		if gFrame != nil && lFrame != nil {
			for _, f := range []string{"q_index", "base_q_index", "loop_filter_level", "auto_speed", "projected_frame_size"} {
				gv := gFrame[f]
				lv := lFrame[f]
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("panning_360p_cbr frame%d FRAME %-22s govpx=%v libvpx=%v%s", frameIdx, f, gv, lv, marker)
			}
		}
	}
}
