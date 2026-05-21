package coracle

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestVP9SpatialSVCEncodeI420RejectsInvalidInputBeforePathLookup(t *testing.T) {
	cfg := vp9SpatialSVCTestConfig()
	if _, _, err := VP9SpatialSVCEncodeI420(nil, cfg); err == nil {
		t.Fatal("VP9SpatialSVCEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVP9SpatialSVCEncoderNotBuilt) {
		t.Fatal("VP9SpatialSVCEncodeI420 looked up the binary before validating input")
	}
}

func TestVP9SpatialSVCEncodeI420UsesConfiguredBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is Unix-only")
	}
	encoder := writeExecutableScript(t, `#!/bin/sh
out=
prev=
for arg do
	if [ "$prev" = "-o" ]; then
		out=$arg
	fi
	prev=$arg
done
printf 'args:%s\n' "$*"
printf 'ivf' > "$out"
`)
	cfg := vp9SpatialSVCTestConfig()
	cfg.BinaryPath = encoder

	raw := make([]byte, 16*16+8*8*2)
	ivf, diag, err := VP9SpatialSVCEncodeI420(raw, cfg)
	if err != nil {
		t.Fatalf("VP9SpatialSVCEncodeI420: %v\n%s", err, diag)
	}
	if string(ivf) != "ivf" {
		t.Fatalf("ivf = %q, want script output", ivf)
	}
	got := string(diag)
	for _, want := range []string{
		"-f 1",
		"-w 16",
		"-h 16",
		"-t 1/30",
		"-b 100",
		"-sl 2",
		"-r 1/2,1/1",
		"-bl 40,60",
		"--min-q=4,4",
		"--max-q=56,56",
		"--lag-in-frames=0",
		"--rc-end-usage=1",
		"--inter-layer-pred=0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diag = %q, want args containing %q", got, want)
		}
	}
}

func TestVP9SpatialSVCConfigRejectsTemporalBitrateMismatch(t *testing.T) {
	cfg := vp9SpatialSVCTestConfig()
	cfg.TemporalLayerCount = 2
	cfg.TemporalLayeringMode = 2
	if err := cfg.validate(); err == nil {
		t.Fatal("VP9SpatialSVCConfig accepted missing temporal-layer bitrates")
	}
	cfg.LayerBitratesKbps = []int{30, 40, 60, 100}
	if err := cfg.validate(); err != nil {
		t.Fatalf("VP9SpatialSVCConfig rejected temporal-layer bitrates: %v", err)
	}
}

func vp9SpatialSVCTestConfig() VP9SpatialSVCConfig {
	return VP9SpatialSVCConfig{
		Width:                    16,
		Height:                   16,
		Frames:                   1,
		Timebase:                 "1/30",
		TotalBitrateKbps:         100,
		LayerCount:               2,
		ScaleFactors:             "1/2,1/1",
		LayerBitratesKbps:        []int{40, 60},
		KeyFrameInterval:         128,
		MinQuantizer:             4,
		MaxQuantizer:             56,
		LagInFrames:              0,
		Threads:                  1,
		Speed:                    8,
		RateControlEndUsage:      1,
		InterLayerPredictionMode: 0,
	}
}
