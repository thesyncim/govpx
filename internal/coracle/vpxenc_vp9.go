package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
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
	want, err := checkedVP9I420Mul(frameSize, frames)
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

func vpxencVP9I420FrameSize(width int, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("coracle: invalid VP9 vpxenc dimensions %dx%d", width, height)
	}
	y, err := checkedVP9I420Mul(width, height)
	if err != nil {
		return 0, err
	}
	uvWidth := width/2 + width%2
	uvHeight := height/2 + height%2
	uv, err := checkedVP9I420Mul(uvWidth, uvHeight)
	if err != nil {
		return 0, err
	}
	chroma, err := checkedVP9I420Mul(uv, 2)
	if err != nil {
		return 0, err
	}
	return checkedVP9I420Add(y, chroma)
}

func checkedVP9I420Mul(a int, b int) (int, error) {
	if a != 0 && b > int(^uint(0)>>1)/a {
		return 0, errors.New("coracle: VP9 vpxenc I420 size overflows int")
	}
	return a * b, nil
}

func checkedVP9I420Add(a int, b int) (int, error) {
	if b > int(^uint(0)>>1)-a {
		return 0, errors.New("coracle: VP9 vpxenc I420 size overflows int")
	}
	return a + b, nil
}
