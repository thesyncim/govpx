//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOracleActiveOddNoise3LFTraceDiagnostic(t *testing.T) {
	if os.Getenv("GOVPX_ACTIVE_ODD_TRACE") != "1" {
		t.Skip("set GOVPX_ACTIVE_ODD_TRACE=1")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	driver := findVpxencFrameFlags(t)
	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 65
		height     = 33
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
		NoiseSensitivity:  3,
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	var govpxTrace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(&govpxTrace)
	enc.SetOracleTracePredictorDump(true, true)
	mustRuntime(t, "SetActiveMap(checker)", enc.SetActiveMap(activeMapPattern("checker", rows, cols), rows, cols))
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	govpxPackets := make([][]byte, 0, len(sources))
	var frame3Internal Image
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		govpxPackets = append(govpxPackets, append([]byte(nil), result.Data...))
		if i == 3 {
			frame3Internal = testImage(width, height)
			copyVP8ImageToPublic(&frame3Internal, &enc.current.Img)
		}
	}
	enc.Close()
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder: %v", err)
	}
	var frame3Decoded Image
	for i, pkt := range govpxPackets[:4] {
		if err := dec.Decode(pkt); err != nil {
			t.Fatalf("Decode frame %d: %v", i, err)
		}
		got, ok := dec.NextFrame()
		if !ok {
			t.Fatalf("Decode frame %d produced no output", i)
		}
		if i == 3 {
			frame3Decoded = got
		}
	}
	if firstPlaneDiff(frame3Internal.Y, frame3Internal.YStride, frame3Decoded.Y, frame3Decoded.YStride, width, height) < 0 &&
		firstPlaneDiff(frame3Internal.U, frame3Internal.UStride, frame3Decoded.U, frame3Decoded.UStride, (width+1)>>1, (height+1)>>1) < 0 &&
		firstPlaneDiff(frame3Internal.V, frame3Internal.VStride, frame3Decoded.V, frame3Decoded.VStride, (width+1)>>1, (height+1)>>1) < 0 {
		t.Logf("frame 3 encoder current matches local decoder output")
	} else {
		t.Logf("frame 3 encoder current differs from local decoder: y=%d u=%d v=%d",
			firstPlaneDiff(frame3Internal.Y, frame3Internal.YStride, frame3Decoded.Y, frame3Decoded.YStride, width, height),
			firstPlaneDiff(frame3Internal.U, frame3Internal.UStride, frame3Decoded.U, frame3Decoded.UStride, (width+1)>>1, (height+1)>>1),
			firstPlaneDiff(frame3Internal.V, frame3Internal.VStride, frame3Decoded.V, frame3Decoded.VStride, (width+1)>>1, (height+1)>>1))
	}
	ivfPath := filepath.Join(t.TempDir(), "govpx-active-odd.ivf")
	rawPath := filepath.Join(t.TempDir(), "govpx-active-odd.i420")
	if err := os.WriteFile(ivfPath, makeIVF(width, height, uint32(fps), 1, govpxPackets), 0o600); err != nil {
		t.Fatalf("write IVF: %v", err)
	}
	vpxdec := filepath.Join("internal", "coracle", "build", "vpxdec")
	if info, err := os.Stat(vpxdec); err != nil || info.Mode()&0o111 == 0 {
		t.Skipf("vpxdec helper not executable at %s", vpxdec)
	}
	if out, err := exec.Command(vpxdec, "--codec=vp8", "--i420", "--output="+rawPath, ivfPath).CombinedOutput(); err != nil {
		t.Fatalf("vpxdec failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read vpxdec raw: %v", err)
	}
	frameSize := width*height + 2*((width+1)>>1)*((height+1)>>1)
	if len(raw) < 4*frameSize {
		t.Fatalf("vpxdec raw size = %d, want at least %d", len(raw), 4*frameSize)
	}
	frame3Raw := raw[3*frameSize : 4*frameSize]
	rawY := frame3Raw[:width*height]
	rawU := frame3Raw[width*height : width*height+((width+1)>>1)*((height+1)>>1)]
	rawV := frame3Raw[width*height+((width+1)>>1)*((height+1)>>1):]
	t.Logf("frame 3 encoder current vs libvpx decoder raw: y=%d u=%d v=%d",
		firstPlaneDiff(frame3Internal.Y, frame3Internal.YStride, rawY, width, width, height),
		firstPlaneDiff(frame3Internal.U, frame3Internal.UStride, rawU, (width+1)>>1, (width+1)>>1, (height+1)>>1),
		firstPlaneDiff(frame3Internal.V, frame3Internal.VStride, rawV, (width+1)>>1, (width+1)>>1, (height+1)>>1))
	tracePath := filepath.Join(t.TempDir(), "frameflags-active-odd-checker-noise3.jsonl")
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", tracePath)
	t.Setenv("GOVPX_ORACLE_PREDICTOR_DUMP", "1")
	t.Setenv("GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS", "1")
	_ = encodeFramesWithFrameFlagsDriver(t, driver, "trace-active-odd-checker-noise3", opts, targetKbps, sources, nil, []string{"--active-map=checker", "--noise-sensitivity=3"})
	libvpxTrace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}
	logRows := func(label string, trace []byte) {
		t.Helper()
		dec := json.NewDecoder(bytes.NewReader(trace))
		for {
			var row map[string]any
			if err := dec.Decode(&row); err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("%s trace decode: %v", label, err)
			}
			typ, _ := row["type"].(string)
			fi, _ := row["frame_index"].(float64)
			if (typ == "lf_trial" || typ == "frame") && fi >= 3 && fi <= 5 {
				t.Logf("%s %v", label, row)
			}
		}
	}
	logRows("govpx", govpxTrace.Bytes())
	logRows("libvpx", libvpxTrace)
	compareReconstructedRows := func(aLabel string, aTrace []byte, bLabel string, bTrace []byte) {
		t.Helper()
		readRows := func(trace []byte) map[string]string {
			rows := map[string]string{}
			dec := json.NewDecoder(bytes.NewReader(trace))
			for {
				var row map[string]any
				if err := dec.Decode(&row); err != nil {
					if err == io.EOF {
						break
					}
					t.Fatalf("trace decode: %v", err)
				}
				typ, _ := row["type"].(string)
				fi, _ := row["frame_index"].(float64)
				if typ != "reconstructed" || int(fi) != 3 {
					continue
				}
				mbRow, _ := row["mb_row"].(float64)
				mbCol, _ := row["mb_col"].(float64)
				plane, _ := row["plane"].(string)
				hex, _ := row["hex"].(string)
				rows[fmt.Sprintf("%02d/%02d/%s", int(mbRow), int(mbCol), plane)] = hex
			}
			return rows
		}
		aRows := readRows(aTrace)
		bRows := readRows(bTrace)
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				for _, plane := range []string{"y", "u", "v"} {
					key := fmt.Sprintf("%02d/%02d/%s", r, c, plane)
					if aRows[key] != bRows[key] {
						t.Logf("first reconstructed mismatch frame=3 key=%s %s_len=%d %s_len=%d", key, aLabel, len(aRows[key]), bLabel, len(bRows[key]))
						t.Logf("%s %s", aLabel, aRows[key])
						t.Logf("%s %s", bLabel, bRows[key])
						return
					}
				}
			}
		}
		t.Logf("frame 3 reconstructed rows match")
	}
	compareReconstructedRows("govpx", govpxTrace.Bytes(), "libvpx", libvpxTrace)
	comparePlaneRows := func(rowType string, frame int, aLabel string, aTrace []byte, bLabel string, bTrace []byte) {
		t.Helper()
		readRows := func(trace []byte) map[string]string {
			rows := map[string]string{}
			dec := json.NewDecoder(bytes.NewReader(trace))
			for {
				var row map[string]any
				if err := dec.Decode(&row); err != nil {
					if err == io.EOF {
						break
					}
					t.Fatalf("trace decode: %v", err)
				}
				typ, _ := row["type"].(string)
				fi, _ := row["frame_index"].(float64)
				if typ != rowType || int(fi) != frame {
					continue
				}
				plane, _ := row["plane"].(string)
				hex, _ := row["hex"].(string)
				rows[plane] = hex
			}
			return rows
		}
		aRows := readRows(aTrace)
		bRows := readRows(bTrace)
		for _, plane := range []string{"y", "u", "v"} {
			if aRows[plane] != bRows[plane] {
				t.Logf("first %s mismatch frame=%d plane=%s %s_len=%d %s_len=%d", rowType, frame, plane, aLabel, len(aRows[plane]), bLabel, len(bRows[plane]))
				t.Logf("%s %s", aLabel, aRows[plane])
				t.Logf("%s %s", bLabel, bRows[plane])
				return
			}
		}
		t.Logf("%s frame=%d planes match", rowType, frame)
	}
	comparePlaneRows("last_ref_window", 4, "govpx", govpxTrace.Bytes(), "libvpx", libvpxTrace)
}

func firstPlaneDiff(a []byte, aStride int, b []byte, bStride int, width int, height int) int {
	for y := range height {
		for x := range width {
			if a[y*aStride+x] != b[y*bStride+x] {
				return y*width + x
			}
		}
	}
	return -1
}
