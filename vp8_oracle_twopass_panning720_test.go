//go:build govpx_oracle_trace

package govpx

import (
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleTwoPassPanning720p1500ReferenceEnvelope(t *testing.T) {
	vp8test.RequireOracle(t, "VP8 720p two-pass panning reference envelope")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		frames     = 16
		targetKbps = 1500
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = imageFromYCbCr(testutil.NewTexturedPanningYCbCr(width, height, i))
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      63,
		QuantizerRangeSet: true,
		CQLevel:           16,
		KeyFrameInterval:  120,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
	}

	govpxStats := captureGovpxFirstPassStats(t, opts, sources)
	fpfData, libvpxIVF, diag, err := vp8test.VpxencVP8TwoPassEncodeI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8TwoPassConfig{
			FirstPassBinaryPath:  vpxenc,
			SecondPassBinaryPath: vpxencOracle,
			Common: vp8test.VpxencVP8Config{
				Width:             width,
				Height:            height,
				Frames:            frames,
				Deadline:          "good",
				CPUUsed:           0,
				LagInFrames:       0,
				AutoAltRef:        false,
				TargetBitrateKbps: targetKbps,
				MinQ:              4,
				MaxQ:              63,
				Timebase:          "1/30",
				FPS:               "30/1",
				KeyFrameDistSet:   true,
				KeyFrameMinDist:   120,
				KeyFrameMaxDist:   120,
				ExtraArgs:         []string{"--end-usage=vbr"},
			},
		},
	)
	if err != nil {
		t.Fatalf("vpxenc two-pass encode failed: %v\n%s", err, diag)
	}
	libvpxStats := parseLibvpxFirstPassStats(t, fpfData)
	maxField, maxAbs := compareFirstPassStatsLoose(t, "panning720p-1500", govpxStats, libvpxStats, defaultFirstPassLooseTolerances)
	t.Logf("first-pass stats max divergence: field=%s |delta|=%.4g", maxField, maxAbs)

	govpxOpts := opts
	govpxOpts.TwoPassStats = govpxStats
	govpxFrames := encodeFramesWithGovpx(t, govpxOpts, sources)
	libvpxFrames, err := testutil.IVFFramePayloads(libvpxIVF)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	assertVP8FramePatternMatches(t, "panning720p-1500", govpxFrames, libvpxFrames)

	govpxBytes := totalPayloadBytes(govpxFrames)
	libvpxBytes := totalPayloadBytes(libvpxFrames)
	rateDeltaPct := 100 * (float64(govpxBytes) - float64(libvpxBytes)) / float64(libvpxBytes)
	t.Logf("payload bytes: govpx=%d libvpx=%d delta=%+.3f%%", govpxBytes, libvpxBytes, rateDeltaPct)
	if math.Abs(rateDeltaPct) > 3.0 {
		t.Fatalf("payload delta = %+.3f%%, want within 3.0%% of libvpx", rateDeltaPct)
	}

	govpxLibvpxStatsOpts := opts
	govpxLibvpxStatsOpts.TwoPassStats = libvpxStats
	govpxLibvpxStatsFrames := encodeFramesWithGovpx(t, govpxLibvpxStatsOpts, sources)
	assertVP8FramePatternMatches(t, "panning720p-1500-libvpx-stats", govpxLibvpxStatsFrames, libvpxFrames)
	statsMatchedBytes := totalPayloadBytes(govpxLibvpxStatsFrames)
	statsMatchedDeltaPct := 100 * (float64(statsMatchedBytes) - float64(libvpxBytes)) / float64(libvpxBytes)
	t.Logf("payload bytes with libvpx stats: govpx=%d libvpx=%d delta=%+.3f%%", statsMatchedBytes, libvpxBytes, statsMatchedDeltaPct)
	if math.Abs(statsMatchedDeltaPct) > 3.0 {
		t.Fatalf("payload delta with libvpx stats = %+.3f%%, want within 3.0%% of libvpx", statsMatchedDeltaPct)
	}
}

func assertVP8FramePatternMatches(t *testing.T, label string, got [][]byte, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s frame count: govpx=%d libvpx=%d", label, len(got), len(want))
	}
	for i := range got {
		gotInfo, err := PeekVP8StreamInfo(got[i])
		if err != nil {
			t.Fatalf("%s govpx frame %d PeekVP8StreamInfo: %v", label, i, err)
		}
		wantInfo, err := PeekVP8StreamInfo(want[i])
		if err != nil {
			t.Fatalf("%s libvpx frame %d PeekVP8StreamInfo: %v", label, i, err)
		}
		if gotInfo.KeyFrame != wantInfo.KeyFrame || gotInfo.ShowFrame != wantInfo.ShowFrame {
			t.Fatalf("%s frame %d pattern: govpx key=%v show=%v libvpx key=%v show=%v",
				label, i, gotInfo.KeyFrame, gotInfo.ShowFrame, wantInfo.KeyFrame, wantInfo.ShowFrame)
		}
	}
}

func totalPayloadBytes(frames [][]byte) int {
	total := 0
	for _, frame := range frames {
		total += len(frame)
	}
	return total
}
