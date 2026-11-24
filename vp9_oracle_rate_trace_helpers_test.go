//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func captureVP9RateTraceRows(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
) []vp9test.RateTraceRow {
	t.Helper()
	return captureVP9RateTraceRowsWithHooks(t, opts, sources, flags, nil)
}

func captureVP9RateTraceRowsWithHooks(t *testing.T, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags,
	beforeFrame func(*VP9Encoder, int),
) []vp9test.RateTraceRow {
	t.Helper()
	var trace bytes.Buffer
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	enc.SetOracleTraceWriter(&trace)
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
	return vp9test.ParseRateTraceRows(t, trace.Bytes())
}

func captureLibvpxVP9RateTraceRows(t *testing.T, width int, height int,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
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
		vp9LibvpxFrameFlags(flags), extraArgs...)
	var headers vp9test.HeaderStreamState
	for i := range rows {
		if rows[i].Dropped {
			continue
		}
		headers.EnrichRateTraceRowFromPacket(t, &rows[i], packets[i])
	}
	return rows
}
