package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/thesyncim/govpx/internal/testutil"
)

var errOraclePathEmpty = errors.New("coracle: oracle path is empty")

// VpxdecVP8Config describes a stock vpxdec VP8 decode run.
type VpxdecVP8Config struct {
	BinaryPath string
}

// VpxdecVP8SummaryIVF runs stock vpxdec over an in-memory IVF stream with
// --noblit --summary. diag contains combined stdout/stderr.
func VpxdecVP8SummaryIVF(ivf []byte, cfg VpxdecVP8Config) (diag []byte, err error) {
	bin, err := cfg.vpxdecPath()
	if err != nil {
		return nil, err
	}
	_, diag, err = runVpxdecVP8IVFWithOutput(ivf, "govpx-vpxdec-vp8-summary-*", func(ivfPath string, _ string) []string {
		return []string{"--codec=vp8", "--noblit", "--summary", ivfPath}
	}, bin)
	return diag, err
}

// VpxdecVP8DecodeI420 decodes an in-memory IVF stream with stock vpxdec and
// returns the raw I420 bytes written before process exit. Malformed inputs may
// return both partial raw output and a non-nil process error.
func VpxdecVP8DecodeI420(ivf []byte, cfg VpxdecVP8Config) (raw []byte, diag []byte, err error) {
	bin, err := cfg.vpxdecPath()
	if err != nil {
		return nil, nil, err
	}
	return runVpxdecVP8IVFWithOutput(ivf, "govpx-vpxdec-vp8-i420-*", func(ivfPath string, rawPath string) []string {
		return []string{"--codec=vp8", "--i420", "--output=" + rawPath, ivfPath}
	}, bin)
}

// VpxdecVP8ChecksumArgs runs the VP8 checksum oracle with args and parses its
// JSONL frame checksums. diag always contains combined stdout/stderr.
func VpxdecVP8ChecksumArgs(oracle string, args []string) (frames []testutil.FrameChecksum, diag []byte, err error) {
	if oracle == "" {
		return nil, nil, errOraclePathEmpty
	}
	cmd := exec.Command(oracle, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, out, err
	}
	frames, err = testutil.ParseFrameChecksumJSONLines(out)
	if err != nil {
		return nil, out, fmt.Errorf("coracle: parse VP8 checksum oracle output: %w", err)
	}
	return frames, out, nil
}

// VpxdecVP8ChecksumFile runs the VP8 checksum oracle mode against an IVF file.
func VpxdecVP8ChecksumFile(oracle string, mode string, path string) (frames []testutil.FrameChecksum, diag []byte, err error) {
	return VpxdecVP8ChecksumArgs(oracle, []string{mode, path})
}

// VpxdecVP8ChecksumFileExpectError runs the VP8 checksum oracle mode and
// returns the process error. A nil error means the oracle accepted the stream.
func VpxdecVP8ChecksumFileExpectError(oracle string, mode string, path string) (diag []byte, err error) {
	if oracle == "" {
		return nil, errOraclePathEmpty
	}
	cmd := exec.Command(oracle, mode, path)
	out, err := cmd.CombinedOutput()
	return out, err
}

// ReadFrameChecksumJSONLFile reads a checksum JSONL file produced by one of the
// oracle helpers.
func ReadFrameChecksumJSONLFile(path string) (frames []testutil.FrameChecksum, data []byte, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	frames, err = testutil.ParseFrameChecksumJSONLines(data)
	if err != nil {
		return nil, data, fmt.Errorf("coracle: parse frame checksum log: %w", err)
	}
	return frames, data, nil
}

func (cfg VpxdecVP8Config) vpxdecPath() (string, error) {
	if cfg.BinaryPath != "" {
		return cfg.BinaryPath, nil
	}
	return VpxdecPath()
}

func runVpxdecVP8IVFWithOutput(ivf []byte, tempPattern string, argsFor func(ivfPath string, rawPath string) []string, bin string) (raw []byte, diag []byte, err error) {
	dir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	ivfPath := filepath.Join(dir, "input.ivf")
	rawPath := filepath.Join(dir, "output.i420")
	if err := os.WriteFile(ivfPath, ivf, 0o600); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, argsFor(ivfPath, rawPath)...)
	cmd.Env = os.Environ()
	diag, runErr := cmd.CombinedOutput()
	raw, readErr := os.ReadFile(rawPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			raw = nil
		} else {
			return nil, diag, readErr
		}
	}
	return raw, diag, runErr
}
