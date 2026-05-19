package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// encodeWithAnalysis runs a deterministic VP8 encode for the given
// frame count, returning the per-frame packet bytes, their concatenated
// SHA-256 hash, and the number of dropped frames. The caller controls
// the analysis configuration; the rest of the encode parameters are
// fixed so two runs with different Analysis values can be compared
// byte-for-byte.
func encodeWithAnalysis(t *testing.T, width, height, frames int, cfg VP8AnalysisConfig) ([][]byte, string, int) {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    false,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    30,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Threads:             1,
		Analysis:            cfg,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder cfg=%+v: %v", cfg, err)
	}
	defer e.Close()

	img := testImage(width, height)
	buf := make([]byte, width*height*4)

	packets := make([][]byte, 0, frames)
	h := sha256.New()
	dropped := 0
	for i := range frames {
		// Fill the source deterministically; the exact recipe does
		// not matter as long as it is identical between runs.
		for j := range img.Y {
			img.Y[j] = byte((j*7 + i*13) & 0xFF)
		}
		for j := range img.U {
			img.U[j] = byte(96 + ((j + i*3) & 0x3F))
		}
		for j := range img.V {
			img.V[j] = byte(144 + ((j*2 + i*5) & 0x3F))
		}
		result, err := e.EncodeInto(buf, img, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d cfg=%+v: %v", i, cfg, err)
		}
		if result.Dropped {
			dropped++
			packets = append(packets, nil)
			continue
		}
		pkt := append([]byte(nil), result.Data...)
		packets = append(packets, pkt)
		h.Write(pkt)
	}
	return packets, hex.EncodeToString(h.Sum(nil)), dropped
}

// TestVP8AnalysisOffBaselineStable confirms two VP8AnalysisOff runs
// produce identical bitstreams. Without this, the byte-parity test
// below cannot be trusted: a flaky encoder would make any comparison
// meaningless.
func TestVP8AnalysisOffBaselineStable(t *testing.T) {
	const (
		width  = 320
		height = 240
		frames = 16
	)
	cfg := DefaultVP8AnalysisConfig()
	_, hashA, droppedA := encodeWithAnalysis(t, width, height, frames, cfg)
	_, hashB, droppedB := encodeWithAnalysis(t, width, height, frames, cfg)
	if hashA != hashB {
		t.Fatalf("VP8AnalysisOff is not deterministic: %s vs %s", hashA, hashB)
	}
	if droppedA != droppedB {
		t.Fatalf("VP8AnalysisOff dropped-frame count mismatch: %d vs %d", droppedA, droppedB)
	}
	t.Logf("VP8AnalysisOff baseline sha256=%s dropped=%d", hashA, droppedA)
}

// TestVP8AnalysisObserveCPUByteParity is the primary byte-parity proof.
// VP8AnalysisObserveCPU with every collection flag enabled must produce
// a bitstream that is byte-identical to VP8AnalysisOff for the same
// input stream, frame-by-frame.
func TestVP8AnalysisObserveCPUByteParity(t *testing.T) {
	const (
		width  = 320
		height = 240
		frames = 16
	)
	off := DefaultVP8AnalysisConfig()
	observe := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		ByteParityRequired: true,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}

	offPackets, offHash, offDropped := encodeWithAnalysis(t, width, height, frames, off)
	obsPackets, obsHash, obsDropped := encodeWithAnalysis(t, width, height, frames, observe)

	if offHash != obsHash {
		t.Fatalf("byte parity violation: VP8AnalysisOff sha256=%s, VP8AnalysisObserveCPU sha256=%s",
			offHash, obsHash)
	}
	if offDropped != obsDropped {
		t.Fatalf("dropped-frame mismatch: off=%d observe=%d", offDropped, obsDropped)
	}
	if len(offPackets) != len(obsPackets) {
		t.Fatalf("packet count mismatch: off=%d observe=%d", len(offPackets), len(obsPackets))
	}
	for i := range offPackets {
		if !bytes.Equal(offPackets[i], obsPackets[i]) {
			t.Fatalf("frame %d differs: off=%d bytes, observe=%d bytes",
				i, len(offPackets[i]), len(obsPackets[i]))
		}
	}
	t.Logf("byte parity confirmed sha256=%s frames=%d dropped=%d", offHash, frames, offDropped)
}

// TestVP8AnalysisObserveCPUFlagsByteParity confirms that flipping
// individual collection flags one at a time still preserves byte
// parity with VP8AnalysisOff. This catches regressions where a
// particular collector accidentally reads/writes encoder state.
func TestVP8AnalysisObserveCPUFlagsByteParity(t *testing.T) {
	const (
		width  = 176
		height = 144
		frames = 12
	)
	off := DefaultVP8AnalysisConfig()
	_, offHash, _ := encodeWithAnalysis(t, width, height, frames, off)
	flagSets := []struct {
		name string
		cfg  VP8AnalysisConfig
	}{
		{"observe-bare", VP8AnalysisConfig{Mode: VP8AnalysisObserveCPU}},
		{"observe-motion", VP8AnalysisConfig{Mode: VP8AnalysisObserveCPU, CollectMotionHints: true}},
		{"observe-skipmap", VP8AnalysisConfig{Mode: VP8AnalysisObserveCPU, CollectSkipMap: true}},
		{"observe-complexity", VP8AnalysisConfig{Mode: VP8AnalysisObserveCPU, CollectComplexity: true}},
		{"observe-skip+complexity", VP8AnalysisConfig{Mode: VP8AnalysisObserveCPU, CollectSkipMap: true, CollectComplexity: true}},
	}
	for _, fs := range flagSets {
		t.Run(fs.name, func(t *testing.T) {
			_, h, _ := encodeWithAnalysis(t, width, height, frames, fs.cfg)
			if h != offHash {
				t.Fatalf("byte parity violation %s: off=%s got=%s", fs.name, offHash, h)
			}
		})
	}
}

// TestVP8AnalysisEdgeFrameSizes confirms the analyzer hook handles the
// smallest valid VP8 frame sizes and non-MB-aligned sizes without
// breaking encode and without disturbing parity. The framework must
// not crash or skew the bitstream on awkward dimensions.
func TestVP8AnalysisEdgeFrameSizes(t *testing.T) {
	sizes := []struct {
		w, h int
	}{
		{16, 16},
		{17, 17}, // non-MB-aligned both axes
		{32, 16}, // wide
		{16, 32}, // tall
		{96, 48},
	}
	observe := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	for _, sz := range sizes {
		t.Run("edge", func(t *testing.T) {
			const frames = 6
			_, hOff, _ := encodeWithAnalysis(t, sz.w, sz.h, frames, DefaultVP8AnalysisConfig())
			_, hObserve, _ := encodeWithAnalysis(t, sz.w, sz.h, frames, observe)
			if hOff != hObserve {
				t.Fatalf("edge %dx%d byte parity violation: off=%s observe=%s",
					sz.w, sz.h, hOff, hObserve)
			}
		})
	}
}

// TestVP8AnalysisStatsPopulated confirms the CPU observer actually
// records what the configuration requested. This is the "analysis data
// exists and is measurable" criterion from the patch spec.
func TestVP8AnalysisStatsPopulated(t *testing.T) {
	const (
		width  = 176
		height = 144
		frames = 4
	)
	cfg := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 600,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  30,
		Threads:           1,
		Analysis:          cfg,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	if e.AnalysisMode() != VP8AnalysisObserveCPU {
		t.Fatalf("AnalysisMode mismatch: got %v", e.AnalysisMode())
	}

	img := testImage(width, height)
	buf := make([]byte, width*height*4)
	for i := range frames {
		for j := range img.Y {
			img.Y[j] = byte((i*j + 17) & 0xFF)
		}
		if _, err := e.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	fa := e.LastFrameAnalysis()
	if fa == nil {
		t.Fatal("LastFrameAnalysis: nil while observer is configured")
	}
	if !fa.Observed {
		t.Fatal("LastFrameAnalysis.Observed: expected true after EncodeInto")
	}
	expectedCols := (width + 15) >> 4
	expectedRows := (height + 15) >> 4
	expectedMBs := expectedCols * expectedRows
	if fa.MBCols != expectedCols || fa.MBRows != expectedRows {
		t.Fatalf("MB dims = (%d,%d); want (%d,%d)", fa.MBCols, fa.MBRows, expectedCols, expectedRows)
	}
	if len(fa.MB) != expectedMBs {
		t.Fatalf("len(MB) = %d; want %d", len(fa.MB), expectedMBs)
	}
	stats := e.LastAnalysisStats()
	if stats == nil {
		t.Fatal("LastAnalysisStats: nil while observer is configured")
	}
	if stats.BlocksTotal != expectedMBs {
		t.Fatalf("stats.BlocksTotal = %d; want %d", stats.BlocksTotal, expectedMBs)
	}
	if stats.AnalysisTimeNS <= 0 {
		t.Fatalf("stats.AnalysisTimeNS = %d; want positive (complexity enabled)", stats.AnalysisTimeNS)
	}
	// Per-MB coordinates must be filled in for the raster.
	for i, mb := range fa.MB {
		col := int16(i % expectedCols)
		row := int16(i / expectedCols)
		if mb.MBX != col || mb.MBY != row {
			t.Fatalf("MB[%d] coord = (%d,%d); want (%d,%d)", i, mb.MBX, mb.MBY, col, row)
		}
	}
}

// TestVP8AnalysisOffStatsNil confirms that VP8AnalysisOff really does
// short-circuit at the encoder boundary. The accessor must return nil
// to make the "zero-cost when disabled" path observable from outside.
func TestVP8AnalysisOffStatsNil(t *testing.T) {
	const (
		width  = 96
		height = 64
		frames = 2
	)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 300,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  10,
		Threads:           1,
		// Analysis: zero value -> VP8AnalysisOff.
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()
	if e.AnalysisMode() != VP8AnalysisOff {
		t.Fatalf("AnalysisMode = %v; want VP8AnalysisOff", e.AnalysisMode())
	}
	img := testImage(width, height)
	buf := make([]byte, width*height*4)
	for i := range frames {
		if _, err := e.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	if got := e.LastFrameAnalysis(); got != nil {
		t.Fatalf("LastFrameAnalysis with VP8AnalysisOff = %+v; want nil", got)
	}
	if got := e.LastAnalysisStats(); got != nil {
		t.Fatalf("LastAnalysisStats with VP8AnalysisOff = %+v; want nil", got)
	}
}

// TestVP8AnalysisNormalizeForcesByteParity confirms that without
// UseEncodeHints, Normalize forces ByteParityRequired=true regardless
// of caller intent — observation-only analyzers must never affect the
// bitstream.
func TestVP8AnalysisNormalizeForcesByteParity(t *testing.T) {
	cfg := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		ByteParityRequired: false,
	}
	normalized := cfg.Normalize()
	if !normalized.ByteParityRequired {
		t.Fatal("Normalize did not force ByteParityRequired=true when UseEncodeHints is false")
	}
	if normalized.AffectsEncodeDecisions() {
		t.Fatal("AffectsEncodeDecisions must return false when UseEncodeHints is false")
	}
}

// TestVP8AnalysisUseEncodeHintsDropsParity confirms that opting into
// UseEncodeHints is the only way to set ByteParityRequired=false — and
// that it is forced false automatically, so the caller cannot opt into
// hint consumption while also requesting parity.
func TestVP8AnalysisUseEncodeHintsDropsParity(t *testing.T) {
	cfg := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		UseEncodeHints:     true,
		ByteParityRequired: true, // caller asked for both; Normalize must resolve
	}
	normalized := cfg.Normalize()
	if normalized.ByteParityRequired {
		t.Fatal("Normalize did not drop ByteParityRequired when UseEncodeHints is true")
	}
	if !normalized.AffectsEncodeDecisions() {
		t.Fatal("AffectsEncodeDecisions must return true when UseEncodeHints is true")
	}
}

// TestVP8AnalysisToggleByteParity confirms the encoder can be
// constructed multiple times alternating between off and observe and
// always produces the AnalysisOff hash; this maps to the spec's
// "analysis can be enabled/disabled without changing output" requirement.
func TestVP8AnalysisToggleByteParity(t *testing.T) {
	const (
		width  = 160
		height = 120
		frames = 8
	)
	off := DefaultVP8AnalysisConfig()
	observe := VP8AnalysisConfig{
		Mode:               VP8AnalysisObserveCPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}
	_, hOff1, _ := encodeWithAnalysis(t, width, height, frames, off)
	_, hObs, _ := encodeWithAnalysis(t, width, height, frames, observe)
	_, hOff2, _ := encodeWithAnalysis(t, width, height, frames, off)
	if hOff1 != hObs || hObs != hOff2 {
		t.Fatalf("toggle parity violation: off1=%s observe=%s off2=%s", hOff1, hObs, hOff2)
	}
}
