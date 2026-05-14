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
}

type vp9RateScoreboardRow struct {
	FrameIndex        int
	Dropped           bool
	DropReason        string
	BaseQIndex        int
	SizeBits          int
	BufferLevelBits   int
	RefreshFrameFlags uint8
	TemporalLayerID   int
	TemporalLayerSync bool
	RecodeAllowed     bool
	RecodeLoopCount   int
}

func captureVP9RateScoreboardRows(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
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
		header, _ := parseVP9EncoderHeaderForTest(t, frame.Data)
		rows[i].RefreshFrameFlags = header.RefreshFrameFlags
	}
	return rows
}

func parseVP9RateScoreboardRows(t *testing.T, trace []byte) []vp9RateScoreboardRow {
	t.Helper()
	rows := make([]vp9RateScoreboardRow, 0, bytes.Count(trace, []byte("\n")))
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var raw struct {
			Row               string `json:"row"`
			FrameIndex        int    `json:"frame_index"`
			Dropped           bool   `json:"dropped"`
			DropReason        string `json:"drop_reason"`
			BaseQIndex        int    `json:"base_qindex"`
			SizeBytes         int    `json:"size_bytes"`
			SizeBits          int    `json:"size_bits"`
			BufferLevelBits   int    `json:"buffer_level_bits"`
			RefreshFrameFlags uint8  `json:"refresh_frame_flags"`
			TemporalLayerID   int    `json:"temporal_layer_id"`
			TemporalLayerSync bool   `json:"temporal_layer_sync"`
			RecodeAllowed     bool   `json:"recode_allowed"`
			RecodeLoopCount   int    `json:"recode_loop_count"`
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
			FrameIndex:        raw.FrameIndex,
			Dropped:           raw.Dropped,
			DropReason:        raw.DropReason,
			BaseQIndex:        raw.BaseQIndex,
			SizeBits:          sizeBits,
			BufferLevelBits:   raw.BufferLevelBits,
			RefreshFrameFlags: raw.RefreshFrameFlags,
			TemporalLayerID:   raw.TemporalLayerID,
			TemporalLayerSync: raw.TemporalLayerSync,
			RecodeAllowed:     raw.RecodeAllowed,
			RecodeLoopCount:   raw.RecodeLoopCount,
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

func pctDelta(got int, want int) float64 {
	den := math.Max(1, math.Abs(float64(want)))
	return math.Abs(float64(got-want)) * 100 / den
}

func formatVP9RateScoreboardRows(govpxRows, libvpxRows []vp9RateScoreboardRow) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,govpx_drop,libvpx_drop,govpx_q,libvpx_q,govpx_bits,libvpx_bits,govpx_buffer,libvpx_buffer,govpx_refresh,libvpx_refresh")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		fmt.Fprintf(&b, "%d,%t,%t,%d,%d,%d,%d,%d,%d,%#x,%#x\n",
			g.FrameIndex, g.Dropped, l.Dropped, g.BaseQIndex, l.BaseQIndex,
			g.SizeBits, l.SizeBits, g.BufferLevelBits, l.BufferLevelBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags)
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

func newVP9PanningYCbCrForRateTest(width, height int, frame int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			row[x] = byte(24 + ((x+frame*3)*7+y*11+(x*y+frame*13)%37)%208)
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvWidth {
			cb[x] = byte(64 + ((x+frame)*5+y*3)%128)
			cr[x] = byte(72 + (x*3+(y+frame)*7)%112)
		}
	}
	return img
}
