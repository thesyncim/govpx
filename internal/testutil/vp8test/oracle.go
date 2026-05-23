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

// ChecksumOracle is a resolved VP8 libvpx checksum oracle.
type ChecksumOracle struct {
	path string
}

// NewChecksumOracle resolves the VP8 checksum oracle or skips t.
func NewChecksumOracle(t testing.TB) ChecksumOracle {
	t.Helper()
	return ChecksumOracle{path: coracletest.ChecksumOracle(t)}
}

// Vpxenc resolves the pinned VP8 vpxenc binary or skips t.
func Vpxenc(t testing.TB) string {
	t.Helper()
	return coracletest.Vpxenc(t)
}

// Vpxdec resolves the pinned VP8 vpxdec binary or skips t.
func Vpxdec(t testing.TB) string {
	t.Helper()
	return coracletest.Vpxdec(t)
}

// VpxTemporalSVCEncoder resolves libvpx's temporal SVC sample encoder or skips t.
func VpxTemporalSVCEncoder(t testing.TB) string {
	t.Helper()
	return coracletest.VpxTemporalSVCEncoder(t)
}

// Frames runs the checksum oracle in normal decode mode.
func (o ChecksumOracle) Frames(t testing.TB, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	return coracletest.RunVP8ChecksumOracle(t, o.path, ivf)
}

// FramesMode runs the checksum oracle in a named decode mode.
func (o ChecksumOracle) FramesMode(t testing.TB, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleMode(t, o.path, mode, ivf)
}

// FramesModeExpectError runs the checksum oracle and returns the decode error.
func (o ChecksumOracle) FramesModeExpectError(t testing.TB, mode string, ivf []byte) error {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleModeExpectError(t, o.path, mode, ivf)
}

// File runs the checksum oracle against an IVF file path.
func (o ChecksumOracle) File(t testing.TB, path string) []testutil.FrameChecksum {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleFile(t, o.path, path)
}

// FileExpectError runs the checksum oracle against an invalid IVF file path.
func (o ChecksumOracle) FileExpectError(t testing.TB, path string) error {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleFileExpectError(t, o.path, path)
}

// FramesWithControlScript runs a decoder control-script mode and returns both
// visible-frame and copy-reference checksums.
func (o ChecksumOracle) FramesWithControlScript(t testing.TB, mode string,
	script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleControlScriptWithCopyLog(t, o.path,
		mode, script, ivf)
}

// ThreadedFramesWithControlScript runs the threaded decoder control-script
// mode and returns both visible-frame and copy-reference checksums.
func (o ChecksumOracle) ThreadedFramesWithControlScript(t testing.TB,
	threads int, script []string, ivf []byte,
) ([]testutil.FrameChecksum, []testutil.FrameChecksum) {
	t.Helper()
	return coracletest.RunVP8ChecksumOracleThreadedControlScriptWithCopyLog(t,
		o.path, threads, script, ivf)
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
