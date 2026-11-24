//go:build govpx_oracle_trace

package vp9test

import (
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

type VpxdecOptions struct {
	SkipLoopFilter             bool
	PostProcess                bool
	PostProcessFlags           int
	PostProcessDeblockingLevel int
	PostProcessNoiseLevel      int
	InvertTileDecodeOrder      bool
	SVCSpatialLayerSet         bool
	SVCSpatialLayer            int
}

// SpatialSVCConfig is the root-independent libvpx spatial-SVC encoder
// configuration used by VP9 oracle tests.
type SpatialSVCConfig struct {
	Width                    int
	Height                   int
	Frames                   int
	Timebase                 string
	TotalBitrateKbps         int
	LayerCount               int
	ScaleFactors             string
	LayerBitratesKbps        []int
	TemporalLayerCount       int
	TemporalLayeringMode     int
	KeyFrameInterval         int
	MinQuantizer             int
	MaxQuantizer             int
	LagInFrames              int
	Threads                  int
	Speed                    int
	RateControlEndUsage      int
	InterLayerPredictionMode int
}

// RequireOracle skips t unless the external libvpx oracle suite is enabled.
func RequireOracle(t testing.TB, name string) {
	t.Helper()
	coracletest.SkipWithoutOracle(t, name)
}

// StrictEnv reports whether a VP9 oracle strict-mode environment flag is set.
func StrictEnv(name string) bool {
	return testutil.EnvFlag(name)
}

// RequireEnvFlag skips t unless name is set to 1.
func RequireEnvFlag(t testing.TB, name, label string) {
	t.Helper()
	if !StrictEnv(name) {
		t.Skip("set " + name + "=1 to run " + label)
	}
}

// RequireVpxdec resolves the pinned VP9 vpxdec binary or skips t.
func RequireVpxdec(t testing.TB) string {
	t.Helper()
	return coracletest.VpxdecVP9(t)
}

// RequireVpxenc resolves the pinned VP9 vpxenc binary or skips t.
func RequireVpxenc(t testing.TB) string {
	t.Helper()
	return coracletest.VpxencVP9(t)
}

// RequireVpxencFrameFlags resolves the VP9 frame-flags helper or skips t.
func RequireVpxencFrameFlags(t testing.TB) string {
	t.Helper()
	return coracletest.VpxencVP9FrameFlags(t)
}

func VpxdecI420(t testing.TB, ivf []byte) []byte {
	t.Helper()
	out, diag, err := vpxdecI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}
	return out
}

func VpxdecI420Result(ivf []byte) ([]byte, error) {
	out, _, err := vpxdecI420(ivf)
	return out, err
}

func vpxdecI420(ivf []byte) (out []byte, diag []byte, err error) {
	return coracle.VpxdecVP9DecodeI420(ivf)
}

func VpxdecAccepts(t testing.TB, label string, width, height int, packets ...[]byte) {
	t.Helper()
	out, err := coracle.VpxdecVP9Decode(BuildVP9IVF(width, height, packets...))
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected %s: %v\nvpxdec:\n%s", label, err, out)
	}
}

func VpxdecI420WithOptions(t testing.TB, ivf []byte, opts VpxdecOptions) []byte {
	t.Helper()
	out, diag, err := coracle.VpxdecVP9DecodeI420WithOptions(ivf,
		coracle.VpxdecVP9Options{
			SkipLoopFilter:             opts.SkipLoopFilter,
			PostProcess:                opts.PostProcess,
			PostProcessFlags:           opts.PostProcessFlags,
			PostProcessDeblockingLevel: opts.PostProcessDeblockingLevel,
			PostProcessNoiseLevel:      opts.PostProcessNoiseLevel,
			InvertTileDecodeOrder:      opts.InvertTileDecodeOrder,
			SVCSpatialLayerSet:         opts.SVCSpatialLayerSet,
			SVCSpatialLayer:            opts.SVCSpatialLayer,
		})
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}
	return out
}

func VpxdecWebMI420(t testing.TB, webm []byte) []byte {
	t.Helper()
	out, diag, err := coracle.VpxdecVP9DecodeWebMI420(webm)
	if err != nil {
		t.Fatalf("vpxdec-vp9 WebM decode failed: %v\n%s", err, diag)
	}
	return out
}

func VpxdecRejectsI420(t testing.TB, ivf []byte) {
	t.Helper()
	if _, _, err := coracle.VpxdecVP9DecodeI420(ivf); err == nil {
		t.Fatal("libvpx vpxdec accepted invalid VP90 IVF")
	}
}

func VpxencPackets(t testing.TB, sources []*image.YCbCr, extraArgs ...string) [][]byte {
	t.Helper()
	packets, diag, err := VpxencPacketsResult(sources, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return packets
}

func VpxencPacketsResult(sources []*image.YCbCr, extraArgs ...string) ([][]byte, []byte, error) {
	width, height, err := sameSizeSources("VP9 vpxenc source", sources)
	if err != nil {
		return nil, nil, err
	}
	raw := appendSourcesI420(nil, sources)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		return nil, diag, err
	}
	packets, err := testutil.IVFFramePayloads(ivf)
	if err != nil {
		return nil, diag, err
	}
	return packets, diag, nil
}

func VpxencIVF(t testing.TB, sources []*image.YCbCr, extraArgs ...string) []byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 vpxenc IVF source", sources)
	raw := appendSourcesI420(nil, sources)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return ivf
}

func VpxencFirstPassStats(t testing.TB, sources []*image.YCbCr, extraArgs ...string) []FirstPassStats {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 first-pass source", sources)
	raw := appendSourcesI420(nil, sources)
	data, diag, err := coracle.VpxencVP9FirstPassStatsI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("VpxencVP9FirstPassStatsI420 failed: %v\n%s", err, diag)
	}
	return ParseFirstPassStats(t, data)
}

func VpxencFrameFlagPackets(t testing.TB, sources []*image.YCbCr, frameFlags []uint32, extraArgs ...string) [][]byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 frame-flags source", sources)
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 frame-flags has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	raw := appendSourcesI420(nil, sources)
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, len(sources), frameFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	return RequireIVFPackets(t, ivf, len(sources))
}

func VpxencFrameFlagTracePackets(t testing.TB, sources []*image.YCbCr, frameFlags []uint32, extraArgs ...string) ([]RateTraceRow, [][]byte) {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 frame-flags trace source", sources)
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 frame-flags trace has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	raw := appendSourcesI420(nil, sources)
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), frameFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags trace failed: %v\n%s", err, diag)
	}
	rows := ParseRateTraceRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	return rows, vpxencPacketsForTraceRows(t, "libvpx VP9", ivf, rows, true)
}

func VpxencVariableFrameFlagTracePackets(t testing.TB, sources []*image.YCbCr,
	frameFlags []uint32, invisibleFrames []bool, extraArgs ...string,
) ([]RateTraceRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 variable-size frame-flags trace source")
	}
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 variable-size frame-flags trace has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	if len(invisibleFrames) > len(sources) {
		t.Fatalf("VP9 invisible frame schedule has %d entries for %d source frames",
			len(invisibleFrames), len(sources))
	}
	frameSizes := make([]coracle.VpxencVP9FrameSize, len(sources))
	var raw []byte
	for i, src := range sources {
		frameSizes[i] = coracle.VpxencVP9FrameSize{
			Width:  src.Rect.Dx(),
			Height: src.Rect.Dy(),
		}
		raw = AppendI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420WithFrameSizes(
		raw, frameSizes, frameFlags, invisibleFrames, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags variable trace failed: %v\n%s", err, diag)
	}
	rows := ParseRateTraceRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 variable trace rows = %d, want %d",
			len(rows), len(sources))
	}
	return rows, vpxencPacketsForTraceRows(t, "libvpx VP9 variable", ivf,
		rows, false)
}

// VpxencTwoPassIVF runs the pinned VP9 vpxenc binary through pass 1 and pass 2.
func VpxencTwoPassIVF(t testing.TB, sources []*image.YCbCr, extraArgs ...string) []byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 two-pass source", sources)
	raw := appendSourcesI420(nil, sources)
	ivf, diag, err := coracle.VpxencVP9TwoPassEncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 two-pass encode failed: %v\n%s", err, diag)
	}
	return ivf
}

// VpxencTwoPassPackets returns the pass-2 IVF payloads from VpxencTwoPassIVF.
func VpxencTwoPassPackets(t testing.TB, sources []*image.YCbCr, extraArgs ...string) [][]byte {
	t.Helper()
	ivf := VpxencTwoPassIVF(t, sources, extraArgs...)
	return ParseIVFFrames(t, ivf)
}

// VpxencFrameFlagCopyReferenceLog runs the VP9 frame-flags helper and returns
// the generated copy-reference log path.
func VpxencFrameFlagCopyReferenceLog(t testing.TB, name string,
	sources []*image.YCbCr, frameFlags []uint32, controlScript []string,
	extraArgs ...string,
) string {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 copy-reference source", sources)
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 copy-reference frame flags has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	logPath := filepath.Join(t.TempDir(), name+".log")
	args := append([]string(nil), extraArgs...)
	args = append(args, "--copy-ref-log="+logPath)
	if len(controlScript) != 0 {
		args = append(args, "--control-script="+strings.Join(controlScript, ","))
	}
	raw := appendSourcesI420(nil, sources)
	if _, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, len(sources), frameFlags, args...); err != nil {
		t.Fatalf("vpxenc-vp9-frameflags copy-reference failed: %v\n%s",
			err, diag)
	}
	return logPath
}

// SpatialSVCPackets runs libvpx's VP9 spatial-SVC sample encoder and returns
// one payload per access unit.
func SpatialSVCPackets(t testing.TB, raw []byte, cfg SpatialSVCConfig) [][]byte {
	t.Helper()
	if cfg.Timebase == "" {
		cfg.Timebase = "1/30"
	}
	packets, diag, err := coracle.VP9SpatialSVCPayloadsI420(raw,
		coracle.VP9SpatialSVCConfig{
			Width:                    cfg.Width,
			Height:                   cfg.Height,
			Frames:                   cfg.Frames,
			Timebase:                 cfg.Timebase,
			TotalBitrateKbps:         cfg.TotalBitrateKbps,
			LayerCount:               cfg.LayerCount,
			ScaleFactors:             cfg.ScaleFactors,
			LayerBitratesKbps:        cfg.LayerBitratesKbps,
			TemporalLayerCount:       cfg.TemporalLayerCount,
			TemporalLayeringMode:     cfg.TemporalLayeringMode,
			KeyFrameInterval:         cfg.KeyFrameInterval,
			MinQuantizer:             cfg.MinQuantizer,
			MaxQuantizer:             cfg.MaxQuantizer,
			LagInFrames:              cfg.LagInFrames,
			Threads:                  cfg.Threads,
			Speed:                    cfg.Speed,
			RateControlEndUsage:      cfg.RateControlEndUsage,
			InterLayerPredictionMode: cfg.InterLayerPredictionMode,
		})
	if err != nil {
		if errors.Is(err, coracle.ErrVP9SpatialSVCEncoderNotBuilt) {
			t.Skip("set GOVPX_VP9_SPATIAL_SVC_ENCODER to a libvpx v1.16.0 vp9_spatial_svc_encoder binary")
		}
		t.Fatalf("VP9SpatialSVCPayloadsI420: %v\n%s", err, diag)
	}
	return packets
}

func appendSourcesI420(dst []byte, sources []*image.YCbCr) []byte {
	for _, src := range sources {
		dst = AppendI420(dst, src)
	}
	return dst
}

func vpxencPacketsForTraceRows(t testing.TB, label string, ivf []byte,
	rows []RateTraceRow, allowDropped bool,
) [][]byte {
	t.Helper()
	packets := make([][]byte, len(rows))
	wantPackets := 0
	for i, row := range rows {
		if row.Dropped {
			if !allowDropped {
				t.Fatalf("%s trace row %d unexpectedly dropped", label, i)
			}
			continue
		}
		wantPackets++
	}
	gotPackets, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if gotPackets != wantPackets {
		t.Fatalf("%s IVF packets = %d, want %d", label, gotPackets, wantPackets)
	}
	if wantPackets == 0 {
		return packets
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	packetIndex := 0
	for i, row := range rows {
		if row.Dropped {
			continue
		}
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, packetIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", packetIndex, err)
		}
		packets[i] = append([]byte(nil), frame.Data...)
		packetIndex++
	}
	return packets
}

func requireSameSizeSources(t testing.TB, label string, sources []*image.YCbCr) (width, height int) {
	t.Helper()
	width, height, err := sameSizeSources(label, sources)
	if err != nil {
		t.Fatal(err)
	}
	return width, height
}

func sameSizeSources(label string, sources []*image.YCbCr) (width, height int, err error) {
	if len(sources) == 0 {
		return 0, 0, fmt.Errorf("empty %s", label)
	}
	width = sources[0].Rect.Dx()
	height = sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			return 0, 0, fmt.Errorf("%s %d dimension mismatch: got %dx%d want %dx%d",
				label, i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	return width, height, nil
}
