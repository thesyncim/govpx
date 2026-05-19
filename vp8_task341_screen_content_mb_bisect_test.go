//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"image"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Task341ScreenContentMBBisect performs the per-MB localization of
// the inter-mode RD-picker divergence on a 720p screen-content (synthetic
// glyph-translation) fixture, replaying the same source the BD-rate gate
// (TestVP8FeatureBDRate720pScreenContentCBR) drives. The fixture exposes a
// +36% BD-rate gap that #340 traced to mid-Q (Q=62) bit-spend overshoot at
// attempt 2 of the recode loop; this probe pins the FIRST per-MB mode
// flip that diverges between govpx and libvpx so the fix can target a
// specific picker site rather than re-instrumenting the recode loop.
//
// Method:
//  1. Encode 2 frames (KF + 1 inter) of makeVP8ScreenTextWindowFrame at
//     1280x720, ScreenContentMode=1, default Deadline (BestQuality), CBR
//     2 Mbps. Capture govpx's per-MB inter-mode RD trace.
//  2. Run the patched libvpx vpxenc-oracle on the same raw I420 source,
//     same args, same flags. Capture its per-MB trace.
//  3. Diff per-MB (mb_row, mb_col, mode, ref_frame, mv) for the FIRST
//     inter frame (frame_index=1).
//  4. Histogram the divergent-mode picks (govpx vs libvpx) and log the
//     first divergent MB's full RD-scoreboard candidate dump.
//
// This test is logging-only (always passes); it pins the localization
// state on stdout so the next iteration can target a specific picker
// branch.
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8Task341ScreenContentMBBisect -v
func TestVP8Task341ScreenContentMBBisect(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #341 screen-content MB bisect")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		frameCount = 2
		targetKbps = 2000
	)

	// Generate the SAME source the BD-rate fixture uses. We need 2 frames
	// so the second one (an inter frame) drives the RD picker on the
	// integer-pel glyph translation that triggers the divergence.
	ycbcrSources := make([]*image.YCbCr, frameCount)
	govpxSources := make([]Image, frameCount)
	for i := range ycbcrSources {
		yc := task341MakeScreenTextWindowFrame(width, height, i)
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

	// libvpx side via the patched vpxenc-oracle.
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "task341.yuv")
	ivfPath := filepath.Join(dir, "task341.ivf")
	libvpxTracePath := filepath.Join(dir, "task341.jsonl")
	task341WriteI420(t, yuvPath, govpxSources)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--best",
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--screen-content-mode=1",
		"--threads=1",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
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

	govpxOut := "/tmp/govpx_task341_screen_content.jsonl"
	libvpxOut := "/tmp/libvpx_task341_screen_content.jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("task341 govpx_trace=%s libvpx_trace=%s govpx_bytes=%d libvpx_bytes=%d",
		govpxOut, libvpxOut, govpxTraceBuf.Len(), len(libvpxTrace))

	// Per-MB scoreboard analysis on the inter frame (frame_index=1).
	for _, frameIdx := range []uint64{0, 1} {
		gRows := task210ParseMBRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := task210ParseMBRowsForFrame(libvpxTrace, frameIdx)
		t.Logf("task341 frame%d govpx_mb_rows=%d libvpx_mb_rows=%d", frameIdx, len(gRows), len(lRows))

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

		// Mode + ref-frame histograms for divergent MBs.
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
			modePair := gm + "|" + lm
			refPair := gref + "|" + lref
			if gm != lm {
				modeMismatches++
				modePairs[modePair]++
			}
			if gref != lref {
				refMismatches++
				refPairs[refPair]++
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
		t.Logf("task341 frame%d mode_mismatches=%d ref_mismatches=%d mv_mismatches=%d total_mbs=%d",
			frameIdx, modeMismatches, refMismatches, mvMismatches, len(keys))

		// Sort histogram entries for stable logging.
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
			t.Logf("task341 frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			t.Logf("task341 frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			t.Logf("task341 frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !task210FieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
		} else {
			t.Logf("task341 frame%d NO_DIV — all MBs match (mode, ref, mv)", frameIdx)
		}

		// Inter-candidate scoreboard dump for the first divergent MB.
		if frameIdx == 1 && firstDiv[0] >= 0 {
			task341LogInterCandidateScoreboardAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		}
	}
}

// task341MakeScreenTextWindowFrame is a verbatim copy of
// makeVP8ScreenTextWindowFrame so the probe test can call it without
// taking a dependency on feature_quality_gates_vp8_test.go's helpers.
// Sync any updates with feature_quality_gates_vp8_test.go:570.
func task341MakeScreenTextWindowFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(idx)*4099 + 31))
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			noise := r.Intn(7) - 3
			v := 28 + noise
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			row[x] = byte(v)
		}
	}
	const cell = 16
	const glyph = 8
	xoff := (idx * glyph) % cell
	for gy := 0; gy < height; gy += cell {
		for gx := 0; gx < width; gx += cell {
			cellHash := (gx/cell)*1103515245 + (gy/cell)*12345
			on := cellHash&0x07 < 5
			if !on {
				continue
			}
			lumaHi := byte(208 + (cellHash>>3)&0x1F)
			lumaLo := byte(168 + (cellHash>>11)&0x1F)
			x0 := gx + xoff
			y0 := gy
			for dy := range glyph {
				y := y0 + dy
				if y < 0 || y >= height {
					continue
				}
				row := img.Y[y*img.YStride:]
				for dx := range glyph {
					x := x0 + dx
					if x < 0 || x >= width {
						continue
					}
					if (dx^dy)&1 == 0 {
						row[x] = lumaHi
					} else {
						row[x] = lumaLo
					}
				}
			}
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = byte(128 + ((x+idx)*3)&0x03)
			cr[x] = byte(128 + ((y+idx*2)*3)&0x03)
		}
	}
	return img
}

func task341WriteI420(t *testing.T, path string, frames []Image) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create %s: %v", path, err)
	}
	defer file.Close()
	for i, frame := range frames {
		if err := task341WritePlane(file, frame.Y, frame.YStride, frame.Width, frame.Height); err != nil {
			t.Fatalf("write frame %d Y: %v", i, err)
		}
		uvW := (frame.Width + 1) >> 1
		uvH := (frame.Height + 1) >> 1
		if err := task341WritePlane(file, frame.U, frame.UStride, uvW, uvH); err != nil {
			t.Fatalf("write frame %d U: %v", i, err)
		}
		if err := task341WritePlane(file, frame.V, frame.VStride, uvW, uvH); err != nil {
			t.Fatalf("write frame %d V: %v", i, err)
		}
	}
}

func task341WritePlane(file *os.File, plane []byte, stride int, width int, height int) error {
	for row := range height {
		if _, err := file.Write(plane[row*stride : row*stride+width]); err != nil {
			return err
		}
	}
	return nil
}

// task341LogInterCandidateScoreboardAt parses both traces for inter_candidate
// rows at (frameIndex, mbRow, mbCol) and emits a side-by-side dump of the
// per-mode RD scoreboard (rate, distortion, RDCOST, became_best). This is
// the localized scoreboard the next-iteration fix targets.
func task341LogInterCandidateScoreboardAt(t *testing.T, gov, lib []byte, frameIdx uint64, mb [2]int) {
	gCands := task341ParseInterCandidatesForMB(gov, frameIdx, mb)
	lCands := task341ParseInterCandidatesForMB(lib, frameIdx, mb)
	t.Logf("task341 frame%d MB(%d,%d) inter_candidate scoreboard:", frameIdx, mb[0], mb[1])
	t.Logf("  govpx_candidates=%d libvpx_candidates=%d", len(gCands), len(lCands))
	// Index by mode_index for side-by-side dump.
	gByIdx := map[int]map[string]any{}
	lByIdx := map[int]map[string]any{}
	idxs := map[int]struct{}{}
	for _, c := range gCands {
		mi, _ := c["mode_index"].(float64)
		gByIdx[int(mi)] = c
		idxs[int(mi)] = struct{}{}
	}
	for _, c := range lCands {
		mi, _ := c["mode_index"].(float64)
		lByIdx[int(mi)] = c
		idxs[int(mi)] = struct{}{}
	}
	orderedIdx := make([]int, 0, len(idxs))
	for i := range idxs {
		orderedIdx = append(orderedIdx, i)
	}
	sort.Ints(orderedIdx)
	for _, mi := range orderedIdx {
		g, gok := gByIdx[mi]
		l, lok := lByIdx[mi]
		if !gok && !lok {
			continue
		}
		t.Logf("  mode_index=%d:", mi)
		fields := []string{"mode", "ref_frame", "ref_slot", "threshold", "outcome", "became_best", "rate", "rate_y", "rate_uv", "distortion", "distortion_uv", "score", "yrd", "sse"}
		for _, f := range fields {
			var gv, lv any
			if gok {
				gv = g[f]
			}
			if lok {
				lv = l[f]
			}
			marker := ""
			if gok && lok && !task210FieldsEqual(gv, lv) {
				marker = " <DIFF>"
			}
			t.Logf("    %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
		}
	}
}

func task341ParseInterCandidatesForMB(trace []byte, frameIdx uint64, mb [2]int) []map[string]any {
	rows := []map[string]any{}
	all := task341ParseInterCandidatesForFrame(trace, frameIdx)
	for _, r := range all {
		row, _ := r["mb_row"].(float64)
		col, _ := r["mb_col"].(float64)
		if int(row) == mb[0] && int(col) == mb[1] {
			rows = append(rows, r)
		}
	}
	return rows
}

func task341ParseInterCandidatesForFrame(trace []byte, frameIdx uint64) []map[string]any {
	rows := []map[string]any{}
	for _, line := range bytes.Split(trace, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		ty, _ := row["type"].(string)
		if ty != "inter_candidate" {
			continue
		}
		fi, ok := row["frame_index"].(float64)
		if !ok || uint64(fi) != frameIdx {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}
