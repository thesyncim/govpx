//go:build govpx_oracle_trace

package vp8test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// Oracle is a resolved VP8 libvpx checksum oracle.
type Oracle struct {
	path string
}

// RequireOracle skips t unless the external libvpx oracle suite is enabled.
func RequireOracle(t testing.TB, name string) {
	t.Helper()
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run " + name)
	}
}

// NewChecksumOracle resolves the VP8 checksum oracle or skips t.
func NewChecksumOracle(t testing.TB) Oracle {
	t.Helper()
	path := coracletest.ChecksumOracle(t)
	return Oracle{path: path}
}

// Vpxenc resolves the pinned VP8 vpxenc binary or skips t.
func Vpxenc(t testing.TB) string {
	t.Helper()
	path := coracletest.Vpxenc(t)
	return path
}

// Vpxdec resolves the pinned VP8 vpxdec binary or skips t.
func Vpxdec(t testing.TB) string {
	t.Helper()
	path := coracletest.Vpxdec(t)
	return path
}

// VpxTemporalSVCEncoder resolves libvpx's temporal SVC sample encoder or skips t.
func VpxTemporalSVCEncoder(t testing.TB) string {
	t.Helper()
	path := coracletest.VpxTemporalSVCEncoder(t)
	return path
}

// VpxencOracle resolves the patched VP8 encoder trace oracle or skips t.
func VpxencOracle(t testing.TB) string {
	t.Helper()
	path := coracletest.VpxencOracle(t)
	return path
}

// VpxencFrameFlags resolves the VP8 per-frame flag driver or skips t.
func VpxencFrameFlags(t testing.TB) string {
	t.Helper()
	path := coracletest.VpxencFrameFlags(t)
	return path
}

// VpxencFrameFlagsOracle resolves the VP8 frame-flags trace driver or skips t.
func VpxencFrameFlagsOracle(t testing.TB) string {
	t.Helper()
	path := coracletest.VpxencFrameFlagsOracle(t)
	return path
}

// Frames runs the checksum oracle in normal decode mode.
func (o Oracle) Frames(t testing.TB, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := writeIVF(t, "govpx-keyframe.ivf", ivf)
	return o.File(t, path)
}

// FramesMode runs the checksum oracle in a named decode mode.
func (o Oracle) FramesMode(t testing.TB, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := writeIVF(t, "govpx-"+mode+".ivf", ivf)
	return o.FileMode(t, mode, path)
}

// FramesModeExpectError runs the checksum oracle and returns the decode error.
func (o Oracle) FramesModeExpectError(t testing.TB, mode string, ivf []byte) error {
	t.Helper()
	path := writeIVF(t, "govpx-"+mode+".ivf", ivf)
	return o.FileModeExpectError(t, mode, path)
}

// File runs the checksum oracle against an IVF file path.
func (o Oracle) File(t testing.TB, path string) []testutil.FrameChecksum {
	t.Helper()
	return o.FileMode(t, "decode", path)
}

// FileMode runs the checksum oracle in mode against an IVF file path.
func (o Oracle) FileMode(t testing.TB, mode string, path string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumFile(o.path, mode, path)
	if err != nil {
		failChecksumOracle(t, out, err)
	}
	return frames
}

// FileExpectError runs the checksum oracle against an invalid IVF file path.
func (o Oracle) FileExpectError(t testing.TB, path string) error {
	t.Helper()
	return o.FileModeExpectError(t, "decode", path)
}

// FileModeExpectError runs the checksum oracle and returns the process error.
func (o Oracle) FileModeExpectError(t testing.TB, mode string, path string) error {
	t.Helper()
	out, err := coracle.VpxdecVP8ChecksumFileExpectError(o.path, mode, path)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("libvpx oracle failed to start: %v\n%s", err, out)
	}
	return err
}

// FramesWithControlScript runs a decoder control-script mode and returns both
// visible-frame and copy-reference checksums.
func (o Oracle) FramesWithControlScript(t testing.TB, mode string,
	script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := writeIVFInDir(t, dir, "govpx-"+mode+".ivf", ivf)
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	args := []string{mode, strings.Join(script, ","), copyLogPath, path}
	frames := o.Args(t, args)
	return frames, readCopyReferenceLog(t, copyLogPath)
}

// ThreadedFramesWithControlScript runs the threaded decoder control-script
// mode and returns both visible-frame and copy-reference checksums.
func (o Oracle) ThreadedFramesWithControlScript(t testing.TB,
	threads int, script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	dir := t.TempDir()
	path := writeIVFInDir(t, dir, "govpx-decode-threaded-controls.ivf", ivf)
	copyLogPath := filepath.Join(dir, "copy-reference.jsonl")
	args := []string{
		"decode-threaded-controls-copylog",
		strconv.Itoa(threads),
		strings.Join(script, ","),
		copyLogPath,
		path,
	}
	frames := o.Args(t, args)
	return frames, readCopyReferenceLog(t, copyLogPath)
}

// Args runs the checksum oracle with raw mode arguments.
func (o Oracle) Args(t testing.TB, args []string) []testutil.FrameChecksum {
	t.Helper()
	frames, out, err := coracle.VpxdecVP8ChecksumArgs(o.path, args)
	if err != nil {
		failChecksumOracle(t, out, err)
	}
	return frames
}

// UpdateBaselines reports whether oracle scoreboard baselines should be rewritten.
func UpdateBaselines() bool {
	update := coracletest.UpdateBaselines()
	return update
}

// ReadOrWriteJSONBaseline returns an existing baseline or writes current.
func ReadOrWriteJSONBaseline[T any](t testing.TB, path string, current T) (T, bool) {
	t.Helper()
	baseline, wrote := coracletest.ReadOrWriteJSONBaseline(t, path, current)
	return baseline, wrote
}

// ReadOptionalJSONBaseline reads an existing JSON baseline when updates are off.
func ReadOptionalJSONBaseline[T any](t testing.TB, path string) (T, bool) {
	t.Helper()
	baseline, ok := coracletest.ReadOptionalJSONBaseline[T](t, path)
	return baseline, ok
}

// WriteJSONBaseline writes v as a stable JSON baseline.
func WriteJSONBaseline(t testing.TB, path string, v any) {
	t.Helper()
	coracletest.WriteJSONBaseline(t, path, v)
}

// VpxdecSummaryIVF runs vpxdec's summary path and fails t on decode errors.
func VpxdecSummaryIVF(t testing.TB, binary string, ivf []byte) {
	t.Helper()
	diag, err := coracle.VpxdecVP8SummaryIVF(ivf,
		coracle.VpxdecVP8Config{BinaryPath: binary})
	if err != nil {
		t.Fatalf("vpxdec failed: %v\n%s", err, diag)
	}
}

func writeIVF(t testing.TB, name string, ivf []byte) string {
	t.Helper()
	return writeIVFInDir(t, t.TempDir(), name, ivf)
}

func writeIVFInDir(t testing.TB, dir string, name string, ivf []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}

func readCopyReferenceLog(t testing.TB, path string) []testutil.FrameChecksum {
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

func failChecksumOracle(t testing.TB, out []byte, err error) {
	t.Helper()
	if errors.Is(err, testutil.ErrInvalidOracleOutput) {
		t.Fatalf("libvpx oracle produced invalid output:\n%s", out)
	}
	t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
}
