package coracletest

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

// ChecksumOracle resolves the VP8 checksum oracle binary or skips the test
// when the pinned libvpx v1.16.0 helper has not been built.
func ChecksumOracle(t testing.TB) string {
	t.Helper()
	path, err := coracle.ChecksumOraclePath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrChecksumOracleNotBuilt) {
		t.Skip("set GOVPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
	}
	t.Fatalf("ChecksumOraclePath: %v", err)
	return ""
}

// Vpxenc resolves the pinned stock VP8 vpxenc binary or skips the test when
// it has not been built.
func Vpxenc(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencNotBuilt) {
		t.Skip("set GOVPX_VPXENC to a libvpx v1.16.0 vpxenc binary")
	}
	t.Fatalf("VpxencPath: %v", err)
	return ""
}

// VpxTemporalSVCEncoder resolves libvpx's temporal SVC sample encoder or
// skips the test when it has not been built.
func VpxTemporalSVCEncoder(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxTemporalSVCEncoderPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxTemporalSVCEncoderNotBuilt) {
		t.Skip("set GOVPX_VPX_TEMPORAL_SVC_ENCODER to a libvpx v1.16.0 vpx_temporal_svc_encoder binary")
	}
	t.Fatalf("VpxTemporalSVCEncoderPath: %v", err)
	return ""
}

// VpxencOracle resolves the patched VP8 encoder trace oracle or skips the
// test when it has not been built.
func VpxencOracle(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencOraclePath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencOracleNotBuilt) {
		t.Skip("set GOVPX_VPXENC_ORACLE to the patched libvpx vpxenc oracle binary")
	}
	t.Fatalf("VpxencOraclePath: %v", err)
	return ""
}

// VpxencFrameFlags resolves the companion VP8 encoder driver used for
// per-frame flag scheduling. The combined frame-flags plus trace binary wins
// because it is a strict superset of the plain driver.
func VpxencFrameFlags(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencFrameFlagsPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencFrameFlagsNotBuilt) {
		t.Skip("vpxenc-frameflags binary not available; set GOVPX_VPXENC_FRAMEFLAGS_ORACLE/GOVPX_VPXENC_FRAMEFLAGS or run internal/coracle/build_vpxenc_frameflags_oracle.sh")
	}
	t.Fatalf("VpxencFrameFlagsPath: %v", err)
	return ""
}

// VpxencFrameFlagsOracle resolves the VP8 encoder driver that exposes both
// per-frame flag scheduling and the per-MB JSONL oracle trace.
func VpxencFrameFlagsOracle(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencFrameFlagsOraclePath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencFrameFlagsOracleNotBuilt) {
		t.Skip("vpxenc-frameflags-oracle binary not available; set GOVPX_VPXENC_FRAMEFLAGS_ORACLE or run internal/coracle/build_vpxenc_frameflags_oracle.sh")
	}
	t.Fatalf("VpxencFrameFlagsOraclePath: %v", err)
	return ""
}
