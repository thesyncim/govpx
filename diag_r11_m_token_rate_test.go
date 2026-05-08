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

// TestDiagR11MTokenRate is a diagnostic for the R11-M token-rate audit at
// 720p realtime CBR cpu=8. It captures per-frame size_bytes plus per-MB
// (mode, eob_sum, qcoeff abs-sum, token-class histogram) rows from both
// govpx and libvpx using the cmd/govpx-bench bench-frame fixture and prints:
//
//   - per-frame govpx/libvpx size_bytes ratio + Q match
//   - global qcoeff abs-sum / EOB-sum / nonzero-count ratios per frame
//   - per-token-class histogram (ONE, TWO, THREE, FOUR, CAT1..CAT6, ZERO)
//     counts on both sides
//   - per-(blockType, coefBand) zero/nonzero counts
//
// R11-M finding (2026-05-09, parity-close-r11-m-token-rate):
//
//   - At matched Q (frames 1-19 in the panning fixture, Q=106 both sides),
//     govpx and libvpx produce per-frame byte counts within +/-0.5%. EOB
//     sums match exactly (e.g. frame 1: gov=136595 lib=136595). The four
//     R11-M hypotheses (band-zero-bit-cost lookup, coef-prob context,
//     excessive non-zero coeffs, ZeroToken vs token-tree) would all
//     manifest as byte-ratio drift even at matched Q -- they don't.
//   - The bench harness's avg_interframe_bytes ratio of ~1.30-1.40x
//     against libvpx is driven by **mode-decision divergence under
//     wall-clock autoSpeed adaptation**, not coefficient-token rate.
//     vp8_auto_select_speed (encoder.go libvpxAutoSelectSpeed) evolves
//     e.autoSpeed based on avgPickModeTime / avgEncodeTime, and the
//     converged value diverges from libvpx because the per-frame timing
//     varies between cold and warm cache, producing different mode picks.
//     Capturing the trace itself (oracle hooks per MB) inflates wall-clock
//     timing and pushes autoSpeed away from the bench-measured trajectory.
//   - The libvpx-side qcoeff dump in the per-MB row (build_vpxenc_oracle.sh
//     line 511) captures qcoeff *after* vp8_dequant_idct_add_y_block has
//     zeroed Y-block qcoeff[0..1] for blocks where eob<2; the trace's
//     qcoeff is therefore unreliable for blockType 0 (Y_NO_DC) and 2 (UV).
//     EOB sums and Y2 (blockType 1) qcoeff remain reliable.
//
// Conclusion: R11-M's premise of a coefficient-token rate gap is not
// supported by the data on this branch. The bench-harness gap traces to
// upstream mode-decision parity (covered by parity-close work on inter
// candidate scoring, RD thresholds, autoSpeed convergence), not to the
// tokenize.go / tree.go writers.
//
// Skipped by default; run with:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_DIAG_R11M=1 \
//	  go test -run TestDiagR11MTokenRate -v -timeout 5m
//
// Set GOVPX_DIAG_R11M_BENCH=1 to use the cmd/govpx-bench bench-frame
// fixture; GOVPX_DIAG_R11M_NOISE=1 for the scoreboard noise fixture;
// otherwise defaults to encoderValidationPanningFrame.
func TestDiagR11MTokenRate(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle token-rate diag")
	}
	if os.Getenv("GOVPX_DIAG_R11M") != "1" {
		t.Skip("set GOVPX_DIAG_R11M=1 to enable R11-M token-rate diag")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1500
		frames     = 30
		cpuUsed    = 8
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
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    30,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	useNoise := os.Getenv("GOVPX_DIAG_R11M_NOISE") == "1"
	useBench := os.Getenv("GOVPX_DIAG_R11M_BENCH") == "1"
	sources := make([]Image, frames)
	for i := range sources {
		if useBench {
			sources[i] = r11MBenchmarkFrame(width, height, i)
		} else if useNoise {
			sources[i] = scoreboardBenchNoiseFrame(width, height, i)
		} else {
			sources[i] = encoderValidationPanningFrame(width, height, i)
		}
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	// Match cmd/govpx-bench's libvpx parity flags exactly, including the
	// KF interval. captureLibvpxEncoderTrace defaults --kf-min/max-dist=999
	// regardless of opts.KeyFrameInterval, so an explicit override is the
	// only way to keep the trace's libvpx side consistent with the bench
	// harness's libvpx side.
	extra := []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
		"--kf-min-dist=30", "--kf-max-dist=30",
	}
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-r11-m", opts, targetKbps, sources, extra)

	govFr := r11MFrameRows(t, govpxTrace)
	libFr := r11MFrameRows(t, libvpxTrace)
	t.Log("FRAME size_bytes / Q ratio:")
	for fi := 0; fi < len(govFr) && fi < len(libFr); fi++ {
		gb := govFr[fi].SizeBytes
		lb := libFr[fi].SizeBytes
		ratio := 0.0
		if lb > 0 {
			ratio = float64(gb) / float64(lb)
		}
		t.Logf("  frame %d (gov:%s/lib:%s) gov=%d lib=%d ratio=%.3f gov_q=%d lib_q=%d",
			fi, govFr[fi].FrameType, libFr[fi].FrameType,
			gb, lb, ratio, govFr[fi].QIndex, libFr[fi].QIndex)
	}

	govStats := r11MAggregate(t, govpxTrace)
	libStats := r11MAggregate(t, libvpxTrace)
	dumpRatios := func(label string, frame int) {
		gov := govStats[frame]
		lib := libStats[frame]
		if gov == nil || lib == nil {
			return
		}
		t.Logf("[%s] frame=%d", label, frame)
		t.Logf("  qcoeff_abs_sum gov=%d lib=%d ratio=%.3f", gov.QSum, lib.QSum, ratioOr1(gov.QSum, lib.QSum))
		t.Logf("  eob_sum        gov=%d lib=%d ratio=%.3f", gov.EOBSum, lib.EOBSum, ratioOr1(gov.EOBSum, lib.EOBSum))
		t.Logf("  nonzero_count  gov=%d lib=%d ratio=%.3f", gov.Nonzero, lib.Nonzero, ratioOr1(gov.Nonzero, lib.Nonzero))
		t.Logf("  skip_count     gov=%d lib=%d", gov.Skip, lib.Skip)
		t.Logf("  total_mbs      gov=%d lib=%d", gov.MBCount, lib.MBCount)
		t.Logf("  per-token-class abs counts (token: gov, lib, gov-lib):")
		classes := []string{"ZERO", "ONE", "TWO", "THREE", "FOUR", "CAT1", "CAT2", "CAT3", "CAT4", "CAT5", "CAT6"}
		for _, c := range classes {
			gv := gov.TokClass[c]
			lv := lib.TokClass[c]
			t.Logf("    %-5s gov=%-9d lib=%-9d delta=%+d ratio=%.3f", c, gv, lv, gv-lv, ratioOr1(gv, lv))
		}
		// Per (blockType, coefBand) nonzero count.
		t.Logf("  per-block-type nonzero counts (blockType: gov, lib):")
		for bt := 0; bt < 4; bt++ {
			t.Logf("    bt=%d gov_nz=%d lib_nz=%d", bt, gov.BlockTypeNZ[bt], lib.BlockTypeNZ[bt])
		}
	}
	// Pick the highest-ratio inter frame for deep dive.
	worst := -1
	worstRatio := 0.0
	for fi := 1; fi < len(govFr) && fi < len(libFr); fi++ {
		if libFr[fi].SizeBytes <= 0 {
			continue
		}
		r := float64(govFr[fi].SizeBytes) / float64(libFr[fi].SizeBytes)
		if r > worstRatio {
			worstRatio = r
			worst = fi
		}
	}
	if worst >= 0 {
		dumpRatios("worst-inter", worst)
	}
	dumpRatios("frame-1", 1)

	// Print per-MB top divergent eob_sum cases for the worst frame.
	if worst >= 0 {
		type div struct {
			Row, Col, GovEOB, LibEOB int
			GovQ, LibQ               int64
		}
		govMB := r11MPerMB(t, govpxTrace, worst)
		libMB := r11MPerMB(t, libvpxTrace, worst)
		divs := []div{}
		for k, gv := range govMB {
			lv, ok := libMB[k]
			if !ok {
				continue
			}
			d := div{
				Row: k.Row, Col: k.Col,
				GovEOB: gv.EOB, LibEOB: lv.EOB,
				GovQ: gv.QSum, LibQ: lv.QSum,
			}
			divs = append(divs, d)
		}
		sort.Slice(divs, func(i, j int) bool {
			return (divs[i].GovEOB - divs[i].LibEOB) > (divs[j].GovEOB - divs[j].LibEOB)
		})
		t.Logf("worst-inter top 16 (gov_eob - lib_eob) MBs (frame=%d):", worst)
		for i := 0; i < len(divs) && i < 16; i++ {
			d := divs[i]
			t.Logf("  MB(%d,%d) gov_eob=%d lib_eob=%d delta_eob=%+d gov_qsum=%d lib_qsum=%d",
				d.Row, d.Col, d.GovEOB, d.LibEOB, d.GovEOB-d.LibEOB, d.GovQ, d.LibQ)
		}
	}
	_ = fmt.Sprintf
}

// r11MBenchmarkFrame mirrors cmd/govpx-bench/main.go makeBenchmarkFrame so the
// per-MB token-rate diag exercises the exact source content the bench harness
// uses for its inter-byte ratio comparison.
func r11MBenchmarkFrame(width, height, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func ratioOr1(a, b int64) float64 {
	if b == 0 {
		if a == 0 {
			return 1.0
		}
		return 0.0
	}
	return float64(a) / float64(b)
}

type r11MFrameRow struct {
	FrameIndex int
	FrameType  string
	SizeBytes  int
	QIndex     int
}

func r11MFrameRows(t *testing.T, trace []byte) []r11MFrameRow {
	t.Helper()
	out := []r11MFrameRow{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "frame" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		ft, _ := row["frame_type"].(string)
		sb, _ := row["size_bytes"].(float64)
		q, _ := row["q_index"].(float64)
		out = append(out, r11MFrameRow{FrameIndex: int(fi), FrameType: ft, SizeBytes: int(sb), QIndex: int(q)})
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FrameIndex < out[j].FrameIndex })
	return out
}

type r11MAggStats struct {
	QSum        int64
	EOBSum      int64
	Nonzero     int64
	Skip        int64
	MBCount     int64
	TokClass    map[string]int64
	BlockTypeNZ [4]int64
}

func r11MAggregate(t *testing.T, trace []byte) map[int]*r11MAggStats {
	t.Helper()
	out := map[int]*r11MAggStats{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		fr := int(fi)
		st, ok := out[fr]
		if !ok {
			st = &r11MAggStats{TokClass: map[string]int64{}}
			out[fr] = st
		}
		st.MBCount++
		var skip bool
		if sv, ok := row["skip"].(bool); ok {
			skip = sv
		} else if sv, ok := row["skip"].(float64); ok {
			skip = sv != 0
		}
		if skip {
			st.Skip++
		}
		if eob, ok := row["eob_sum"].(float64); ok {
			st.EOBSum += int64(eob)
		}
		mode, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		isIntra := ref == "INTRA_FRAME"
		isB4x4 := mode == "B_PRED" || mode == "SPLITMV"
		// blockType per libvpx: 0=Y_NO_DC, 1=Y2, 2=UV, 3=Y_W_DC. For B_PRED/SPLITMV
		// the Y blocks are blockType 3 with no Y2; otherwise Y blocks blockType 0 + Y2 blockType 1.
		_ = isIntra
		qc, _ := row["qcoeff"].([]any)
		for blockIdx, blk := range qc {
			blk2, _ := blk.([]any)
			bt := 0
			if blockIdx < 16 {
				if isB4x4 {
					bt = 3
				} else {
					bt = 0
				}
			} else if blockIdx < 24 {
				bt = 2
			} else if blockIdx == 24 {
				bt = 1
				if isB4x4 {
					continue
				}
			}
			for _, c := range blk2 {
				cv, _ := c.(float64)
				v := int64(cv)
				if v == 0 {
					st.TokClass["ZERO"]++
					continue
				}
				st.Nonzero++
				st.BlockTypeNZ[bt]++
				abs := v
				if abs < 0 {
					abs = -abs
				}
				st.QSum += abs
				switch {
				case abs == 1:
					st.TokClass["ONE"]++
				case abs == 2:
					st.TokClass["TWO"]++
				case abs == 3:
					st.TokClass["THREE"]++
				case abs == 4:
					st.TokClass["FOUR"]++
				case abs <= 6:
					st.TokClass["CAT1"]++
				case abs <= 10:
					st.TokClass["CAT2"]++
				case abs <= 18:
					st.TokClass["CAT3"]++
				case abs <= 34:
					st.TokClass["CAT4"]++
				case abs <= 66:
					st.TokClass["CAT5"]++
				default:
					st.TokClass["CAT6"]++
				}
			}
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

type r11MMBKey struct {
	Row, Col int
}

type r11MMBStats struct {
	EOB  int
	QSum int64
}

func r11MPerMB(t *testing.T, trace []byte, frame int) map[r11MMBKey]*r11MMBStats {
	t.Helper()
	out := map[r11MMBKey]*r11MMBStats{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if int(fi) != frame {
			continue
		}
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		k := r11MMBKey{Row: int(mr), Col: int(mc)}
		st := &r11MMBStats{}
		if eob, ok := row["eob_sum"].(float64); ok {
			st.EOB = int(eob)
		}
		qc, _ := row["qcoeff"].([]any)
		for _, blk := range qc {
			blk2, _ := blk.([]any)
			for _, c := range blk2 {
				cv, _ := c.(float64)
				v := int64(cv)
				if v < 0 {
					v = -v
				}
				st.QSum += v
			}
		}
		out[k] = st
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}
