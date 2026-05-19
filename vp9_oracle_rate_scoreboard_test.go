//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9OracleRateBehaviorScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 rate-behavior scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 10
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9YCbCrForTest(width, height, uint8(96+i*11), 128, 128)
	}

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
		DropFrameAllowed:    false,
		DropFrameWaterMark:  0,
		TemporalScalability: TemporalScalabilityConfig{},
	}
	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
	}

	govpxRows := captureVP9RateScoreboardRows(t, opts, sources, nil)
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height, sources,
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
		sizePctMax = math.Max(sizePctMax, pctDelta(g.SizeBits, l.SizeBits))
		bufferPctMax = math.Max(bufferPctMax, pctDelta(g.BufferLevelBits, l.BufferLevelBits))
	}

	t.Logf("VP9 CBR rate scoreboard: rows=%d refresh_matches=%d/%d max_q_drift=%.0f max_size_delta_pct=%.2f max_buffer_delta_pct=%.2f",
		len(govpxRows), refreshMatches, len(govpxRows), qDriftMax, sizePctMax,
		bufferPctMax)
	t.Logf("VP9 CBR rate scoreboard rows:\n%s", formatVP9RateScoreboardRows(govpxRows, libvpxRows))

	if refreshMatches != len(govpxRows) {
		t.Fatalf("refresh flags matched %d/%d rows", refreshMatches, len(govpxRows))
	}
	if os.Getenv("GOVPX_VP9_RATE_SCOREBOARD_STRICT") == "1" {
		if qDriftMax != 0 || sizePctMax != 0 || bufferPctMax != 0 {
			t.Fatalf("strict VP9 rate scoreboard drift: max_q=%.0f max_size_pct=%.2f max_buffer_pct=%.2f",
				qDriftMax, sizePctMax, bufferPctMax)
		}
	}
}

func TestVP9OracleQHistogramScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 Q histogram scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 12
	type qHistCase struct {
		name      string
		opts      VP9EncoderOptions
		flags     []EncodeFlags
		extraArgs []string
	}
	cases := []qHistCase{
		{
			name:      "cbr-panning",
			opts:      vp9OracleCBROptions(width, height, 700),
			extraArgs: vp9OracleCBRArgs(700, 600, 400, 500, 0),
		},
		{
			name: "cbr-force-key",
			opts: vp9OracleCBROptions(width, height, 650),
			flags: vp9OracleFlagAt(frames, 5,
				EncodeForceKeyFrame),
			extraArgs: vp9OracleCBRArgs(650, 600, 400, 500, 0),
		},
		{
			name: "fixed-q-window",
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(width, height, 700)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--min-q=20", "--max-q=20"),
		},
		{
			name: "cbr-cyclic-aq",
			opts: func() VP9EncoderOptions {
				opts := vp9OracleCBROptions(width, height, 700)
				opts.AQMode = VP9AQCyclicRefresh
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(700, 600, 400, 500, 0),
				"--aq-mode=3"),
		},
		{
			name: "vbr-panning",
			opts: VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     RateControlVBR,
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
			opts: VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     RateControlCQ,
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
			opts: VP9EncoderOptions{
				Width:               width,
				Height:              height,
				FPS:                 30,
				RateControlModeSet:  true,
				RateControlMode:     RateControlQ,
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
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRows(t, tc.opts, sources,
				tc.flags)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width,
				height, sources, tc.flags, tc.extraArgs)
			govpxHist := vp9QHistogram(govpxRows)
			libvpxHist := vp9QHistogram(libvpxRows)
			distance, mismatchedBins := vp9HistogramDistance(govpxHist,
				libvpxHist)
			t.Logf("VP9 Q histogram scoreboard %s: distance=%d mismatched_bins=%d govpx=%s libvpx=%s",
				tc.name, distance, mismatchedBins,
				formatVP9QHistogram(govpxHist),
				formatVP9QHistogram(libvpxHist))
			if os.Getenv("GOVPX_VP9_QHIST_STRICT") == "1" &&
				distance != 0 {
				t.Fatalf("strict VP9 Q histogram mismatch %s: distance=%d bins=%d",
					tc.name, distance, mismatchedBins)
			}
		})
	}
}

func TestVP9OracleRateBufferMatrixScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 CBR buffer matrix scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 12
	type bufferCase struct {
		name      string
		opts      VP9EncoderOptions
		extraArgs []string
		wantDrop  bool
	}
	cbrOpts := func(targetKbps, bufSize, bufInitial, bufOptimal, drop int) VP9EncoderOptions {
		opts := vp9OracleCBROptions(width, height, targetKbps)
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
			extraArgs: vp9OracleCBRArgs(140, 400, 300, 350, 0),
		},
		{
			name:      "low-bitrate-tight-buffer-drop",
			opts:      cbrOpts(140, 400, 300, 350, 60),
			extraArgs: vp9OracleCBRArgs(140, 400, 300, 350, 60),
			wantDrop:  true,
		},
		{
			name:      "large-buffer-highrate",
			opts:      cbrOpts(1200, 2000, 1500, 1800, 0),
			extraArgs: vp9OracleCBRArgs(1200, 2000, 1500, 1800, 0),
		},
		{
			name: "fixed-q-drop-pressure",
			opts: func() VP9EncoderOptions {
				opts := cbrOpts(140, 400, 300, 350, 60)
				opts.MinQuantizer = 20
				opts.MaxQuantizer = 20
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(140, 400, 300, 350, 60),
				"--min-q=20", "--max-q=20"),
			wantDrop: true,
		},
		{
			name: "wide-q-drop-pressure",
			opts: func() VP9EncoderOptions {
				opts := cbrOpts(100, 300, 200, 250, 80)
				opts.MinQuantizer = 0
				opts.MaxQuantizer = 63
				return opts
			}(),
			extraArgs: append(vp9OracleCBRArgs(100, 300, 200, 250, 80),
				"--min-q=0", "--max-q=63"),
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := newVP9OracleTransitionSources(width, height, frames)
			govpxRows := captureVP9RateScoreboardRows(t, tc.opts, sources, nil)
			libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
				sources, nil, tc.extraArgs)
			stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
			govpxDrops := vp9DroppedFrameIndices(govpxRows)
			libvpxDrops := vp9DroppedFrameIndices(libvpxRows)
			t.Logf("VP9 CBR buffer matrix scoreboard %s: %s govpx_drops=%v libvpx_drops=%v",
				tc.name, stats, govpxDrops, libvpxDrops)
			t.Logf("VP9 CBR buffer matrix rows %s:\n%s",
				tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
			if tc.wantDrop && (len(govpxDrops) == 0 || len(libvpxDrops) == 0) {
				t.Fatalf("drop fixture %s did not drop on both sides: govpx=%v libvpx=%v",
					tc.name, govpxDrops, libvpxDrops)
			}
			if os.Getenv("GOVPX_VP9_BUFFER_MATRIX_STRICT") == "1" &&
				stats.hasMismatch() {
				t.Fatalf("strict VP9 CBR buffer matrix mismatch %s: %s",
					tc.name, stats)
			}
		})
	}
}

func TestVP9OracleCBRKeyframeVariancePartitionScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 CBR keyframe variance scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}
	opts := vp9OracleCBROptions(width, height, 600)
	flags := vp9OracleFlagAt(frames, 3, EncodeForceKeyFrame)
	extraArgs := vp9OracleCBRArgs(600, 600, 400, 500, 0)

	govpxRows := captureVP9RateScoreboardRows(t, opts, sources, flags)
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height,
		sources, flags, extraArgs)
	if len(govpxRows) != frames || len(libvpxRows) != frames {
		t.Fatalf("CBR keyframe variance rows: govpx=%d libvpx=%d, want %d/%d",
			len(govpxRows), len(libvpxRows), frames, frames)
	}
	t.Logf("VP9 CBR keyframe variance rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
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

func TestVP9OracleRateDropPressureScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 rate drop-pressure scoreboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	const width, height, frames = 64, 64, 32
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = newVP9PanningYCbCrForRateTest(width, height, i)
	}

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
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

	govpxRows := captureVP9RateScoreboardRows(t, opts, sources, nil)
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height, sources,
		nil, extraArgs)
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("drop-pressure rows: govpx=%d libvpx=%d", len(govpxRows), len(libvpxRows))
	}
	govpxDrops := vp9DroppedFrameIndices(govpxRows)
	libvpxDrops := vp9DroppedFrameIndices(libvpxRows)
	t.Logf("VP9 CBR drop-pressure scoreboard: govpx_drops=%v libvpx_drops=%v",
		govpxDrops, libvpxDrops)
	t.Logf("VP9 CBR drop-pressure rows:\n%s",
		formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	if len(libvpxDrops) == 0 {
		t.Fatal("drop-pressure fixture did not make libvpx drop any frames")
	}
	if len(govpxDrops) == 0 {
		t.Fatal("drop-pressure fixture did not make govpx drop any frames")
	}
	if got := vp9DropReasonCount(govpxRows, "watermark_decimation"); got == 0 {
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
	if os.Getenv("GOVPX_VP9_RATE_DROP_STRICT") == "1" &&
		!vp9SameIntSlice(govpxDrops, libvpxDrops) {
		t.Fatalf("strict VP9 drop indices: govpx=%v libvpx=%v",
			govpxDrops, libvpxDrops)
	}
	if os.Getenv("GOVPX_VP9_RATE_DROP_STRICT") == "1" {
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

type vp9RateScoreboardRow struct {
	FrameIndex           int
	Flags                uint32
	Dropped              bool
	DropReason           string
	KeyFrame             bool
	ShowFrame            bool
	CodedWidth           int
	CodedHeight          int
	BaseQIndex           int
	PublicQuantizer      int
	SizeBytes            int
	SizeBits             int
	FirstPartitionSize   int
	TargetBitrateKbps    int
	FrameTargetBits      int
	BufferLevelBits      int
	BufferOptimalBits    int
	RefreshFrameFlags    uint8
	RefreshFrameContext  bool
	ErrorResilient       bool
	FrameParallel        bool
	FrameContextIdx      int
	TxMode               int
	InterpFilter         int
	ReferenceMode        int
	CompoundAllowed      bool
	ReferenceMask        uint8
	LoopFilterLevel      int
	TemporalLayerID      int
	TemporalLayerCount   int
	TemporalLayerSync    bool
	TL0PICIDX            uint8
	RecodeAllowed        bool
	RecodeLoopCount      int
	ActiveBestQ          int
	ActiveWorstQ         int
	RateCorrectionFactor float64
	TileLog2Cols         int
	TileLog2Rows         int
}

func captureVP9RateScoreboardRows(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
) []vp9RateScoreboardRow {
	return captureVP9RateScoreboardRowsWithHooks(t, opts, sources, flags, nil)
}

func captureVP9RateScoreboardRowsWithHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) []vp9RateScoreboardRow {
	t.Helper()
	var trace bytes.Buffer
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	enc.SetVP9OracleTraceWriter(&trace)
	dstSize, err := vp9AllocatingEncodeBufferSize(opts.Width, opts.Height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if _, err := enc.EncodeIntoWithFlagsResult(src, dst, f); err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
	}
	return parseVP9RateScoreboardRows(t, trace.Bytes())
}

func captureLibvpxVP9RateScoreboardRows(t *testing.T, width int, height int,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) []vp9RateScoreboardRow {
	t.Helper()
	libvpxFlags := make([]uint32, len(flags))
	for i, f := range flags {
		libvpxFlags[i] = vp9FrameFlagsForLibvpx(f)
	}
	var raw []byte
	for _, src := range sources {
		raw = appendVP9YCbCrI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), libvpxFlags, extraArgs...)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	rows := parseVP9RateScoreboardRows(t, trace)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		var frame testutil.IVFFrame
		frame, offset, err = testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", i, err)
		}
		enrichVP9RateScoreboardRowFromPacket(t, &rows[i], frame.Data)
	}
	return rows
}

func parseVP9RateScoreboardRows(t *testing.T, trace []byte) []vp9RateScoreboardRow {
	t.Helper()
	rows := make([]vp9RateScoreboardRow, 0, bytes.Count(trace, []byte("\n")))
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var raw struct {
			Row                  string  `json:"row"`
			FrameIndex           int     `json:"frame_index"`
			Flags                uint32  `json:"flags"`
			Dropped              bool    `json:"dropped"`
			DropReason           string  `json:"drop_reason"`
			KeyFrame             bool    `json:"key_frame"`
			ShowFrame            bool    `json:"show_frame"`
			CodedWidth           int     `json:"coded_width"`
			CodedHeight          int     `json:"coded_height"`
			BaseQIndex           int     `json:"base_qindex"`
			PublicQuantizer      int     `json:"public_quantizer"`
			SizeBytes            int     `json:"size_bytes"`
			SizeBits             int     `json:"size_bits"`
			FirstPartitionSize   int     `json:"first_partition_size"`
			TargetBitrateKbps    int     `json:"target_bitrate_kbps"`
			FrameTargetBits      int     `json:"frame_target_bits"`
			BufferLevelBits      int     `json:"buffer_level_bits"`
			BufferOptimalBits    int     `json:"buffer_optimal_bits"`
			RefreshFrameFlags    uint8   `json:"refresh_frame_flags"`
			RefreshFrameContext  bool    `json:"refresh_frame_context"`
			ErrorResilient       bool    `json:"error_resilient"`
			FrameParallel        bool    `json:"frame_parallel"`
			FrameContextIdx      int     `json:"frame_context_idx"`
			TxMode               int     `json:"tx_mode"`
			InterpFilter         int     `json:"interp_filter"`
			ReferenceMode        int     `json:"reference_mode"`
			CompoundAllowed      bool    `json:"compound_allowed"`
			ReferenceMask        uint8   `json:"reference_mask"`
			LoopFilterLevel      int     `json:"loop_filter_level"`
			TemporalLayerID      int     `json:"temporal_layer_id"`
			TemporalLayerCount   int     `json:"temporal_layer_count"`
			TemporalLayerSync    bool    `json:"temporal_layer_sync"`
			TL0PICIDX            uint8   `json:"tl0_pic_idx"`
			RecodeAllowed        bool    `json:"recode_allowed"`
			RecodeLoopCount      int     `json:"recode_loop_count"`
			ActiveBestQ          int     `json:"active_best_q"`
			ActiveWorstQ         int     `json:"active_worst_q"`
			RateCorrectionFactor float64 `json:"rate_correction_factor"`
			TileLog2Cols         int     `json:"tile_log2_cols"`
			TileLog2Rows         int     `json:"tile_log2_rows"`
		}
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			t.Fatalf("VP9 rate trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if raw.Row != "vp9_frame" {
			continue
		}
		sizeBits := raw.SizeBits
		if sizeBits == 0 && raw.SizeBytes != 0 {
			sizeBits = raw.SizeBytes * 8
		}
		rows = append(rows, vp9RateScoreboardRow{
			FrameIndex:           raw.FrameIndex,
			Flags:                raw.Flags,
			Dropped:              raw.Dropped,
			DropReason:           raw.DropReason,
			KeyFrame:             raw.KeyFrame,
			ShowFrame:            raw.ShowFrame,
			CodedWidth:           raw.CodedWidth,
			CodedHeight:          raw.CodedHeight,
			BaseQIndex:           raw.BaseQIndex,
			PublicQuantizer:      raw.PublicQuantizer,
			SizeBytes:            raw.SizeBytes,
			SizeBits:             sizeBits,
			FirstPartitionSize:   raw.FirstPartitionSize,
			TargetBitrateKbps:    raw.TargetBitrateKbps,
			FrameTargetBits:      raw.FrameTargetBits,
			BufferLevelBits:      raw.BufferLevelBits,
			BufferOptimalBits:    raw.BufferOptimalBits,
			RefreshFrameFlags:    raw.RefreshFrameFlags,
			RefreshFrameContext:  raw.RefreshFrameContext,
			ErrorResilient:       raw.ErrorResilient,
			FrameParallel:        raw.FrameParallel,
			FrameContextIdx:      raw.FrameContextIdx,
			TxMode:               raw.TxMode,
			InterpFilter:         raw.InterpFilter,
			ReferenceMode:        raw.ReferenceMode,
			CompoundAllowed:      raw.CompoundAllowed,
			ReferenceMask:        raw.ReferenceMask,
			LoopFilterLevel:      raw.LoopFilterLevel,
			TemporalLayerID:      raw.TemporalLayerID,
			TemporalLayerCount:   raw.TemporalLayerCount,
			TemporalLayerSync:    raw.TemporalLayerSync,
			TL0PICIDX:            raw.TL0PICIDX,
			RecodeAllowed:        raw.RecodeAllowed,
			RecodeLoopCount:      raw.RecodeLoopCount,
			ActiveBestQ:          raw.ActiveBestQ,
			ActiveWorstQ:         raw.ActiveWorstQ,
			RateCorrectionFactor: raw.RateCorrectionFactor,
			TileLog2Cols:         raw.TileLog2Cols,
			TileLog2Rows:         raw.TileLog2Rows,
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan VP9 rate trace: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("VP9 rate trace has no vp9_frame rows:\n%s", trace)
	}
	return rows
}

func enrichVP9RateScoreboardRowFromPacket(t *testing.T, row *vp9RateScoreboardRow, packet []byte) {
	t.Helper()
	header, _ := parseVP9EncoderHeaderForTest(t, packet)
	comp, _, _ := readVP9CompressedHeaderForOracleTest(t, packet, header)
	row.KeyFrame = header.FrameType == common.KeyFrame
	row.ShowFrame = header.ShowFrame
	if header.Width != 0 {
		row.CodedWidth = int(header.Width)
	}
	if header.Height != 0 {
		row.CodedHeight = int(header.Height)
	}
	row.BaseQIndex = int(header.Quant.BaseQindex)
	row.PublicQuantizer = vp9QIndexToPublicQuantizer(int(header.Quant.BaseQindex))
	row.SizeBytes = len(packet)
	row.SizeBits = len(packet) * 8
	row.FirstPartitionSize = int(header.FirstPartitionSize)
	row.RefreshFrameFlags = header.RefreshFrameFlags
	row.RefreshFrameContext = header.RefreshFrameContext
	row.ErrorResilient = header.ErrorResilientMode
	row.FrameParallel = header.FrameParallelDecoding
	row.FrameContextIdx = int(header.FrameContextIdx)
	row.TxMode = int(comp.TxMode)
	row.InterpFilter = int(header.InterpFilter)
	row.ReferenceMode = int(comp.ReferenceMode)
	row.CompoundAllowed = header.FrameType != common.KeyFrame && !header.IntraOnly &&
		vp9dec.CompoundReferenceAllowed(vp9dec.FrameRefSignBias(&header))
	row.ReferenceMask = vp9ReferenceMaskFromLibvpxFrameFlags(row.Flags)
	row.LoopFilterLevel = int(header.Loopfilter.FilterLevel)
	row.TileLog2Cols = int(header.Tile.Log2TileCols)
	row.TileLog2Rows = int(header.Tile.Log2TileRows)
}

func vp9ReferenceMaskFromLibvpxFrameFlags(flags uint32) uint8 {
	const (
		libvpxNoRefLast = 1 << 16
		libvpxNoRefGF   = 1 << 17
		libvpxNoRefARF  = 1 << 21
	)
	var mask uint8
	if flags&libvpxNoRefLast == 0 {
		mask |= 1 << uint(vp9dec.LastFrame)
	}
	if flags&libvpxNoRefGF == 0 {
		mask |= 1 << uint(vp9dec.GoldenFrame)
	}
	if flags&libvpxNoRefARF == 0 {
		mask |= 1 << uint(vp9dec.AltrefFrame)
	}
	return mask
}

func pctDelta(got int, want int) float64 {
	den := math.Max(1, math.Abs(float64(want)))
	return math.Abs(float64(got-want)) * 100 / den
}

func formatVP9RateScoreboardRows(govpxRows, libvpxRows []vp9RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,govpx_flags,libvpx_flags,govpx_drop,libvpx_drop,govpx_key,libvpx_key,govpx_show,libvpx_show,govpx_width,libvpx_width,govpx_height,libvpx_height,govpx_q,libvpx_q,govpx_public_q,libvpx_public_q,govpx_active_best_q,libvpx_active_best_q,govpx_active_worst_q,libvpx_active_worst_q,govpx_rate_correction,libvpx_rate_correction,govpx_recode_allowed,libvpx_recode_allowed,govpx_recode_loops,libvpx_recode_loops,govpx_bytes,libvpx_bytes,govpx_bits,libvpx_bits,govpx_first_part,libvpx_first_part,govpx_target,libvpx_target,govpx_frame_target,libvpx_frame_target,govpx_buffer,libvpx_buffer,govpx_buffer_opt,libvpx_buffer_opt,govpx_refresh,libvpx_refresh,govpx_refresh_ctx,libvpx_refresh_ctx,govpx_tx,libvpx_tx,govpx_filter,libvpx_filter,govpx_refmode,libvpx_refmode,govpx_refmask,libvpx_refmask,govpx_lf,libvpx_lf,govpx_tile_cols,libvpx_tile_cols,govpx_tid,libvpx_tid,govpx_tlayers,libvpx_tlayers,govpx_tl0,libvpx_tl0,govpx_tsync,libvpx_tsync")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		fmt.Fprintf(&b, "%d,%#x,%#x,%t,%t,%t,%t,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6g,%.6g,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%t,%t,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%t,%t\n",
			g.FrameIndex, g.Flags, l.Flags, g.Dropped, l.Dropped, g.KeyFrame,
			l.KeyFrame, g.ShowFrame, l.ShowFrame, g.CodedWidth, l.CodedWidth,
			g.CodedHeight, l.CodedHeight, g.BaseQIndex, l.BaseQIndex,
			g.PublicQuantizer, l.PublicQuantizer, g.ActiveBestQ, l.ActiveBestQ,
			g.ActiveWorstQ, l.ActiveWorstQ, g.RateCorrectionFactor,
			l.RateCorrectionFactor, g.RecodeAllowed, l.RecodeAllowed,
			g.RecodeLoopCount, l.RecodeLoopCount, g.SizeBytes, l.SizeBytes,
			g.SizeBits, l.SizeBits, g.FirstPartitionSize, l.FirstPartitionSize,
			g.TargetBitrateKbps, l.TargetBitrateKbps, g.FrameTargetBits,
			l.FrameTargetBits, g.BufferLevelBits, l.BufferLevelBits,
			g.BufferOptimalBits, l.BufferOptimalBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.RefreshFrameContext, l.RefreshFrameContext,
			g.TxMode, l.TxMode, g.InterpFilter, l.InterpFilter,
			g.ReferenceMode, l.ReferenceMode, g.ReferenceMask, l.ReferenceMask,
			g.LoopFilterLevel, l.LoopFilterLevel, g.TileLog2Cols,
			l.TileLog2Cols, g.TemporalLayerID, l.TemporalLayerID,
			g.TemporalLayerCount, l.TemporalLayerCount, g.TL0PICIDX,
			l.TL0PICIDX, g.TemporalLayerSync, l.TemporalLayerSync)
	}
	return b.String()
}

func vp9DroppedFrameIndices(rows []vp9RateScoreboardRow) []int {
	out := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Dropped {
			out = append(out, row.FrameIndex)
		}
	}
	return out
}

func vp9DropReasonCount(rows []vp9RateScoreboardRow, reason string) int {
	count := 0
	for _, row := range rows {
		if row.DropReason == reason {
			count++
		}
	}
	return count
}

func vp9SameIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func vp9QHistogram(rows []vp9RateScoreboardRow) [256]int {
	var hist [256]int
	for _, row := range rows {
		if row.Dropped {
			continue
		}
		if uint(row.BaseQIndex) < uint(len(hist)) {
			hist[row.BaseQIndex]++
		}
	}
	return hist
}

func vp9HistogramDistance(a, b [256]int) (distance, mismatchedBins int) {
	for i := range a {
		d := a[i] - b[i]
		if d != 0 {
			mismatchedBins++
			if d < 0 {
				d = -d
			}
			distance += d
		}
	}
	return distance, mismatchedBins
}

func formatVP9QHistogram(hist [256]int) string {
	var b bytes.Buffer
	first := true
	for q, count := range hist {
		if count == 0 {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d:%d", q, count)
		first = false
	}
	if first {
		return "empty"
	}
	return b.String()
}
