//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

// frameFlagsForLibvpx mirrors the bit layout of the govpx
// [EncodeFlags] enum so the libvpx-side companion driver
// (`vpxenc-frameflags`) can be fed the same per-frame schedule.
// Both implementations target libvpx's stable
// VPX_EFLAG_FORCE_KF / VP8_EFLAG_NO_REF_* / VP8_EFLAG_NO_UPD_* /
// VPX_EFLAG_FORCE_GF / VPX_EFLAG_FORCE_ARF / VPX_EFLAG_NO_UPD_ENTROPY
// constants (defined in vp8cx.h and vpx_encoder.h).
func frameFlagsForLibvpx(f EncodeFlags) uint32 {
	const (
		libvpxForceKF      = 1 << 0  // VPX_EFLAG_FORCE_KF
		libvpxNoRefLast    = 1 << 16 // VP8_EFLAG_NO_REF_LAST
		libvpxNoRefGF      = 1 << 17 // VP8_EFLAG_NO_REF_GF
		libvpxNoUpdLast    = 1 << 18 // VP8_EFLAG_NO_UPD_LAST
		libvpxForceGF      = 1 << 19 // VP8_EFLAG_FORCE_GF
		libvpxNoUpdEntropy = 1 << 20 // VP8_EFLAG_NO_UPD_ENTROPY
		libvpxNoRefARF     = 1 << 21 // VP8_EFLAG_NO_REF_ARF
		libvpxNoUpdGF      = 1 << 22 // VP8_EFLAG_NO_UPD_GF
		libvpxNoUpdARF     = 1 << 23 // VP8_EFLAG_NO_UPD_ARF
		libvpxForceARF     = 1 << 24 // VP8_EFLAG_FORCE_ARF
	)
	var out uint32
	if f&EncodeForceKeyFrame != 0 {
		out |= libvpxForceKF
	}
	if f&EncodeNoUpdateLast != 0 {
		out |= libvpxNoUpdLast
	}
	if f&EncodeNoUpdateGolden != 0 {
		out |= libvpxNoUpdGF
	}
	if f&EncodeNoUpdateAltRef != 0 {
		out |= libvpxNoUpdARF
	}
	if f&EncodeNoReferenceLast != 0 {
		out |= libvpxNoRefLast
	}
	if f&EncodeNoReferenceGolden != 0 {
		out |= libvpxNoRefGF
	}
	if f&EncodeNoReferenceAltRef != 0 {
		out |= libvpxNoRefARF
	}
	if f&EncodeForceGoldenFrame != 0 {
		out |= libvpxForceGF
	}
	if f&EncodeForceAltRefFrame != 0 {
		out |= libvpxForceARF
	}
	if f&EncodeNoUpdateEntropy != 0 {
		out |= libvpxNoUpdEntropy
	}
	// EncodeInvisibleFrame is a govpx-specific hidden-frame marker
	// that maps to "encode then suppress show_frame"; libvpx does
	// not have a single flag bit for it. The frame-flag driver gets
	// the hidden-frame schedule through --invisible-frames instead.
	return out
}

func forceKeyTemporalTwoLayerFlags(frames int, forceFrames map[int]bool) []EncodeFlags {
	flags := temporalTwoLayerFlags(frames)
	for frame := range forceFrames {
		if frame >= 0 && frame < len(flags) {
			flags[frame] |= EncodeForceKeyFrame
		}
	}
	return flags
}

func forceKeyAPIEncodeFlags(flags []EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, len(flags))
	for i, flag := range flags {
		out[i] = flag &^ EncodeForceKeyFrame
	}
	return out
}

func encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t *testing.T, opts EncoderOptions, sources []Image, forceFrames map[int]bool, flags []EncodeFlags, setup func(*testing.T, *VP8Encoder), apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	if setup != nil {
		setup(t, enc)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		if forceFrames[i] {
			enc.ForceKeyFrame()
		}
		var frameFlags EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, frameFlags)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			t.Fatalf("flush packet dropped, want full stream")
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// repeatFlag returns a slice of length 1+n with index 0 set to 0
// (initial keyframe receives no flag) and indices 1..n set to f.
func repeatFlag(n int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, n+1)
	for i := 1; i <= n; i++ {
		out[i] = f
	}
	return out
}

// everyNFlag returns a per-frame schedule of length frames, skipping the
// initial keyframe and setting f on every n-th inter frame.
func everyNFlag(frames int, n int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, frames)
	if n <= 0 {
		return out
	}
	for i := n; i < frames; i += n {
		out[i] = f
	}
	return out
}

func encodeFramesWithGovpxFrameFlags(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			t.Fatalf("flush packet dropped, want full stream")
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

func encodeFramesWithFrameFlagsDriver(t *testing.T, driver, _ string, opts EncoderOptions, targetKbps int, sources []Image, flags []EncodeFlags, extraArgs []string) [][]byte {
	t.Helper()
	libvpxFlags := make([]uint32, len(sources))
	invisibleFrames := make([]bool, len(sources))
	haveInvisible := false
	for i := range libvpxFlags {
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		libvpxFlags[i] = frameFlagsForLibvpx(f)
		if f&EncodeInvisibleFrame != 0 {
			invisibleFrames[i] = true
			haveInvisible = true
		}
	}
	if !haveInvisible {
		invisibleFrames = nil
	}

	cfg := vp8test.VpxencVP8FrameFlagsConfig{
		BinaryPath:        driver,
		Width:             opts.Width,
		Height:            opts.Height,
		Frames:            len(sources),
		FPSNum:            opts.FPS,
		FPSDen:            1,
		TargetBitrateKbps: targetKbps,
		MinQ:              opts.MinQuantizer,
		MaxQ:              opts.MaxQuantizer,
		KeyFrameMinDist:   999,
		KeyFrameMaxDist:   999,
		Deadline:          libvpxOracleDeadline(opts.Deadline),
		CPUUsed:           opts.CpuUsed,
		EndUsage:          libvpxFrameFlagsEndUsage(opts.RateControlMode),
		AutoAltRef:        false,
		TokenPartitions:   opts.TokenPartitions,
		CQLevel:           opts.CQLevel,
		Threads:           opts.Threads,
		FrameFlags:        libvpxFlags,
		InvisibleFrames:   invisibleFrames,
		ExtraArgs:         extraArgs,
	}
	frames, diag, err := vp8test.VpxencVP8FrameFlagsPayloadsI420(
		encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc-frameflags failed: %v\n%s", err, diag)
	}
	return frames
}

func libvpxFrameFlagsEndUsage(mode RateControlMode) string {
	switch mode {
	case RateControlVBR:
		return "vbr"
	case RateControlCQ:
		return "cq"
	case RateControlQ:
		return "q"
	default:
		return "cbr"
	}
}
