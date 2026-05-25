package benchcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVpxencEncodeTimeUnits(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		ok        bool
		frames    int
		totalNS   int64
		bytesWant int
	}{
		{
			name:      "microseconds",
			stderr:    "\rPass 1/1 frame    1/0      0B       0 us 0.00 fps   \rPass 1/1 frame    3/3   1234B   45000 us  66.67 fps   \n",
			ok:        true,
			frames:    3,
			totalNS:   45_000 * int64(time.Microsecond),
			bytesWant: 1234,
		},
		{
			name:      "milliseconds-when-long",
			stderr:    "Pass 1/1 frame   30/30 567890B   12345 ms   2.43 fps   \n",
			ok:        true,
			frames:    30,
			totalNS:   12_345 * int64(time.Millisecond),
			bytesWant: 567890,
		},
		{
			name:   "no-progress-output",
			stderr: "some unrelated logging\nthat does not match\n",
			ok:     false,
		},
		{
			name:   "frames-zero",
			stderr: "Pass 1/1 frame    0/0      0B       0 us 0.00 fps   \n",
			ok:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseVpxencEncodeTime([]byte(tt.stderr))
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (got=%+v)", ok, tt.ok, got)
			}
			if !ok {
				return
			}
			if got.frames != tt.frames {
				t.Fatalf("frames = %d, want %d", got.frames, tt.frames)
			}
			if got.totalNS != tt.totalNS {
				t.Fatalf("totalNS = %d, want %d", got.totalNS, tt.totalNS)
			}
			if got.bytes != tt.bytesWant {
				t.Fatalf("bytes = %d, want %d", got.bytes, tt.bytesWant)
			}
		})
	}
}

func TestImageSSIM(t *testing.T) {
	src := makeBenchmarkFrame(16, 16, 0)
	same := makeBenchmarkFrame(16, 16, 0)
	if got := imageSSIM(src, same); got != 1 {
		t.Fatalf("identical SSIM = %f, want 1", got)
	}
	changed := makeBenchmarkFrame(16, 16, 1)
	if got := imageSSIM(src, changed); got <= 0 || got >= 1 {
		t.Fatalf("changed SSIM = %f, want between 0 and 1", got)
	}
}

func TestParseFFmpegVMAFStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmaf.json")
	data := []byte(`{
  "frames": [
    {"frameNum": 0, "metrics": {"integer_vmaf": 91.25, "integer_motion2": 0.1}},
    {"frameNum": 1, "metrics": {"vmaf": 92.50}}
  ],
  "pooled_metrics": {"integer_vmaf": {"mean": 91.875}}
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	values, err := parseFFmpegVMAFStats(path)
	if err != nil {
		t.Fatalf("parseFFmpegVMAFStats returned error: %v", err)
	}
	if len(values) != 2 || values[0] != 91.25 || values[1] != 92.50 {
		t.Fatalf("values = %v, want [91.25 92.5]", values)
	}
}

func TestPlotArtifactsIncludeVMAF(t *testing.T) {
	report := plotComparisonReport{
		Width:       16,
		Height:      16,
		Frames:      2,
		FPS:         30,
		BitrateKbps: 1200,
		Mode:        "realtime",
		Govpx: plotEncoderSummary{
			EncodeFPS:         60,
			OutputBitrateKbps: 1100,
			AverageVMAF:       91,
			AveragePSNR:       40,
			AverageSSIM:       0.98,
		},
		Libvpx: plotEncoderSummary{
			EncodeFPS:         120,
			OutputBitrateKbps: 1200,
			AverageVMAF:       93,
			AveragePSNR:       41,
			AverageSSIM:       0.99,
		},
		FramesData: []plotFrameComparison{
			{Frame: 0, GovpxVMAF: 90, LibvpxVMAF: 92, GovpxPSNR: 39, LibvpxPSNR: 40, GovpxSSIM: 0.97, LibvpxSSIM: 0.98, GovpxBytes: 100, LibvpxBytes: 110},
			{Frame: 1, GovpxVMAF: 92, LibvpxVMAF: 94, GovpxPSNR: 41, LibvpxPSNR: 42, GovpxSSIM: 0.99, LibvpxSSIM: 0.995, GovpxBytes: 90, LibvpxBytes: 100},
		},
	}
	csv := renderPlotCSV(report)
	if !strings.Contains(csv, "govpx_vmaf,libvpx_vmaf") || !strings.Contains(csv, "0,90.000000,92.000000") {
		t.Fatalf("CSV missing VMAF columns/data:\n%s", csv)
	}
	svg := renderPlotSVG(report)
	if !strings.Contains(svg, "VMAF") || !strings.Contains(svg, "govpx 60.00 fps") {
		t.Fatalf("SVG missing VMAF summary:\n%s", svg)
	}
	text := formatPlotReport(report)
	if !strings.Contains(text, "vmaf=91.000") || !strings.Contains(text, "vmaf_delta=-2.000") {
		t.Fatalf("text report missing VMAF data:\n%s", text)
	}
}
