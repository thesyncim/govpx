package coracle

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// VP9 vpxdec oracle harness. Spawns the matching libvpx vpxdec
// binary (built via internal/coracle/build_vpxdec_vp9.sh) and
// pipes a govpx-produced VP9 IVF stream through it. The
// byte-parity gate is: when vpxdec exits 0 and writes a frame,
// our encoder's output is structurally valid VP9.
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
	if env := os.Getenv("GOVPX_VPXDEC_VP9_BIN"); env != "" {
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			vpxdecVP9Path = env
			return
		}
	}
	// Default: <coracle pkg dir>/build/vpxdec-vp9.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		vpxdecVP9Err = ErrVpxdecVP9NotBuilt
		return
	}
	candidate := filepath.Join(filepath.Dir(file), "build", "vpxdec-vp9")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		vpxdecVP9Path = candidate
		return
	}
	vpxdecVP9Err = ErrVpxdecVP9NotBuilt
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
	bin, err := VpxdecVP9Path()
	if err != nil {
		return nil, nil, err
	}
	in, err := os.CreateTemp("", "govpx-vp9-*.ivf")
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(ivf); err != nil {
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

	cmd := exec.Command(bin, "--codec=vp9", "--rawvideo", "--i420",
		"--output="+outName, in.Name())
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
