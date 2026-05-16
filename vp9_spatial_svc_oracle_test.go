//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9OracleSpatialSVCScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 spatial SVC oracle scoreboard")
	}
	spatialSVC := findVP9SpatialSVCEncoder(t)

	const frames = 4
	const baseW, baseH = 32, 32
	const topW, topH = 64, 64
	for _, tc := range []struct {
		name     string
		temporal TemporalScalabilityConfig
	}{
		{name: "spatial-only"},
		{
			name: "spatial-temporal-two-layer",
			temporal: TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourcesTop := make([]*image.YCbCr, frames)
			sourcesBase := make([]*image.YCbCr, frames)
			var raw []byte
			for frame := 0; frame < frames; frame++ {
				y := uint8(64 + frame*13)
				u := uint8(120 + frame)
				v := uint8(136 - frame)
				sourcesTop[frame] = newVP9YCbCrForTest(topW, topH, y, u, v)
				sourcesBase[frame] = newVP9YCbCrForTest(baseW, baseH, y, u, v)
				raw = appendVP9YCbCrI420(raw, sourcesTop[frame])
			}

			govpxFrames := encodeGovpxVP9SpatialSVCOracle(t, sourcesBase,
				sourcesTop, tc.temporal)
			libvpxPackets := encodeLibvpxVP9SpatialSVCOracle(t, spatialSVC,
				raw, topW, topH, frames, tc.temporal)
			if len(libvpxPackets) != frames {
				t.Fatalf("libvpx spatial SVC packets = %d, want %d",
					len(libvpxPackets), frames)
			}

			matches := 0
			firstMismatch := -1
			var rows strings.Builder
			fmt.Fprintln(&rows, "frame,match,first_diff,govpx_bytes,libvpx_bytes,govpx_layers,libvpx_layers,govpx_layer0,libvpx_layer0,govpx_layer1,libvpx_layer1,govpx_tl,expected_tl,govpx_tl0picidx,govpx_base_refresh,libvpx_base_refresh,govpx_top_refresh,libvpx_top_refresh,govpx_base_q,libvpx_base_q,govpx_top_q,libvpx_top_q,govpx_base_key,libvpx_base_key,govpx_top_key,libvpx_top_key")
			for frame := 0; frame < frames; frame++ {
				govpxPacket := govpxFrames[frame].data
				govpxResult := govpxFrames[frame].result
				govpxSF := parseVP9SpatialSVCOracleSuperframe(t, "govpx",
					frame, govpxPacket)
				libvpxSF := parseVP9SpatialSVCOracleSuperframe(t, "libvpx",
					frame, libvpxPackets[frame])
				if govpxSF.count != 2 || libvpxSF.count != 2 {
					t.Fatalf("frame %d layer counts = govpx:%d libvpx:%d, want 2/2",
						frame, govpxSF.count, libvpxSF.count)
				}
				govpxBase := readVP9SpatialSVCOracleHeader(t, "govpx",
					frame, 0, govpxSF.frames[0], baseW, baseH)
				govpxTop := readVP9SpatialSVCOracleHeader(t, "govpx",
					frame, 1, govpxSF.frames[1], topW, topH)
				libvpxBase := readVP9SpatialSVCOracleHeader(t, "libvpx",
					frame, 0, libvpxSF.frames[0], baseW, baseH)
				libvpxTop := readVP9SpatialSVCOracleHeader(t, "libvpx",
					frame, 1, libvpxSF.frames[1], topW, topH)
				assertVP9SpatialSVCOracleDimensions(t, "govpx", frame,
					govpxBase, govpxTop)
				assertVP9SpatialSVCOracleDimensions(t, "libvpx", frame,
					libvpxBase, libvpxTop)
				assertVP9SpatialSVCOracleTemporal(t, frame, tc.temporal,
					govpxResult)

				match := bytes.Equal(govpxPacket, libvpxPackets[frame])
				if match {
					matches++
				} else if firstMismatch < 0 {
					firstMismatch = frame
				}
				firstDiff := firstByteDiff(govpxPacket, libvpxPackets[frame])
				fmt.Fprintf(&rows, "%d,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%#02x,%#02x,%#02x,%#02x,%d,%d,%d,%d,%t,%t,%t,%t\n",
					frame, match, firstDiff, len(govpxPacket),
					len(libvpxPackets[frame]), govpxSF.count, libvpxSF.count,
					len(govpxSF.frames[0]), len(libvpxSF.frames[0]),
					len(govpxSF.frames[1]), len(libvpxSF.frames[1]),
					govpxResult.Layers[0].TemporalLayerID,
					vp9SpatialSVCOracleExpectedTemporalLayer(t,
						tc.temporal, frame),
					govpxResult.Layers[0].TL0PICIDX,
					govpxBase.RefreshFrameFlags,
					libvpxBase.RefreshFrameFlags,
					govpxTop.RefreshFrameFlags,
					libvpxTop.RefreshFrameFlags,
					govpxBase.Quant.BaseQindex,
					libvpxBase.Quant.BaseQindex,
					govpxTop.Quant.BaseQindex,
					libvpxTop.Quant.BaseQindex,
					govpxBase.FrameType == common.KeyFrame,
					libvpxBase.FrameType == common.KeyFrame,
					govpxTop.FrameType == common.KeyFrame,
					libvpxTop.FrameType == common.KeyFrame)
			}
			t.Logf("VP9 spatial SVC oracle scoreboard: matches=%d/%d first_mismatch=%d",
				matches, frames, firstMismatch)
			t.Logf("VP9 spatial SVC oracle rows:\n%s", rows.String())
			if os.Getenv("GOVPX_VP9_SPATIAL_SVC_BYTE_STRICT") == "1" &&
				matches != frames {
				t.Fatalf("strict VP9 spatial SVC byte parity: matches=%d/%d",
					matches, frames)
			}
		})
	}
}

type vp9SpatialSVCOracleGovpxFrame struct {
	data   []byte
	result VP9SpatialSVCEncodeResult
}

func encodeGovpxVP9SpatialSVCOracle(t *testing.T,
	sourcesBase, sourcesTop []*image.YCbCr,
	temporal TemporalScalabilityConfig,
) []vp9SpatialSVCOracleGovpxFrame {
	t.Helper()
	if len(sourcesBase) != len(sourcesTop) {
		t.Fatalf("govpx spatial SVC source counts = %d/%d",
			len(sourcesBase), len(sourcesTop))
	}
	cbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		return VP9EncoderOptions{
			Width:               width,
			Height:              height,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   kbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			TemporalScalability: temporal,
		}
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			cbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	frames := make([]vp9SpatialSVCOracleGovpxFrame, len(sourcesBase))
	for frame := range sourcesBase {
		result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
			sourcesBase[frame],
			sourcesTop[frame],
		}, dst)
		if err != nil {
			t.Fatalf("govpx EncodeIntoWithResult[%d]: %v", frame, err)
		}
		packet := append([]byte(nil), result.Data...)
		result.Data = nil
		for layer := 0; layer < int(result.LayerCount); layer++ {
			result.Layers[layer].Data = nil
		}
		frames[frame] = vp9SpatialSVCOracleGovpxFrame{
			data:   packet,
			result: result,
		}
	}
	return frames
}

func encodeLibvpxVP9SpatialSVCOracle(t *testing.T, spatialSVC string,
	raw []byte, width, height, frames int,
	temporal TemporalScalabilityConfig,
) [][]byte {
	t.Helper()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "spatial.ivf")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", inPath, err)
	}
	bitrates := "300,700"
	temporalArgs := vp9SpatialSVCOracleTemporalArgs(t, temporal)
	if temporal.Enabled {
		bitrates = vp9SpatialSVCOracleLayerBitrates(t, temporal)
	}
	args := []string{
		"-f", fmt.Sprint(frames),
		"-w", fmt.Sprint(width),
		"-h", fmt.Sprint(height),
		"-t", "1/30",
		"-b", "1000",
		"-sl", "2",
		"-r", "1/2,1/1",
	}
	args = append(args, temporalArgs...)
	args = append(args,
		"-bl", bitrates,
		"-k", "128",
		"--min-q=4,4",
		"--max-q=56,56",
		"--lag-in-frames=0",
		"-th", "1",
		"-sp", "8",
		"--rc-end-usage=1",
		"--inter-layer-pred=0",
		inPath,
		"-o", outPath,
	)
	out, err := exec.Command(spatialSVC, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("vp9_spatial_svc_encoder failed: %v\n%s", err, out)
	}
	ivf, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read %s: %v", outPath, err)
	}
	return parseIVFFramePayloads(t, ivf)
}

func vp9SpatialSVCOracleTemporalArgs(t *testing.T,
	cfg TemporalScalabilityConfig,
) []string {
	t.Helper()
	if !cfg.Enabled {
		return nil
	}
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
	}
	mode, err := vp9SpatialSVCOracleTemporalLayeringMode(cfg.Mode)
	if err != nil {
		t.Fatalf("libvpx spatial SVC temporal mode %d: %v", cfg.Mode, err)
	}
	return []string{
		"-tl", fmt.Sprint(pattern.Layers),
		"-tlm", mode,
	}
}

func vp9SpatialSVCOracleTemporalLayeringMode(
	mode TemporalLayeringMode,
) (string, error) {
	switch mode {
	case TemporalLayeringTwoLayers:
		return "2", nil
	case TemporalLayeringThreeLayers:
		return "3", nil
	default:
		return "", ErrInvalidConfig
	}
}

func vp9SpatialSVCOracleLayerBitrates(t *testing.T,
	cfg TemporalScalabilityConfig,
) string {
	t.Helper()
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
	}
	base, _, err := normalizeTemporalBitrates(cfg, pattern.Layers, 300)
	if err != nil {
		t.Fatalf("base normalizeTemporalBitrates(%d): %v", cfg.Mode, err)
	}
	top, _, err := normalizeTemporalBitrates(cfg, pattern.Layers, 700)
	if err != nil {
		t.Fatalf("top normalizeTemporalBitrates(%d): %v", cfg.Mode, err)
	}
	values := make([]int, 0, pattern.Layers*2)
	for layer := 0; layer < pattern.Layers; layer++ {
		values = append(values, base.LayerTargetBitrateKbps[layer])
	}
	for layer := 0; layer < pattern.Layers; layer++ {
		values = append(values, top.LayerTargetBitrateKbps[layer])
	}
	return vp9OracleIntCSV(values)
}

func findVP9SpatialSVCEncoder(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("GOVPX_VP9_SPATIAL_SVC_ENCODER"); path != "" {
		return path
	}
	if path, err := exec.LookPath("vp9_spatial_svc_encoder"); err == nil {
		return path
	}
	candidates := []string{
		filepath.Join("internal", "coracle", "build", "vp9_spatial_svc_encoder"),
		filepath.Join("internal", "coracle", "build", "libvpx-v1.16.0-vpxdec-vp9", "examples", "vp9_spatial_svc_encoder"),
		filepath.Join("internal", "coracle", "build", "libvpx-v1.16.0-vpxenc", "examples", "vp9_spatial_svc_encoder"),
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() &&
			st.Mode()&0o111 != 0 {
			return path
		}
	}
	t.Skip("set GOVPX_VP9_SPATIAL_SVC_ENCODER to a libvpx v1.16.0 vp9_spatial_svc_encoder binary")
	return ""
}

func parseVP9SpatialSVCOracleSuperframe(t *testing.T, side string, frame int,
	packet []byte,
) vp9SuperframeIndex {
	t.Helper()
	sf, err := vp9ParseSuperframe(packet)
	if err != nil {
		t.Fatalf("%s frame %d superframe parse: %v", side, frame, err)
	}
	return sf
}

func readVP9SpatialSVCOracleHeader(t *testing.T, side string, frame, layer int,
	packet []byte, refWidth, refHeight int,
) vp9dec.UncompressedHeader {
	t.Helper()
	var br vp9dec.BitReader
	br.Init(packet)
	header, err := vp9dec.ReadUncompressedHeader(&br, nil,
		func(uint8) (uint32, uint32) {
			return uint32(refWidth), uint32(refHeight)
		})
	if err != nil {
		t.Fatalf("%s frame %d layer %d ReadUncompressedHeader: %v",
			side, frame, layer, err)
	}
	return header
}

func assertVP9SpatialSVCOracleDimensions(t *testing.T, side string, frame int,
	base, top vp9dec.UncompressedHeader,
) {
	t.Helper()
	if base.Width != 32 || base.Height != 32 || !base.ShowFrame {
		t.Fatalf("%s frame %d base header = %+v, want visible 32x32",
			side, frame, base)
	}
	if top.Width != 64 || top.Height != 64 || !top.ShowFrame {
		t.Fatalf("%s frame %d top header = %+v, want visible 64x64",
			side, frame, top)
	}
}

func assertVP9SpatialSVCOracleTemporal(t *testing.T, frame int,
	cfg TemporalScalabilityConfig, result VP9SpatialSVCEncodeResult,
) {
	t.Helper()
	wantLayer := vp9SpatialSVCOracleExpectedTemporalLayer(t, cfg, frame)
	wantLayers := 1
	if cfg.Enabled {
		pattern, ok := temporalLayeringPattern(cfg.Mode)
		if !ok {
			t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
		}
		wantLayers = pattern.Layers
	}
	for layer := 0; layer < int(result.LayerCount); layer++ {
		got := result.Layers[layer]
		if got.TemporalLayerID != wantLayer ||
			got.TemporalLayerCount != wantLayers {
			t.Fatalf("frame %d spatial %d temporal = id:%d count:%d, want %d/%d",
				frame, layer, got.TemporalLayerID, got.TemporalLayerCount,
				wantLayer, wantLayers)
		}
	}
}

func vp9SpatialSVCOracleExpectedTemporalLayer(t *testing.T,
	cfg TemporalScalabilityConfig, frame int,
) int {
	t.Helper()
	if !cfg.Enabled {
		return 0
	}
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok || pattern.Periodicity <= 0 {
		t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
	}
	return pattern.LayerID[frame%pattern.Periodicity]
}
