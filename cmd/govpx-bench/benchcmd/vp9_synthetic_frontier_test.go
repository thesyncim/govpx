//go:build govpx_oracle_trace

package benchcmd

import (
	"bytes"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9BenchmarkSyntheticByteParityFrontier(t *testing.T) {
	vp9test.RequireVpxenc(t)
	if !vp9test.StrictEnv("GOVPX_VP9_BENCH_SYNTH_FRONTIER") {
		t.Skip("set GOVPX_VP9_BENCH_SYNTH_FRONTIER=1 to run the 720p benchmark synthetic frontier")
	}

	cfg := benchConfig{
		Codec:       codecVP9,
		Width:       1280,
		Height:      720,
		Frames:      480,
		FPS:         30,
		BitrateKbps: 2500,
		Mode:        "realtime",
		CpuUsed:     8,
		Threads:     1,
		SkipQuality: true,
	}
	govpxPackets, libvpxPackets := encodeVP9BenchmarkSyntheticPackets(t, cfg)
	prefix := matchedVP9PacketRecordPrefix(govpxPackets, libvpxPackets)
	minPackets := min(len(govpxPackets), len(libvpxPackets))
	t.Logf("benchmark synthetic 720p realtime cpu8 matched emitted packets=%d/%d govpx_packets=%d libvpx_packets=%d govpx_drops=%d libvpx_drops=%d",
		prefix, minPackets, len(govpxPackets), len(libvpxPackets), cfg.Frames-len(govpxPackets), cfg.Frames-len(libvpxPackets))
	const (
		wantPackets          = 468
		wantDrops            = 12
		wantPrefix           = 1
		wantDivergenceSource = 10
		wantGovpxBytes       = 11179
		wantLibvpxBytes      = 11136
		wantFirstByteDiff    = 4
	)
	if len(govpxPackets) != wantPackets || len(libvpxPackets) != wantPackets {
		t.Fatalf("benchmark synthetic emitted packets govpx=%d libvpx=%d, want %d each",
			len(govpxPackets), len(libvpxPackets), wantPackets)
	}
	if cfg.Frames-len(govpxPackets) != wantDrops || cfg.Frames-len(libvpxPackets) != wantDrops {
		t.Fatalf("benchmark synthetic drops govpx=%d libvpx=%d, want %d each",
			cfg.Frames-len(govpxPackets), cfg.Frames-len(libvpxPackets), wantDrops)
	}
	if prefix == minPackets && len(govpxPackets) == len(libvpxPackets) {
		t.Fatalf("480-frame benchmark synthetic unexpectedly byte-exact; update docs/perf-phase2-plan.md and promote this to a gate")
	}
	if prefix != wantPrefix {
		t.Fatalf("benchmark synthetic byte frontier moved to emitted packet %d, want %d; update docs/perf-phase2-plan.md before changing perf-sensitive paths",
			prefix, wantPrefix)
	}

	g, l := govpxPackets[prefix], libvpxPackets[prefix]
	firstDiff := testutil.FirstByteDiff(g.data, l.data)
	if g.sourceIndex != wantDivergenceSource || l.sourceIndex != wantDivergenceSource ||
		len(g.data) != wantGovpxBytes || len(l.data) != wantLibvpxBytes || firstDiff != wantFirstByteDiff {
		t.Fatalf("benchmark synthetic divergence moved: packet=%d govpx_src=%d libvpx_src=%d govpx=%d bytes libvpx=%d bytes first_diff=%d",
			prefix, g.sourceIndex, l.sourceIndex, len(g.data), len(l.data), firstDiff)
	}
	t.Logf("pinned benchmark synthetic divergence: emitted packet=%d source=%d govpx=%d bytes libvpx=%d bytes first_diff=%d",
		prefix, g.sourceIndex, len(g.data), len(l.data), firstDiff)
}

type vp9PacketRecord struct {
	sourceIndex int
	data        []byte
}

func matchedVP9PacketRecordPrefix(got, want []vp9PacketRecord) int {
	n := min(len(got), len(want))
	for i := range n {
		if got[i].sourceIndex != want[i].sourceIndex || !bytes.Equal(got[i].data, want[i].data) {
			return i
		}
	}
	return n
}

func encodeVP9BenchmarkSyntheticPackets(t *testing.T, cfg benchConfig) ([]vp9PacketRecord, []vp9PacketRecord) {
	t.Helper()
	deadline, _, err := benchmarkDeadline(cfg.Mode)
	if err != nil {
		t.Fatalf("benchmarkDeadline: %v", err)
	}

	frames := make([]govpx.Image, cfg.Frames)
	ycbcr := make([]*image.YCbCr, cfg.Frames)
	for i := range frames {
		frames[i] = makeBenchmarkFrame(cfg.Width, cfg.Height, i)
		ycbcr[i] = imageToYCbCr(frames[i])
	}

	opts := vp9BenchmarkEncoderOptions(cfg, deadline)
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	dst := make([]byte, max(4096, cfg.Width*cfg.Height*6))
	govpxPackets := make([]vp9PacketRecord, 0, cfg.Frames)
	for i := range ycbcr {
		result, err := enc.EncodeIntoWithResult(ycbcr[i], dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		govpxPackets = append(govpxPackets, vp9PacketRecord{
			sourceIndex: i,
			data:        append([]byte(nil), result.Data...),
		})
	}

	parity := parityFor(cfg)
	libvpxArgs := libvpxVP9ParityFlags(cfg, parity, "--rt")
	libvpxIVF := vp9test.VpxencIVF(t, ycbcr, libvpxArgs...)
	libvpxPackets := vp9BenchmarkSyntheticIVFRecords(t, libvpxIVF)
	return govpxPackets, libvpxPackets
}

func vp9BenchmarkSyntheticIVFRecords(t *testing.T, data []byte) []vp9PacketRecord {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(data)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	out := make([]vp9PacketRecord, 0)
	for packetIndex := 0; offset < len(data); packetIndex++ {
		frame, next, err := testutil.NextIVFFrame(data, offset, packetIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d]: %v", packetIndex, err)
		}
		out = append(out, vp9PacketRecord{
			sourceIndex: int(frame.Timestamp),
			data:        append([]byte(nil), frame.Data...),
		})
		offset = next
	}
	return out
}
