//go:build govpx_oracle_trace

package vp9oracle

import (
	"bytes"
	"errors"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func CaptureLookaheadPackets(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr,
) [][]byte {
	t.Helper()
	return captureLookaheadPackets(t, opts, sources, nil)
}

func CaptureLookaheadPacketsWithFlushes(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr, flushAfter []int,
) [][]byte {
	t.Helper()
	return captureLookaheadPackets(t, opts, sources, flushAfter)
}

func CaptureGovpxAutoAltRefPacketRows(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 auto-alt-ref source")
	}
	opts.Width = sources[0].Rect.Dx()
	opts.Height = sources[0].Rect.Dy()
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	var trace bytes.Buffer
	enc.SetOracleTraceWriter(&trace)
	dstSize, err := EncodeBufferSize(opts.Width, opts.Height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	packets := make([][]byte, 0, len(sources)+1)
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	packets = append(packets, drainLookaheadFlush(t, enc, dst)...)
	rows := vp9test.ParseRateTraceRows(t, trace.Bytes())
	if len(rows) != len(packets) {
		t.Fatalf("govpx auto-alt-ref trace rows = %d, packets = %d",
			len(rows), len(packets))
	}
	var headers vp9test.HeaderStreamState
	for i := range rows {
		headers.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func CaptureLibvpxAutoAltRefPacketRows(t testing.TB,
	sources []*image.YCbCr, extraArgs ...string,
) ([]vp9test.RateTraceRow, [][]byte) {
	t.Helper()
	rows, packets := vp9test.VpxencFrameFlagTracePackets(t, sources, nil,
		extraArgs...)
	var headers vp9test.HeaderStreamState
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		headers.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows, packets
}

func captureLookaheadPackets(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flushAfter []int,
) [][]byte {
	t.Helper()
	width, height := validateFixedSources(t, "VP9 lookahead", sources, nil)
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	flushSet := flushIndexSet(flushAfter)
	packets := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeIntoWithResult(src, dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			// Keep filling the lookahead queue.
		} else if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		} else {
			if result.Dropped {
				t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
			}
			packets = append(packets, append([]byte(nil), result.Data...))
		}
		if flushSet[i] {
			packets = append(packets, drainLookaheadFlush(t, enc, dst)...)
		}
	}
	packets = append(packets, drainLookaheadFlush(t, enc, dst)...)
	if len(packets) != len(sources) {
		t.Fatalf("VP9 lookahead packets = %d, want %d",
			len(packets), len(sources))
	}
	return packets
}

func drainLookaheadFlush(t testing.TB, enc *govpx.VP9Encoder, dst []byte) [][]byte {
	t.Helper()
	var packets [][]byte
	for {
		result, err := enc.FlushIntoWithResult(dst)
		if errors.Is(err, govpx.ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		if result.Dropped {
			t.Fatal("FlushIntoWithResult unexpectedly dropped")
		}
		packets = append(packets, append([]byte(nil), result.Data...))
	}
	return packets
}

func flushIndexSet(indexes []int) map[int]bool {
	set := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		set[index] = true
	}
	return set
}
