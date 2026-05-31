//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// temporalSVCLayerSummary captures the per-layer rate-control parity
// row for a single TestVP8OracleTemporalSVCParity fixture.
type temporalSVCLayerSummary struct {
	LayerID               int
	GovpxInputFrames      int
	LibvpxInputFrames     int
	GovpxEncodedFrames    int
	LibvpxEncodedFrames   int
	GovpxDroppedFrames    int
	LibvpxDroppedFrames   int
	DroppedDelta          int
	GovpxBitrateKbps      float64
	LibvpxBitrateKbps     float64
	LayerTargetKbps       int
	GovpxRateMismatchPct  float64
	LibvpxRateMismatchPct float64
	RateMismatchDeltaPct  float64
}

// temporalSVCFixtureSummary aggregates the per-fixture parity metrics
// (one TestVP8OracleTemporalSVCParity row).
type temporalSVCFixtureSummary struct {
	Name                string
	Frames              int
	Layers              int
	LayerIDMatchPct     float64
	TL0PicIdxMatchPct   float64
	SyncFlagsMatchPct   float64
	RefreshBitsMatchPct float64
	Layers_             []temporalSVCLayerSummary
}

// temporalSVCFixtureSpec, temporalSVCFrameRow and temporalSVCAccounting
// are file-scope to keep the helpers below simple.
type temporalSVCFixtureSpec = struct {
	Name             string
	Width            int
	Height           int
	FPS              int
	Frames           int
	Bitrates         [3]int
	Speed            int
	ErrorResilient   bool
	KeyFrameInterval int
	FrameDropThresh  int
}

// govpxTemporalSVCTrace holds the per-frame row recovered from a govpx
// EncodeInto sequence plus the final temporal accounting state.
type govpxTemporalSVCTrace struct {
	frames     []temporalSVCFrameRow
	accounting [MaxTemporalLayers]temporalLayerAccounting
}

type temporalSVCFrameRow struct {
	layerID       int
	tl0picidx     int
	layerSync     bool
	keyFrame      bool
	dropped       bool
	refreshLast   bool
	refreshGolden bool
	refreshAltRef bool
	sizeBytes     int
}

func captureGovpxTemporalSVCTrace(t *testing.T, fx temporalSVCFixtureSpec, sources []Image) govpxTemporalSVCTrace {
	t.Helper()
	cumulative := [MaxTemporalLayers]int{}
	for i := range 3 {
		cumulative[i] = fx.Bitrates[i]
	}
	opts := EncoderOptions{
		Width:               fx.Width,
		Height:              fx.Height,
		FPS:                 fx.FPS,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   fx.Bitrates[2],
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -fx.Speed, // libvpx example does VP8E_SET_CPUUSED(-speed)
		KeyFrameInterval:    fx.KeyFrameInterval,
		ErrorResilient:      fx.ErrorResilient,
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		DropFrameAllowed:    fx.FrameDropThresh > 0,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringThreeLayers,
			LayerTargetBitrateKbps: cumulative,
		},
	}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, fx.Width*fx.Height*3+4096)
	rows := make([]temporalSVCFrameRow, len(sources))
	pattern := enc.temporal.pattern
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		// Recover the per-frame refresh bits from the pattern's
		// EFLAG_NO_UPD_* mask: govpx's interFrameAttempt sets
		// RefreshLast/Golden/AltRef from the same flags the libvpx
		// example wires per pattern slot. Keyframes always refresh
		// all three.
		flagIdx := i % pattern.FlagPeriodicity
		patternFlags := pattern.Flags[flagIdx]
		row := temporalSVCFrameRow{
			layerID:   result.TemporalLayerID,
			tl0picidx: int(result.TL0PICIDX),
			layerSync: result.TemporalLayerSync,
			keyFrame:  result.KeyFrame,
			dropped:   result.Dropped,
			sizeBytes: result.SizeBytes,
		}
		if result.KeyFrame {
			row.refreshLast = true
			row.refreshGolden = true
			row.refreshAltRef = true
		} else {
			row.refreshLast = patternFlags&EncodeNoUpdateLast == 0
			row.refreshGolden = patternFlags&EncodeNoUpdateGolden == 0
			row.refreshAltRef = patternFlags&EncodeNoUpdateAltRef == 0
		}
		rows[i] = row
	}
	return govpxTemporalSVCTrace{
		frames:     rows,
		accounting: enc.temporal.accounting,
	}
}

// libvpxTemporalSVCStats holds the per-layer rate-control stats parsed
// from the printout_rate_control_summary stdout of
// vpx_temporal_svc_encoder.
type libvpxTemporalSVCStats struct {
	layers []libvpxTemporalLayerStat
}

type libvpxTemporalLayerStat struct {
	inputFrames   int
	encodedFrames int
	droppedFrames int
	bitrateKbps   float64
	pfb           float64 // target per-frame-bandwidth
}

func captureLibvpxTemporalSVCStats(t *testing.T, svcEncoder string, fx temporalSVCFixtureSpec, sources []Image) libvpxTemporalSVCStats {
	t.Helper()
	_, diag, err := vp8test.VpxTemporalSVCEncodeI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxTemporalSVCConfig{
			BinaryPath:         svcEncoder,
			Width:              fx.Width,
			Height:             fx.Height,
			Frames:             len(sources),
			FPS:                fx.FPS,
			Speed:              fx.Speed,
			FrameDropThreshold: fx.FrameDropThresh,
			ErrorResilient:     fx.ErrorResilient,
			Threads:            1,
			LayeringMode:       4,
			LayerBitratesKbps:  []int{fx.Bitrates[0], fx.Bitrates[1], fx.Bitrates[2]},
		},
	)
	if err != nil {
		t.Fatalf("vpx_temporal_svc_encoder failed: %v\n%s", err, diag)
	}
	return parseLibvpxTemporalSVCSummary(t, string(diag), 3)
}

// parseLibvpxTemporalSVCSummary scans the multi-block stdout produced
// by printout_rate_control_summary() and returns per-layer cumulative
// stats. The block format (one per layer) is:
//
//	For layer#: 0
//	Bitrate (target vs actual): %d %f
//	Average frame size (target vs actual): %f %f
//	Average rate_mismatch: %f
//	Number of input frames, encoded (non-key) frames, and perc dropped frames: %d %d %f
func parseLibvpxTemporalSVCSummary(t *testing.T, output string, layers int) libvpxTemporalSVCStats {
	t.Helper()
	stats := make([]libvpxTemporalLayerStat, layers)
	seen := make([]bool, layers)
	currentLayer := -1
	for raw := range strings.SplitSeq(output, "\n") {
		line := strings.TrimSpace(raw)
		var lid int
		if _, err := fmt.Sscanf(line, "For layer#: %d", &lid); err == nil {
			currentLayer = lid
			continue
		}
		if currentLayer < 0 || currentLayer >= layers {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Bitrate (target vs actual):"):
			fields := strings.Fields(line)
			if len(fields) < 2 {
				t.Fatalf("malformed temporal SVC bitrate line: %q", line)
			}
			actual, err := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err != nil {
				t.Fatalf("parse bitrate from %q: %v", line, err)
			}
			stats[currentLayer].bitrateKbps = actual
		case strings.HasPrefix(line, "Average frame size (target vs actual):"):
			fields := strings.Fields(line)
			if len(fields) < 2 {
				t.Fatalf("malformed temporal SVC frame-size line: %q", line)
			}
			pfb, err := strconv.ParseFloat(fields[len(fields)-2], 64)
			if err != nil {
				t.Fatalf("parse pfb from %q: %v", line, err)
			}
			stats[currentLayer].pfb = pfb
		case strings.HasPrefix(line, "Number of input frames, encoded"):
			fields := strings.Fields(line)
			if len(fields) < 3 {
				t.Fatalf("malformed temporal SVC stats line: %q", line)
			}
			inputFrames, err := strconv.Atoi(fields[len(fields)-3])
			if err != nil {
				t.Fatalf("parse input frames from %q: %v", line, err)
			}
			encodedFrames, err := strconv.Atoi(fields[len(fields)-2])
			if err != nil {
				t.Fatalf("parse encoded frames from %q: %v", line, err)
			}
			droppedPct, err := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err != nil {
				t.Fatalf("parse dropped pct from %q: %v", line, err)
			}
			stats[currentLayer].inputFrames = inputFrames
			stats[currentLayer].encodedFrames = encodedFrames
			// libvpx computes:
			//   num_dropped = layer_input_frames - layer_enc_frames -
			//                 (1 iff layer == 0).
			//   droppedPct = 100 * num_dropped / layer_input_frames.
			// Recover num_dropped from droppedPct so we don't have to
			// special-case the seed keyframe twice.
			stats[currentLayer].droppedFrames = int(math.Round(droppedPct * float64(inputFrames) / 100.0))
			seen[currentLayer] = true
		}
	}
	for layer := range layers {
		if !seen[layer] {
			t.Fatalf("temporal SVC output missing layer %d stats:\n%s", layer, output)
		}
	}
	return libvpxTemporalSVCStats{layers: stats}
}

func rateMismatchPct(actualKbps, targetKbps float64) float64 {
	if targetKbps <= 0 {
		return 0
	}
	return 100.0 * math.Abs(actualKbps-targetKbps) / targetKbps
}

// flattenTemporalSVCSummary expands a fixture summary plus its per-layer
// rows into a single flat key->value map. The cmd/parity-report
// tool renders one column per metric per fixture, so we want all
// per-layer fields at the top level with a "l<N>_" prefix.
//
// Acceptance-band fields (whose name ends in _match_pct or contains
// _delta_pct) are recognized by parity-report's classify() and
// rendered as "Npp" gap cells.
func flattenTemporalSVCSummary(s temporalSVCFixtureSummary) map[string]any {
	out := map[string]any{
		"name":                   s.Name,
		"frames":                 s.Frames,
		"layers":                 s.Layers,
		"layer_id_match_pct":     s.LayerIDMatchPct,
		"tl0_picidx_match_pct":   s.TL0PicIdxMatchPct,
		"sync_flags_match_pct":   s.SyncFlagsMatchPct,
		"refresh_bits_match_pct": s.RefreshBitsMatchPct,
	}
	for _, l := range s.Layers_ {
		prefix := fmt.Sprintf("l%d_", l.LayerID)
		out[prefix+"govpx_input_frames"] = l.GovpxInputFrames
		out[prefix+"libvpx_input_frames"] = l.LibvpxInputFrames
		out[prefix+"govpx_encoded_frames"] = l.GovpxEncodedFrames
		out[prefix+"libvpx_encoded_frames"] = l.LibvpxEncodedFrames
		out[prefix+"govpx_dropped_frames"] = l.GovpxDroppedFrames
		out[prefix+"libvpx_dropped_frames"] = l.LibvpxDroppedFrames
		out[prefix+"dropped_delta"] = l.DroppedDelta
		out[prefix+"govpx_bitrate_kbps"] = l.GovpxBitrateKbps
		out[prefix+"libvpx_bitrate_kbps"] = l.LibvpxBitrateKbps
		out[prefix+"layer_target_kbps"] = l.LayerTargetKbps
		out[prefix+"govpx_rate_mismatch_pct"] = l.GovpxRateMismatchPct
		out[prefix+"libvpx_rate_mismatch_pct"] = l.LibvpxRateMismatchPct
		out[prefix+"rate_mismatch_delta_pct"] = l.RateMismatchDeltaPct
	}
	return out
}

// baselineInt unwraps a baseline JSON field that should be an int value.
// JSON unmarshaling in encoding/json types numbers as float64, so we
// convert and round.
func baselineInt(v any) int {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(math.Round(x))
	}
	return 0
}

// baselineFloat unwraps a baseline JSON numeric field as a float64.
func baselineFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
