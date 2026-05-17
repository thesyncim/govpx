//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestZZRuntimeTrace0BB(t *testing.T) {
	driver := findVpxencFrameFlags(t)
	tc := oracleRuntimeControlFuzzCaseFromBytes([]byte("020bA0C)a"))
	var govTrace bytes.Buffer
	enc, err := NewVP8Encoder(tc.opts)
	if err != nil {
		t.Fatal(err)
	}
	enc.SetOracleTraceWriter(&govTrace)
	buf := make([]byte, tc.opts.Width*tc.opts.Height*4+4096)
	for i, src := range tc.sources {
		if fn := tc.apply[i]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if i < len(tc.flags) {
			f = tc.flags[i]
		}
		if _, err := enc.EncodeInto(buf, src, uint64(i), 1, f); err != nil {
			t.Fatalf("gov frame %d: %v", i, err)
		}
	}

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "in.yuv")
	ivfPath := filepath.Join(dir, "out.ivf")
	tracePath := filepath.Join(dir, "trace.jsonl")
	writeEncoderValidationI420(t, yuvPath, tc.sources)
	flagsCSV := make([]string, len(tc.sources))
	for i := range flagsCSV {
		var f EncodeFlags
		if i < len(tc.flags) {
			f = tc.flags[i]
		}
		flagsCSV[i] = strconv.FormatUint(uint64(frameFlagsForLibvpx(f)), 10)
	}
	args := []string{
		"--infile=" + yuvPath,
		"--outfile=" + ivfPath,
		"--width=" + strconv.Itoa(tc.opts.Width),
		"--height=" + strconv.Itoa(tc.opts.Height),
		"--fps-num=" + strconv.Itoa(tc.opts.FPS),
		"--fps-den=1",
		"--frames=" + strconv.Itoa(len(tc.sources)),
		"--target-bitrate=" + strconv.Itoa(tc.targetKbps),
		"--min-q=" + strconv.Itoa(tc.opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(tc.opts.MaxQuantizer),
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--deadline=rt",
		"--cpu-used=" + strconv.Itoa(tc.opts.CpuUsed),
		"--end-usage=cbr",
		"--auto-alt-ref=0",
		"--token-parts=" + strconv.Itoa(tc.opts.TokenPartitions),
		"--frame-flags=" + strings.Join(flagsCSV, ","),
		"--control-script=" + strings.Join(tc.script, ","),
	}
	cmd := exec.Command(driver, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lib run: %v\n%s", err, out)
	}
	libTrace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("script=%s", strings.Join(tc.script, ","))
	t.Logf("gov projected:\n%s", projectOracleDecisionTrace(t, govTrace.Bytes()))
	t.Logf("lib projected:\n%s", projectOracleDecisionTrace(t, libTrace))
	t.Logf("gov mb4:\n%s", zzFrameMBRates(t, govTrace.Bytes(), 4))
	t.Logf("lib mb4:\n%s", zzFrameMBRates(t, libTrace, 4))
}

func zzFrameMBRates(t *testing.T, trace []byte, frame int) string {
	t.Helper()
	var out strings.Builder
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row["type"] != "mb" || int(traceFloat(row["frame_index"])) != frame {
			continue
		}
		out.WriteString("r=")
		out.WriteString(strconv.Itoa(int(traceFloat(row["mb_row"]))))
		out.WriteString(" c=")
		out.WriteString(strconv.Itoa(int(traceFloat(row["mb_col"]))))
		out.WriteString(" mode=")
		out.WriteString(row["mode"].(string))
		out.WriteString(" ref=")
		out.WriteString(row["ref_frame"].(string))
		out.WriteString(" skip=")
		out.WriteString(strconv.FormatBool(row["skip"].(bool)))
		out.WriteString(" rate=")
		out.WriteString(strconv.Itoa(int(traceFloat(row["mb_rate"]))))
		out.WriteString(" agg=")
		out.WriteString(strconv.Itoa(int(traceFloat(row["aggregated_rate"]))))
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
