//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

type EncoderHook func(*govpx.VP9Encoder, int)

func CaptureRateTraceRows(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flags []govpx.EncodeFlags,
) []vp9test.RateTraceRow {
	t.Helper()
	return CaptureRateTraceRowsWithHooks(t, opts, sources, flags, nil)
}

func CaptureRateTraceRowsWithHooks(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, beforeFrame EncoderHook,
) []vp9test.RateTraceRow {
	t.Helper()
	width, height := validateFixedSources(t, "VP9 rate trace", sources, flags)
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	var trace bytes.Buffer
	enc.SetOracleTraceWriter(&trace)
	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f govpx.EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if _, err := enc.EncodeIntoWithFlagsResult(src, dst, f); err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
	}
	return vp9test.ParseRateTraceRows(t, trace.Bytes())
}

func CaptureLibvpxRateTraceRows(t testing.TB, width int, height int,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, extraArgs []string,
) []vp9test.RateTraceRow {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx rate-trace source")
	}
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	rows, packets := vp9test.VpxencFrameFlagTracePackets(t, sources,
		LibvpxFrameFlags(flags), extraArgs...)
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		vp9test.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows
}

func CaptureStreamParityPacketRowsWithHooks(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
	flags []govpx.EncodeFlags, extraArgs []string, beforeFrame EncoderHook,
) ([]vp9test.RateTraceRow, [][]byte, []vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	govpxRows, govpxPackets := CaptureGovpxStreamParityPacketRowsWithHooks(t,
		opts, sources, flags, beforeFrame)
	libvpxRows, libvpxPackets := CaptureLibvpxStreamParityPacketRows(t,
		sources, flags, extraArgs)
	return govpxRows, govpxPackets, libvpxRows, libvpxPackets
}

func CaptureStreamParityPackets(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
	flags []govpx.EncodeFlags, extraArgs []string,
) ([][]byte, [][]byte) {
	t.Helper()
	return CaptureStreamParityPacketsWithHooks(t, opts, sources, flags,
		extraArgs, nil)
}

func CaptureStreamParityPacketsWithHooks(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
	flags []govpx.EncodeFlags, extraArgs []string, beforeFrame EncoderHook,
) ([][]byte, [][]byte) {
	t.Helper()
	_, govpxPackets, _, libvpxPackets := CaptureStreamParityPacketRowsWithHooks(t,
		opts, sources, flags, extraArgs, beforeFrame)
	return govpxPackets, libvpxPackets
}

func CaptureGovpxStreamParityPacketRowsWithHooks(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
	flags []govpx.EncodeFlags, beforeFrame EncoderHook,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	width, height := validateFixedSources(t, "VP9 stream parity", sources, flags)
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	var trace bytes.Buffer
	enc.SetOracleTraceWriter(&trace)
	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f govpx.EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		if f&govpx.EncodeInvisibleFrame != 0 {
			t.Fatalf("frame %d uses EncodeInvisibleFrame, which has no VP9 libvpx flag bit", i)
		}
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := vp9test.ParseRateTraceRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		if len(packets[i]) == 0 {
			t.Fatalf("govpx VP9 row %d was not dropped but has no packet", i)
		}
		vp9test.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func CaptureLibvpxStreamParityPacketRows(t testing.TB,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, extraArgs []string,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	rows, packets := vp9test.VpxencFrameFlagTracePackets(t, sources,
		LibvpxFrameFlags(flags), extraArgs...)
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		vp9test.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func CaptureVariablePacketRows(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, beforeFrame EncoderHook,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	validateVariableSources(t, "VP9 variable packet trace", sources, flags)
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	var trace bytes.Buffer
	enc.SetOracleTraceWriter(&trace)
	packets := make([][]byte, len(sources))
	for i, src := range sources {
		if beforeFrame != nil {
			beforeFrame(enc, i)
		}
		var f govpx.EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		dstSize, err := EncodeBufferSize(src.Rect.Dx(), src.Rect.Dy())
		if err != nil {
			t.Fatalf("EncodeBufferSize frame %d: %v", i, err)
		}
		dst := make([]byte, dstSize)
		result, err := enc.EncodeIntoWithFlagsResult(src, dst, f)
		if err != nil {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithFlagsResult frame %d unexpectedly dropped", i)
		}
		packets[i] = append([]byte(nil), result.Data...)
	}
	rows := vp9test.ParseRateTraceRows(t, trace.Bytes())
	if len(rows) != len(sources) {
		t.Fatalf("govpx VP9 variable trace rows = %d, want %d", len(rows), len(sources))
	}
	for i := range rows {
		vp9test.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func CaptureLibvpxVariablePacketRows(t testing.TB,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, invisible []bool,
	extraArgs []string,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 libvpx variable packet source")
	}
	rows, packets := vp9test.VpxencVariableFrameFlagTracePackets(t, sources,
		LibvpxFrameFlags(flags), invisible, extraArgs...)
	for i := range rows {
		vp9test.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func validateFixedSources(t testing.TB, label string, sources []*image.YCbCr,
	flags []govpx.EncodeFlags,
) (width int, height int) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("empty %s source", label)
	}
	if len(flags) > len(sources) {
		t.Fatalf("%s flag count = %d, want <= %d", label, len(flags), len(sources))
	}
	width = sources[0].Rect.Dx()
	height = sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("source %d dimension mismatch: got %dx%d want %dx%d",
				i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	return width, height
}

func validateVariableSources(t testing.TB, label string, sources []*image.YCbCr,
	flags []govpx.EncodeFlags,
) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("empty %s source", label)
	}
	if len(flags) > len(sources) {
		t.Fatalf("%s flag count = %d, want <= %d", label, len(flags), len(sources))
	}
}
