package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"testing"
)

// TestDiagSkipBreakout720p compares per-frame inter-mode skip rates between
// govpx and libvpx for a 1280x720 RT/CBR/Q=56-saturated workload, mirroring
// the cmd/govpx-bench parity flags so the harness can localize where the
// inter-frame byte gap lives.
//
// R9-3 finding (2026-05-08, panning-style synthetic content matching
// cmd/govpx-bench): under matched CBR buffer flags
//
//	--buf-sz=600 --buf-initial-sz=400 --buf-optimal-sz=500
//	--undershoot-pct=100 --overshoot-pct=15 --noise-sensitivity=0
//
// govpx skips MORE than libvpx (6.84% vs 3.93% of inter MBs) and emits FEWER
// non-zero coefficients (0.797x of libvpx's tteob), yet its inter bitstream
// is ~1.74x larger per frame (12722 vs 7298 B). The byte gap is therefore
// not a skip-cost / encode_breakout calibration miss: at static_thresh=0
// (the default for both vpxenc and govpx) the encode_breakout `sse2*2 < 0`
// gate inside pickinter.c is always false and the same is true of govpx's
// staticInterFastEncodeBreakout. The bytes-per-coefficient gap lives in the
// coefficient-token entropy stage instead, not in the skip / breakout path.
//
// Gated on GOVPX_DIAG_SKIP_BREAKOUT=1 so it never runs in CI.
func TestDiagSkipBreakout720p(t *testing.T) {
	if os.Getenv("GOVPX_DIAG_SKIP_BREAKOUT") != "1" {
		t.Skip("set GOVPX_DIAG_SKIP_BREAKOUT=1 to run skip-breakout diagnostic")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 600
		frames     = 30
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  fps,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = makeBenchmarkLikeFrame(width, height, i)
	}

	gov := captureGovpxEncoderTraceWithBytes(t, opts, sources)
	libvpxArgs := []string{"--end-usage=cbr",
		fmt.Sprintf("--buf-sz=%d", 600),
		fmt.Sprintf("--buf-initial-sz=%d", 400),
		fmt.Sprintf("--buf-optimal-sz=%d", 500),
		fmt.Sprintf("--undershoot-pct=%d", 100),
		fmt.Sprintf("--overshoot-pct=%d", 15),
		"--noise-sensitivity=0",
	}
	lib := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-skip-720", opts, targetKbps, sources, libvpxArgs)
	// Also capture libvpx ivf bytes per-frame.
	libBytes := captureLibvpxIVFFrameBytes(t, vpxencOracle, opts, targetKbps, sources, libvpxArgs)
	libTotalAll, libTotalInter, libInterFrames, libKey := 0, 0, 0, 0
	for i, n := range libBytes {
		t.Logf("libvpx frame %d size=%d", i, n)
		libTotalAll += n
		if i == 0 {
			libKey = n
		} else {
			libTotalInter += n
			libInterFrames++
		}
	}
	libAvgInter := 0.0
	if libInterFrames > 0 {
		libAvgInter = float64(libTotalInter) / float64(libInterFrames)
	}
	t.Logf("libvpx total=%d key=%d inter_avg=%.1f inter_frames=%d", libTotalAll, libKey, libAvgInter, libInterFrames)

	govStats := parseSkipPerFrame(t, gov)
	libStats := parseSkipPerFrame(t, lib)

	keys := make([]int, 0, len(govStats))
	for k := range govStats {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	t.Logf("frame | type | mb | gov_skip | lib_skip | gov_skip_pct | lib_skip_pct | gov_inter | lib_inter")
	totalMB := 0
	totalGovSkip := 0
	totalLibSkip := 0
	totalGovInter := 0
	totalLibInter := 0
	totalGovIntra := 0
	totalLibIntra := 0
	for _, k := range keys {
		g, ok1 := govStats[k]
		l, ok2 := libStats[k]
		if !ok1 || !ok2 {
			continue
		}
		gPct := 0.0
		lPct := 0.0
		if g.Total > 0 {
			gPct = 100 * float64(g.Skip) / float64(g.Total)
		}
		if l.Total > 0 {
			lPct = 100 * float64(l.Skip) / float64(l.Total)
		}
		ftype := "INTER"
		if g.IsKey {
			ftype = "KEY"
		}
		t.Logf("%5d | %5s | %4d | %8d | %8d | %11.2f | %11.2f | %9d | %9d",
			k, ftype, g.Total, g.Skip, l.Skip, gPct, lPct, g.Inter, l.Inter)
		if !g.IsKey {
			totalMB += g.Total
			totalGovSkip += g.Skip
			totalLibSkip += l.Skip
			totalGovInter += g.Inter
			totalLibInter += l.Inter
			totalGovIntra += g.Intra
			totalLibIntra += l.Intra
		}
	}
	if totalMB > 0 {
		t.Logf("AGGREGATE inter frames: total_mb=%d gov_skip=%d (%.2f%%) lib_skip=%d (%.2f%%) gov_inter=%d lib_inter=%d gov_intra=%d lib_intra=%d",
			totalMB,
			totalGovSkip, 100*float64(totalGovSkip)/float64(totalMB),
			totalLibSkip, 100*float64(totalLibSkip)/float64(totalMB),
			totalGovInter, totalLibInter,
			totalGovIntra, totalLibIntra)
	}

	// Now also look at total EOB sums per frame (proxy for coefficient bytes).
	govEOBSum := perFrameTotalEOB(t, gov)
	libEOBSum := perFrameTotalEOB(t, lib)
	totGovEOB, totLibEOB := 0, 0
	t.Logf("frame | gov_total_eob | lib_total_eob | delta")
	for _, k := range keys {
		if g, ok := govEOBSum[k]; ok {
			l := libEOBSum[k]
			t.Logf("%5d | %12d | %12d | %5d", k, g, l, g-l)
			if k != 0 {
				totGovEOB += g
				totLibEOB += l
			}
		}
	}
	t.Logf("AGGREGATE EOB inter frames: gov=%d lib=%d ratio=%.3f", totGovEOB, totLibEOB, float64(totGovEOB)/float64(totLibEOB))

	// Now find MBs where libvpx skipped but govpx didn't (most interesting)
	libOnlySkipExamples := findLibOnlySkipMBs(t, gov, lib, 20)
	t.Logf("first %d (frame,row,col) where lib skipped but gov did not:", len(libOnlySkipExamples))
	for _, ex := range libOnlySkipExamples {
		t.Logf("  frame=%d row=%d col=%d gov_mode=%s gov_ref=%s gov_eob_sum=%d lib_mode=%s lib_ref=%s lib_eob_sum=%d",
			ex.FrameIndex, ex.MBRow, ex.MBCol,
			ex.GovMode, ex.GovRef, ex.GovEOBSum,
			ex.LibMode, ex.LibRef, ex.LibEOBSum)
	}

	// And the converse
	govOnlySkipExamples := findGovOnlySkipMBs(t, gov, lib, 20)
	t.Logf("first %d (frame,row,col) where gov skipped but lib did not:", len(govOnlySkipExamples))
	for _, ex := range govOnlySkipExamples {
		t.Logf("  frame=%d row=%d col=%d gov_mode=%s gov_ref=%s gov_eob_sum=%d lib_mode=%s lib_ref=%s lib_eob_sum=%d",
			ex.FrameIndex, ex.MBRow, ex.MBCol,
			ex.GovMode, ex.GovRef, ex.GovEOBSum,
			ex.LibMode, ex.LibRef, ex.LibEOBSum)
	}
}

type frameSkipStats struct {
	Total int
	Skip  int
	Inter int
	Intra int
	IsKey bool
}

func parseSkipPerFrame(t *testing.T, trace []byte) map[int]*frameSkipStats {
	t.Helper()
	out := map[int]*frameSkipStats{}
	frameTypes := map[uint64]string{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		switch typ {
		case "frame":
			fi := readUint64(row, "frame_index")
			ft, _ := row["frame_type"].(string)
			frameTypes[fi] = ft
		case "mb":
			fi := readUint64(row, "frame_index")
			s, ok := out[int(fi)]
			if !ok {
				s = &frameSkipStats{}
				out[int(fi)] = s
				if frameTypes[fi] == "key" || frameTypes[fi] == "KEY" {
					s.IsKey = true
				}
			}
			s.Total++
			skip, _ := row["skip"].(bool)
			if skip {
				s.Skip++
			}
			ref, _ := row["ref_frame"].(string)
			if ref == "intra" || ref == "INTRA" || ref == "INTRA_FRAME" {
				s.Intra++
			} else {
				s.Inter++
			}
		}
	}
	return out
}

type skipMBExample struct {
	FrameIndex int
	MBRow      int
	MBCol      int
	GovMode    string
	GovRef     string
	GovEOBSum  int
	LibMode    string
	LibRef     string
	LibEOBSum  int
}

func findLibOnlySkipMBs(t *testing.T, gov, lib []byte, limit int) []skipMBExample {
	t.Helper()
	govMap := loadMBMap(t, gov)
	libMap := loadMBMap(t, lib)
	var out []skipMBExample
	keys := make([]string, 0, len(libMap))
	for k := range libMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		l := libMap[k]
		g, ok := govMap[k]
		if !ok {
			continue
		}
		if l.Skip && !g.Skip {
			out = append(out, skipMBExample{
				FrameIndex: l.FrameIndex,
				MBRow:      l.MBRow,
				MBCol:      l.MBCol,
				GovMode:    g.Mode, GovRef: g.RefFrame, GovEOBSum: g.EOBSum,
				LibMode: l.Mode, LibRef: l.RefFrame, LibEOBSum: l.EOBSum,
			})
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func findGovOnlySkipMBs(t *testing.T, gov, lib []byte, limit int) []skipMBExample {
	t.Helper()
	govMap := loadMBMap(t, gov)
	libMap := loadMBMap(t, lib)
	var out []skipMBExample
	keys := make([]string, 0, len(govMap))
	for k := range govMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := govMap[k]
		l, ok := libMap[k]
		if !ok {
			continue
		}
		if g.Skip && !l.Skip {
			out = append(out, skipMBExample{
				FrameIndex: g.FrameIndex,
				MBRow:      g.MBRow,
				MBCol:      g.MBCol,
				GovMode:    g.Mode, GovRef: g.RefFrame, GovEOBSum: g.EOBSum,
				LibMode: l.Mode, LibRef: l.RefFrame, LibEOBSum: l.EOBSum,
			})
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

type mbInfo struct {
	FrameIndex int
	MBRow      int
	MBCol      int
	Mode       string
	RefFrame   string
	Skip       bool
	EOBSum     int
}

func loadMBMap(t *testing.T, trace []byte) map[string]mbInfo {
	t.Helper()
	out := map[string]mbInfo{}
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
		fi := int(readUint64(row, "frame_index"))
		r := int(readUint64(row, "mb_row"))
		c := int(readUint64(row, "mb_col"))
		mode, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		skip, _ := row["skip"].(bool)
		eobSum := int(readUint64(row, "eob_sum"))
		key := fmt.Sprintf("%d:%d:%d", fi, r, c)
		out[key] = mbInfo{
			FrameIndex: fi,
			MBRow:      r,
			MBCol:      c,
			Mode:       mode,
			RefFrame:   ref,
			Skip:       skip,
			EOBSum:     eobSum,
		}
	}
	return out
}

// captureGovpxEncoderTraceWithBytes captures the trace AND reports per-frame
// payload bytes via t.Logf so the diag can report actual byte sizes alongside
// skip-rate to localize where the inter-frame byte gap lives.
func captureGovpxEncoderTraceWithBytes(t *testing.T, opts EncoderOptions, sources []Image) []byte {
	t.Helper()
	var trace bytes.Buffer
	opts.OracleTraceWriter = &trace
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, opts.Width*opts.Height*3)
	totalBytes := 0
	keyBytes := 0
	interBytes := 0
	interFrames := 0
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		n := len(result.Data)
		totalBytes += n
		if result.KeyFrame {
			keyBytes += n
		} else {
			interBytes += n
			interFrames++
		}
		t.Logf("govpx frame %d size=%d isKey=%v", i, n, result.KeyFrame)
	}
	avgInter := 0.0
	if interFrames > 0 {
		avgInter = float64(interBytes) / float64(interFrames)
	}
	t.Logf("govpx total=%d key=%d inter_avg=%.1f inter_frames=%d", totalBytes, keyBytes, avgInter, interFrames)
	return append([]byte(nil), trace.Bytes()...)
}

// captureLibvpxIVFFrameBytes runs vpxenc on the same parity flags as the bench
// (and the trace harness, with the supplied extraArgs appended) and returns
// the bitstream byte count for each emitted frame, parsed straight from the
// IVF container.
func captureLibvpxIVFFrameBytes(t *testing.T, vpxenc string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) []int {
	t.Helper()
	dir := t.TempDir()
	yuvPath := dir + "/in.yuv"
	ivfPath := dir + "/out.ivf"
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--good"
	if opts.Deadline == DeadlineRealtime {
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8", "--ivf", "--quiet", deadlineArg,
		fmt.Sprintf("--cpu-used=%d", opts.CpuUsed),
		"--lag-in-frames=0", "--auto-alt-ref=0",
		fmt.Sprintf("--kf-min-dist=%d", opts.KeyFrameInterval),
		fmt.Sprintf("--kf-max-dist=%d", opts.KeyFrameInterval),
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		fmt.Sprintf("--min-q=%d", opts.MinQuantizer),
		fmt.Sprintf("--max-q=%d", opts.MaxQuantizer),
		"--i420",
		fmt.Sprintf("--width=%d", opts.Width),
		fmt.Sprintf("--height=%d", opts.Height),
		fmt.Sprintf("--timebase=1/%d", opts.FPS),
		fmt.Sprintf("--fps=%d/1", opts.FPS),
		fmt.Sprintf("--limit=%d", len(sources)),
		fmt.Sprintf("--threads=1"),
		fmt.Sprintf("--passes=1"),
		fmt.Sprintf("--lag-in-frames=0"),
		fmt.Sprintf("--output=%s", ivfPath),
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := execCommand(vpxenc, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read ivf: %v", err)
	}
	// Parse IVF: 32-byte file header, then per-frame { size:u32, pts:u64, payload }
	if len(data) < 32 {
		return nil
	}
	out := []int{}
	off := 32
	for off+12 <= len(data) {
		size := int(uint32(data[off]) | uint32(data[off+1])<<8 | uint32(data[off+2])<<16 | uint32(data[off+3])<<24)
		off += 12
		if off+size > len(data) {
			break
		}
		out = append(out, size)
		off += size
	}
	return out
}

// execCommand wraps exec.Command so callers don't have to import os/exec.
func execCommand(name string, args ...string) *execWrapper {
	return &execWrapper{name: name, args: args}
}

type execWrapper struct {
	name string
	args []string
}

func (e *execWrapper) CombinedOutput() ([]byte, error) {
	c := exec.Command(e.name, e.args...)
	return c.CombinedOutput()
}

// perFrameTotalEOB sums each frame's total eob across all MBs.
func perFrameTotalEOB(t *testing.T, trace []byte) map[int]int {
	t.Helper()
	out := map[int]int{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if t, _ := row["type"].(string); t != "mb" {
			continue
		}
		fi := int(readUint64(row, "frame_index"))
		out[fi] += int(readUint64(row, "eob_sum"))
	}
	return out
}

// makeBenchmarkLikeFrame mirrors cmd/govpx-bench/main.go:makeBenchmarkFrame
// so the diagnostic exercises the same content the 720p bench uses.
func makeBenchmarkLikeFrame(width, height, index int) Image {
	img := testImage(width, height)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func readUint64(row map[string]any, key string) uint64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return uint64(x)
	case int:
		return uint64(x)
	case int64:
		return uint64(x)
	case uint64:
		return x
	case string:
		var i uint64
		fmt.Sscan(x, &i)
		return i
	}
	return 0
}
