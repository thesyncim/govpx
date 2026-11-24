//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"math"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9OracleRateBehaviorParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 rate-behavior trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, uint8(96+i*11), 128, 128)
	}

	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
		DropFrameAllowed:    false,
		DropFrameWaterMark:  0,
		TemporalScalability: govpx.TemporalScalabilityConfig{},
	}
	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--exact-fps-timebase",
	}

	govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, nil)
	libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height, sources,
		nil, extraArgs)
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("rate rows: govpx=%d libvpx=%d", len(govpxRows), len(libvpxRows))
	}

	var qDriftMax, sizePctMax, bufferPctMax float64
	refreshMatches := 0
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.FrameIndex != l.FrameIndex {
			t.Fatalf("row %d frame_index: govpx=%d libvpx=%d", i, g.FrameIndex, l.FrameIndex)
		}
		if g.Dropped || l.Dropped {
			t.Fatalf("row %d dropped: govpx=%t libvpx=%t, want no-drop fixture", i, g.Dropped, l.Dropped)
		}
		if g.RefreshFrameFlags == l.RefreshFrameFlags {
			refreshMatches++
		}
		if g.RecodeAllowed || l.RecodeAllowed || g.RecodeLoopCount != 0 || l.RecodeLoopCount != 0 {
			t.Fatalf("row %d recode: govpx allowed=%t loops=%d libvpx allowed=%t loops=%d, want one-pass VP9 no-recode",
				i, g.RecodeAllowed, g.RecodeLoopCount, l.RecodeAllowed, l.RecodeLoopCount)
		}
		if g.FrameTargetBits != l.FrameTargetBits {
			t.Fatalf("row %d frame target bits: govpx=%d libvpx=%d",
				i, g.FrameTargetBits, l.FrameTargetBits)
		}
		qDriftMax = math.Max(qDriftMax, math.Abs(float64(g.BaseQIndex-l.BaseQIndex)))
		sizePctMax = math.Max(sizePctMax, vp9test.PctDelta(g.SizeBits, l.SizeBits))
		bufferPctMax = math.Max(bufferPctMax, vp9test.PctDelta(g.BufferLevelBits, l.BufferLevelBits))
	}

	t.Logf("VP9 CBR rate trace: rows=%d refresh_matches=%d/%d max_q_drift=%.0f max_size_delta_pct=%.2f max_buffer_delta_pct=%.2f",
		len(govpxRows), refreshMatches, len(govpxRows), qDriftMax, sizePctMax,
		bufferPctMax)
	t.Logf("VP9 CBR rate trace rows:\n%s", vp9test.FormatRateTraceRows(govpxRows, libvpxRows))

	if refreshMatches != len(govpxRows) {
		t.Fatalf("refresh flags matched %d/%d rows", refreshMatches, len(govpxRows))
	}
	if vp9test.StrictEnv("GOVPX_VP9_RATE_TRACE_STRICT") {
		if qDriftMax != 0 || sizePctMax != 0 || bufferPctMax != 0 {
			t.Fatalf("strict VP9 rate trace drift: max_q=%.0f max_size_pct=%.2f max_buffer_pct=%.2f",
				qDriftMax, sizePctMax, bufferPctMax)
		}
	}
}

func TestVP9OracleQuantizerHistogramParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 Q histogram trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 12
	type qHistCase struct {
		name      string
		opts      govpx.VP9EncoderOptions
		flags     []govpx.EncodeFlags
		extraArgs []string
	}
	cases := []qHistCase{
		{
			name:      "cbr-panning",
			opts:      vp9oracle.CBROptions(width, height, 700),
			extraArgs: vp9oracle.CBRArgs(700, 600, 400, 500, 0),
		},
		{
			name: "cbr-force-key",
			opts: vp9oracle.CBROptions(width, height, 650),
			flags: vp9oracle.FlagAt(frames, 5,
				govpx.EncodeForceKeyFrame),
			extraArgs: vp9oracle.CBRArgs(650, 600, 400, 500, 0),
		},
		{
			name: "fixed-q-window",
			opts: func() govpx.VP9EncoderOptions {
				opts := vp9oracle.CBROptions(width, height, 700)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--min-q=20", "--max-q=20"),
		},
		{
			name: "cbr-cyclic-aq",
			opts: func() govpx.VP9EncoderOptions {
				opts := vp9oracle.CBROptions(width, height, 700)
				opts.AQMode = govpx.VP9AQCyclicRefresh
				return opts
			}(),
			extraArgs: append(vp9oracle.CBRArgs(700, 600, 400, 500, 0),
				"--aq-mode=3"),
		},
		{
			name: "vbr-panning",
			opts: govpx.VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     govpx.RateControlVBR,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=vbr",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
			},
		},
		{
			name: "cq-panning",
			opts: govpx.VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     govpx.RateControlCQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=cq",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
		},
		{
			name: "q-panning",
			opts: govpx.VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     govpx.RateControlQ,
				TargetBitrateKbps:   700,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				CQLevel:             20,
				MaxKeyframeInterval: 128,
			},
			extraArgs: []string{
				"--end-usage=q",
				"--target-bitrate=700",
				"--min-q=4",
				"--max-q=56",
				"--cq-level=20",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows := vp9oracle.CaptureRateTraceRows(t, tc.opts, sources,
				tc.flags)
			libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width,
				height, sources, tc.flags, tc.extraArgs)
			govpxHist := vp9test.QHistogram(govpxRows)
			libvpxHist := vp9test.QHistogram(libvpxRows)
			distance, mismatchedBins := vp9test.HistogramDistance(govpxHist,
				libvpxHist)
			t.Logf("VP9 Q histogram trace %s: distance=%d mismatched_bins=%d govpx=%s libvpx=%s",
				tc.name, distance, mismatchedBins,
				vp9test.FormatQHistogram(govpxHist),
				vp9test.FormatQHistogram(libvpxHist))
			if vp9test.StrictEnv("GOVPX_VP9_QHIST_STRICT") &&
				distance != 0 {
				t.Fatalf("strict VP9 Q histogram mismatch %s: distance=%d bins=%d",
					tc.name, distance, mismatchedBins)
			}
		})
	}
}

func TestVP9OracleRateBufferMatrixParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 CBR buffer matrix trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 12
	type bufferCase struct {
		name      string
		opts      govpx.VP9EncoderOptions
		extraArgs []string
		wantDrop  bool
	}
	cbrOpts := func(targetKbps, bufSize, bufInitial, bufOptimal, drop int) govpx.VP9EncoderOptions {
		opts := vp9oracle.CBROptions(width, height, targetKbps)
		opts.BufferSizeMs = bufSize
		opts.BufferInitialSizeMs = bufInitial
		opts.BufferOptimalSizeMs = bufOptimal
		if drop > 0 {
			opts.DropFrameAllowed = true
			opts.DropFrameWaterMark = drop
		}
		return opts
	}
	cases := []bufferCase{
		{
			name:      "low-bitrate-tight-buffer-no-drop",
			opts:      cbrOpts(140, 400, 300, 350, 0),
			extraArgs: vp9oracle.CBRArgs(140, 400, 300, 350, 0),
		},
		{
			name:      "low-bitrate-tight-buffer-drop",
			opts:      cbrOpts(140, 400, 300, 350, 60),
			extraArgs: vp9oracle.CBRArgs(140, 400, 300, 350, 60),
			wantDrop:  true,
		},
		{
			name:      "large-buffer-highrate",
			opts:      cbrOpts(1200, 2000, 1500, 1800, 0),
			extraArgs: vp9oracle.CBRArgs(1200, 2000, 1500, 1800, 0),
		},
		{
			name: "fixed-q-drop-pressure",
			opts: func() govpx.VP9EncoderOptions {
				opts := cbrOpts(140, 400, 300, 350, 60)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			extraArgs: append(vp9oracle.CBRArgs(140, 400, 300, 350, 60),
				"--min-q=20", "--max-q=20"),
			wantDrop: true,
		},
		{
			name: "wide-q-drop-pressure",
			opts: func() govpx.VP9EncoderOptions {
				opts := cbrOpts(100, 300, 200, 250, 80)
				opts.MinQuantizer = 0
				opts.MaxQuantizer = 63
				return opts
			}(),
			extraArgs: append(vp9oracle.CBRArgs(100, 300, 200, 250, 80),
				"--min-q=0", "--max-q=63"),
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := vp9oracle.TransitionSources(width, height, frames)
			govpxRows := vp9oracle.CaptureRateTraceRows(t, tc.opts, sources, nil)
			libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height,
				sources, nil, tc.extraArgs)
			stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9oracle.RateTraceFlagMapper)
			govpxDrops := vp9test.DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9test.DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 CBR buffer matrix trace %s: %s govpx_drops=%v libvpx_drops=%v",
				tc.name, stats, govpxDrops, libvpxDrops)
			t.Logf("VP9 CBR buffer matrix rows %s:\n%s",
				tc.name, vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
			if tc.wantDrop && (len(govpxDrops) == 0 || len(libvpxDrops) == 0) {
				t.Fatalf("drop fixture %s did not drop on both sides: govpx=%v libvpx=%v",
					tc.name, govpxDrops, libvpxDrops)
			}
			if vp9test.StrictEnv("GOVPX_VP9_BUFFER_MATRIX_STRICT") &&
				stats.HasMismatch() {
				t.Fatalf("strict VP9 CBR buffer matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleCBRKeyframeVariancePartitionParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 CBR keyframe variance trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9oracle.CBROptions(width, height, 600)
	flags := vp9oracle.FlagAt(frames, 3, govpx.EncodeForceKeyFrame)
	extraArgs := vp9oracle.CBRArgs(600, 600, 400, 500, 0)

	govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, flags)
	libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height,
		sources, flags, extraArgs)
	if len(govpxRows) != frames || len(libvpxRows) != frames {
		t.Fatalf("CBR keyframe variance rows: govpx=%d libvpx=%d, want %d/%d",
			len(govpxRows), len(libvpxRows), frames, frames)
	}
	t.Logf("VP9 CBR keyframe variance rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	for _, frame := range [...]int{0, 3} {
		g := govpxRows[frame]
		l := libvpxRows[frame]
		if !g.KeyFrame || !l.KeyFrame || g.Dropped || l.Dropped {
			t.Fatalf("frame %d key/drop: govpx=(%t,%t) libvpx=(%t,%t)",
				frame, g.KeyFrame, g.Dropped, l.KeyFrame, l.Dropped)
		}
		sizeDelta := g.SizeBytes - l.SizeBytes
		if sizeDelta < 0 {
			sizeDelta = -sizeDelta
		}
		firstPartDelta := g.FirstPartitionSize - l.FirstPartitionSize
		if firstPartDelta < 0 {
			firstPartDelta = -firstPartDelta
		}
		if sizeDelta > 1 || firstPartDelta > 1 {
			t.Fatalf("frame %d key variance drift: size_delta=%d first_part_delta=%d",
				frame, sizeDelta, firstPartDelta)
		}
	}
}

func TestVP9OracleRateDropPressureParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 rate drop-pressure trace")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 32
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}

	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   120,
		BufferSizeMs:        400,
		BufferInitialSizeMs: 300,
		BufferOptimalSizeMs: 350,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	}
	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=120",
		"--buf-sz=400",
		"--buf-initial-sz=300",
		"--buf-optimal-sz=350",
		"--drop-frame=60",
	}

	govpxRows := vp9oracle.CaptureRateTraceRows(t, opts, sources, nil)
	libvpxRows := vp9oracle.CaptureLibvpxRateTraceRows(t, width, height, sources,
		nil, extraArgs)
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("drop-pressure rows: govpx=%d libvpx=%d", len(govpxRows), len(libvpxRows))
	}
	govpxDrops := vp9test.DroppedFrameIndices(govpxRows)
	libvpxDrops := vp9test.DroppedFrameIndices(libvpxRows)
	t.Logf("VP9 CBR drop-pressure trace: govpx_drops=%v libvpx_drops=%v",
		govpxDrops, libvpxDrops)
	t.Logf("VP9 CBR drop-pressure rows:\n%s",
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
	if len(libvpxDrops) == 0 {
		t.Fatal("drop-pressure fixture did not make libvpx drop any frames")
	}
	if len(govpxDrops) == 0 {
		t.Fatal("drop-pressure fixture did not make govpx drop any frames")
	}
	if got := vp9test.DropReasonCount(govpxRows, "watermark_decimation"); got == 0 {
		t.Fatalf("drop-pressure fixture did not exercise govpx watermark decimation: rows=%v",
			govpxDrops)
	}
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.FrameIndex != l.FrameIndex {
			t.Fatalf("row %d frame_index: govpx=%d libvpx=%d", i, g.FrameIndex, l.FrameIndex)
		}
		if g.RecodeAllowed || l.RecodeAllowed || g.RecodeLoopCount != 0 || l.RecodeLoopCount != 0 {
			t.Fatalf("row %d recode: govpx allowed=%t loops=%d libvpx allowed=%t loops=%d, want one-pass VP9 no-recode",
				i, g.RecodeAllowed, g.RecodeLoopCount, l.RecodeAllowed, l.RecodeLoopCount)
		}
		if g.TemporalLayerID != 0 || l.TemporalLayerID != 0 ||
			g.TemporalLayerSync || l.TemporalLayerSync {
			t.Fatalf("row %d temporal fields: govpx=(%d,%t) libvpx=(%d,%t), want base-layer only",
				i, g.TemporalLayerID, g.TemporalLayerSync, l.TemporalLayerID,
				l.TemporalLayerSync)
		}
	}
	if vp9test.StrictEnv("GOVPX_VP9_RATE_DROP_STRICT") &&
		!vp9test.SameIntSlice(govpxDrops, libvpxDrops) {
		t.Fatalf("strict VP9 drop indices: govpx=%v libvpx=%v",
			govpxDrops, libvpxDrops)
	}
	if vp9test.StrictEnv("GOVPX_VP9_RATE_DROP_STRICT") {
		keySizeDelta := govpxRows[0].SizeBytes - libvpxRows[0].SizeBytes
		if keySizeDelta < 0 {
			keySizeDelta = -keySizeDelta
		}
		keyFirstPartDelta := govpxRows[0].FirstPartitionSize -
			libvpxRows[0].FirstPartitionSize
		if keyFirstPartDelta < 0 {
			keyFirstPartDelta = -keyFirstPartDelta
		}
		if keySizeDelta > 1 || keyFirstPartDelta > 1 {
			t.Fatalf("strict VP9 drop key partition drift: size_delta=%d first_part_delta=%d",
				keySizeDelta, keyFirstPartDelta)
		}
	}
}
