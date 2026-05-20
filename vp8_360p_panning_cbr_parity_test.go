//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Panning360pMBParity performs per-MB localization of any
// VP8 mode / ref / MV / rate divergence on the 360p panning CBR
// fixture (TestVP8FeatureBDRate360pPanningCBR, pinned at +1.111%
// BD-rate / -0.118 dB BD-PSNR after task #341). The fixture's gate is
// the default +5.0% / -0.5 dB so the BD-rate measurement passes by a
// comfortable +3.9pp headroom; the audit here is to confirm the
// residual is cubic-fit jitter on synthetic content, not an actionable
// libvpx port gap.
//
// Method mirrors task #343 (RT cpu_used=8 bisect on the 720p panning
// fixture): drive 16 frames of makeVP8PanningFrame through both govpx
// VP8 and the patched vpxenc-oracle at the same CBR ladder rung the
// gate uses; emit MB-level + frame-level oracle traces; diff mode /
// ref / mv per MB and rate / q_index / skip per frame.
//
// CBR ladder under audit: 300 / 600 / 1200 / 2400 kbps. Per the
// task #353 fixture trace
//
//	govpx_rate_psnr  = [816.6/39.51, 1206.6/46.07, 1369.5/48.17, 1500.9/48.56]
//	libvpx_rate_psnr = [821.1/39.34, 1212.2/46.10, 1429.5/48.34, 1522.9/48.61]
//
// The top three rungs are saturated near PSNR-Y ~48.5 dB (the 16-frame
// synthetic panning content has a hard PSNR ceiling), so rate differs
// in absolute kbps but PSNR barely moves. The cubic fit picks up that
// asymmetric saturation as a small positive BD-rate.
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  GOVPX_TASK353_TARGET_KBPS=300 \
//	  go test -tags govpx_oracle_trace -run TestVP8Panning360pMBParity -v
func TestVP8Panning360pMBParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #353 360p panning CBR MB bisect")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 640
		height     = 360
		frameCount = 16
	)
	targetKbps := 1200
	if v := os.Getenv("GOVPX_TASK353_TARGET_KBPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			targetKbps = n
		}
	}

	// Same source the BD-rate fixture uses (makeVP8PanningFrame in
	// feature_quality_gates_vp8_test.go — package govpx_test, not
	// accessible from this package-internal probe; verbatim copy via
	// task343MakePanningFrame in vp8_realtime_cpu8_mb_parity_test.go,
	// which is the same generator the BD-rate fixture uses).
	ycbcrSources := make([]*image.YCbCr, frameCount)
	govpxSources := make([]Image, frameCount)
	for i := range ycbcrSources {
		yc := task343MakePanningFrame(width, height, i)
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

	// Match TestVP8FeatureBDRate360pPanningCBR encoder options:
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

	// libvpx side via the patched vpxenc-oracle. Match the BD-rate
	// driver's CLI flags: --good (Speed 0), --end-usage=cbr,
	// --target-bitrate=<kbps>, --min-q/--max-q, --threads=1,
	// kf-min/max-dist=999 (single keyframe at frame 0).
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "task353.yuv")
	ivfPath := filepath.Join(dir, "task353.ivf")
	libvpxTracePath := filepath.Join(dir, "task353.jsonl")
	task341WriteI420(t, yuvPath, govpxSources)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--good",
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--threads=1",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(govpxSources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+libvpxTracePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("vpxenc-oracle args: %v", args)
		t.Logf("vpxenc-oracle output:\n%s", out)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}
	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	govpxOut := "/tmp/govpx_task353_360p_panning.jsonl"
	libvpxOut := "/tmp/libvpx_task353_360p_panning.jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("task353 target_kbps=%d govpx_trace=%s libvpx_trace=%s govpx_bytes=%d libvpx_bytes=%d",
		targetKbps, govpxOut, libvpxOut, govpxTraceBuf.Len(), len(libvpxTrace))

	frameProbeList := []uint64{0, 1, 2, 3, 7, 15}
	for _, frameIdx := range frameProbeList {
		gRows := task210ParseMBRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := task210ParseMBRowsForFrame(libvpxTrace, frameIdx)
		t.Logf("task353 frame%d govpx_mb_rows=%d libvpx_mb_rows=%d", frameIdx, len(gRows), len(lRows))

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
		t.Logf("task353 frame%d mode_mismatches=%d ref_mismatches=%d mv_mismatches=%d total_mbs=%d",
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
			t.Logf("task353 frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			t.Logf("task353 frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			t.Logf("task353 frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !task210FieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
			task341LogInterCandidateScoreboardAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		} else {
			t.Logf("task353 frame%d NO_DIV — all MBs match (mode, ref, mv)", frameIdx)
		}

		// Frame-level rate/Q probe (task343-style: surfaces autoSpeed,
		// rate-control, and q_index divergence between encoders).
		gFrame := task343ParseFrameRow(govpxTraceBuf.Bytes(), frameIdx)
		lFrame := task343ParseFrameRow(libvpxTrace, frameIdx)
		if gFrame != nil && lFrame != nil {
			for _, f := range []string{"q_index", "base_q_index", "loop_filter_level", "auto_speed", "projected_frame_size"} {
				gv := gFrame[f]
				lv := lFrame[f]
				marker := ""
				if !task210FieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("task353 frame%d FRAME %-22s govpx=%v libvpx=%v%s", frameIdx, f, gv, lv, marker)
			}
		}
	}
}
