//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

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
