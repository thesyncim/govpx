//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type frameFlagsQuantizerLogEntry struct {
	Frame         int
	HaveInput     bool
	Emitted       int
	LastQuantizer int
}

type govpxQuantizerCallTrace struct {
	Frame              int
	Buffered           bool
	Dropped            bool
	ResultPublic       int
	ResultInternal     int
	PacketInternal     int
	LastPublic         int
	LastInternal       int
	LastQuantizerValid bool
}

func TestOracleEncoderQuantizerMetadataParityAcrossDrops(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run quantizer metadata parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		frames     = 30
		width      = 64
		height     = 64
		targetKbps = 50
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -3,
		Tuning:              TunePSNR,
		BufferSizeMs:        200,
		BufferInitialSizeMs: 100,
		BufferOptimalSizeMs: 150,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	}
	extraArgs := []string{
		"--target-bitrate=50",
		"--buf-sz=200",
		"--buf-initial-sz=100",
		"--buf-optimal-sz=150",
		"--drop-frame=60",
	}

	govpxFrames, trace := encodeFramesWithGovpxQuantizerTrace(t, opts, sources, nil, nil)
	libvpxFrames, logEntries := encodeFramesWithFrameFlagsDriverQuantizerLog(t, driver, "quantizer-metadata-drops", opts, targetKbps, sources, nil, extraArgs)
	assertSegmentByteParity(t, "quantizer-metadata-drops", govpxFrames, libvpxFrames, 0)
	assertQuantizerMetadataParity(t, trace, logEntries)
}

func TestOracleEncoderQuantizerMetadataParityDropControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run quantizer metadata parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		frames     = 30
		width      = 64
		height     = 64
		targetKbps = 50
	)
	baseOpts := func() EncoderOptions {
		return EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 fps,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   targetKbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			KeyFrameInterval:    999,
			Deadline:            DeadlineRealtime,
			CpuUsed:             -3,
			Tuning:              TunePSNR,
			BufferSizeMs:        200,
			BufferInitialSizeMs: 100,
			BufferOptimalSizeMs: 150,
			DropFrameAllowed:    true,
			DropFrameWaterMark:  60,
		}
	}
	baseArgs := func() []string {
		return []string{
			"--target-bitrate=50",
			"--buf-sz=200",
			"--buf-initial-sz=100",
			"--buf-optimal-sz=150",
			"--drop-frame=60",
		}
	}

	cases := []struct {
		name      string
		opts      EncoderOptions
		source    func(int) Image
		apply     map[int]func(*testing.T, *VP8Encoder)
		extraArgs []string
	}{
		{
			name: "rtc-external",
			opts: func() EncoderOptions {
				opts := baseOpts()
				opts.RTCExternalRateControl = true
				return opts
			}(),
			extraArgs: append(baseArgs(), "--rtc-external=1"),
		},
		{
			name: "error-resilient-token8",
			opts: func() EncoderOptions {
				opts := baseOpts()
				opts.ErrorResilient = true
				opts.ErrorResilientPartitions = true
				opts.TokenPartitions = 3
				return opts
			}(),
			extraArgs: append(baseArgs(), "--error-resilient=3", "--token-parts=3"),
		},
		{
			name: "active-checker",
			opts: baseOpts(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: activeMapApply("checker"),
			},
			extraArgs: append(baseArgs(), "--active-map=checker"),
		},
		{
			name: "roi-border1",
			opts: baseOpts(),
			source: func(frame int) Image {
				return encoderValidationSegmentedFrame(width, height, frame)
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: roiMapApply("border1"),
			},
			extraArgs: append(baseArgs(), "--roi-map=border1"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source
			if source == nil {
				source = func(frame int) Image {
					return encoderValidationPanningFrame(width, height, frame)
				}
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = source(i)
			}
			govpxFrames, trace := encodeFramesWithGovpxQuantizerTrace(t, tc.opts, sources, nil, tc.apply)
			libvpxFrames, logEntries := encodeFramesWithFrameFlagsDriverQuantizerLog(t, driver, "quantizer-metadata-drop-cross-"+tc.name, tc.opts, targetKbps, sources, nil, tc.extraArgs)
			assertSegmentByteParity(t, "quantizer-metadata-drop-cross-"+tc.name, govpxFrames, libvpxFrames, 0)
			assertQuantizerMetadataParity(t, trace, logEntries)
		})
	}
}

func encodeFramesWithFrameFlagsDriverQuantizerLog(t *testing.T, driver, name string, opts EncoderOptions, targetKbps int, sources []Image, flags []EncodeFlags, extraArgs []string) ([][]byte, []frameFlagsQuantizerLogEntry) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), name+".quantizers.log")
	args := append([]string{}, extraArgs...)
	args = append(args, "--quantizer-log="+logPath)
	frames := encodeFramesWithFrameFlagsDriver(t, driver, name, opts, targetKbps, sources, flags, args)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read quantizer log %s: %v", logPath, err)
	}
	return frames, parseFrameFlagsQuantizerLog(t, string(data))
}

func parseFrameFlagsQuantizerLog(t *testing.T, data string) []frameFlagsQuantizerLogEntry {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	out := make([]frameFlagsQuantizerLogEntry, 0, len(lines))
	for _, line := range lines {
		var entry frameFlagsQuantizerLogEntry
		seen := map[string]bool{}
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				t.Fatalf("malformed quantizer log field %q in line %q", field, line)
			}
			seen[key] = true
			switch key {
			case "frame":
				entry.Frame = parseQuantizerLogInt(t, value, line)
			case "have_input":
				entry.HaveInput = parseQuantizerLogBool(t, value, line)
			case "emitted":
				entry.Emitted = parseQuantizerLogInt(t, value, line)
			case "last_quantizer":
				entry.LastQuantizer = parseQuantizerLogInt(t, value, line)
			default:
				t.Fatalf("unknown quantizer log field %q in line %q", key, line)
			}
		}
		for _, key := range []string{"frame", "have_input", "emitted", "last_quantizer"} {
			if !seen[key] {
				t.Fatalf("quantizer log line missing %s: %q", key, line)
			}
		}
		out = append(out, entry)
	}
	return out
}

func parseQuantizerLogInt(t *testing.T, value, line string) int {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("parse quantizer log int %q in line %q: %v", value, line, err)
	}
	return n
}

func parseQuantizerLogBool(t *testing.T, value, line string) bool {
	t.Helper()
	switch value {
	case "0":
		return false
	case "1":
		return true
	default:
		t.Fatalf("parse quantizer log bool %q in line %q: want 0 or 1", value, line)
		return false
	}
}

func encodeFramesWithGovpxQuantizerTrace(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder)) ([][]byte, []govpxQuantizerCallTrace) {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	trace := make([]govpxQuantizerCallTrace, 0, len(sources))
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
		entry := govpxQuantizerCallTrace{Frame: i}
		if errors.Is(err, ErrFrameNotReady) {
			entry.Buffered = true
			entry.LastPublic, entry.LastInternal, entry.LastQuantizerValid = enc.LastQuantizer()
			trace = append(trace, entry)
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		entry.Dropped = result.Dropped
		entry.ResultPublic = result.Quantizer
		entry.ResultInternal = result.InternalQuantizer
		if !result.Dropped {
			entry.PacketInternal = packetBaseQIndex(t, result.Data)
			if result.InternalQuantizer != entry.PacketInternal {
				t.Fatalf("frame %d EncodeResult internal quantizer = %d, want packet qindex %d", i, result.InternalQuantizer, entry.PacketInternal)
			}
			if result.Quantizer != libvpxQIndexToPublicQuantizer(entry.PacketInternal) {
				t.Fatalf("frame %d EncodeResult public quantizer = %d, want %d for qindex %d", i, result.Quantizer, libvpxQIndexToPublicQuantizer(entry.PacketInternal), entry.PacketInternal)
			}
			out = append(out, append([]byte(nil), result.Data...))
		}
		entry.LastPublic, entry.LastInternal, entry.LastQuantizerValid = enc.LastQuantizer()
		trace = append(trace, entry)
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if !result.Dropped {
			t.Fatalf("unexpected flush packet with no lookahead: %d bytes", len(result.Data))
		}
	}
	return out, trace
}

func assertQuantizerMetadataParity(t *testing.T, trace []govpxQuantizerCallTrace, logEntries []frameFlagsQuantizerLogEntry) {
	t.Helper()
	if len(logEntries) != len(trace)+1 {
		t.Fatalf("quantizer log entries = %d, want %d input calls plus EOS", len(logEntries), len(trace)+1)
	}
	lastInternal := 0
	haveLast := false
	sawDrop := false
	for i, got := range trace {
		want := logEntries[i]
		if want.Frame != got.Frame || !want.HaveInput {
			t.Fatalf("log entry %d = frame:%d have_input:%t, want frame:%d have_input:true", i, want.Frame, want.HaveInput, got.Frame)
		}
		switch {
		case got.Buffered:
			if want.Emitted != 0 {
				t.Fatalf("frame %d libvpx emitted %d packets, want 0 for buffered input", got.Frame, want.Emitted)
			}
		case got.Dropped:
			sawDrop = true
			if want.Emitted != 0 {
				t.Fatalf("frame %d libvpx emitted %d packets, want 0 for dropped input", got.Frame, want.Emitted)
			}
		default:
			if want.Emitted != 1 {
				t.Fatalf("frame %d libvpx emitted %d packets, want 1", got.Frame, want.Emitted)
			}
			if want.LastQuantizer != got.ResultInternal {
				t.Fatalf("frame %d libvpx last quantizer = %d, want EncodeResult internal quantizer %d", got.Frame, want.LastQuantizer, got.ResultInternal)
			}
			lastInternal = got.ResultInternal
			haveLast = true
		}
		if haveLast {
			if !got.LastQuantizerValid {
				t.Fatalf("frame %d LastQuantizer returned !ok, want prior committed qindex %d", got.Frame, lastInternal)
			}
			if got.LastInternal != lastInternal || got.LastPublic != libvpxQIndexToPublicQuantizer(lastInternal) {
				t.Fatalf("frame %d LastQuantizer = public:%d internal:%d, want public:%d internal:%d", got.Frame, got.LastPublic, got.LastInternal, libvpxQIndexToPublicQuantizer(lastInternal), lastInternal)
			}
			if want.LastQuantizer != lastInternal {
				t.Fatalf("frame %d libvpx last quantizer = %d, want prior committed qindex %d", got.Frame, want.LastQuantizer, lastInternal)
			}
		} else if got.LastQuantizerValid {
			t.Fatalf("frame %d LastQuantizer returned ok before any committed packet", got.Frame)
		}
	}
	if !sawDrop {
		t.Fatalf("test case did not exercise a dropped input")
	}
	eos := logEntries[len(trace)]
	if eos.Frame != len(trace) || eos.HaveInput {
		t.Fatalf("EOS log entry = frame:%d have_input:%t, want frame:%d have_input:false", eos.Frame, eos.HaveInput, len(trace))
	}
	if eos.Emitted != 0 {
		t.Fatalf("EOS emitted %d packets, want 0 for no-lookahead drop stream", eos.Emitted)
	}
	if haveLast && eos.LastQuantizer != lastInternal {
		t.Fatalf("EOS last quantizer = %d, want final committed qindex %d", eos.LastQuantizer, lastInternal)
	}
}
