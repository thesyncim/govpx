//go:build govpx_oracle_trace

package vp9test

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

func VpxencPackets(t testing.TB, sources []*image.YCbCr, extraArgs ...string) [][]byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 vpxenc source", sources)
	var raw []byte
	for _, src := range sources {
		raw = AppendI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return RequireIVFPackets(t, ivf, len(sources))
}

func VpxencFrameFlagPackets(t testing.TB, sources []*image.YCbCr, frameFlags []uint32, extraArgs ...string) [][]byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 frame-flags source", sources)
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 frame-flags has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	var raw []byte
	for _, src := range sources {
		raw = AppendI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9FrameFlagsEncodeI420(raw, width,
		height, len(sources), frameFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags encode failed: %v\n%s", err, diag)
	}
	return RequireIVFPackets(t, ivf, len(sources))
}

func VpxencFrameFlagTracePackets(t testing.TB, sources []*image.YCbCr, frameFlags []uint32, extraArgs ...string) ([]RateScoreboardRow, [][]byte) {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 frame-flags trace source", sources)
	if len(frameFlags) > len(sources) {
		t.Fatalf("VP9 frame-flags trace has %d entries for %d source frames",
			len(frameFlags), len(sources))
	}
	var raw []byte
	for _, src := range sources {
		raw = AppendI420(raw, src)
	}
	ivf, trace, diag, err := coracle.VpxencVP9FrameFlagsTraceI420(raw, width,
		height, len(sources), frameFlags, extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9-frameflags trace failed: %v\n%s", err, diag)
	}
	rows := ParseRateScoreboardRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	return rows, vpxencPacketsForTraceRows(t, "libvpx VP9", ivf, rows, true)
}

func VpxencVariableFrameFlagTracePackets(t testing.TB, sources []*image.YCbCr,
	frameFlags []uint32, invisibleFrames []bool, extraArgs ...string,
) ([]RateScoreboardRow, [][]byte) {
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
	rows := ParseRateScoreboardRows(t, trace)
	if len(rows) != len(sources) {
		t.Fatalf("libvpx VP9 variable trace rows = %d, want %d",
			len(rows), len(sources))
	}
	return rows, vpxencPacketsForTraceRows(t, "libvpx VP9 variable", ivf,
		rows, false)
}

func vpxencPacketsForTraceRows(t testing.TB, label string, ivf []byte,
	rows []RateScoreboardRow, allowDropped bool,
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
	if len(sources) == 0 {
		t.Fatalf("empty %s", label)
	}
	width = sources[0].Rect.Dx()
	height = sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("%s %d dimension mismatch: got %dx%d want %dx%d",
				label, i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	return width, height
}
