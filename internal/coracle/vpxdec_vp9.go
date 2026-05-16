package coracle

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

// VP9 vpxdec oracle harness. Spawns the matching libvpx vpxdec
// binary (built via internal/coracle/build_vpxdec_vp9.sh) and
// pipes a govpx-produced VP9 IVF stream through it. This is a structural
// acceptance check: when vpxdec exits 0, the packet stream is valid VP9.
//
// The binary path defaults to internal/coracle/build/vpxdec-vp9
// relative to the package's source location. Override via
// GOVPX_VPXDEC_VP9_BIN.

// ErrVpxdecVP9NotBuilt is returned when the harness can't find the
// vpxdec-vp9 binary. Callers gate the test on this error with
// t.Skip so CI environments without libvpx still pass.
var ErrVpxdecVP9NotBuilt = errors.New(
	"coracle: vpxdec-vp9 binary not built (run internal/coracle/build_vpxdec_vp9.sh)")

var (
	vpxdecVP9Once sync.Once
	vpxdecVP9Path string
	vpxdecVP9Err  error
)

// VpxdecVP9Path returns the resolved absolute path to the
// VP9-enabled vpxdec binary, or ErrVpxdecVP9NotBuilt if the build
// script hasn't been run.
func VpxdecVP9Path() (string, error) {
	vpxdecVP9Once.Do(resolveVpxdecVP9)
	return vpxdecVP9Path, vpxdecVP9Err
}

func resolveVpxdecVP9() {
	vpxdecVP9Path, vpxdecVP9Err = resolveVP9ToolPath(
		"GOVPX_VPXDEC_VP9_BIN", "vpxdec-vp9", ErrVpxdecVP9NotBuilt)
}

func resolveVP9ToolPath(envName string, binaryName string, notBuilt error) (string, error) {
	if env := os.Getenv(envName); env != "" {
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return env, nil
		}
	}
	// Default: <coracle pkg dir>/build/<binaryName>.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", notBuilt
	}
	candidate := filepath.Join(filepath.Dir(file), "build", binaryName)
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, nil
	}
	return "", notBuilt
}

// VpxdecVP9Decode pipes the IVF-wrapped VP9 stream `ivf` through
// vpxdec-vp9 in --noblit mode (parse-only, no YUV output). Returns
// the combined stderr+stdout for diagnostics and an error if
// vpxdec exits non-zero.
//
// Skip the test with t.Skip if err is ErrVpxdecVP9NotBuilt.
func VpxdecVP9Decode(ivf []byte) ([]byte, error) {
	bin, err := VpxdecVP9Path()
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "govpx-vp9-*.ivf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(ivf); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "--codec=vp9", "--noblit", "--summary", tmp.Name())
	return cmd.CombinedOutput()
}

// VpxdecVP9DecodeI420 decodes an IVF-wrapped VP9 stream through libvpx
// vpxdec and returns the concatenated visible-frame I420 bytes. diag
// contains combined vpxdec stdout/stderr for failure messages.
func VpxdecVP9DecodeI420(ivf []byte) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(ivf, "govpx-vp9-*.ivf", VpxdecVP9Options{})
}

// VpxdecVP9DecodeWebMI420 decodes a WebM-wrapped VP9 stream through libvpx
// vpxdec and returns the concatenated visible-frame I420 bytes.
func VpxdecVP9DecodeWebMI420(webm []byte) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(webm, "govpx-vp9-*.webm", VpxdecVP9Options{})
}

// VpxdecVP9Options carries optional runtime knobs for vpxdec VP9 decode.
// Threads <= 0 leaves vpxdec at its single-threaded default. RowMT and
// LoopFilterOpt mirror libvpx vpxdec's --row-mt and --lpf-opt CLI flags;
// they expose the VP9D_SET_ROW_MT and VP9D_SET_LOOP_FILTER_OPT control
// codes the govpx VP9 decoder also wires through SetRowMT / SetLoopFilterOpt.
type VpxdecVP9Options struct {
	Threads       int
	RowMT         bool
	LoopFilterOpt bool
}

// VpxdecVP9DecodeI420WithOptions is VpxdecVP9DecodeI420 with the additional
// row-mt / lpf-opt / threads knobs surfaced through vpxdec's CLI.
func VpxdecVP9DecodeI420WithOptions(ivf []byte, opts VpxdecVP9Options) (raw []byte, diag []byte, err error) {
	return vpxdecVP9DecodeI420(ivf, "govpx-vp9-*.ivf", opts)
}

func vpxdecVP9DecodeI420(input []byte, tempPattern string, opts VpxdecVP9Options) (raw []byte, diag []byte, err error) {
	bin, err := VpxdecVP9Path()
	if err != nil {
		return nil, nil, err
	}
	in, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(input); err != nil {
		in.Close()
		return nil, nil, err
	}
	if err := in.Close(); err != nil {
		return nil, nil, err
	}

	out, err := os.CreateTemp("", "govpx-vp9-*.i420")
	if err != nil {
		return nil, nil, err
	}
	outName := out.Name()
	if err := out.Close(); err != nil {
		os.Remove(outName)
		return nil, nil, err
	}
	defer os.Remove(outName)

	args := []string{"--codec=vp9", "--rawvideo", "--i420",
		"--output=" + outName}
	if opts.Threads > 0 {
		args = append(args, "-t", strconv.Itoa(opts.Threads))
	}
	if opts.RowMT {
		args = append(args, "--row-mt=1")
	}
	if opts.LoopFilterOpt {
		args = append(args, "--lpf-opt=1")
	}
	args = append(args, in.Name())
	cmd := exec.Command(bin, args...)
	diag, err = cmd.CombinedOutput()
	if err != nil {
		return nil, diag, err
	}
	raw, err = os.ReadFile(outName)
	if err != nil {
		return nil, diag, err
	}
	return raw, diag, nil
}

