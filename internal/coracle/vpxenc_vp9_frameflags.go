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
func VpxencVP9FrameFlagsEncodeI420(raw []byte, width int, height int, frames int, frameFlags []uint32, extraArgs ...string) (ivf []byte, diag []byte, err error) {
	frameSize, err := vpxencVP9I420FrameSize(width, height)
	if err != nil {
		return nil, nil, err
	}
	if frames <= 0 {
		return nil, nil, fmt.Errorf("coracle: VP9 frame-flags frame count %d must be positive", frames)
	}
	if len(frameFlags) > frames {
		return nil, nil, fmt.Errorf("coracle: VP9 frame-flags has %d entries for %d frames", len(frameFlags), frames)
	}
	want, err := checkedVP9I420Mul(frameSize, frames)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) != want {
		return nil, nil, fmt.Errorf("coracle: VP9 frame-flags raw I420 size = %d, want %d for %dx%d x %d frames",
			len(raw), want, width, height, frames)
	}

	bin, err := VpxencVP9FrameFlagsPath()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "govpx-vpxenc-vp9-frameflags-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
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
	args = append(args, extraArgs...)
	cmd := exec.Command(bin, args...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	ivf, err = os.ReadFile(outPath)
	if err != nil {
		return nil, diag, err
	}
	return ivf, diag, nil
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
