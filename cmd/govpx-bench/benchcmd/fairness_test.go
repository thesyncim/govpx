package benchcmd

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVpxdecSummary(t *testing.T) {
	stderr := []byte("48 decoded frames/48 showed frames in 42150 us (1138.79 fps)\r\n")
	got, ok := parseVpxdecSummary(stderr)
	if !ok {
		t.Fatal("parseVpxdecSummary failed on valid vpxdec --summary output")
	}
	if got.decoded != 48 || got.showed != 48 || got.totalNS != 42150*int64(time.Microsecond) {
		t.Fatalf("got %+v, want decoded=48 showed=48 totalNS=42150000", got)
	}
	if _, ok := parseVpxdecSummary([]byte("48 decoded frames/0.024 seconds\n")); ok {
		t.Fatal("parseVpxdecSummary unexpectedly matched non-summary line")
	}
}

func TestCollectEncodeFairnessWarnings(t *testing.T) {
	cfg := benchConfig{Codec: codecVP8, Mode: "realtime", CpuUsed: 8}
	rep := benchReport{
		Codec:         codecVP8,
		Mode:          "realtime",
		DroppedFrames: 8,
		Reference: &referenceReport{
			DroppedFrames: 4,
			TimingSource:  "wall",
		},
	}
	warnings := collectEncodeFairnessWarnings(cfg, rep)
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want 3 warnings", warnings)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"adaptive CPU speed", "frame drop mismatch", "subprocess wall clock"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}

	// Clean fair config should produce zero warnings.
	cleanCfg := benchConfig{Codec: codecVP8, Mode: "realtime", CpuUsed: -8}
	cleanRep := benchReport{
		Codec: codecVP8,
		Mode:  "realtime",
		Reference: &referenceReport{
			TimingSource: "vpxenc-stats",
		},
	}
	if got := collectEncodeFairnessWarnings(cleanCfg, cleanRep); len(got) != 0 {
		t.Fatalf("clean warnings = %v, want none", got)
	}
}

func TestCollectDecodeFairnessWarnings(t *testing.T) {
	cfg := benchConfig{Codec: codecVP8, Threads: 4}
	rep := decodeBenchReport{
		Codec:         codecVP8,
		DecodedFrames: 30,
		Reference: &decodeReferenceReport{
			DecodedFrames: 29,
			TimingSource:  "oracle-bench",
		},
	}
	warnings := collectDecodeFairnessWarnings(cfg, rep)
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"decoded frame count mismatch", "VP8 decode thread mismatch"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestNewFlagsDropFrameRunsContentInput(t *testing.T) {
	var cfg benchConfig
	opts := defaultBenchCLIOptions()
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	registerBenchFlags(fs, &cfg, &opts)
	if err := fs.Parse([]string{"-drop-frame=0", "-runs=3", "-content=panning", "-cpu-used=-8"}); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !cfg.DropFrameSet || cfg.DropFrame != 0 {
		t.Fatalf("DropFrame = (%v, %d), want (true, 0)", cfg.DropFrameSet, cfg.DropFrame)
	}
	if !cfg.CpuUsedSet || cfg.CpuUsed != -8 {
		t.Fatalf("CpuUsed = (%v, %d), want (true, -8)", cfg.CpuUsedSet, cfg.CpuUsed)
	}
	if cfg.Runs != 3 || cfg.Content != "panning" {
		t.Fatalf("Runs=%d Content=%q, want 3 / panning", cfg.Runs, cfg.Content)
	}
	p := parityFor(cfg)
	if p.DropFrameAllowed || p.DropFrameWaterMark != 0 {
		t.Fatalf("parityFor drop = (%v, %d), want disabled", p.DropFrameAllowed, p.DropFrameWaterMark)
	}
}

func TestLoadY4MAndIVFInput(t *testing.T) {
	dir := t.TempDir()
	y4mPath := filepath.Join(dir, "sample.y4m")
	f0 := makePanningFrame(16, 16, 0)
	f1 := makePanningFrame(16, 16, 1)
	var buf []byte
	buf = append(buf, "YUV4MPEG2 W16 H16 F25:1 Ip A0:0 C420jpeg\n"...)
	for _, f := range []struct{ y, u, v []byte }{{f0.Y, f0.U, f0.V}, {f1.Y, f1.U, f1.V}} {
		buf = append(buf, "FRAME\n"...)
		buf = append(buf, f.y...)
		buf = append(buf, f.u...)
		buf = append(buf, f.v...)
	}
	if err := os.WriteFile(y4mPath, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, fps, err := loadY4MFrames(y4mPath, 1)
	if err != nil {
		t.Fatalf("loadY4MFrames error: %v", err)
	}
	if len(loaded) != 1 || fps != 25 || loaded[0].Width != 16 || loaded[0].Height != 16 {
		t.Fatalf("loaded = len=%d fps=%d %dx%d", len(loaded), fps, loaded[0].Width, loaded[0].Height)
	}

	// IVF round-trip test.
	ivfPath := filepath.Join(dir, "sample.ivf")
	ivfBytes := makeBenchmarkIVFForCodec(codecVP9, 16, 16, 30, [][]byte{{1, 2, 3}, {4, 5, 6}})
	if err := os.WriteFile(ivfPath, ivfBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	stream, err := loadIVFInput(ivfPath, 2)
	if err != nil {
		t.Fatalf("loadIVFInput error: %v", err)
	}
	if stream.codec != codecVP9 || stream.width != 16 || stream.height != 16 || len(stream.packets) != 2 {
		t.Fatalf("stream = %+v", stream)
	}
}

func TestEncodeSuiteStandardCases(t *testing.T) {
	cases, err := encodeSuiteCases("standard")
	if err != nil {
		t.Fatalf("encodeSuiteCases(standard) error: %v", err)
	}
	if len(cases) < 2 {
		t.Fatalf("standard suite cases = %d, want >= 2", len(cases))
	}
	for _, c := range cases {
		if !c.noDrop || c.content != "panning" {
			t.Fatalf("standard case %s = %+v, want noDrop=true content=panning", c.name, c)
		}
	}
}

func TestWindowedSSIMDetectsLocalDistortion(t *testing.T) {
	src := makePanningFrame(32, 32, 0)
	if got := imageSSIM(src, src); got != 1 {
		t.Fatalf("identical SSIM = %f, want 1", got)
	}
	// Swap pixels inside each 8x8 block in a way that preserves global mean and variance
	// but destroys local structure. Global SSIM would report ~1.0; 8x8 windowed SSIM drops.
	corrupt := makePanningFrame(32, 32, 0)
	for i := 0; i < len(corrupt.Y)/2; i++ {
		j := len(corrupt.Y) - 1 - i
		corrupt.Y[i], corrupt.Y[j] = corrupt.Y[j], corrupt.Y[i]
	}
	if got := imageSSIM(src, corrupt); got > 0.90 {
		t.Fatalf("8x8 windowed SSIM on spatially reversed luma = %f, want < 0.90", got)
	}
}
