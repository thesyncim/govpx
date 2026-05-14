package govpx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestOracleLibvpxChecksumMatchesEncodeIntoInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 2 {
		t.Fatalf("oracle frame count = %d, want 2", len(oracleFrames))
	}
	want := []testutil.FrameChecksum{
		checksumFrame(0, true, true, reconstructed),
		checksumFrame(1, false, true, reconstructed),
	}
	for i := range want {
		if !testutil.SameFrameChecksum(oracleFrames[i], want[i]) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want[i]))
		}
	}
}

func TestOracleLibvpxChecksumMatchesTemporalBaseLayer(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	for _, tc := range temporalOracleTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TemporalScalabilityConfig{
				Enabled:                true,
				Mode:                   tc.mode,
				LayerTargetBitrateKbps: tc.bitrates,
			}
			e := newTemporalTestEncoder(t, cfg)
			packet := make([]byte, 8192)
			basePackets := make([][]byte, 0, tc.frameCount)
			baseKeyFrames := make([]bool, 0, tc.frameCount)
			for i := 0; i < tc.frameCount; i++ {
				src := rateControlTestFrame(16, 16, i)
				result, err := e.EncodeInto(packet, src, uint64(i), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", i, err)
				}
				if result.Dropped {
					t.Fatalf("EncodeInto %d dropped, want full temporal oracle sequence", i)
				}
				if result.TemporalLayerID == 0 {
					basePackets = append(basePackets, append([]byte(nil), result.Data...))
					baseKeyFrames = append(baseKeyFrames, result.KeyFrame)
				}
			}
			if len(basePackets) < 2 {
				t.Fatalf("base packet count = %d, want at least 2", len(basePackets))
			}

			govpxFrames := decodeFrameSequence(t, basePackets...)
			ivf := makeIVF(16, 16, 30, 1, basePackets)
			oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
			if len(oracleFrames) != len(govpxFrames) {
				t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
			}
			for i := range govpxFrames {
				want := checksumFrame(i, baseKeyFrames[i], true, govpxFrames[i])
				if !testutil.SameFrameChecksum(oracleFrames[i], want) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
				}
			}
		})
	}
}

func TestOracleLibvpxChecksumMatchesTemporalFullSequence(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	for _, tc := range temporalOracleTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TemporalScalabilityConfig{
				Enabled:                true,
				Mode:                   tc.mode,
				LayerTargetBitrateKbps: tc.bitrates,
			}
			e := newTemporalTestEncoder(t, cfg)
			packet := make([]byte, 8192)
			packets := make([][]byte, 0, tc.frameCount)
			keyFrames := make([]bool, 0, tc.frameCount)
			for i := 0; i < tc.frameCount; i++ {
				src := rateControlTestFrame(16, 16, i)
				result, err := e.EncodeInto(packet, src, uint64(i), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", i, err)
				}
				if result.Dropped {
					t.Fatalf("EncodeInto %d dropped, want full temporal oracle sequence", i)
				}
				packets = append(packets, append([]byte(nil), result.Data...))
				keyFrames = append(keyFrames, result.KeyFrame)
			}

			govpxFrames := decodeFrameSequence(t, packets...)
			ivf := makeIVF(16, 16, 30, 1, packets)
			oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
			if len(oracleFrames) != len(govpxFrames) {
				t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
			}
			for i := range govpxFrames {
				want := checksumFrame(i, keyFrames[i], true, govpxFrames[i])
				if !testutil.SameFrameChecksum(oracleFrames[i], want) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
				}
			}
		})
	}
}

func TestOracleLibvpxTemporalSVCExampleStreams(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx temporal SVC oracle tests")
	}
	oracle := findChecksumOracle(t)
	svcEncoder := findVpxTemporalSVCEncoder(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		frameCount = 8
	)
	sources := make([]Image, frameCount)
	for i := range sources {
		sources[i] = rateControlTestFrame(width, height, i)
	}
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "temporal-svc.yuv")
	writeEncoderValidationI420(t, yuvPath, sources)
	outputBase := filepath.Join(dir, "temporal-svc")
	cmd := exec.Command(svcEncoder,
		yuvPath, outputBase, "vp8",
		strconv.Itoa(width), strconv.Itoa(height),
		"1", strconv.Itoa(fps),
		"8", "0", "1", "1", "1",
		"720", "1200",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpx_temporal_svc_encoder failed: %v\n%s", err, out)
	}
	stats := parseTemporalSVCExampleLayerStats(t, string(out), 2)
	assertGovpxTemporalAccountingMatchesLibvpxExample(t, sources, stats)

	for layer := range 2 {
		ivfPath := fmt.Sprintf("%s_%d.ivf", outputBase, layer)
		ivf, err := os.ReadFile(ivfPath)
		if err != nil {
			t.Fatalf("ReadFile %s returned error: %v", ivfPath, err)
		}
		govpxChecksums := decodeIVFChecksums(t, ivf)
		libvpxChecksums := runLibvpxChecksumOracle(t, oracle, ivf)
		assertFrameChecksumsEqual(t, fmt.Sprintf("libvpx temporal SVC layer %d decoded by govpx", layer), govpxChecksums, libvpxChecksums)
		if layer == 0 && len(govpxChecksums) != frameCount/2 {
			t.Fatalf("base layer checksum count = %d, want %d", len(govpxChecksums), frameCount/2)
		}
		if layer == 1 && len(govpxChecksums) != frameCount {
			t.Fatalf("full layer checksum count = %d, want %d", len(govpxChecksums), frameCount)
		}
	}
}

type temporalSVCExampleLayerStats struct {
	InputFrames         int
	EncodedFrames       int
	TargetFrameSizeBits float64
	DroppedPct          float64
}

func parseTemporalSVCExampleLayerStats(t *testing.T, output string, layers int) []temporalSVCExampleLayerStats {
	t.Helper()
	stats := make([]temporalSVCExampleLayerStats, layers)
	seen := make([]bool, layers)
	currentLayer := -1
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		var layer int
		if _, err := fmt.Sscanf(line, "For layer#: %d", &layer); err == nil {
			currentLayer = layer
			continue
		}
		if currentLayer < 0 || currentLayer >= layers || !strings.HasPrefix(line, "Number of input frames, encoded") {
			if currentLayer >= 0 && currentLayer < layers && strings.HasPrefix(line, "Average frame size") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					t.Fatalf("malformed temporal SVC frame-size line: %q", line)
				}
				targetFrameSize, err := strconv.ParseFloat(fields[len(fields)-2], 64)
				if err != nil {
					t.Fatalf("parse target frame size from %q returned error: %v", line, err)
				}
				stats[currentLayer].TargetFrameSizeBits = targetFrameSize
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("malformed temporal SVC stats line: %q", line)
		}
		inputFrames, err := strconv.Atoi(fields[len(fields)-3])
		if err != nil {
			t.Fatalf("parse input frames from %q returned error: %v", line, err)
		}
		encodedFrames, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil {
			t.Fatalf("parse encoded frames from %q returned error: %v", line, err)
		}
		droppedPct, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			t.Fatalf("parse dropped pct from %q returned error: %v", line, err)
		}
		stats[currentLayer].InputFrames = inputFrames
		stats[currentLayer].EncodedFrames = encodedFrames
		stats[currentLayer].DroppedPct = droppedPct
		seen[currentLayer] = true
	}
	for layer := range layers {
		if !seen[layer] {
			t.Fatalf("temporal SVC output did not include layer %d stats:\n%s", layer, output)
		}
	}
	return stats
}

func assertGovpxTemporalAccountingMatchesLibvpxExample(t *testing.T, sources []Image, stats []temporalSVCExampleLayerStats) {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               sources[0].Width,
		Height:              sources[0].Height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -8,
		KeyFrameInterval:    3000,
		ErrorResilient:      true,
		TokenPartitions:     int(vp8common.TwoPartition),
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{720, 1200},
		},
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, sources[0].Width*sources[0].Height*3)
	sawLayerTarget := make([]bool, len(stats))
	for i, source := range sources {
		result, err := e.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto %d dropped, want full temporal SVC accounting sequence", i)
		}
		if !result.KeyFrame && result.TemporalLayerID >= 0 && result.TemporalLayerID < len(stats) {
			// The example's "Average frame size (target vs actual)" target is
			// layer_pfb: the non-cumulative layer budget divided by the layer's
			// effective frame rate. It is not this_frame_target, which includes
			// buffer adjustment, nor the cumulative layer buffer bandwidth.
			wantTarget := int(stats[result.TemporalLayerID].TargetFrameSizeBits + 0.5)
			gotTarget := e.temporal.temporalLayerFrameTargetBits(result.TemporalLayerID, e.timing)
			if gotTarget != wantTarget {
				t.Fatalf("frame %d layer %d temporal layer target bits = %d, want libvpx example %.0f", i, result.TemporalLayerID, gotTarget, stats[result.TemporalLayerID].TargetFrameSizeBits)
			}
			sawLayerTarget[result.TemporalLayerID] = true
		}
	}
	cumulativeTotal := 0
	for layer := range stats {
		cumulativeTotal += stats[layer].InputFrames
		accounting := e.temporal.accounting[layer]
		if accounting.InputFrames != stats[layer].InputFrames {
			t.Fatalf("layer %d input frames = %d, want libvpx example %d", layer, accounting.InputFrames, stats[layer].InputFrames)
		}
		if accounting.EncodedFrames != stats[layer].EncodedFrames {
			t.Fatalf("layer %d encoded non-key frames = %d, want libvpx example %d", layer, accounting.EncodedFrames, stats[layer].EncodedFrames)
		}
		if accounting.TotalEncodedFrames != cumulativeTotal {
			t.Fatalf("layer %d cumulative encoded frames = %d, want %d", layer, accounting.TotalEncodedFrames, cumulativeTotal)
		}
		if stats[layer].DroppedPct != 0 {
			t.Fatalf("layer %d libvpx example dropped pct = %.2f, want 0", layer, stats[layer].DroppedPct)
		}
		if stats[layer].EncodedFrames > 0 && !sawLayerTarget[layer] {
			t.Fatalf("layer %d had encoded frames but no govpx temporal target was checked", layer)
		}
	}
}

type temporalOracleCase struct {
	name       string
	mode       TemporalLayeringMode
	bitrates   [MaxTemporalLayers]int
	frameCount int
}

func temporalOracleTestCases() []temporalOracleCase {
	return []temporalOracleCase{
		{name: "one-layer", mode: TemporalLayeringOneLayer, frameCount: 6},
		{name: "two-layers", mode: TemporalLayeringTwoLayers, frameCount: 6},
		{name: "two-layers-three-frame", mode: TemporalLayeringTwoLayersThreeFrame, frameCount: 7},
		{name: "three-layers-six-frame", mode: TemporalLayeringThreeLayersSixFrame, frameCount: 13},
		{name: "three-layers-no-inter-layer-prediction", mode: TemporalLayeringThreeLayersNoInterLayerPrediction, frameCount: 9},
		{name: "three-layers-layer-one-prediction", mode: TemporalLayeringThreeLayersLayerOnePrediction, frameCount: 9},
		{name: "three-layers", mode: TemporalLayeringThreeLayers, frameCount: 9},
		{name: "five-layers", mode: TemporalLayeringFiveLayers, bitrates: [MaxTemporalLayers]int{200, 400, 700, 950, 1200}, frameCount: 18},
		{name: "two-layers-with-sync", mode: TemporalLayeringTwoLayersWithSync, frameCount: 9},
		{name: "three-layers-with-sync", mode: TemporalLayeringThreeLayersWithSync, frameCount: 9},
		{name: "three-layers-altref-with-sync", mode: TemporalLayeringThreeLayersAltRefWithSync, frameCount: 9},
		{name: "three-layers-one-reference", mode: TemporalLayeringThreeLayersOneReference, frameCount: 9},
		{name: "three-layers-no-sync", mode: TemporalLayeringThreeLayersNoSync, frameCount: 9},
	}
}
