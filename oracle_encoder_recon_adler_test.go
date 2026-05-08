package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestOracleReconstructionAdler32Match locks in the byte-identity reconstruction
// win by comparing per-frame y/u/v Adler32, q_index, and size_bytes against the
// libvpx oracle on a small panning fixture.
func TestOracleReconstructionAdler32Match(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle reconstruction comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	baseOpts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
	}
	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
	}{
		{name: "realtime-cbr-cpu0", deadline: DeadlineRealtime, cpuUsed: 0},
		{name: "realtime-cbr-cpu4", deadline: DeadlineRealtime, cpuUsed: 4},
		{name: "realtime-cbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8},
		{name: "good-quality-cbr-cpu5", deadline: DeadlineGoodQuality, cpuUsed: 5},
	}
	for _, cfg := range cases {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			opts := baseOpts
			opts.Deadline = cfg.deadline
			opts.CpuUsed = cfg.cpuUsed
			govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "recon-adler-"+cfg.name, opts, targetKbps, sources, []string{"--end-usage=cbr"})

			gFrames := oracleTraceFrameRows(t, govpxTrace)
			lFrames := oracleTraceFrameRows(t, libvpxTrace)
			if len(gFrames) != frames {
				t.Fatalf("[%s] govpx frame rows = %d, want %d", cfg.name, len(gFrames), frames)
			}
			if len(lFrames) != frames {
				t.Fatalf("[%s] libvpx frame rows = %d, want %d", cfg.name, len(lFrames), frames)
			}
			for i := 0; i < frames; i++ {
				g := gFrames[i]
				l := lFrames[i]
				if g["y_adler32"] != l["y_adler32"] {
					t.Errorf("[%s] frame %d y_adler32 govpx=%v libvpx=%v", cfg.name, i, g["y_adler32"], l["y_adler32"])
				}
				if g["u_adler32"] != l["u_adler32"] {
					t.Errorf("[%s] frame %d u_adler32 govpx=%v libvpx=%v", cfg.name, i, g["u_adler32"], l["u_adler32"])
				}
				if g["v_adler32"] != l["v_adler32"] {
					t.Errorf("[%s] frame %d v_adler32 govpx=%v libvpx=%v", cfg.name, i, g["v_adler32"], l["v_adler32"])
				}
				if g["q_index"] != l["q_index"] {
					t.Errorf("[%s] frame %d q_index govpx=%v libvpx=%v", cfg.name, i, g["q_index"], l["q_index"])
				}
				gSize := traceFloat(g["size_bytes"])
				lSize := traceFloat(l["size_bytes"])
				if lSize <= 0 {
					t.Errorf("[%s] frame %d libvpx size_bytes = %v, want >0", cfg.name, i, l["size_bytes"])
					continue
				}
				if math.Abs((gSize-lSize)/lSize) > 0.01 {
					t.Errorf("[%s] frame %d size_bytes govpx=%v libvpx=%v exceeds ±1.0%%", cfg.name, i, gSize, lSize)
				}
			}
		})
	}
}

// oracleTraceFrameRows returns frame rows in trace order.
func oracleTraceFrameRows(t *testing.T, trace []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if typ, _ := row["type"].(string); typ == "frame" {
			rows = append(rows, row)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func traceFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}
