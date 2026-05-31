package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ErrVpxencVP9FrameFlagsNotBuilt is returned when the harness can't find the
// vpxenc-vp9-frameflags binary. Run
// internal/coracle/build_vpxenc_vp9_frameflags.sh to build it.
var ErrVpxencVP9FrameFlagsNotBuilt = errors.New(
	"coracle: vpxenc-vp9-frameflags binary not built (run internal/coracle/build_vpxenc_vp9_frameflags.sh)")

var (
	vpxencVP9FrameFlagsOnce sync.Once
	vpxencVP9FrameFlagsPath string
	vpxencVP9FrameFlagsErr  error
)

// VpxencVP9FrameFlagsPath returns the resolved absolute path to the VP9
// per-frame flag encoder helper, or ErrVpxencVP9FrameFlagsNotBuilt if the build
// script has not been run. Override the binary with
// GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN.
func VpxencVP9FrameFlagsPath() (string, error) {
	vpxencVP9FrameFlagsOnce.Do(resolveVpxencVP9FrameFlags)
	return vpxencVP9FrameFlagsPath, vpxencVP9FrameFlagsErr
}

func resolveVpxencVP9FrameFlags() {
	vpxencVP9FrameFlagsPath, vpxencVP9FrameFlagsErr = resolveVP9ToolPath(
		"GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN", "vpxenc-vp9-frameflags",
		ErrVpxencVP9FrameFlagsNotBuilt)
}

// VpxencVP9FrameFlagsEncodeI420 encodes raw I420 frames with the pinned VP9
// per-frame flag helper and returns an IVF stream. frameFlags are libvpx
// vpx_codec_encode flags indexed by input frame; missing entries default to
// zero. Defaults match VpxencVP9EncodeI420 unless extraArgs override them.
// extraArgs may also pass VP9 oracle knobs such as temporal ts_* config for
// trace tests.
func VpxencVP9FrameFlagsEncodeI420(raw []byte, width int, height int, frames int, frameFlags []uint32, extraArgs ...string) (ivf []byte, diag []byte, err error) {
	ivf, _, diag, err = runVpxencVP9FrameFlagsI420(raw, width, height, frames,
		frameFlags, false, extraArgs...)
	return ivf, diag, err
}

// VpxencVP9FrameFlagsTraceI420 encodes raw I420 frames with the pinned VP9
// per-frame flag helper and returns both the IVF stream and JSONL per-frame
// rate trace emitted by that helper.
func VpxencVP9FrameFlagsTraceI420(raw []byte, width int, height int, frames int, frameFlags []uint32, extraArgs ...string) (ivf []byte, trace []byte, diag []byte, err error) {
	return runVpxencVP9FrameFlagsI420(raw, width, height, frames, frameFlags,
		true, extraArgs...)
}

// VpxencVP9FrameSize describes one raw I420 input frame for VP9 helper runs
// whose coded size changes at runtime.
type VpxencVP9FrameSize struct {
	Width  int
	Height int
}

// VpxencVP9FrameFlagsTraceI420WithFrameSizes encodes a concatenated raw I420
// stream whose frame dimensions are listed in frameSizes, returning both the
// IVF stream and the helper's JSONL per-frame rate trace. Dimension changes
// are passed to the helper as VP9 runtime resize controls before the matching
// input frame. invisibleFrames is indexed by input frame and clears VP9
// show_frame in the helper output for visibility oracle tests.
func VpxencVP9FrameFlagsTraceI420WithFrameSizes(raw []byte, frameSizes []VpxencVP9FrameSize, frameFlags []uint32, invisibleFrames []bool, extraArgs ...string) (ivf []byte, trace []byte, diag []byte, err error) {
	return runVpxencVP9FrameFlagsI420WithFrameSizes(raw, frameSizes,
		frameFlags, invisibleFrames, true, extraArgs...)
}

func runVpxencVP9FrameFlagsI420WithFrameSizes(raw []byte, frameSizes []VpxencVP9FrameSize, frameFlags []uint32, invisibleFrames []bool, traceOut bool, extraArgs ...string) (ivf []byte, trace []byte, diag []byte, err error) {
	if len(frameSizes) == 0 {
		return nil, nil, nil, errors.New("coracle: VP9 variable frame-size run has no frames")
	}
	if len(frameFlags) > len(frameSizes) {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 frame-flags has %d entries for %d frames", len(frameFlags), len(frameSizes))
	}
	if len(invisibleFrames) > len(frameSizes) {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 invisible frame schedule has %d entries for %d frames", len(invisibleFrames), len(frameSizes))
	}
	want := 0
	controls := make([]string, len(frameSizes))
	controlsNeeded := false
	prevWidth := frameSizes[0].Width
	prevHeight := frameSizes[0].Height
	for i, size := range frameSizes {
		frameSize, err := vpxencVP9I420FrameSize(size.Width, size.Height)
		if err != nil {
			return nil, nil, nil, err
		}
		want, err = checkedI420Add("VP9 vpxenc", want, frameSize)
		if err != nil {
			return nil, nil, nil, err
		}
		controls[i] = "-"
		if i != 0 && (size.Width != prevWidth || size.Height != prevHeight) {
			controls[i] = "resize:" + strconv.Itoa(size.Width) + "x" +
				strconv.Itoa(size.Height)
			controlsNeeded = true
		}
		prevWidth = size.Width
		prevHeight = size.Height
	}
	if len(raw) != want {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 variable frame-size raw I420 size = %d, want %d for %d frames",
			len(raw), want, len(frameSizes))
	}
	args := append([]string(nil), extraArgs...)
	if controlsNeeded {
		args = append(args, "--control-script="+strings.Join(controls, ","))
	}
	if len(invisibleFrames) != 0 {
		args = append(args, "--invisible-frames="+joinVP9BoolSchedule(invisibleFrames))
	}
	return runVpxencVP9FrameFlagsI420Raw(raw, frameSizes[0].Width,
		frameSizes[0].Height, len(frameSizes), frameFlags, traceOut, args...)
}

func runVpxencVP9FrameFlagsI420(raw []byte, width int, height int, frames int, frameFlags []uint32, traceOut bool, extraArgs ...string) (ivf []byte, trace []byte, diag []byte, err error) {
	frameSize, err := vpxencVP9I420FrameSize(width, height)
	if err != nil {
		return nil, nil, nil, err
	}
	if frames <= 0 {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 frame-flags frame count %d must be positive", frames)
	}
	if len(frameFlags) > frames {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 frame-flags has %d entries for %d frames", len(frameFlags), frames)
	}
	want, err := checkedI420Mul("VP9 vpxenc", frameSize, frames)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(raw) != want {
		return nil, nil, nil, fmt.Errorf("coracle: VP9 frame-flags raw I420 size = %d, want %d for %dx%d x %d frames",
			len(raw), want, width, height, frames)
	}
	return runVpxencVP9FrameFlagsI420Raw(raw, width, height, frames, frameFlags,
		traceOut, extraArgs...)
}

func runVpxencVP9FrameFlagsI420Raw(raw []byte, width int, height int, frames int, frameFlags []uint32, traceOut bool, extraArgs ...string) (ivf []byte, trace []byte, diag []byte, err error) {
	bin, err := VpxencVP9FrameFlagsPath()
	if err != nil {
		return nil, nil, nil, err
	}
	dir, err := os.MkdirTemp("", "govpx-vpxenc-vp9-frameflags-*")
	if err != nil {
		return nil, nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	tracePath := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, nil, err
	}

	args := []string{
		"--infile=" + inPath,
		"--outfile=" + outPath,
		"--width=" + strconv.Itoa(width),
		"--height=" + strconv.Itoa(height),
		"--frames=" + strconv.Itoa(frames),
		"--fps-num=30",
		"--fps-den=1",
		"--deadline=rt",
		"--cpu-used=8",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--aq-mode=0",
		"--row-mt=0",
		"--tile-columns=0",
		"--tile-rows=0",
		"--end-usage=q",
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--kf-min-dist=0",
		"--kf-max-dist=128",
	}
	if len(frameFlags) != 0 {
		args = append(args, "--frame-flags="+joinVP9FrameFlags(frameFlags))
	}
	if traceOut {
		args = append(args, "--trace-out="+tracePath)
	}
	args = append(args, extraArgs...)
	cmd := exec.Command(bin, args...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, nil, diag, err
	}
	ivf, err = os.ReadFile(outPath)
	if err != nil {
		return nil, nil, diag, err
	}
	if traceOut {
		trace, err = os.ReadFile(tracePath)
		if err != nil {
			return nil, nil, diag, err
		}
	}
	return ivf, trace, diag, nil
}

func joinVP9FrameFlags(flags []uint32) string {
	var b strings.Builder
	for i, flag := range flags {
		if i != 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatUint(uint64(flag), 10))
	}
	return b.String()
}

func joinVP9BoolSchedule(values []bool) string {
	var b strings.Builder
	for i, v := range values {
		if i != 0 {
			b.WriteByte(',')
		}
		if v {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}
