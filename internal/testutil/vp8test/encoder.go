//go:build govpx_oracle_trace

package vp8test

import (
	"github.com/thesyncim/govpx/internal/coracle"
)

// VpxencVP8Config is the root-independent VP8 vpxenc configuration used by
// oracle tests.
type VpxencVP8Config = coracle.VpxencVP8Config

// VpxencVP8FrameFlagsConfig is the VP8 frame-flags encoder configuration used
// by oracle tests.
type VpxencVP8FrameFlagsConfig = coracle.VpxencVP8FrameFlagsConfig

// VpxencVP8TwoPassConfig is the VP8 two-pass encoder configuration used by
// oracle tests.
type VpxencVP8TwoPassConfig = coracle.VpxencVP8TwoPassConfig

// VpxTemporalSVCConfig is the libvpx temporal SVC sample-encoder
// configuration used by oracle tests.
type VpxTemporalSVCConfig = coracle.VpxTemporalSVCConfig

// VpxdecVP8Config is the VP8 vpxdec configuration used by oracle tests.
type VpxdecVP8Config = coracle.VpxdecVP8Config

// ErrVpxdecNotBuilt reports a missing VP8 vpxdec binary.
var ErrVpxdecNotBuilt = coracle.ErrVpxdecNotBuilt

// VpxdecPath resolves the pinned VP8 vpxdec binary.
func VpxdecPath() (string, error) {
	path, err := coracle.VpxdecPath()
	return path, err
}

// VpxencVP8EncodeI420 encodes raw I420 with the pinned stock VP8 vpxenc.
func VpxencVP8EncodeI420(raw []byte, cfg VpxencVP8Config) ([]byte, []byte, error) {
	ivf, diag, err := coracle.VpxencVP8EncodeI420(raw, cfg)
	return ivf, diag, err
}

// VpxencVP8OracleFramePayloadsI420 returns patched VP8 vpxenc frame payloads.
func VpxencVP8OracleFramePayloadsI420(raw []byte, cfg VpxencVP8Config) ([][]byte, []byte, error) {
	frames, diag, err := coracle.VpxencVP8OracleFramePayloadsI420(raw, cfg)
	return frames, diag, err
}

// VpxencVP8OracleTraceI420 returns the patched VP8 encoder trace.
func VpxencVP8OracleTraceI420(raw []byte, cfg VpxencVP8Config) ([]byte, []byte, error) {
	trace, diag, err := coracle.VpxencVP8OracleTraceI420(raw, cfg)
	return trace, diag, err
}

// VpxencVP8OracleEncodeTraceI420 returns both IVF data and the patched VP8
// encoder trace.
func VpxencVP8OracleEncodeTraceI420(raw []byte, cfg VpxencVP8Config) ([]byte, []byte, []byte, error) {
	ivf, trace, diag, err := coracle.VpxencVP8OracleEncodeTraceI420(raw, cfg)
	return ivf, trace, diag, err
}

// VpxencVP8FirstPassStatsI420 returns libvpx VP8 first-pass stats.
func VpxencVP8FirstPassStatsI420(raw []byte, cfg VpxencVP8Config) ([]byte, []byte, error) {
	stats, diag, err := coracle.VpxencVP8FirstPassStatsI420(raw, cfg)
	return stats, diag, err
}

// VpxencVP8TwoPassEncodeI420 runs VP8 vpxenc pass 1 and pass 2.
func VpxencVP8TwoPassEncodeI420(raw []byte, cfg VpxencVP8TwoPassConfig) ([]byte, []byte, []byte, error) {
	stats, ivf, diag, err := coracle.VpxencVP8TwoPassEncodeI420(raw, cfg)
	return stats, ivf, diag, err
}

// VpxencVP8TwoPassTraceI420 runs VP8 vpxenc pass 1 and a traced pass 2.
func VpxencVP8TwoPassTraceI420(raw []byte, cfg VpxencVP8TwoPassConfig) ([]byte, []byte, []byte, error) {
	stats, trace, diag, err := coracle.VpxencVP8TwoPassTraceI420(raw, cfg)
	return stats, trace, diag, err
}

// VpxencVP8FrameFlagsEncodeTraceI420 runs the VP8 frame-flags trace helper.
func VpxencVP8FrameFlagsEncodeTraceI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) ([]byte, []byte, []byte, error) {
	ivf, trace, diag, err := coracle.VpxencVP8FrameFlagsEncodeTraceI420(raw,
		cfg)
	return ivf, trace, diag, err
}

// VpxencVP8FrameFlagsPayloadsI420 returns frame payloads from the VP8
// frame-flags helper.
func VpxencVP8FrameFlagsPayloadsI420(raw []byte, cfg VpxencVP8FrameFlagsConfig) ([][]byte, []byte, error) {
	frames, diag, err := coracle.VpxencVP8FrameFlagsPayloadsI420(raw, cfg)
	return frames, diag, err
}

// VpxTemporalSVCEncodeI420 runs libvpx's temporal SVC sample encoder.
func VpxTemporalSVCEncodeI420(raw []byte, cfg VpxTemporalSVCConfig) ([][]byte, []byte, error) {
	ivfs, diag, err := coracle.VpxTemporalSVCEncodeI420(raw, cfg)
	return ivfs, diag, err
}

// VpxTemporalSVCPayloadsI420 runs libvpx's temporal SVC sample encoder and
// returns per-layer payloads.
func VpxTemporalSVCPayloadsI420(raw []byte, cfg VpxTemporalSVCConfig) ([][][]byte, []byte, error) {
	layers, diag, err := coracle.VpxTemporalSVCPayloadsI420(raw, cfg)
	return layers, diag, err
}

// VpxdecVP8DecodeI420 decodes IVF with stock vpxdec and returns raw I420.
func VpxdecVP8DecodeI420(ivf []byte, cfg VpxdecVP8Config) ([]byte, []byte, error) {
	raw, diag, err := coracle.VpxdecVP8DecodeI420(ivf, cfg)
	return raw, diag, err
}

// VP8VpxencThreadsArg reports whether vpxenc arguments request parallel VP8
// encoding.
func VP8VpxencThreadsArg(args []string) (int, bool) {
	threads, parallel := coracle.VP8VpxencThreadsArg(args)
	return threads, parallel
}
