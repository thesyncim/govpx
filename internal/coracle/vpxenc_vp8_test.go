package coracle

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestVpxencVP8ConfigArgs(t *testing.T) {
	cfg := VpxencVP8Config{
		Width:                64,
		Height:               32,
		Frames:               3,
		Deadline:             "rt",
		DisableWarningPrompt: true,
		CPUUsed:              -4,
		LagInFrames:          5,
		AutoAltRef:           true,
		TargetBitrateKbps:    700,
		MinQ:                 4,
		MaxQ:                 56,
		Timebase:             "1/90000",
		FPS:                  "30/1",
		KeyFrameDistSet:      true,
		KeyFrameMinDist:      9,
		KeyFrameMaxDist:      11,
		ExtraArgs:            []string{"--end-usage=vbr", "--sharpness=3"},
	}
	args := cfg.vpxencArgs("input.i420", "output.ivf")

	for _, want := range []string{
		"--codec=vp8",
		"--disable-warning-prompt",
		"--rt",
		"--cpu-used=-4",
		"--lag-in-frames=5",
		"--auto-alt-ref=1",
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--width=64",
		"--height=32",
		"--timebase=1/90000",
		"--fps=30/1",
		"--limit=3",
		"--output=output.ivf",
		"--kf-min-dist=9",
		"--kf-max-dist=11",
		"--end-usage=vbr",
		"--sharpness=3",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, strings.Join(args, " "))
		}
	}
	if got := args[len(args)-1]; got != "input.i420" {
		t.Fatalf("last arg = %q, want input path", got)
	}
}

func TestVpxencVP8ConfigArgsDefaultDeadlineAndKeyFrameDistance(t *testing.T) {
	cfg := VpxencVP8Config{
		Width:             16,
		Height:            16,
		Frames:            1,
		TargetBitrateKbps: 100,
		Timebase:          "1/30",
		FPS:               "30/1",
	}
	args := cfg.vpxencArgs("in.yuv", "out.ivf")

	if !slices.Contains(args, "--good") {
		t.Fatalf("args missing default deadline: %s", strings.Join(args, " "))
	}
	for _, unwanted := range []string{"--kf-min-dist=0", "--kf-max-dist=0"} {
		if slices.Contains(args, unwanted) {
			t.Fatalf("args contained unset key-frame distance %q: %s", unwanted, strings.Join(args, " "))
		}
	}
}

func TestVpxencVP8ConfigCanUseLibvpxDefaultQuantizers(t *testing.T) {
	cfg := VpxencVP8Config{
		Width:             16,
		Height:            16,
		Frames:            1,
		TargetBitrateKbps: 100,
		FPS:               "30/1",
		OmitQuantizerArgs: true,
	}
	args := cfg.vpxencArgs("in.yuv", "out.ivf")

	for _, unwanted := range []string{"--min-q=0", "--max-q=0"} {
		if slices.Contains(args, unwanted) {
			t.Fatalf("args contained %q despite OmitQuantizerArgs: %s", unwanted, strings.Join(args, " "))
		}
	}
}

func TestVpxencVP8ConfigTwoPassArgs(t *testing.T) {
	cfg := VpxencVP8Config{
		Width:             64,
		Height:            64,
		Frames:            8,
		TargetBitrateKbps: 500,
		MinQ:              4,
		MaxQ:              56,
		Timebase:          "1/30",
		FPS:               "30/1",
		ExtraArgs:         []string{"--end-usage=vbr"},
	}
	args := cfg.vpxencTwoPassArgs("input.i420", "pass2.ivf", "firstpass.fpf", 2)

	for _, want := range []string{
		"--passes=2",
		"--pass=2",
		"--fpf=firstpass.fpf",
		"--end-usage=vbr",
		"--output=pass2.ivf",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, strings.Join(args, " "))
		}
	}
	if got := args[len(args)-1]; got != "input.i420" {
		t.Fatalf("last arg = %q, want input path", got)
	}
}

func TestVpxencVP8ConfigWithExtraArgsDoesNotMutateBase(t *testing.T) {
	cfg := VpxencVP8Config{ExtraArgs: []string{"--end-usage=vbr"}}
	passCfg := cfg.withExtraArgs([]string{"--threads=4", "--arnr-maxframes=3"})

	for _, want := range []string{"--end-usage=vbr", "--threads=4", "--arnr-maxframes=3"} {
		if !slices.Contains(passCfg.ExtraArgs, want) {
			t.Fatalf("pass-specific args missing %q: %v", want, passCfg.ExtraArgs)
		}
	}
	if slices.Contains(cfg.ExtraArgs, "--threads=4") {
		t.Fatalf("base config was mutated: %v", cfg.ExtraArgs)
	}
}

func TestVpxencVP8FrameFlagsConfigArgs(t *testing.T) {
	cfg := VpxencVP8FrameFlagsConfig{
		Width:             64,
		Height:            32,
		Frames:            4,
		FPSNum:            30000,
		FPSDen:            1001,
		TargetBitrateKbps: 900,
		MinQ:              6,
		MaxQ:              48,
		KeyFrameMinDist:   7,
		KeyFrameMaxDist:   13,
		Deadline:          "best",
		CPUUsed:           2,
		EndUsage:          "cq",
		AutoAltRef:        true,
		TokenPartitions:   2,
		CQLevel:           20,
		Threads:           8,
		FrameFlags:        []uint32{1, 2},
		InvisibleFrames:   []bool{false, true, false, true},
		ExtraArgs:         []string{"--threads=3", "--drop-frame=10"},
	}
	args := cfg.vpxencArgs("input.i420", "output.ivf")

	for _, want := range []string{
		"--infile=input.i420",
		"--outfile=output.ivf",
		"--width=64",
		"--height=32",
		"--fps-num=30000",
		"--fps-den=1001",
		"--frames=4",
		"--target-bitrate=900",
		"--min-q=6",
		"--max-q=48",
		"--kf-min-dist=7",
		"--kf-max-dist=13",
		"--deadline=best",
		"--cpu-used=2",
		"--end-usage=cq",
		"--auto-alt-ref=1",
		"--token-parts=2",
		"--frame-flags=1,2,0,0",
		"--invisible-frames=0,1,0,1",
		"--cq-level=20",
		"--threads=3",
		"--drop-frame=10",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, strings.Join(args, " "))
		}
	}
	if slices.Contains(args, "--threads=8") {
		t.Fatalf("args included default thread count despite explicit override: %s", strings.Join(args, " "))
	}
}

func TestVpxencVP8FrameFlagsConfigArgsDefaultDeadlineAndEndUsage(t *testing.T) {
	cfg := VpxencVP8FrameFlagsConfig{
		Width:             16,
		Height:            16,
		Frames:            2,
		FPSNum:            30,
		FPSDen:            1,
		TargetBitrateKbps: 100,
	}
	args := cfg.vpxencArgs("in.yuv", "out.ivf")

	for _, want := range []string{"--deadline=good", "--end-usage=cbr", "--frame-flags=0,0"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, strings.Join(args, " "))
		}
	}
	for _, unwanted := range []string{"--invisible-frames=", "--cq-level=0", "--threads=0"} {
		for _, arg := range args {
			if strings.HasPrefix(arg, unwanted) {
				t.Fatalf("args contained unset optional value %q in %s", arg, strings.Join(args, " "))
			}
		}
	}
}

func TestVpxTemporalSVCConfigArgs(t *testing.T) {
	cfg := VpxTemporalSVCConfig{
		Width:              64,
		Height:             32,
		Frames:             8,
		FPS:                30,
		Speed:              5,
		FrameDropThreshold: 7,
		ErrorResilient:     true,
		Threads:            2,
		LayeringMode:       4,
		LayerBitratesKbps:  []int{200, 400, 700},
	}
	args := cfg.args("input.i420", "layer")

	want := []string{
		"input.i420", "layer", "vp8", "64", "32", "1", "30",
		"5", "7", "1", "2", "4", "200", "400", "700",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestVP8VpxencThreadsArg(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantThreads  int
		wantParallel bool
	}{
		{name: "parallel", args: []string{"--end-usage=cbr", "--threads=4"}, wantThreads: 4, wantParallel: true},
		{name: "serial", args: []string{"--threads=1", "--end-usage=cbr"}, wantThreads: 1, wantParallel: false},
		{name: "absent", args: []string{"--end-usage=cbr"}, wantThreads: 0, wantParallel: false},
		{name: "nondecimal", args: []string{"--threads=fast"}, wantThreads: 0, wantParallel: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotThreads, gotParallel := VP8VpxencThreadsArg(tt.args)
			if gotThreads != tt.wantThreads || gotParallel != tt.wantParallel {
				t.Fatalf("VP8VpxencThreadsArg(%v) = (%d, %t), want (%d, %t)",
					tt.args, gotThreads, gotParallel, tt.wantThreads, tt.wantParallel)
			}
		})
	}
}

func TestValidateI420Raw(t *testing.T) {
	if got, err := i420FrameSize("VP8", 3, 3); err != nil {
		t.Fatalf("i420FrameSize returned error: %v", err)
	} else if got != 17 {
		t.Fatalf("i420FrameSize = %d, want 17", got)
	}
	if err := validateI420Raw("VP8", make([]byte, 34), 3, 3, 2); err != nil {
		t.Fatalf("validateI420Raw returned error: %v", err)
	}
	if err := validateI420Raw("VP8", make([]byte, 33), 3, 3, 2); err == nil {
		t.Fatal("validateI420Raw accepted short input")
	}
	if err := validateI420Raw("VP8", make([]byte, 17), 3, 3, 0); err == nil {
		t.Fatal("validateI420Raw accepted zero frames")
	}
}

func TestVpxencVP8EncodeI420ValidatesBeforePathLookup(t *testing.T) {
	if _, _, err := VpxencVP8OracleEncodeI420(nil, VpxencVP8Config{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8OracleEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencOracleNotBuilt) {
		t.Fatal("VpxencVP8OracleEncodeI420 looked up helper before validating input")
	}
	if _, _, err := VpxencVP8EncodeI420(nil, VpxencVP8Config{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8EncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencNotBuilt) {
		t.Fatal("VpxencVP8EncodeI420 looked up helper before validating input")
	}
	if _, _, err := VpxencVP8FrameFlagsEncodeI420(nil, VpxencVP8FrameFlagsConfig{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8FrameFlagsEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencFrameFlagsNotBuilt) {
		t.Fatal("VpxencVP8FrameFlagsEncodeI420 looked up helper before validating input")
	}
	if _, _, _, err := VpxencVP8FrameFlagsEncodeTraceI420(nil, VpxencVP8FrameFlagsConfig{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8FrameFlagsEncodeTraceI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencFrameFlagsNotBuilt) {
		t.Fatal("VpxencVP8FrameFlagsEncodeTraceI420 looked up helper before validating input")
	}
	if _, _, err := VpxencVP8OracleTraceI420(nil, VpxencVP8Config{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8OracleTraceI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencOracleNotBuilt) {
		t.Fatal("VpxencVP8OracleTraceI420 looked up helper before validating input")
	}
	if _, _, err := VpxencVP8FirstPassStatsI420(nil, VpxencVP8Config{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8FirstPassStatsI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencNotBuilt) {
		t.Fatal("VpxencVP8FirstPassStatsI420 looked up helper before validating input")
	}
	if _, _, _, err := VpxencVP8OracleEncodeTraceI420(nil, VpxencVP8Config{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxencVP8OracleEncodeTraceI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencOracleNotBuilt) {
		t.Fatal("VpxencVP8OracleEncodeTraceI420 looked up helper before validating input")
	}
	if _, _, _, err := VpxencVP8TwoPassEncodeI420(nil, VpxencVP8TwoPassConfig{
		Common: VpxencVP8Config{Width: 16, Height: 16, Frames: 1},
	}); err == nil {
		t.Fatal("VpxencVP8TwoPassEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencNotBuilt) || errors.Is(err, ErrVpxencOracleNotBuilt) {
		t.Fatal("VpxencVP8TwoPassEncodeI420 looked up helper before validating input")
	}
	if _, _, _, err := VpxencVP8TwoPassTraceI420(nil, VpxencVP8TwoPassConfig{
		Common: VpxencVP8Config{Width: 16, Height: 16, Frames: 1},
	}); err == nil {
		t.Fatal("VpxencVP8TwoPassTraceI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencNotBuilt) || errors.Is(err, ErrVpxencOracleNotBuilt) {
		t.Fatal("VpxencVP8TwoPassTraceI420 looked up helper before validating input")
	}
	if _, _, err := VpxTemporalSVCEncodeI420(nil, VpxTemporalSVCConfig{Width: 16, Height: 16, Frames: 1}); err == nil {
		t.Fatal("VpxTemporalSVCEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxTemporalSVCEncoderNotBuilt) {
		t.Fatal("VpxTemporalSVCEncodeI420 looked up helper before validating input")
	}
}
