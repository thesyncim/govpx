package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/thesyncim/govpx/internal/testutil"
)

var errOraclePathEmpty = errors.New("coracle: oracle path is empty")

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
