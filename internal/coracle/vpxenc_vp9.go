package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// ErrVpxencVP9NotBuilt is returned when the harness can't find the
// vpxenc-vp9 binary. Run internal/coracle/build_vpxdec_vp9.sh to build
// the paired VP9 vpxdec/vpxenc tools.
var ErrVpxencVP9NotBuilt = errors.New(
	"coracle: vpxenc-vp9 binary not built (run internal/coracle/build_vpxdec_vp9.sh)")

var (
	vpxencVP9Once sync.Once
	vpxencVP9Path string
	vpxencVP9Err  error
)

// VpxencVP9Path returns the resolved absolute path to the VP9-enabled
// vpxenc binary, or ErrVpxencVP9NotBuilt if the build script has not
// been run. Override the binary with GOVPX_VPXENC_VP9_BIN.
func VpxencVP9Path() (string, error) {
	vpxencVP9Once.Do(resolveVpxencVP9)
	return vpxencVP9Path, vpxencVP9Err
}

func resolveVpxencVP9() {
	vpxencVP9Path, vpxencVP9Err = resolveVP9ToolPath(
		"GOVPX_VPXENC_VP9_BIN", "vpxenc-vp9", ErrVpxencVP9NotBuilt)
}

// VpxencVP9EncodeI420 encodes raw I420 frames with the pinned VP9 vpxenc
// tool and returns an IVF stream. Defaults keep the corpus deterministic,
// single-layer, and Profile 0; extra args are appended before the input path
// so callers can override individual vpxenc knobs.
func VpxencVP9EncodeI420(raw []byte, width int, height int, frames int, extraArgs ...string) (ivf []byte, diag []byte, err error) {
	frameSize, err := vpxencVP9I420FrameSize(width, height)
	if err != nil {
		return nil, nil, err
	}
	if frames <= 0 {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc frame count %d must be positive", frames)
	}
	want, err := checkedI420Mul("VP9 vpxenc", frameSize, frames)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) != want {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc raw I420 size = %d, want %d for %dx%d x %d frames",
			len(raw), want, width, height, frames)
	}

	bin, err := VpxencVP9Path()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "govpx-vpxenc-vp9-*")
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
		"--codec=vp9",
		"--ivf",
		"--quiet",
		"--rt",
		"--cpu-used=8",
		"--profile=0",
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
		"--i420",
		"--width=" + strconv.Itoa(width),
		"--height=" + strconv.Itoa(height),
		"--fps=30/1",
		"--limit=" + strconv.Itoa(frames),
		"--output=" + outPath,
	}
	args = append(args, extraArgs...)
	args = append(args, inPath)
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

// VpxencVP9FirstPassStatsI420 runs the pinned VP9 vpxenc tool in first-pass
// mode and returns the raw FIRSTPASS_STATS file. Defaults target the VOD
// good-quality path; extra args are appended before the input path so callers
// can override individual vpxenc knobs.
func VpxencVP9FirstPassStatsI420(raw []byte, width int, height int, frames int, extraArgs ...string) (stats []byte, diag []byte, err error) {
	frameSize, err := vpxencVP9I420FrameSize(width, height)
	if err != nil {
		return nil, nil, err
	}
	if frames <= 0 {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc first-pass frame count %d must be positive", frames)
	}
	want, err := checkedI420Mul("VP9 vpxenc", frameSize, frames)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) != want {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc first-pass raw I420 size = %d, want %d for %dx%d x %d frames",
			len(raw), want, width, height, frames)
	}

	bin, err := VpxencVP9Path()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "govpx-vpxenc-vp9-firstpass-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	fpfPath := filepath.Join(dir, "firstpass.fpf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}

	args := []string{
		"--codec=vp9",
		"--ivf",
		"--quiet",
		"--good",
		"--cpu-used=4",
		"--passes=2",
		"--pass=1",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--i420",
		"--width=" + strconv.Itoa(width),
		"--height=" + strconv.Itoa(height),
		"--fps=30/1",
		"--limit=" + strconv.Itoa(frames),
		"--output=" + outPath,
	}
	args = append(args, extraArgs...)
	args = append(args, inPath)
	cmd := exec.Command(bin, args...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	stats, err = os.ReadFile(fpfPath)
	if err != nil {
		return nil, diag, err
	}
	return stats, diag, nil
}

// VpxencVP9TwoPassEncodeI420 runs the pinned VP9 vpxenc tool through pass 1
// and pass 2 with a shared first-pass stats file, returning the pass-2 IVF
// stream. Defaults match VpxencVP9FirstPassStatsI420; extra args are appended
// before the input path for both passes so callers can set the same public
// controls on each run.
func VpxencVP9TwoPassEncodeI420(raw []byte, width int, height int, frames int, extraArgs ...string) (ivf []byte, diag []byte, err error) {
	frameSize, err := vpxencVP9I420FrameSize(width, height)
	if err != nil {
		return nil, nil, err
	}
	if frames <= 0 {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc two-pass frame count %d must be positive", frames)
	}
	want, err := checkedI420Mul("VP9 vpxenc", frameSize, frames)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) != want {
		return nil, nil, fmt.Errorf("coracle: VP9 vpxenc two-pass raw I420 size = %d, want %d for %dx%d x %d frames",
			len(raw), want, width, height, frames)
	}

	bin, err := VpxencVP9Path()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "govpx-vpxenc-vp9-twopass-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "input.i420")
	fpfPath := filepath.Join(dir, "firstpass.fpf")
	pass1Path := filepath.Join(dir, "pass1.ivf")
	pass2Path := filepath.Join(dir, "pass2.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return nil, nil, err
	}

	baseArgs := []string{
		"--codec=vp9",
		"--ivf",
		"--quiet",
		"--good",
		"--cpu-used=4",
		"--passes=2",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=700",
		"--min-q=4",
		"--max-q=56",
		"--i420",
		"--width=" + strconv.Itoa(width),
		"--height=" + strconv.Itoa(height),
		"--fps=30/1",
		"--limit=" + strconv.Itoa(frames),
	}
	pass1Args := append([]string{}, baseArgs...)
	pass1Args = append(pass1Args, "--pass=1", "--output="+pass1Path)
	pass1Args = append(pass1Args, extraArgs...)
	pass1Args = append(pass1Args, inPath)
	pass1Diag, err := exec.Command(bin, pass1Args...).CombinedOutput()
	diag = append(diag, pass1Diag...)
	if err != nil {
		return nil, diag, err
	}

	pass2Args := append([]string{}, baseArgs...)
	pass2Args = append(pass2Args, "--pass=2", "--output="+pass2Path)
	pass2Args = append(pass2Args, extraArgs...)
	pass2Args = append(pass2Args, inPath)
	pass2Diag, err := exec.Command(bin, pass2Args...).CombinedOutput()
	diag = append(diag, pass2Diag...)
	if err != nil {
		return nil, diag, err
	}
	ivf, err = os.ReadFile(pass2Path)
	if err != nil {
		return nil, diag, err
	}
	return ivf, diag, nil
}

func vpxencVP9I420FrameSize(width int, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("coracle: invalid VP9 vpxenc dimensions %dx%d", width, height)
	}
	size, ok := buffers.I420FrameSize(width, height)
	if !ok {
		return 0, errors.New("coracle: VP9 vpxenc I420 size overflows int")
	}
	return size, nil
}
