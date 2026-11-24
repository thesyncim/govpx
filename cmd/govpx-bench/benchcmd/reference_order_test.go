package benchcmd

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReferenceFirstCLI(t *testing.T) {
	for _, first := range []bool{false, true} {
		cfg := benchConfig{}
		opts := defaultBenchCLIOptions()
		fs := flag.NewFlagSet("bench", flag.ContinueOnError)
		registerBenchFlags(fs, &cfg, &opts)
		var args []string
		if first {
			args = []string{"-reference-first"}
		}
		if err := fs.Parse(args); err != nil || cfg.ReferenceFirst != first {
			t.Fatalf("first=%v parse=%v configured=%v", first, err, cfg.ReferenceFirst)
		}
	}
}

// Two distinct failures prove which encoder starts first without relying on
// timings, sleeps, or a production test hook.
func TestReferenceFirstExecutionOrder(t *testing.T) {
	for _, codec := range []string{codecVP8, codecVP9} {
		for _, first := range []bool{false, true} {
			t.Run(codec+"/"+map[bool]string{false: "govpx-first", true: "reference-first"}[first], func(t *testing.T) {
				dir := t.TempDir()
				marker := filepath.Join(dir, "reference-ran")
				t.Setenv("GOVPX_REFERENCE_ORDER_MARKER", marker)
				bin := filepath.Join(dir, "reference")
				if err := os.WriteFile(bin, []byte("#!/bin/sh\n: > \"$GOVPX_REFERENCE_ORDER_MARKER\"\nprintf reference-order-sentinel >&2\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
				cfg := benchConfig{Width: 16, Height: 16, Frames: 1, FPS: 30, BitrateKbps: 100, Mode: "realtime", Codec: codec, SkipQuality: true, ReferenceFirst: first, CPUProfile: filepath.Join(dir, "missing", "profile")}
				var err error
				if codec == codecVP8 {
					cfg.LibvpxVpxenc = bin
					_, err = runBenchmark(cfg)
				} else {
					cfg.LibvpxVpxencVP9 = bin
					_, err = runVP9Benchmark(cfg)
				}
				if err == nil {
					t.Fatal("expected the first encoder's failure")
				}
				_, markerErr := os.Stat(marker)
				if (markerErr == nil) != first {
					t.Fatalf("reference ran=%v, want %v (error=%v)", markerErr == nil, first, err)
				}
				want := "create cpu profile"
				if first {
					want = "reference-order-sentinel"
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error=%v, want first failure %q", err, want)
				}
			})
		}
	}
}

func TestReferenceFirstPreservesOutputs(t *testing.T) {
	for _, codec := range []string{codecVP8, codecVP9} {
		t.Run(codec, func(t *testing.T) {
			bin := fakeVpxencPath(t)
			var reports [2]benchReport
			for i := range reports {
				cfg := benchConfig{Width: 16, Height: 16, Frames: 2, FPS: 30, BitrateKbps: 100, Mode: "realtime", Codec: codec, SkipQuality: true, CpuUsed: -4, ReferenceFirst: i == 1}
				var err error
				if codec == codecVP8 {
					cfg.LibvpxVpxenc = bin
					reports[i], err = runBenchmark(cfg)
				} else {
					cfg.CpuUsed = 8
					cfg.LibvpxVpxencVP9 = bin
					reports[i], err = runVP9Benchmark(cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				if reports[i].Reference == nil || reports[i].ReferenceFirst != cfg.ReferenceFirst || reports[i].Reference.TimingSource != "vpxenc-stats" {
					t.Fatalf("reference/order/timing missing: %+v", reports[i])
				}
			}
			a, b := reports[0], reports[1]
			if a.OutputBytes != b.OutputBytes || a.EncodedFrames != b.EncodedFrames || a.DroppedFrames != b.DroppedFrames || !reflect.DeepEqual(a.QuantizerHist, b.QuantizerHist) || a.Reference.OutputBytes != b.Reference.OutputBytes || a.Reference.EncodedFrames != b.Reference.EncodedFrames || a.Reference.DroppedFrames != b.Reference.DroppedFrames || !reflect.DeepEqual(a.Reference.ParityFlags, b.Reference.ParityFlags) {
				t.Fatal("changing comparison order changed encoded outputs or controls")
			}
			data, err := json.Marshal(a)
			if err != nil || strings.Contains(string(data), "reference_first") {
				t.Fatalf("default JSON contract changed: %s, %v", data, err)
			}
		})
	}
}
