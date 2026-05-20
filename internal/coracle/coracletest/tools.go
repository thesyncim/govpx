package coracletest

import (
	"errors"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

// SkipWithoutOracle skips t unless the slow oracle gates are explicitly
// enabled for the local run.
func SkipWithoutOracle(t testing.TB, name string) {
	t.Helper()
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run " + name)
	}
}

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

// VpxdecVP9 resolves the pinned VP9 vpxdec binary or skips the test when it
// has not been built.
func VpxdecVP9(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxdecVP9Path()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
		t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
	}
	t.Fatalf("VpxdecVP9Path: %v", err)
	return ""
}

// VpxencVP9 resolves the pinned VP9 vpxenc binary or skips the test when it
// has not been built.
func VpxencVP9(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencVP9Path()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencVP9NotBuilt) {
		t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
	}
	t.Fatalf("VpxencVP9Path: %v", err)
	return ""
}

// VpxencVP9FrameFlags resolves the VP9 frame-flags encoder helper or skips
// the test when it has not been built.
func VpxencVP9FrameFlags(t testing.TB) string {
	t.Helper()
	path, err := coracle.VpxencVP9FrameFlagsPath()
	if err == nil {
		return path
	}
	if errors.Is(err, coracle.ErrVpxencVP9FrameFlagsNotBuilt) {
		t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
	}
	t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	return ""
}
