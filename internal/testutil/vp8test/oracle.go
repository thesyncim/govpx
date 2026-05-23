//go:build govpx_oracle_trace

package vp8test

import (
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// VpxencConfig is the root-independent VP8 vpxenc configuration used by
// oracle tests.
type VpxencConfig = coracle.VpxencVP8Config

// TemporalSVCConfig is the libvpx temporal SVC sample-encoder configuration
// used by oracle tests.
type TemporalSVCConfig = coracle.VpxTemporalSVCConfig

// Oracle is a resolved VP8 libvpx checksum oracle.
type Oracle struct {
	path string
}

// NewChecksumOracle resolves the VP8 checksum oracle or skips t.
func NewChecksumOracle(t testing.TB) Oracle {
	t.Helper()
	return Oracle{path: ChecksumOracle(t)}
}

// ChecksumOracle resolves the VP8 checksum oracle binary or skips t.
func ChecksumOracle(t testing.TB) string {
	t.Helper()
	path := coracletest.ChecksumOracle(t)
	return path
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
	return coracletest.RunVP8ChecksumOracle(t, o.path, ivf)
}

// FramesMode runs the checksum oracle in a named decode mode.
func (o Oracle) FramesMode(t testing.TB, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleMode(t, o.path, mode, ivf)
}

// FramesModeExpectError runs the checksum oracle and returns the decode error.
func (o Oracle) FramesModeExpectError(t testing.TB, mode string, ivf []byte) error {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleModeExpectError(t, o.path, mode, ivf)
}

// File runs the checksum oracle against an IVF file path.
func (o Oracle) File(t testing.TB, path string) []testutil.FrameChecksum {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleFile(t, o.path, path)
}

// FileExpectError runs the checksum oracle against an invalid IVF file path.
func (o Oracle) FileExpectError(t testing.TB, path string) error {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleFileExpectError(t, o.path, path)
}

// FramesWithControlScript runs a decoder control-script mode and returns both
// visible-frame and copy-reference checksums.
func (o Oracle) FramesWithControlScript(t testing.TB, mode string,
	script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleControlScriptWithCopyLog(t, o.path,
		mode, script, ivf)
}

// ThreadedFramesWithControlScript runs the threaded decoder control-script
// mode and returns both visible-frame and copy-reference checksums.
func (o Oracle) ThreadedFramesWithControlScript(t testing.TB,
	threads int, script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleThreadedControlScriptWithCopyLog(t,
		o.path, threads, script, ivf)
}

// RunVP8ChecksumOracle runs the checksum oracle in normal decode mode.
func RunVP8ChecksumOracle(t testing.TB, oracle string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	frames := coracletest.RunVP8ChecksumOracle(t, oracle, ivf)
	return frames
}

// RunVP8ChecksumOracleFile runs the checksum oracle against an IVF file path.
func RunVP8ChecksumOracleFile(t testing.TB, oracle string, path string) []testutil.FrameChecksum {
	t.Helper()
	frames := coracletest.RunVP8ChecksumOracleFile(t, oracle, path)
	return frames
}

// RunVP8ChecksumOracleFileExpectError returns the oracle error for an invalid IVF path.
func RunVP8ChecksumOracleFileExpectError(t testing.TB, oracle string, path string) error {
	t.Helper()
	err := coracletest.RunVP8ChecksumOracleFileExpectError(t, oracle, path)
	return err
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

// VpxencIVF encodes raw I420 with the pinned VP8 vpxenc wrapper.
func VpxencIVF(t testing.TB, raw []byte, cfg VpxencConfig) []byte {
	t.Helper()
	ivf, diag, err := coracle.VpxencVP8EncodeI420(raw, cfg)
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, diag)
	}
	return ivf
}

// TemporalSVCIVFs encodes raw I420 with libvpx's temporal SVC sample encoder.
func TemporalSVCIVFs(t testing.TB, raw []byte, cfg TemporalSVCConfig) ([][]byte, []byte) {
	t.Helper()
	ivfs, diag, err := coracle.VpxTemporalSVCEncodeI420(raw, cfg)
	if err != nil {
		t.Fatalf("vpx_temporal_svc_encoder failed: %v\n%s", err, diag)
	}
	return ivfs, diag
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
