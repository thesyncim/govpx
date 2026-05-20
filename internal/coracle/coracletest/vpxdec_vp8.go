package coracletest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

// RunVP8ChecksumOracle writes ivf to a temporary file, runs the VP8 checksum
// oracle in normal decode mode, and parses the JSONL frame checksums.
func RunVP8ChecksumOracle(t testing.TB, oracle string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return RunVP8ChecksumOracleFile(t, oracle, path)
}

// RunVP8ChecksumOracleMode writes ivf to a temporary file and runs the VP8
// checksum oracle in mode.
func RunVP8ChecksumOracleMode(t testing.TB, oracle string, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return RunVP8ChecksumOracleFileMode(t, oracle, mode, path)
}

// RunVP8ChecksumOracleControlScriptWithCopyLog runs a VP8 decode-control mode
// and returns the normal frame checksums plus the copy-reference log.
func RunVP8ChecksumOracleControlScriptWithCopyLog(t testing.TB, oracle string, mode string, script []string, ivf []byte) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "govpx-"+mode+".ivf")
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	frames := RunVP8ChecksumOracleArgs(t, oracle, []string{mode, strings.Join(script, ","), copyLogPath, path})
	return frames, ReadVP8CopyReferenceLog(t, copyLogPath)
}

// RunVP8ChecksumOracleThreadedControlScriptWithCopyLog runs the threaded
// VP8 decode-control mode and returns the normal frame checksums plus the
// copy-reference log.
func RunVP8ChecksumOracleThreadedControlScriptWithCopyLog(t testing.TB, oracle string, threads int, script []string, ivf []byte) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "govpx-decode-threaded-controls.ivf")
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	args := []string{"decode-threaded-controls-copylog", strconv.Itoa(threads), strings.Join(script, ","), copyLogPath, path}
	frames := RunVP8ChecksumOracleArgs(t, oracle, args)
	return frames, ReadVP8CopyReferenceLog(t, copyLogPath)
}

// RunVP8ChecksumOracleModeExpectError writes ivf to a temporary file and
// returns the oracle process error for a mode that should reject it.
func RunVP8ChecksumOracleModeExpectError(t testing.TB, oracle string, mode string, ivf []byte) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return RunVP8ChecksumOracleFileModeExpectError(t, oracle, mode, path)
}

// RunVP8ChecksumOracleFile runs the VP8 checksum oracle in normal decode mode
// against an IVF file.
func RunVP8ChecksumOracleFile(t testing.TB, oracle string, path string) []testutil.FrameChecksum {
	t.Helper()
	return RunVP8ChecksumOracleFileMode(t, oracle, "decode", path)
}

// RunVP8ChecksumOracleFileMode runs the VP8 checksum oracle mode against an
// IVF file.
func RunVP8ChecksumOracleFileMode(t testing.TB, oracle string, mode string, path string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumFile(oracle, mode, path)
	if err != nil {
		failVP8ChecksumOracle(t, out, err)
	}
	return frames
}

// RunVP8ChecksumOracleArgs runs the VP8 checksum oracle with args.
func RunVP8ChecksumOracleArgs(t testing.TB, oracle string, args []string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumArgs(oracle, args)
	if err != nil {
		failVP8ChecksumOracle(t, out, err)
	}
	return frames
}

// ReadVP8CopyReferenceLog parses a copy-reference checksum log.
func ReadVP8CopyReferenceLog(t testing.TB, path string) []testutil.FrameChecksum {
	t.Helper()
	frames, data, err := coracle.ReadFrameChecksumJSONLFile(path)
	if err != nil {
		if errors.Is(err, testutil.ErrInvalidOracleOutput) {
			t.Fatalf("libvpx copy-reference log produced invalid output:\n%s", data)
		}
		t.Fatalf("ParseFrameChecksumJSONLines copy-reference log returned error: %v", err)
	}
	return frames
}

// RunVP8ChecksumOracleFileExpectError runs the normal VP8 decode mode and
// returns the oracle process error.
func RunVP8ChecksumOracleFileExpectError(t testing.TB, oracle string, path string) error {
	t.Helper()
	return RunVP8ChecksumOracleFileModeExpectError(t, oracle, "decode", path)
}

// RunVP8ChecksumOracleFileModeExpectError runs the VP8 checksum oracle mode
// and returns the oracle process error.
func RunVP8ChecksumOracleFileModeExpectError(t testing.TB, oracle string, mode string, path string) error {
	t.Helper()
	out, err := coracle.VpxdecVP8ChecksumFileExpectError(oracle, mode, path)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("libvpx oracle failed to start: %v\n%s", err, out)
	}
	return err
}

func failVP8ChecksumOracle(t testing.TB, out []byte, err error) {
	t.Helper()
	if errors.Is(err, testutil.ErrInvalidOracleOutput) {
		t.Fatalf("libvpx oracle produced invalid output:\n%s", out)
	}
	t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
}
