//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleTwoPassPanning720p1500ReferenceEnvelope(t *testing.T) {
	vp8test.RequireOracle(t, "VP8 720p two-pass panning reference envelope")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	opts, sources := vp8Panning720p1500Fixture()
	const targetKbps = 1500

	govpxStats := captureGovpxFirstPassStats(t, opts, sources)
	fpfData, libvpxIVF, diag, err := vp8test.VpxencVP8TwoPassEncodeI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8TwoPassConfig{
			FirstPassBinaryPath:  vpxenc,
			SecondPassBinaryPath: vpxencOracle,
			Common: vp8test.VpxencVP8Config{
				Width:             opts.Width,
				Height:            opts.Height,
				Frames:            len(sources),
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

func TestVP8OracleTwoPassPanning720p1500SecondPassAllocationParity(t *testing.T) {
	vp8test.RequireOracle(t, "VP8 720p two-pass panning second-pass allocation parity")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	opts, sources := vp8Panning720p1500Fixture()
	const targetKbps = 1500

	fpfData, libvpxTrace, diag, err := vp8test.VpxencVP8TwoPassTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8TwoPassConfig{
			FirstPassBinaryPath:  vpxenc,
			SecondPassBinaryPath: vpxencOracle,
			Common: vp8OracleTraceConfig(
				"",
				opts,
				len(sources),
				targetKbps,
				nil,
				[]string{"--end-usage=vbr"},
			),
		},
	)
	if err != nil {
		t.Fatalf("vpxenc two-pass trace failed: %v\n%s", err, diag)
	}

	govpxOpts := opts
	govpxOpts.TwoPassStats = parseLibvpxFirstPassStats(t, fpfData)
	govpxTrace := captureGovpxEncoderTrace(t, govpxOpts, sources)

	govpxRows := secondPassRateRowsFromTrace(t, govpxTrace)
	libvpxRows := secondPassRateRowsFromTrace(t, libvpxTrace)
	report, diffs := scoreSecondPassAlloc("panning720p-1500-vbr", govpxRows, libvpxRows)
	t.Logf("second-pass parity report: frames=%d q_match=%.2f%% target_match=%.2f%% maxQΔ=%d maxTargetRelΔ=%.4f",
		report.FrameTotal, report.QMatchPct, report.TargetMatchPct,
		report.MaxQIndexDelta, report.MaxTargetRelDelta)
	if report.FrameTotal != len(sources) {
		t.Fatalf("rate row count = %d, want %d", report.FrameTotal, len(sources))
	}
	if report.QMatchPct != 100 || report.TargetMatchPct != 100 {
		for _, d := range diffs {
			if absInt(d.QIndexDelta) > 2 || math.Abs(d.TargetRelDelta) > 0.05 {
				t.Logf("  frame %d qΔ=%d (govpx=%d libvpx=%d) targetΔrel=%.4f (govpx=%d libvpx=%d)",
					d.FrameIndex, d.QIndexDelta, d.QIndexGovpx, d.QIndexLibvpx,
					d.TargetRelDelta, d.TargetGovpx, d.TargetLibvpx)
			}
		}
		t.Fatalf("second-pass allocation diverged: q_match=%.2f%% target_match=%.2f%%, want 100%%/100%%",
			report.QMatchPct, report.TargetMatchPct)
	}
}

func TestVP8OracleTwoPassPanning720p1500Pass2RateControlTraceParity(t *testing.T) {
	vp8test.RequireOracle(t, "VP8 720p two-pass panning pass-2 rate-control trace parity")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	opts, sources := vp8Panning720p1500Fixture()
	const targetKbps = 1500

	fpfData, libvpxTrace, diag, err := vp8test.VpxencVP8TwoPassTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8TwoPassConfig{
			FirstPassBinaryPath:  vpxenc,
			SecondPassBinaryPath: vpxencOracle,
			Common: vp8OracleTraceConfig(
				"",
				opts,
				len(sources),
				targetKbps,
				nil,
				[]string{"--end-usage=vbr"},
			),
		},
	)
	if err != nil {
		t.Fatalf("vpxenc two-pass trace failed: %v\n%s", err, diag)
	}

	govpxOpts := opts
	govpxOpts.TwoPassStats = parseLibvpxFirstPassStats(t, fpfData)
	govpxTrace := captureGovpxEncoderTrace(t, govpxOpts, sources)

	govpxProjected := projectVP8EncoderDecisionTrace(t, govpxTrace)
	libvpxProjected := projectVP8EncoderDecisionTrace(t, libvpxTrace)
	div, err := vp8test.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), vp8test.CompareOptions{
		MaxDivergences: 8,
		IgnoreFields: map[string]bool{
			// Projected size is downstream of the mode/coeff residuals
			// this fixture is meant to expose next; keep this guard on
			// pass-2 q bounds, targets, recode q, and frame headers.
			"projected_frame_size": true,
		},
		NumericFieldTolerances: map[string]float64{
			"this_frame_target": 1,
		},
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Fatalf("projected pass-2 rate-control trace diverged:\n%s\ngovpx first rows:\n%s\nlibvpx first rows:\n%s",
			vp8test.FormatDivergences(div),
			vp8test.FirstTraceRows(govpxProjected, 12),
			vp8test.FirstTraceRows(libvpxProjected, 12))
	}
}

func vp8Panning720p1500Fixture() (EncoderOptions, []Image) {
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
	return EncoderOptions{
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
	}, sources
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
