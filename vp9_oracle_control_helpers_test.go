//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"strings"
	"testing"
)

func vp9OracleTemporalConfig(mode TemporalLayeringMode, targetKbps int) TemporalScalabilityConfig {
	cfg := TemporalScalabilityConfig{Enabled: true, Mode: mode}
	if mode == TemporalLayeringFiveLayers {
		cfg.LayerTargetBitrateKbps = [MaxTemporalLayers]int{
			targetKbps / 7,
			(2 * targetKbps) / 7,
			(4 * targetKbps) / 7,
			(5 * targetKbps) / 7,
			targetKbps,
		}
	}
	return cfg
}

func vp9OracleTemporalArgs(t *testing.T, mode TemporalLayeringMode, targetKbps int) []string {
	t.Helper()
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", mode)
	}
	cfg := vp9OracleTemporalConfig(mode, targetKbps)
	cfg, _, err := normalizeTemporalBitrates(cfg, pattern.Layers, targetKbps)
	if err != nil {
		t.Fatalf("normalizeTemporalBitrates(%d): %v", mode, err)
	}
	bitrates := make([]int, pattern.Layers)
	decimators := make([]int, pattern.Layers)
	for i := 0; i < pattern.Layers; i++ {
		bitrates[i] = cfg.LayerTargetBitrateKbps[i]
		decimators[i] = pattern.RateDecimator[i]
	}
	layerIDs := make([]int, pattern.Periodicity)
	for i := 0; i < pattern.Periodicity; i++ {
		layerIDs[i] = pattern.LayerID[i]
	}
	return []string{
		"--temporal-layers=" + strconv.Itoa(pattern.Layers),
		"--temporal-bitrates=" + vp9OracleIntCSV(bitrates),
		"--temporal-decimators=" + vp9OracleIntCSV(decimators),
		"--temporal-periodicity=" + strconv.Itoa(pattern.Periodicity),
		"--temporal-layer-ids=" + vp9OracleIntCSV(layerIDs),
	}
}

func vp9OracleIntCSV(values []int) string {
	var b strings.Builder
	for i, v := range values {
		if i != 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(v))
	}
	return b.String()
}

func assertVP9TemporalMetadataRows(t *testing.T, rows []vp9test.RateScoreboardRow, expected []expectedTemporalRow, layers int) {
	t.Helper()
	if len(rows) != len(expected) {
		t.Fatalf("temporal metadata rows = %d, want %d", len(rows), len(expected))
	}
	for i := range rows {
		if rows[i].TemporalLayerID != expected[i].layerID ||
			rows[i].TemporalLayerCount != layers ||
			rows[i].TL0PICIDX != uint8(expected[i].tl0picidx) ||
			rows[i].TemporalLayerSync != expected[i].layerSync {
			t.Fatalf("temporal metadata row %d = tid:%d layers:%d tl0:%d sync:%t, want tid:%d layers:%d tl0:%d sync:%t",
				i, rows[i].TemporalLayerID, rows[i].TemporalLayerCount,
				rows[i].TL0PICIDX, rows[i].TemporalLayerSync,
				expected[i].layerID, layers, expected[i].tl0picidx,
				expected[i].layerSync)
		}
	}
}

func vp9OracleLibvpxFrameFlags(flags uint32) uint32 {
	return vp9FrameFlagsForLibvpx(EncodeFlags(flags))
}

func vp9OracleCBROptions(width, height, targetKbps int) VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	}
}

func vp9OracleCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		fmt.Sprintf("--buf-sz=%d", bufSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", bufInitialMs),
		fmt.Sprintf("--buf-optimal-sz=%d", bufOptimalMs),
		fmt.Sprintf("--drop-frame=%d", dropFrame),
		"--exact-fps-timebase",
	}
}

// vp9OracleCyclicRefreshCBROptions is the libvpx-shaped realtime speed-8
// CBR + CYCLIC_REFRESH_AQ profile exercised by cyclic-refresh parity tests.
func vp9OracleCyclicRefreshCBROptions(width, height, targetKbps int) VP9EncoderOptions {
	opts := vp9OracleCBROptions(width, height, targetKbps)
	opts.AQMode = VP9AQCyclicRefresh
	opts.Deadline = DeadlineRealtime
	opts.CpuUsed = -8
	return opts
}

func vp9OracleCyclicRefreshCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return append(vp9OracleCBRArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	)
}

// vp9OracleCyclicRefreshCBRVpxencArgs is the vpxenc-vp9 CLI profile for
// keyframe byte-parity tests (no --exact-fps-timebase, which only the
// frameflags driver accepts).
func vp9OracleCyclicRefreshCBRVpxencArgs(targetKbps, bufSizeMs, bufInitialMs, bufOptimalMs, dropFrame int) []string {
	return []string{
		"--end-usage=cbr",
		fmt.Sprintf("--target-bitrate=%d", targetKbps),
		fmt.Sprintf("--buf-sz=%d", bufSizeMs),
		fmt.Sprintf("--buf-initial-sz=%d", bufInitialMs),
		fmt.Sprintf("--buf-optimal-sz=%d", bufOptimalMs),
		fmt.Sprintf("--drop-frame=%d", dropFrame),
		"--cpu-used=8",
		"--aq-mode=3",
	}
}

func vp9OracleDropFrameArg(opts VP9EncoderOptions) int {
	if !opts.DropFrameAllowed {
		return 0
	}
	return opts.DropFrameWaterMark
}

func mustVP9Runtime(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func newVP9OracleTransitionSources(width, height, frames int) []*image.YCbCr {
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	return sources
}

func vp9OracleFlagAt(frames, index int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	if uint(index) < uint(frames) {
		flags[index] = flag
	}
	return flags
}

func vp9OracleRepeatInterFlag(frames int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		flags[i] = flag
	}
	return flags
}

func vp9OracleRepeatAllFramesFlag(frames int, flag EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		flags[i] = flag
	}
	return flags
}

func vp9ApplyRuntimeControlTransition(t *testing.T, enc *VP9Encoder, frame int) {
	t.Helper()
	switch frame {
	case 2:
		if err := enc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300}); err != nil {
			t.Fatalf("SetRealtimeTarget bitrate at frame %d: %v", frame, err)
		}
	case 4:
		if err := enc.SetRealtimeTarget(RealtimeTarget{
			MinQuantizer: 20,
			MaxQuantizer: 20,
		}); err != nil {
			t.Fatalf("SetRealtimeTarget quantizers at frame %d: %v", frame, err)
		}
	case 5:
		if err := enc.SetRealtimeTarget(RealtimeTarget{FPS: 15}); err != nil {
			t.Fatalf("SetRealtimeTarget fps at frame %d: %v", frame, err)
		}
	case 6:
		if err := enc.SetRealtimeTarget(RealtimeTarget{
			FrameDrop: RealtimeFrameDropDisabled,
		}); err != nil {
			t.Fatalf("SetRealtimeTarget disable drop at frame %d: %v", frame, err)
		}
	case 8:
		if err := enc.SetFrameDropAllowed(true); err != nil {
			t.Fatalf("SetFrameDropAllowed at frame %d: %v", frame, err)
		}
	}
}

func vp9OracleTemporalPatternFlags(pattern temporalPattern, frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		flagIndex := i % pattern.FlagPeriodicity
		f := pattern.Flags[flagIndex]
		if i > 0 && flagIndex == 0 {
			f &^= EncodeForceKeyFrame
		}
		if i == 0 {
			f &^= vp9NoUpdateRefFlags
		}
		flags[i] = f
	}
	return flags
}

func vp9OracleRefRefreshTransitions(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	if frames > 2 {
		flags[2] = EncodeForceGoldenFrame | EncodeNoUpdateLast
	}
	if frames > 4 {
		flags[4] = EncodeForceAltRefFrame | EncodeNoUpdateGolden
	}
	if frames > 6 {
		flags[6] = EncodeForceGoldenFrame | EncodeNoUpdateLast
	}
	return flags
}

func vp9OracleAlternatingReferenceControls(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := 1; i < frames; i++ {
		if i&1 == 0 {
			flags[i] = EncodeNoUpdateGolden | EncodeNoReferenceAltRef
		} else {
			flags[i] = EncodeNoUpdateAltRef | EncodeNoReferenceGolden
		}
	}
	return flags
}
