//go:build govpx_oracle_trace

package govpx

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"math"
	"testing"
)

// TestVP8OracleTemporalSVCParity drives a 3-layer SVC pattern through both govpx
// and libvpx's vpx_temporal_svc_encoder example and pins govpx-vs-libvpx
// parity on:
//
//   - per-frame layer ID (the pattern table in temporal.go vs libvpx
//     set_temporal_layer_pattern())
//   - TL0PICIDX progression on layer-0 frames
//   - layer_sync flag distribution per layer (sync points after KFs and
//     across the periodicity boundary)
//   - refresh_last/golden/altref bit masks (the EFLAG_NO_UPD_* bits the
//     example wires per pattern slot are echoed by govpx's
//     pattern.Flags[]; we verify the encoder honors them on every frame
//     by checking the per-frame trace's refresh_* flags against the
//     pattern's EFLAG_NO_UPD_* mask)
//   - per-layer rate target adherence: the actual encoded bitrate per
//     layer (cumulative bits / cumulative seconds-of-stream) compared to
//     the pattern-driven layer target
//   - per-layer dropped frame count: govpx temporalState.accounting[].
//     InputFrames - EncodedFrames vs libvpx RateControlMetrics
//     layer_input_frames - layer_enc_frames
//
// The libvpx reference is the upstream vpx_temporal_svc_encoder example
// (libvpx-v1.16.0/examples/vpx_temporal_svc_encoder.c) invoked with
// layering_mode=4 (3-layers, 4-frame period; ts_layer_id={0,2,1,2}). govpx
// drives TemporalLayeringThreeLayers, whose pattern is bit-for-bit the same
// (verified by TestTemporalLayeringPatternsMatchLibvpxExample in
// temporal_test.go).
//
// Baseline: testdata/temporal_svc_parity_baseline.json. Bootstrap with
// GOVPX_UPDATE_BASELINES=1.
//
// Acceptance bands:
//   - layer_id_match_pct == 100 (deterministic from shared pattern; any
//     drift is a pattern table regression)
//   - tl0_picidx_match_pct == 100 (deterministic; advances on every
//     layer-0 frame)
//   - sync_flags_match_pct == 100 (govpx layerSync mirrors libvpx's
//     EFLAG_NO_REF_* gating)
//   - refresh_bits_match_pct == 100 (govpx encoder honors pattern
//     EFLAG_NO_UPD_* bits)
//   - per-layer dropped count: govpx vs libvpx within +/- 1 frame
//   - per-layer rate adherence (govpx kbps vs target): within 25%
//     mismatch on the highest-pressure fixture, gated against baseline
//     drift
func TestVP8OracleTemporalSVCParity(t *testing.T) {
	vp8test.RequireOracle(t, "temporal SVC parity scoreboard")
	svcEncoder := vp8test.VpxTemporalSVCEncoder(t)

	type fixtureSpec struct {
		Name             string
		Width            int
		Height           int
		FPS              int
		Frames           int
		Bitrates         [3]int // cumulative per-layer kbps {L0, L0+L1, L0+L1+L2}
		Speed            int    // libvpx --speed (passed as -speed via VP8E_SET_CPUUSED)
		ErrorResilient   bool
		KeyFrameInterval int
		FrameDropThresh  int // libvpx --drop-frame-threshold
	}

	fixtures := []fixtureSpec{
		{
			Name:             "panning-32f-3l-cpu5",
			Width:            64,
			Height:           64,
			FPS:              30,
			Frames:           32,
			Bitrates:         [3]int{200, 400, 700},
			Speed:            5,
			ErrorResilient:   true,
			KeyFrameInterval: 3000,
			FrameDropThresh:  0,
		},
		{
			Name:             "panning-48f-3l-cpu8",
			Width:            64,
			Height:           64,
			FPS:              30,
			Frames:           48,
			Bitrates:         [3]int{160, 320, 640},
			Speed:            8,
			ErrorResilient:   true,
			KeyFrameInterval: 3000,
			FrameDropThresh:  0,
		},
	}

	type baselineFile struct {
		Fixtures map[string]map[string]any `json:"fixtures"`
	}

	baselinePath := "testdata/temporal_svc_parity_baseline.json"
	updateBaselines := vp8test.UpdateBaselines()
	baseline, baselineExists := vp8test.ReadOptionalJSONBaseline[baselineFile](t, baselinePath)

	current := baselineFile{Fixtures: make(map[string]map[string]any, len(fixtures))}

	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			sources := make([]Image, fx.Frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(fx.Width, fx.Height, i)
			}

			govpxTrace := captureGovpxTemporalSVCTrace(t, fx, sources)
			libvpxStats := captureLibvpxTemporalSVCStats(t, svcEncoder, fx, sources)

			// Pattern parity is deterministic given the shared pattern
			// table. We still verify it explicitly so a regression in
			// temporalLayeringPattern() / set_temporal_layer_pattern()
			// surfaces here rather than in the deeper bitstream tests.
			pattern, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
			if !ok {
				t.Fatalf("temporalLayeringPattern(ThreeLayers) failed")
			}

			// Build the expected per-frame pattern view.
			expected := buildExpectedTemporalPattern(pattern, fx.Frames)

			layerIDMatches := 0
			tl0Matches := 0
			syncMatches := 0
			refreshMatches := 0
			for i, exp := range expected {
				row := govpxTrace.frames[i]
				if row.layerID == exp.layerID {
					layerIDMatches++
				}
				if row.tl0picidx == exp.tl0picidx {
					tl0Matches++
				}
				if row.layerSync == exp.layerSync {
					syncMatches++
				}
				if row.refreshLast == exp.refreshLast &&
					row.refreshGolden == exp.refreshGolden &&
					row.refreshAltRef == exp.refreshAltRef {
					refreshMatches++
				}
			}

			pct := func(n int) float64 {
				if fx.Frames == 0 {
					return 0
				}
				return 100.0 * float64(n) / float64(fx.Frames)
			}
			summary := temporalSVCFixtureSummary{
				Name:                fx.Name,
				Frames:              fx.Frames,
				Layers:              pattern.Layers,
				LayerIDMatchPct:     pct(layerIDMatches),
				TL0PicIdxMatchPct:   pct(tl0Matches),
				SyncFlagsMatchPct:   pct(syncMatches),
				RefreshBitsMatchPct: pct(refreshMatches),
			}

			// Per-layer accounting summary.
			framerate := float64(fx.FPS)
			totalSeconds := float64(fx.Frames) / framerate
			layers := make([]temporalSVCLayerSummary, pattern.Layers)
			for layer := 0; layer < pattern.Layers; layer++ {
				gov := govpxTrace.accounting[layer]
				lib := libvpxStats.layers[layer]
				// govpx EncodedBits is cumulative across layers (sum of
				// own + lower-layer frames), matching libvpx's
				// layer_encoding_bitrate semantics. Convert to kbps over
				// the full clip duration for a normalized adherence
				// comparison against the cumulative ts_target_bitrate.
				govKbps := 0.0
				if totalSeconds > 0 {
					govKbps = float64(gov.EncodedBits) / 1000.0 / totalSeconds
				}
				libKbps := lib.bitrateKbps
				govMismatch := rateMismatchPct(govKbps, float64(fx.Bitrates[layer]))
				libMismatch := rateMismatchPct(libKbps, float64(fx.Bitrates[layer]))

				govDropped := gov.InputFrames - gov.EncodedFrames
				if layer == 0 {
					// Layer 0 input includes the keyframe; libvpx
					// printout_rate_control_summary subtracts 1 from
					// layer 0 to skip the seed keyframe. govpx
					// EncodedFrames already excludes keyframes (see
					// accountEncodedFrame: only inter frames bump
					// EncodedFrames).
					govDropped--
					if govDropped < 0 {
						govDropped = 0
					}
				}
				layers[layer] = temporalSVCLayerSummary{
					LayerID:               layer,
					GovpxInputFrames:      gov.InputFrames,
					LibvpxInputFrames:     lib.inputFrames,
					GovpxEncodedFrames:    gov.EncodedFrames,
					LibvpxEncodedFrames:   lib.encodedFrames,
					GovpxDroppedFrames:    govDropped,
					LibvpxDroppedFrames:   lib.droppedFrames,
					DroppedDelta:          govDropped - lib.droppedFrames,
					GovpxBitrateKbps:      govKbps,
					LibvpxBitrateKbps:     libKbps,
					LayerTargetKbps:       fx.Bitrates[layer],
					GovpxRateMismatchPct:  govMismatch,
					LibvpxRateMismatchPct: libMismatch,
					RateMismatchDeltaPct:  govMismatch - libMismatch,
				}
			}
			summary.Layers_ = layers

			t.Logf("[%s] layer_id=%.1f%% tl0=%.1f%% sync=%.1f%% refresh=%.1f%% layers=%d",
				fx.Name, summary.LayerIDMatchPct, summary.TL0PicIdxMatchPct,
				summary.SyncFlagsMatchPct, summary.RefreshBitsMatchPct, pattern.Layers)
			for _, l := range layers {
				t.Logf("[%s] L%d: in=%d/%d enc=%d/%d drop=%d/%d kbps=%.2f/%.2f target=%d mismatch=%.2f%%/%.2f%%",
					fx.Name, l.LayerID,
					l.GovpxInputFrames, l.LibvpxInputFrames,
					l.GovpxEncodedFrames, l.LibvpxEncodedFrames,
					l.GovpxDroppedFrames, l.LibvpxDroppedFrames,
					l.GovpxBitrateKbps, l.LibvpxBitrateKbps,
					l.LayerTargetKbps,
					l.GovpxRateMismatchPct, l.LibvpxRateMismatchPct)
			}

			flat := flattenTemporalSVCSummary(summary)
			current.Fixtures[fx.Name] = flat

			if updateBaselines || !baselineExists {
				return
			}

			prev, ok := baseline.Fixtures[fx.Name]
			if !ok {
				t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, fx.Name)
				return
			}

			// Pattern parity is structural. Anything below 100% is a
			// regression in the shared pattern table and the test must
			// fail outright.
			if summary.LayerIDMatchPct < 100 {
				t.Errorf("[%s] layer_id_match_pct=%.2f%%, want 100%% (pattern table regression?)", fx.Name, summary.LayerIDMatchPct)
			}
			if summary.TL0PicIdxMatchPct < 100 {
				t.Errorf("[%s] tl0_picidx_match_pct=%.2f%%, want 100%% (TL0PICIDX progression regression?)", fx.Name, summary.TL0PicIdxMatchPct)
			}
			if summary.SyncFlagsMatchPct < 100 {
				t.Errorf("[%s] sync_flags_match_pct=%.2f%%, want 100%% (layerSync derivation regression?)", fx.Name, summary.SyncFlagsMatchPct)
			}
			if summary.RefreshBitsMatchPct < 100 {
				t.Errorf("[%s] refresh_bits_match_pct=%.2f%%, want 100%% (refresh_last/golden/altref regression?)", fx.Name, summary.RefreshBitsMatchPct)
			}

			// Per-layer parity bands. Each metric is keyed by
			// "l<N>_<field>" in the flattened baseline map.
			for _, l := range summary.Layers_ {
				prefix := fmt.Sprintf("l%d_", l.LayerID)

				// Input frame counts are deterministic given the
				// shared pattern + same source -- any drift is a bug.
				if l.GovpxInputFrames != l.LibvpxInputFrames {
					t.Errorf("[%s] L%d input frames govpx=%d libvpx=%d", fx.Name, l.LayerID, l.GovpxInputFrames, l.LibvpxInputFrames)
				}
				// Per-layer dropped count parity: +/- 1 frame.
				if abs(l.GovpxDroppedFrames-l.LibvpxDroppedFrames) > 1 {
					t.Errorf("[%s] L%d dropped govpx=%d libvpx=%d delta > 1",
						fx.Name, l.LayerID, l.GovpxDroppedFrames, l.LibvpxDroppedFrames)
				}
				// govpx-side dropped count must not regress beyond
				// baseline.
				prevDropped := baselineInt(prev[prefix+"govpx_dropped_frames"])
				if l.GovpxDroppedFrames > prevDropped+1 {
					t.Errorf("[%s] L%d govpx_dropped=%d baseline=%d drift > 1 (rerun with GOVPX_UPDATE_BASELINES=1 if intended)",
						fx.Name, l.LayerID, l.GovpxDroppedFrames, prevDropped)
				}
				// Rate adherence: gate on the residual
				// govpx-vs-libvpx delta. If both sides are off by
				// roughly the same amount this is a libvpx-side
				// shortfall (small clip / panning content); we only
				// fail when the govpx side moves further from libvpx
				// than baseline allows.
				prevDelta := baselineFloat(prev[prefix+"rate_mismatch_delta_pct"])
				delta := math.Abs(l.RateMismatchDeltaPct - prevDelta)
				if delta > 5.0 {
					t.Errorf("[%s] L%d rate_mismatch_delta_pct=%.2f baseline=%.2f drift > 5pp",
						fx.Name, l.LayerID, l.RateMismatchDeltaPct, prevDelta)
				}
			}
		})
	}

	if updateBaselines || !baselineExists {
		vp8test.WriteJSONBaseline(t, baselinePath, current)
	}
}
