//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9CyclicRefreshParitySeeds pins realtime speed-8 CBR cyclic-AQ schedules.
// Frame 0 (key) byte-matches libvpx; inter frames remain in the open parity
// lane until the cyclic refresh encode path closes.
var vp9CyclicRefreshParitySeeds = [][]byte{
	// (dimBucket=64, frames=6, source=panning)
	{0, 1, 0},
	// (dimBucket=64, frames=4, source=constant)
	{0, 0, 1},
}

func vp9CyclicRefreshFuzzCaseFromBytes(data []byte) vp9CyclicRefreshParityCase {
	const (
		width  = 64
		height = 64
	)
	frames := 6
	if len(data) > 1 {
		switch data[1] % 3 {
		case 0:
			frames = 4
		case 1:
			frames = 6
		default:
			frames = 8
		}
	}
	sourceKind := 0
	if len(data) > 2 {
		sourceKind = int(data[2] % 2)
	}
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		if sourceKind == 1 {
			sources[i] = vp9test.NewYCbCr(width, height, 128, 128, 128)
		} else {
			sources[i] = vp9test.NewPanningYCbCr(width, height, i)
		}
	}
	return vp9CyclicRefreshParityCase{
		name: fmt.Sprintf("cyclic-cbr-rt8-%dx%d-f%d-src%d",
			width, height, frames, sourceKind),
		opts:    vp9OracleCyclicRefreshCBROptions(width, height, 700),
		sources: sources,
		extraArgs: vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500,
			0),
	}
}

type vp9CyclicRefreshParityCase struct {
	name      string
	opts      VP9EncoderOptions
	sources   []*image.YCbCr
	extraArgs []string
}

func TestVP9EncoderVpxencOracleCBRCyclicRefreshKeyframeByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewPanningYCbCr(width, height, 0)
	assertVP9VpxencKeyframeByteParityWithOptions(t, src,
		vp9OracleCyclicRefreshCBROptions(width, height, 700),
		vp9OracleCyclicRefreshCBRVpxencArgs(700, 600, 400, 500, 0))
}

func TestVP9OracleCyclicRefreshCBRRealtimeRateParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh CBR rate parity")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 10
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9OracleCyclicRefreshCBROptions(width, height, 700)
	extraArgs := vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500, 0)

	govpxRows := captureVP9RateScoreboardRows(t, opts, sources, nil)
	libvpxRows := captureLibvpxVP9RateScoreboardRows(t, width, height, sources,
		nil, extraArgs)
	if len(govpxRows) != len(libvpxRows) {
		t.Fatalf("rate rows: govpx=%d libvpx=%d", len(govpxRows), len(libvpxRows))
	}

	var qDriftMax, sizePctMax, bufferPctMax float64
	refreshMatches := 0
	targetMatches := 0
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		if g.Dropped || l.Dropped {
			t.Fatalf("row %d dropped: govpx=%t libvpx=%t", i, g.Dropped, l.Dropped)
		}
		if g.RefreshFrameFlags == l.RefreshFrameFlags {
			refreshMatches++
		}
		if g.FrameTargetBits == l.FrameTargetBits {
			targetMatches++
		}
		qDriftMax = math.Max(qDriftMax, math.Abs(float64(g.BaseQIndex-l.BaseQIndex)))
		sizePctMax = math.Max(sizePctMax, vp9test.PctDelta(g.SizeBits, l.SizeBits))
		bufferPctMax = math.Max(bufferPctMax,
			vp9test.PctDelta(g.BufferLevelBits, l.BufferLevelBits))
	}
	t.Logf("VP9 cyclic CBR scoreboard: rows=%d refresh=%d/%d targets=%d/%d max_q=%.0f max_size_pct=%.2f max_buffer_pct=%.2f",
		len(govpxRows), refreshMatches, len(govpxRows), targetMatches,
		len(govpxRows), qDriftMax, sizePctMax, bufferPctMax)
	t.Logf("VP9 cyclic CBR scoreboard rows:\n%s",
		vp9test.FormatRateScoreboardRows(govpxRows, libvpxRows))

	// Cyclic refresh may schedule golden updates on different frames than
	// libvpx until postencode/resize parity fully closes; keyframe refresh
	// must still match.
	if govpxRows[0].RefreshFrameFlags != libvpxRows[0].RefreshFrameFlags {
		t.Fatalf("keyframe refresh flags: govpx=0x%x libvpx=0x%x",
			govpxRows[0].RefreshFrameFlags, libvpxRows[0].RefreshFrameFlags)
	}
	// Strengthened non-strict gates for the stabilized cyclic lane.
	if refreshMatches != len(govpxRows) {
		t.Fatalf("refresh flags mismatch: got %d/%d want %d/%d",
			refreshMatches, len(govpxRows), len(govpxRows), len(govpxRows))
	}
	if targetMatches < 6 {
		t.Fatalf("frame_target parity regressed: got %d/%d want >= 6/10",
			targetMatches, len(govpxRows))
	}
	if qDriftMax > 1 {
		t.Fatalf("base_qindex drift regressed: max_q=%.0f want <= 1", qDriftMax)
	}
	if sizePctMax > 5.0 {
		t.Fatalf("inter size drift regressed: max_size_pct=%.2f want <= 5.00", sizePctMax)
	}
	if bufferPctMax > 1.0 {
		t.Fatalf("buffer drift regressed: max_buffer_pct=%.2f want <= 1.00", bufferPctMax)
	}
	if vp9test.StrictEnv("GOVPX_VP9_CYCLIC_SCOREBOARD_STRICT") {
		if refreshMatches != len(govpxRows) ||
			targetMatches != len(govpxRows) || qDriftMax != 0 ||
			sizePctMax != 0 || bufferPctMax != 0 {
			t.Fatalf("strict cyclic scoreboard drift: refresh=%d/%d targets=%d/%d max_q=%.0f max_size_pct=%.2f max_buffer_pct=%.2f",
				refreshMatches, len(govpxRows), targetMatches, len(govpxRows),
				qDriftMax, sizePctMax, bufferPctMax)
		}
	}
}

func TestVP9OracleCyclicRefreshSegmentationHeaderFlagsParity(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh segmentation header flags")
	vp9test.RequireVpxencFrameFlags(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9OracleCyclicRefreshCBROptions(width, height, 700)
	extraArgs := vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500, 0)

	govpxPackets := encodeVP9FramesWithGovpx(t, opts, sources, nil)
	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, sources,
		vp9LibvpxFrameFlags(nil), extraArgs...)
	if len(govpxPackets) != len(libvpxPackets) {
		t.Fatalf("packets: govpx=%d libvpx=%d", len(govpxPackets), len(libvpxPackets))
	}

	keyHeader, _ := vp9test.ParseHeader(t, govpxPackets[0])
	temporalMatches := 0
	for _, frame := range []int{1, 2, 3} {
		gHdr := readVP9OraclePacketHeader(t, "govpx", frame, govpxPackets[frame],
			&keyHeader, width, height)
		lHdr := readVP9OraclePacketHeader(t, "libvpx", frame,
			libvpxPackets[frame], &keyHeader, width, height)
		if gHdr.Seg.Enabled != lHdr.Seg.Enabled ||
			gHdr.Seg.UpdateMap != lHdr.Seg.UpdateMap ||
			gHdr.Seg.UpdateData != lHdr.Seg.UpdateData {
			t.Fatalf("frame %d seg flags: govpx enabled=%t updateMap=%t updateData=%t libvpx enabled=%t updateMap=%t updateData=%t",
				frame, gHdr.Seg.Enabled, gHdr.Seg.UpdateMap, gHdr.Seg.UpdateData,
				lHdr.Seg.Enabled, lHdr.Seg.UpdateMap, lHdr.Seg.UpdateData)
		}
		if gHdr.Seg.TemporalUpdate == lHdr.Seg.TemporalUpdate {
			temporalMatches++
		}
	}
	if temporalMatches != 3 {
		t.Fatalf("cyclic refresh segmentation temporal_update matched %d/3 inter frames, want 3/3",
			temporalMatches)
	}
}

func TestVP9OracleCyclicRefreshKeyframeSeedsMatchLibvpx(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh keyframe regression seeds")
	vp9test.RequireVpxencFrameFlags(t)

	pass, fail := 0, 0
	aggSizeDelta := 0
	for idx, seed := range vp9CyclicRefreshParitySeeds {
		tc := vp9CyclicRefreshFuzzCaseFromBytes(seed)
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("cyclic-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, nil)
		want := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(nil), tc.extraArgs...)
		if len(got) == 0 || len(want) == 0 {
			t.Fatalf("%s empty packets", label)
		}
		keyDelta := len(got[0]) - len(want[0])
		if keyDelta < 0 {
			keyDelta = -keyDelta
		}
		aggSizeDelta += keyDelta
		if bytes.Equal(got[0], want[0]) {
			t.Logf("%s PASS keyframe (delta=%+d frames=%d)", label, keyDelta, len(got))
			pass++
			continue
		}
		fail++
		t.Errorf("%s FAIL keyframe: got_len=%d want_len=%d first_byte_diff=%d",
			label, len(got[0]), len(want[0]),
			testutil.FirstByteDiff(got[0], want[0]))
	}
	t.Logf("Cyclic refresh keyframe seeds: PASS=%d FAIL=%d total=%d agg_key_delta=%+d",
		pass, fail, len(vp9CyclicRefreshParitySeeds), aggSizeDelta)
	if fail != 0 {
		t.Fatalf("cyclic refresh keyframe seeds lost byte parity: fail=%d", fail)
	}
}

func TestVP9OracleCyclicRefreshInterParityGapSeedsRemainMeasurable(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 cyclic refresh inter parity-gap seeds")
	vp9test.RequireVpxencFrameFlags(t)

	for idx, seed := range vp9CyclicRefreshParitySeeds {
		tc := vp9CyclicRefreshFuzzCaseFromBytes(seed)
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("cyclic-inter-#%d-%s", idx, hex.EncodeToString(sum[:4]))
		got := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, nil)
		want := vp9test.VpxencFrameFlagPackets(t, tc.sources,
			vp9LibvpxFrameFlags(nil), tc.extraArgs...)
		if len(got) < 2 || len(want) < 2 {
			t.Fatalf("%s need inter frames", label)
		}
		matches := 0
		aggDelta := 0
		for i := 1; i < len(got) && i < len(want); i++ {
			delta := len(got[i]) - len(want[i])
			aggDelta += delta
			if bytes.Equal(got[i], want[i]) {
				matches++
			}
		}
		t.Logf("%s inter byte parity %d/%d total_size_delta=%+d (open lane)",
			label, matches, len(got)-1, aggDelta)
		if matches == len(got)-1 {
			t.Logf("%s WARNING: full inter byte parity — consider promoting seed to vp9CyclicRefreshParitySeeds strict corpus", label)
		}
	}
}

func readVP9OraclePacketHeader(t *testing.T, side string, frame int,
	packet []byte, key *vp9dec.UncompressedHeader, width, height int,
) vp9dec.UncompressedHeader {
	t.Helper()
	if key == nil {
		t.Fatalf("%s frame %d: nil key header", side, frame)
	}
	var br vp9dec.BitReader
	br.Init(packet)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, key,
		func(uint8) (uint32, uint32) { return uint32(width), uint32(height) })
	if err != nil {
		t.Fatalf("%s ReadUncompressedHeader frame %d: %v", side, frame, err)
	}
	return hdr
}
