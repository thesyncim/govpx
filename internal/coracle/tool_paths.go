package coracle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrToolPathInvalid reports an explicitly configured oracle binary path that
// does not name an executable file.
var ErrToolPathInvalid = errors.New("coracle: oracle tool path is invalid")

// ErrChecksumOracleNotBuilt is returned when the VP8 checksum oracle is absent.
var ErrChecksumOracleNotBuilt = errors.New(
	"coracle: govpx-vpx-oracle binary not built (run internal/coracle/build_libvpx.sh)")

// ErrVpxencNotBuilt is returned when the pinned stock vpxenc binary is absent.
var ErrVpxencNotBuilt = errors.New(
	"coracle: vpxenc binary not built (run internal/coracle/build_vpxenc.sh)")

// ErrVpxdecNotBuilt is returned when the pinned stock vpxdec binary is absent.
var ErrVpxdecNotBuilt = errors.New(
	"coracle: vpxdec binary not built (run internal/coracle/build_libvpx.sh)")

// ErrVpxTemporalSVCEncoderNotBuilt is returned when the libvpx temporal SVC
// sample encoder is absent.
var ErrVpxTemporalSVCEncoderNotBuilt = errors.New(
	"coracle: vpx_temporal_svc_encoder binary not built (run internal/coracle/build_vpxenc.sh)")

// ErrVpxencOracleNotBuilt is returned when the patched VP8 trace oracle is
// absent.
var ErrVpxencOracleNotBuilt = errors.New(
	"coracle: vpxenc-oracle binary not built (run internal/coracle/build_vpxenc_oracle.sh)")

// ErrVpxencFrameFlagsNotBuilt is returned when the VP8 frame-flags encoder
// helper is absent.
var ErrVpxencFrameFlagsNotBuilt = errors.New(
	"coracle: vpxenc-frameflags binary not built (run internal/coracle/build_vpxenc_frameflags.sh)")

// ErrVpxencFrameFlagsOracleNotBuilt is returned when the combined VP8
// frame-flags plus trace encoder helper is absent.
var ErrVpxencFrameFlagsOracleNotBuilt = errors.New(
	"coracle: vpxenc-frameflags-oracle binary not built (run internal/coracle/build_vpxenc_frameflags_oracle.sh)")

type toolPathSpec struct {
	envNames   []string
	lookPath   string
	buildNames []string
	notBuilt   error
}

// ChecksumOraclePath resolves the VP8 checksum oracle binary. The
// GOVPX_ORACLE environment variable wins; otherwise PATH and
// internal/coracle/build/govpx-vpx-oracle are checked.
func ChecksumOraclePath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_ORACLE"},
		lookPath:   "govpx-vpx-oracle",
		buildNames: []string{"govpx-vpx-oracle"},
		notBuilt:   ErrChecksumOracleNotBuilt,
	})
}

// VpxencPath resolves the pinned stock VP8 vpxenc binary.
func VpxencPath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_VPXENC"},
		lookPath:   "vpxenc",
		buildNames: []string{"vpxenc"},
		notBuilt:   ErrVpxencNotBuilt,
	})
}

// VpxdecPath resolves the pinned stock VP8 vpxdec binary.
func VpxdecPath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_VPXDEC"},
		lookPath:   "vpxdec",
		buildNames: []string{"vpxdec"},
		notBuilt:   ErrVpxdecNotBuilt,
	})
}

// VpxTemporalSVCEncoderPath resolves libvpx's temporal SVC sample encoder.
func VpxTemporalSVCEncoderPath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_VPX_TEMPORAL_SVC_ENCODER"},
		lookPath:   "vpx_temporal_svc_encoder",
		buildNames: []string{"vpx_temporal_svc_encoder"},
		notBuilt:   ErrVpxTemporalSVCEncoderNotBuilt,
	})
}

// VpxencOraclePath resolves the patched VP8 encoder trace oracle.
func VpxencOraclePath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_VPXENC_ORACLE"},
		buildNames: []string{"vpxenc-oracle"},
		notBuilt:   ErrVpxencOracleNotBuilt,
	})
}

// VpxencFrameFlagsPath resolves the VP8 per-frame flag helper. The combined
// frame-flags plus trace binary is preferred because it is a strict superset of
// the plain helper.
func VpxencFrameFlagsPath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames: []string{
			"GOVPX_VPXENC_FRAMEFLAGS_ORACLE",
			"GOVPX_VPXENC_FRAMEFLAGS",
		},
		buildNames: []string{
			"vpxenc-frameflags-oracle",
			"vpxenc-frameflags",
		},
		notBuilt: ErrVpxencFrameFlagsNotBuilt,
	})
}

// VpxencFrameFlagsOraclePath resolves the combined VP8 per-frame flag plus
// trace helper.
func VpxencFrameFlagsOraclePath() (string, error) {
	return resolveToolPath(toolPathSpec{
		envNames:   []string{"GOVPX_VPXENC_FRAMEFLAGS_ORACLE"},
		buildNames: []string{"vpxenc-frameflags-oracle"},
		notBuilt:   ErrVpxencFrameFlagsOracleNotBuilt,
	})
}

func resolveToolPath(spec toolPathSpec) (string, error) {
	for _, env := range spec.envNames {
		if path := os.Getenv(env); path != "" {
			if executableFile(path) {
				return path, nil
			}
			return "", fmt.Errorf("coracle: %s=%q: %w", env, path, ErrToolPathInvalid)
		}
	}
	if spec.lookPath != "" {
		if path, err := exec.LookPath(spec.lookPath); err == nil {
			return path, nil
		}
	}
	dir, err := packageDir()
	if err != nil {
		return "", spec.notBuilt
	}
	for _, name := range spec.buildNames {
		path := filepath.Join(dir, "build", name)
		if executableFile(path) {
			return path, nil
		}
	}
	return "", spec.notBuilt
}

func packageDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("coracle: caller unavailable")
	}
	return filepath.Dir(file), nil
}

func executableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
