package benchcmd

import (
	"flag"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"io"
	"strings"
	"testing"
)

func TestBenchCLIOptionsDefaultAutoLibvpx(t *testing.T) {
	t.Setenv("GOVPX_VPXENC", "/tmp/should-not-be-used")
	t.Setenv("GOVPX_ORACLE", "/tmp/should-not-be-used")
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := benchConfig{}
	opts := defaultBenchCLIOptions()
	registerBenchFlags(fs, &cfg, &opts)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.autoCompare || opts.buildLibvpx || cfg.LibvpxVpxenc != "" || cfg.LibvpxOracle != "" {
		t.Fatalf("defaults = opts:%+v cfg:%+v, want auto libvpx enabled without pre-resolved paths", opts, cfg)
	}
}

func TestBenchCLIThreadsDefaultIsCodecAware(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		wantThreads int
	}{
		{name: "vp8-default", wantThreads: 1},
		{name: "vp9-realtime-default", args: []string{"-codec=vp9"}, wantThreads: 0},
		{name: "vp9-good-default", args: []string{"-codec=vp9", "-mode=good"}, wantThreads: 1},
		{name: "vp9-explicit-one", args: []string{"-codec=vp9", "-threads=1"}, wantThreads: 1},
		{name: "vp9-explicit-four", args: []string{"-codec=vp9", "-threads=4"}, wantThreads: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("bench", flag.ContinueOnError)
			cfg := benchConfig{}
			opts := defaultBenchCLIOptions()
			registerBenchFlags(fs, &cfg, &opts)
			if err := fs.Parse(tc.args); err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(tc.args) == 0 || !strings.Contains(strings.Join(tc.args, " "), "-threads=") {
				if cfg.Threads != benchThreadsDefault {
					t.Fatalf("raw Threads = %d, want default sentinel", cfg.Threads)
				}
			}
			parity := parityFor(cfg)
			if parity.Threads != tc.wantThreads {
				t.Fatalf("parity Threads = %d, want %d", parity.Threads, tc.wantThreads)
			}
			if benchCodec(cfg) == codecVP9 {
				vp9Opts := vp9BenchmarkEncoderOptions(cfg, govpx.DeadlineRealtime)
				if vp9Opts.Threads != tc.wantThreads {
					t.Fatalf("VP9 encoder Threads = %d, want %d", vp9Opts.Threads, tc.wantThreads)
				}
			}
		})
	}

	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := benchConfig{}
	opts := defaultBenchCLIOptions()
	registerBenchFlags(fs, &cfg, &opts)
	if err := fs.Parse([]string{"-threads=-1"}); err == nil {
		t.Fatal("Parse accepted negative -threads")
	}
}

func TestResolveLibvpxDefaultsDoesNotSelectOracleForEncode(t *testing.T) {
	cfg := benchConfig{}
	resolveLibvpxDefaults(&cfg, false)
	if cfg.LibvpxOracle != "" {
		t.Fatalf("LibvpxOracle = %q, want empty for encode mode", cfg.LibvpxOracle)
	}
}

func TestResolveLibvpxDefaultsDoesNotSelectVpxencForDecode(t *testing.T) {
	cfg := benchConfig{Decode: true}
	resolveLibvpxDefaults(&cfg, false)
	if cfg.LibvpxVpxenc != "" {
		t.Fatalf("LibvpxVpxenc = %q, want empty for decode mode", cfg.LibvpxVpxenc)
	}
}

func TestLibvpxParityFlagsCarryEncoderConfig(t *testing.T) {
	cfg := benchConfig{Width: 64, Height: 64, Frames: 30, FPS: 30, BitrateKbps: 1200, Mode: "realtime", CpuUsed: -4}
	parity := parityFor(cfg)
	flags := libvpxParityFlags(cfg, parity, "--rt")

	required := []string{
		"--passes=1",
		"--lag-in-frames=0",
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", cfg.BitrateKbps),
		fmt.Sprintf("--min-q=%d", parity.MinQuantizer),
		fmt.Sprintf("--max-q=%d", parity.MaxQuantizer),
		fmt.Sprintf("--kf-min-dist=%d", parity.KeyFrameInterval),
		fmt.Sprintf("--kf-max-dist=%d", parity.KeyFrameInterval),
		fmt.Sprintf("--buf-sz=%d", parity.BufferSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", parity.BufferInitialSizeMs),
		fmt.Sprintf("--buf-optimal-sz=%d", parity.BufferOptimalSizeMs),
		fmt.Sprintf("--undershoot-pct=%d", parity.UndershootPct),
		fmt.Sprintf("--overshoot-pct=%d", parity.OvershootPct),
		fmt.Sprintf("--drop-frame=%d", parity.DropFrameWaterMark),
		fmt.Sprintf("--max-intra-rate=%d", parity.MaxIntraBitratePct),
		fmt.Sprintf("--noise-sensitivity=%d", parity.NoiseSensitivity),
		fmt.Sprintf("--static-thresh=%d", parity.StaticThreshold),
		fmt.Sprintf("--threads=%d", parity.Threads),
		fmt.Sprintf("--timebase=1/%d", cfg.FPS),
		"--rt",
		fmt.Sprintf("--cpu-used=%d", parity.CpuUsed),
	}
	have := make(map[string]bool, len(flags))
	for _, f := range flags {
		have[f] = true
	}
	for _, want := range required {
		if !have[want] {
			t.Fatalf("parity flags missing %q\nhave: %v", want, flags)
		}
	}
}

func TestParityForMatchesEncoderDefaults(t *testing.T) {
	// Sanity check that realtime parity defaults mirror the public WebRTC
	// example rather than the simpler validation-only CBR preset. This direct
	// config pins explicit Threads=1 so the assertion is independent of the
	// codec-aware CLI default.
	got := parityFor(benchConfig{FPS: 24, Threads: 1, CpuUsed: 8})
	if got.KeyFrameInterval != 3000 {
		t.Fatalf("KeyFrameInterval = %d, want 3000", got.KeyFrameInterval)
	}
	if got.MinQuantizer != 2 || got.MaxQuantizer != 56 {
		t.Fatalf("quantizer range = [%d,%d], want [2,56]", got.MinQuantizer, got.MaxQuantizer)
	}
	if got.BufferSizeMs != 1000 || got.BufferInitialSizeMs != 500 || got.BufferOptimalSizeMs != 600 {
		t.Fatalf("buffer model = sz:%d init:%d opt:%d, want 1000/500/600", got.BufferSizeMs, got.BufferInitialSizeMs, got.BufferOptimalSizeMs)
	}
	if !got.DropFrameAllowed || got.DropFrameWaterMark != 30 {
		t.Fatalf("drop frame = enabled:%t watermark:%d, want enabled/30", got.DropFrameAllowed, got.DropFrameWaterMark)
	}
	if got.MaxIntraBitratePct != 720 || got.NoiseSensitivity != 4 || got.StaticThreshold != 1 {
		t.Fatalf("webrtc knobs = max-intra:%d noise:%d static:%d, want 720/4/1",
			got.MaxIntraBitratePct, got.NoiseSensitivity, got.StaticThreshold)
	}
	if got.CpuUsed != 8 || got.Threads != 1 {
		t.Fatalf("cpu/threads = %d/%d, want 8/1", got.CpuUsed, got.Threads)
	}
	good := parityFor(benchConfig{Mode: "good", FPS: 24, Threads: 1, CpuUsed: 8})
	if good.KeyFrameInterval != 24 ||
		good.MinQuantizer != 4 ||
		good.BufferSizeMs != 600 ||
		good.DropFrameAllowed ||
		good.MaxIntraBitratePct != 0 ||
		good.NoiseSensitivity != 0 ||
		good.StaticThreshold != 0 {
		t.Fatalf("good-mode parity = %+v, want validation CBR defaults", good)
	}

	// Explicit -threads=0 propagates as 0 to libvpx and govpx, where VP9
	// realtime treats it as its native auto-thread sentinel.
	if got := parityFor(benchConfig{FPS: 24, Threads: 0, CpuUsed: 8}); got.Threads != 0 {
		t.Fatalf("Threads=0 propagates as %d, want 0", got.Threads)
	}
	if got := parityFor(benchConfig{FPS: 24, Threads: 4, CpuUsed: 8}); got.Threads != 4 {
		t.Fatalf("Threads=4 propagates as %d, want 4", got.Threads)
	}

	// Zero FPS falls back to a sane default rather than passing 0 to libvpx.
	if parityFor(benchConfig{FPS: 0}).KeyFrameInterval == 0 {
		t.Fatalf("KeyFrameInterval falls back when FPS is 0")
	}
}

func TestBenchmarkEncoderOptionsMatchLibvpxParityConfig(t *testing.T) {
	cfg := benchConfig{
		Width:       80,
		Height:      64,
		Frames:      4,
		FPS:         24,
		BitrateKbps: 900,
		Threads:     3,
		CpuUsed:     -8,
	}
	parity := parityFor(cfg)
	opts := benchmarkEncoderOptions(cfg, govpx.DeadlineRealtime)
	if opts.MinQuantizer != parity.MinQuantizer || opts.MaxQuantizer != parity.MaxQuantizer {
		t.Fatalf("quantizer range = [%d,%d], want parity [%d,%d]",
			opts.MinQuantizer, opts.MaxQuantizer, parity.MinQuantizer, parity.MaxQuantizer)
	}
	if opts.KeyFrameInterval != parity.KeyFrameInterval {
		t.Fatalf("KeyFrameInterval = %d, want %d", opts.KeyFrameInterval, parity.KeyFrameInterval)
	}
	if opts.BufferSizeMs != parity.BufferSizeMs ||
		opts.BufferInitialSizeMs != parity.BufferInitialSizeMs ||
		opts.BufferOptimalSizeMs != parity.BufferOptimalSizeMs {
		t.Fatalf("buffer model = sz:%d init:%d opt:%d, want %d/%d/%d",
			opts.BufferSizeMs, opts.BufferInitialSizeMs, opts.BufferOptimalSizeMs,
			parity.BufferSizeMs, parity.BufferInitialSizeMs, parity.BufferOptimalSizeMs)
	}
	if opts.UndershootPct != parity.UndershootPct || opts.OvershootPct != parity.OvershootPct {
		t.Fatalf("rate-control percentages = under:%d over:%d, want parity %d/%d",
			opts.UndershootPct, opts.OvershootPct, parity.UndershootPct, parity.OvershootPct)
	}
	if opts.MaxIntraBitratePct != parity.MaxIntraBitratePct ||
		opts.DropFrameAllowed != parity.DropFrameAllowed ||
		opts.DropFrameWaterMark != parity.DropFrameWaterMark ||
		opts.NoiseSensitivity != parity.NoiseSensitivity ||
		opts.StaticThreshold != parity.StaticThreshold {
		t.Fatalf("realtime knobs = max-intra:%d drop:%t/%d noise:%d static:%d, want parity %+v",
			opts.MaxIntraBitratePct, opts.DropFrameAllowed, opts.DropFrameWaterMark,
			opts.NoiseSensitivity, opts.StaticThreshold, parity)
	}
	if opts.Threads != parity.Threads || opts.CpuUsed != parity.CpuUsed {
		t.Fatalf("cpu/threads = %d/%d, want parity %d/%d",
			opts.CpuUsed, opts.Threads, parity.CpuUsed, parity.Threads)
	}
}
