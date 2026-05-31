//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9OracleSpatialSVCParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 spatial SVC oracle trace")

	const frames = 4
	for _, tc := range []struct {
		name       string
		layerCount int
		widths     [VP9MaxSpatialLayers]int
		heights    [VP9MaxSpatialLayers]int
		bitrates   [VP9MaxSpatialLayers]int
		temporal   TemporalScalabilityConfig
	}{
		{
			name:       "spatial-only",
			layerCount: 2,
			widths:     [VP9MaxSpatialLayers]int{32, 64},
			heights:    [VP9MaxSpatialLayers]int{32, 64},
			bitrates:   [VP9MaxSpatialLayers]int{300, 700},
		},
		{
			name:       "spatial-temporal-two-layer",
			layerCount: 2,
			widths:     [VP9MaxSpatialLayers]int{32, 64},
			heights:    [VP9MaxSpatialLayers]int{32, 64},
			bitrates:   [VP9MaxSpatialLayers]int{300, 700},
			temporal: TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			},
		},
		{
			name:       "spatial-only-three-layers",
			layerCount: 3,
			widths:     [VP9MaxSpatialLayers]int{32, 64, 128},
			heights:    [VP9MaxSpatialLayers]int{32, 64, 128},
			bitrates:   [VP9MaxSpatialLayers]int{200, 500, 1000},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sources [VP9MaxSpatialLayers][]*image.YCbCr
			for layer := 0; layer < tc.layerCount; layer++ {
				sources[layer] = make([]*image.YCbCr, frames)
			}
			var raw []byte
			for frame := 0; frame < frames; frame++ {
				y := uint8(64 + frame*13)
				u := uint8(120 + frame)
				v := uint8(136 - frame)
				for layer := 0; layer < tc.layerCount; layer++ {
					sources[layer][frame] = vp9test.NewYCbCr(
						tc.widths[layer], tc.heights[layer], y, u, v)
				}
				raw = vp9test.AppendI420(raw,
					sources[tc.layerCount-1][frame])
			}

			govpxFrames := encodeGovpxVP9SpatialSVCOracle(t, sources,
				tc.layerCount, tc.widths, tc.heights, tc.bitrates,
				tc.temporal)
			libvpxPackets := encodeLibvpxVP9SpatialSVCOracle(t, raw,
				tc.layerCount, tc.widths, tc.heights, tc.bitrates, frames,
				tc.temporal)
			if len(libvpxPackets) != frames {
				t.Fatalf("libvpx spatial SVC packets = %d, want %d",
					len(libvpxPackets), frames)
			}

			matches := 0
			firstMismatch := -1
			var rows strings.Builder
			fmt.Fprintln(&rows, "frame,match,first_diff,govpx_bytes,libvpx_bytes,govpx_layers,libvpx_layers,govpx_layer_bytes,libvpx_layer_bytes,govpx_tl,expected_tl,govpx_tl0picidx,govpx_refresh,libvpx_refresh,govpx_q,libvpx_q,govpx_key,libvpx_key")
			for frame := 0; frame < frames; frame++ {
				govpxPacket := govpxFrames[frame].data
				govpxResult := govpxFrames[frame].result
				govpxSF := parseVP9SpatialSVCOracleSuperframe(t, "govpx",
					frame, govpxPacket)
				libvpxSF := parseVP9SpatialSVCOracleSuperframe(t, "libvpx",
					frame, libvpxPackets[frame])
				if govpxSF.Count != tc.layerCount ||
					libvpxSF.Count != tc.layerCount {
					t.Fatalf("frame %d layer counts = govpx:%d libvpx:%d, want %d/%d",
						frame, govpxSF.Count, libvpxSF.Count,
						tc.layerCount, tc.layerCount)
				}
				var govpxLayerBytes, libvpxLayerBytes [VP9MaxSpatialLayers]int
				var govpxRefresh, libvpxRefresh [VP9MaxSpatialLayers]uint8
				var govpxQ, libvpxQ [VP9MaxSpatialLayers]int
				var govpxKey, libvpxKey [VP9MaxSpatialLayers]bool
				for layer := 0; layer < tc.layerCount; layer++ {
					refW := tc.widths[layer]
					refH := tc.heights[layer]
					govpxHeader := readVP9SpatialSVCOracleHeader(t, "govpx",
						frame, layer, govpxSF.Frames[layer], refW, refH)
					libvpxHeader := readVP9SpatialSVCOracleHeader(t, "libvpx",
						frame, layer, libvpxSF.Frames[layer], refW, refH)
					assertVP9SpatialSVCOracleLayerDimensions(t, "govpx",
						frame, layer, govpxHeader, tc.widths[layer],
						tc.heights[layer])
					assertVP9SpatialSVCOracleLayerDimensions(t, "libvpx",
						frame, layer, libvpxHeader, tc.widths[layer],
						tc.heights[layer])
					assertVP9SpatialSVCOracleHeaderParity(t, frame,
						fmt.Sprintf("layer%d", layer), govpxHeader,
						libvpxHeader)
					govpxLayerBytes[layer] = len(govpxSF.Frames[layer])
					libvpxLayerBytes[layer] = len(libvpxSF.Frames[layer])
					govpxRefresh[layer] = govpxHeader.RefreshFrameFlags
					libvpxRefresh[layer] = libvpxHeader.RefreshFrameFlags
					govpxQ[layer] = int(govpxHeader.Quant.BaseQindex)
					libvpxQ[layer] = int(libvpxHeader.Quant.BaseQindex)
					govpxKey[layer] = govpxHeader.FrameType == common.KeyFrame
					libvpxKey[layer] = libvpxHeader.FrameType == common.KeyFrame
				}
				assertVP9SpatialSVCOracleTemporal(t, frame, tc.temporal,
					govpxResult)

				match := bytes.Equal(govpxPacket, libvpxPackets[frame])
				if match {
					matches++
				} else if firstMismatch < 0 {
					firstMismatch = frame
				}
				firstDiff := testutil.FirstByteDiff(govpxPacket,
					libvpxPackets[frame])
				fmt.Fprintf(&rows, "%d,%t,%d,%d,%d,%d,%d,%s,%s,%d,%d,%d,%s,%s,%s,%s,%s,%s\n",
					frame, match, firstDiff, len(govpxPacket),
					len(libvpxPackets[frame]), govpxSF.Count, libvpxSF.Count,
					vp9SpatialSVCOracleJoinInts(
						govpxLayerBytes[:tc.layerCount]),
					vp9SpatialSVCOracleJoinInts(
						libvpxLayerBytes[:tc.layerCount]),
					govpxResult.Layers[0].TemporalLayerID,
					vp9SpatialSVCOracleExpectedTemporalLayer(t,
						tc.temporal, frame),
					govpxResult.Layers[0].TL0PICIDX,
					vp9SpatialSVCOracleJoinHexBytes(
						govpxRefresh[:tc.layerCount]),
					vp9SpatialSVCOracleJoinHexBytes(
						libvpxRefresh[:tc.layerCount]),
					vp9SpatialSVCOracleJoinInts(govpxQ[:tc.layerCount]),
					vp9SpatialSVCOracleJoinInts(libvpxQ[:tc.layerCount]),
					vp9SpatialSVCOracleJoinBools(govpxKey[:tc.layerCount]),
					vp9SpatialSVCOracleJoinBools(libvpxKey[:tc.layerCount]))
			}
			t.Logf("VP9 spatial SVC oracle trace: matches=%d/%d first_mismatch=%d",
				matches, frames, firstMismatch)
			t.Logf("VP9 spatial SVC oracle rows:\n%s", rows.String())
			if vp9test.StrictEnv("GOVPX_VP9_SPATIAL_SVC_BYTE_STRICT") &&
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
	sources [VP9MaxSpatialLayers][]*image.YCbCr,
	layerCount int,
	widths, heights, bitrates [VP9MaxSpatialLayers]int,
	temporal TemporalScalabilityConfig,
) []vp9SpatialSVCOracleGovpxFrame {
	t.Helper()
	if layerCount < 2 || layerCount > VP9MaxSpatialLayers {
		t.Fatalf("govpx spatial SVC layerCount = %d", layerCount)
	}
	frameCount := len(sources[0])
	for layer := 0; layer < layerCount; layer++ {
		if len(sources[layer]) != frameCount {
			t.Fatalf("govpx spatial SVC source counts layer0:%d layer%d:%d",
				frameCount, layer, len(sources[layer]))
		}
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
	var layerOpts [VP9MaxSpatialLayers]VP9EncoderOptions
	for layer := 0; layer < layerCount; layer++ {
		layerOpts[layer] = cbrLayer(widths[layer], heights[layer],
			bitrates[layer])
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           uint8(layerCount),
		InterLayerPrediction: true,
		Layers:               layerOpts,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	dst := make([]byte, 1<<22)
	frames := make([]vp9SpatialSVCOracleGovpxFrame, frameCount)
	for frame := 0; frame < frameCount; frame++ {
		var srcs [VP9MaxSpatialLayers]*image.YCbCr
		for layer := 0; layer < layerCount; layer++ {
			srcs[layer] = sources[layer][frame]
		}
		result, err := svc.EncodeIntoWithResult(srcs[:layerCount], dst)
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

func encodeLibvpxVP9SpatialSVCOracle(t *testing.T,
	raw []byte,
	layerCount int,
	widths, heights, bitrates [VP9MaxSpatialLayers]int,
	frames int,
	temporal TemporalScalabilityConfig,
) [][]byte {
	t.Helper()
	temporalLayerCount, temporalLayeringMode := vp9SpatialSVCOracleTemporalConfig(t,
		temporal)
	return vp9test.SpatialSVCPackets(t, raw, vp9test.SpatialSVCConfig{
		Width:                    widths[layerCount-1],
		Height:                   heights[layerCount-1],
		Frames:                   frames,
		TotalBitrateKbps:         vp9SpatialSVCOracleTotalBitrate(bitrates, layerCount),
		LayerCount:               layerCount,
		ScaleFactors:             vp9SpatialSVCOracleScaleFactors(t, widths, heights, layerCount),
		LayerBitratesKbps:        vp9SpatialSVCOracleLayerBitrates(t, temporal, bitrates, layerCount),
		TemporalLayerCount:       temporalLayerCount,
		TemporalLayeringMode:     temporalLayeringMode,
		KeyFrameInterval:         128,
		MinQuantizer:             4,
		MaxQuantizer:             56,
		LagInFrames:              0,
		Threads:                  1,
		Speed:                    8,
		RateControlEndUsage:      1,
		InterLayerPredictionMode: 0,
	})
}

func vp9SpatialSVCOracleTemporalConfig(t *testing.T,
	cfg TemporalScalabilityConfig,
) (layerCount int, layeringMode int) {
	t.Helper()
	if !cfg.Enabled {
		return 0, 0
	}
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
	}
	mode, err := vp9SpatialSVCOracleTemporalLayeringMode(cfg.Mode)
	if err != nil {
		t.Fatalf("libvpx spatial SVC temporal mode %d: %v", cfg.Mode, err)
	}
	return pattern.Layers, mode
}

func vp9SpatialSVCOracleTemporalLayeringMode(
	mode TemporalLayeringMode,
) (int, error) {
	switch mode {
	case TemporalLayeringTwoLayers:
		return 2, nil
	case TemporalLayeringThreeLayers:
		return 3, nil
	default:
		return 0, ErrInvalidConfig
	}
}

func vp9SpatialSVCOracleLayerBitrates(t *testing.T,
	cfg TemporalScalabilityConfig,
	bitrates [VP9MaxSpatialLayers]int,
	layerCount int,
) []int {
	t.Helper()
	if !cfg.Enabled {
		return append([]int(nil), bitrates[:layerCount]...)
	}
	pattern, ok := temporalLayeringPattern(cfg.Mode)
	if !ok {
		t.Fatalf("temporalLayeringPattern(%d) failed", cfg.Mode)
	}
	values := make([]int, 0, pattern.Layers*layerCount)
	for spatial := 0; spatial < layerCount; spatial++ {
		normalized, _, err := normalizeTemporalBitrates(cfg, pattern.Layers,
			bitrates[spatial])
		if err != nil {
			t.Fatalf("layer %d normalizeTemporalBitrates(%d): %v",
				spatial, cfg.Mode, err)
		}
		for layer := 0; layer < pattern.Layers; layer++ {
			values = append(values, normalized.LayerTargetBitrateKbps[layer])
		}
	}
	return values
}

func vp9SpatialSVCOracleTotalBitrate(
	bitrates [VP9MaxSpatialLayers]int,
	layerCount int,
) int {
	total := 0
	for layer := 0; layer < layerCount; layer++ {
		total += bitrates[layer]
	}
	return total
}

func vp9SpatialSVCOracleScaleFactors(t *testing.T,
	widths, heights [VP9MaxSpatialLayers]int,
	layerCount int,
) string {
	t.Helper()
	topW := widths[layerCount-1]
	topH := heights[layerCount-1]
	values := make([]string, 0, layerCount)
	for layer := 0; layer < layerCount; layer++ {
		if widths[layer]*topH != heights[layer]*topW {
			t.Fatalf("layer %d scale is not uniform: %dx%d top=%dx%d",
				layer, widths[layer], heights[layer], topW, topH)
		}
		divisor := gcdInt(widths[layer], topW)
		values = append(values, fmt.Sprintf("%d/%d",
			widths[layer]/divisor, topW/divisor))
	}
	return strings.Join(values, ",")
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func vp9SpatialSVCOracleJoinInts(values []int) string {
	var out strings.Builder
	for i, value := range values {
		if i > 0 {
			out.WriteByte('|')
		}
		fmt.Fprint(&out, value)
	}
	return out.String()
}

func vp9SpatialSVCOracleJoinHexBytes(values []uint8) string {
	var out strings.Builder
	for i, value := range values {
		if i > 0 {
			out.WriteByte('|')
		}
		fmt.Fprintf(&out, "%#02x", value)
	}
	return out.String()
}

func vp9SpatialSVCOracleJoinBools(values []bool) string {
	var out strings.Builder
	for i, value := range values {
		if i > 0 {
			out.WriteByte('|')
		}
		fmt.Fprint(&out, value)
	}
	return out.String()
}

func parseVP9SpatialSVCOracleSuperframe(t *testing.T, side string, frame int,
	packet []byte,
) bitstream.SuperframeIndex {
	t.Helper()
	sf, err := bitstream.ParseSuperframe(packet)
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

func assertVP9SpatialSVCOracleLayerDimensions(t *testing.T,
	side string, frame int, layer int,
	header vp9dec.UncompressedHeader, width int, height int,
) {
	t.Helper()
	if header.Width != uint32(width) || header.Height != uint32(height) ||
		!header.ShowFrame {
		t.Fatalf("%s frame %d layer %d header = %+v, want visible %dx%d",
			side, frame, layer, header, width, height)
	}
}

func assertVP9SpatialSVCOracleHeaderParity(t *testing.T, frame int,
	layer string, govpx, libvpx vp9dec.UncompressedHeader,
) {
	t.Helper()
	if govpx.FrameType != libvpx.FrameType ||
		govpx.ShowFrame != libvpx.ShowFrame ||
		govpx.RefreshFrameFlags != libvpx.RefreshFrameFlags {
		t.Fatalf("frame %d %s header parity = govpx type:%d show:%t refresh:%#02x libvpx type:%d show:%t refresh:%#02x",
			frame, layer,
			govpx.FrameType, govpx.ShowFrame, govpx.RefreshFrameFlags,
			libvpx.FrameType, libvpx.ShowFrame, libvpx.RefreshFrameFlags)
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
