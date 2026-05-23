//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleEncoderStreamByteParityTemporalSVC exercises VP8 temporal
// scalability (multi-layer encoding for WebRTC/RTP) at the byte level.
// Both implementations are driven through the libvpx-defined pattern
// table (libvpx-v1.16.0 examples/vpx_temporal_svc_encoder.c
// set_temporal_layer_pattern()); govpx and libvpx pick the same
// per-frame layer ID, refresh flags, sync points, and TL0PICIDX, so the
// per-layer bitstream should be byte-identical when the underlying
// encoder paths are byte-identical.
//
// libvpx's vpx_temporal_svc_encoder writes one IVF per output layer
// (`<outbase>_<i>.ivf` for i in [0, ts_number_layers)). The packet from
// frame F (with temporal layer L_F) is written to every output stream
// i in [L_F, ts_number_layers). govpx emits the same per-frame packets
// in a single sequence with EncodeResult.TemporalLayerID identifying
// the source layer; we apply the same fanout rule to reconstruct the
// per-layer stream on the govpx side and then SHA-256 compare each
// layer frame-by-frame.
//
// Coverage axes (one row per combination):
//   - libvpx layering_mode in {0..11}:
//     mode 0 = 1-layer pass-through (the TS code path with a
//     single layer; pins the "TS off / pattern 0" route).
//     mode 1 = 2-layer 2-frame period.
//     mode 2 = 2-layer 3-frame period; cumulative target split
//     60% / 100% over 4 frames is 60/40 per layer.
//     mode 3 = 3-layer 6-frame period {0,2,2,1,2,2}; rate
//     decimators {6,3,1} produce roughly a 25/25/50 split.
//     mode 4 = 3-layer 4-frame period with no inter-layer prediction.
//     mode 5 = 3-layer 4-frame period {0,2,1,2}; rate decimators
//     {4,2,1} produce 25/25/50 (mode 4's intra-layer
//     disabled twin); the libvpx example calls this the
//     "intra-layer prediction enabled in layer 1, disabled
//     in layer 2" route used by typical WebRTC stacks.
//     mode 6 = default 3-layer 4-frame pattern.
//     mode 7 = 5-layer 16-frame pattern.
//     modes 8..11 cover sync-frame, alt-ref-backed sync, and
//     one-reference temporal patterns.
//   - cpu_used in {0, -3, -8}: the libvpx example takes speed=|cpu|
//     and runs VP8E_SET_CPUUSED(-speed); so we drive govpx with
//     CpuUsed=-3, -8 directly and CpuUsed=0 with speed=0.
//   - 64x64 panning fixture across 32 frames (8 full periods of the
//     4-frame patterns, ~5 periods of the 6-frame pattern).
//   - cross with ErrorResilient (the standard WebRTC config).
//   - cross with threads=2.
//
// Cases that diverge are pinned with `limit:` so the gap surfaces in
// per-frame "byte mismatch (not asserted, ...)" logs without breaking
// the strict gate. limit < 0 silences the gate entirely (used for
// known-deep gaps where even the first frame diverges).
func TestVP8OracleEncoderStreamByteParityTemporalSVC(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run temporal SVC byte-parity gate")
	}
	svcEncoder := vp8test.VpxTemporalSVCEncoder(t)

	const (
		fps    = 30
		frames = 32
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 32, source: encoderValidationPanningFrame}

	cases := []struct {
		name string
		fx   fixture
		// layeringMode is the libvpx vpx_temporal_svc_encoder argv[12]
		// (set_temporal_layer_pattern dispatch). We map it onto the
		// matching TemporalLayeringMode below.
		layeringMode int
		// numLayers must match the layering mode (it's how many
		// cumulative ts_target_bitrate values we pass).
		numLayers int
		// bitratesKbps holds the cumulative per-layer target (kbps).
		// Last value is the total stream budget.
		bitratesKbps [5]int
		// speed is libvpx's positive speed argument. Maps to
		// VP8E_SET_CPUUSED(-speed); govpx CpuUsed is set to -speed.
		speed int
		// errorResilient enables libvpx g_error_resilient = 1 and
		// govpx ErrorResilient = true.
		errorResilient bool
		// threads is libvpx g_threads (== govpx Threads).
		threads int
		// limit caps how many frames must byte-match. 0 = full
		// `frames` budget; positive caps the asserted prefix; negative
		// silences the gate entirely.
		limit int
	}{
		// ---- Mode 0: 1-layer (TS code path with single layer). ----
		{name: "mode0-1layer-cpu0", fx: panning64, layeringMode: 0, numLayers: 1, bitratesKbps: [5]int{700}, speed: 0},
		{name: "mode0-1layer-cpu-3", fx: panning64, layeringMode: 0, numLayers: 1, bitratesKbps: [5]int{700}, speed: 3},
		{name: "mode0-1layer-cpu-8", fx: panning64, layeringMode: 0, numLayers: 1, bitratesKbps: [5]int{700}, speed: 8},

		// ---- Mode 2: 2-layer 3-frame period, 60/40 split (libvpx). ----
		// All three speeds byte-match the full 32-frame clip after the
		// per-layer rate-control + filter-level seed lookup landed
		// (govpx now pulls buffer_level / total_actual_bits / Q rolling
		// averages / rate-correction factors / filter_level back from
		// LAYER_CONTEXT instead of the trailing layer's commit,
		// mirroring libvpx vp8_save_layer_context /
		// vp8_restore_layer_context).
		{name: "mode2-2layer-60-40-cpu0", fx: panning64, layeringMode: 2, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 0},
		{name: "mode2-2layer-60-40-cpu-3", fx: panning64, layeringMode: 2, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 3},
		{name: "mode2-2layer-60-40-cpu-8", fx: panning64, layeringMode: 2, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 8},

		// ---- Mode 1: 2-layer 2-frame period, 60/40 split. ----
		{name: "mode1-2layer-60-40-cpu0", fx: panning64, layeringMode: 1, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 0},
		{name: "mode1-2layer-60-40-cpu-3", fx: panning64, layeringMode: 1, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 3},
		{name: "mode1-2layer-60-40-cpu-8", fx: panning64, layeringMode: 1, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 8},

		// ---- Mode 3: 3-layer 6-frame period, 25/25/50 split. ----
		// All speeds byte-match the full clip with per-layer RC/LF restore,
		// signed gf_overspend_bits, layer-rate key-frame overspend drain,
		// and the libvpx lfc_n entropy-probability snapshot for regular
		// inter-frame RD scoring.
		{name: "mode3-3layer-25-25-50-cpu0", fx: panning64, layeringMode: 3, numLayers: 3, bitratesKbps: [5]int{175, 350, 700}, speed: 0},
		{name: "mode3-3layer-25-25-50-cpu-3", fx: panning64, layeringMode: 3, numLayers: 3, bitratesKbps: [5]int{175, 350, 700}, speed: 3},
		{name: "mode3-3layer-25-25-50-cpu-8", fx: panning64, layeringMode: 3, numLayers: 3, bitratesKbps: [5]int{175, 350, 700}, speed: 8},

		// ---- Mode 5: 3-layer 4-frame period, 20/20/60 split. ----
		// All speeds byte-match the full clip after the TS reference masks,
		// per-layer coding snapshots, signed overspend accounting, and
		// entropy-probability snapshot alignment.
		{name: "mode5-3layer-20-20-60-cpu0", fx: panning64, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 0},
		{name: "mode5-3layer-20-20-60-cpu-3", fx: panning64, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 3},
		{name: "mode5-3layer-20-20-60-cpu-3-32x32", fx: panning32, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 3},
		{name: "mode5-3layer-20-20-60-cpu-8", fx: panning64, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 8},

		// ---- Remaining built-in temporal patterns. ----
		{name: "mode4-3layer-no-inter-pred-cpu0", fx: panning64, layeringMode: 4, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 0},
		{name: "mode4-3layer-no-inter-pred-cpu-3", fx: panning64, layeringMode: 4, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 3},
		{name: "mode4-3layer-no-inter-pred-cpu-8", fx: panning64, layeringMode: 4, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 8},
		{name: "mode6-3layer-default-cpu0", fx: panning64, layeringMode: 6, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 0},
		{name: "mode6-3layer-default-cpu-3", fx: panning64, layeringMode: 6, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3},
		{name: "mode6-3layer-default-cpu-8", fx: panning64, layeringMode: 6, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 8},
		{name: "mode7-5layer-cpu0", fx: panning64, layeringMode: 7, numLayers: 5, bitratesKbps: [5]int{100, 220, 360, 520, 700}, speed: 0},
		{name: "mode7-5layer-cpu-3", fx: panning64, layeringMode: 7, numLayers: 5, bitratesKbps: [5]int{100, 220, 360, 520, 700}, speed: 3},
		{name: "mode7-5layer-cpu-8", fx: panning64, layeringMode: 7, numLayers: 5, bitratesKbps: [5]int{100, 220, 360, 520, 700}, speed: 8},
		{name: "mode8-2layer-sync-cpu0", fx: panning64, layeringMode: 8, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 0},
		{name: "mode8-2layer-sync-cpu-3", fx: panning64, layeringMode: 8, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 3},
		{name: "mode8-2layer-sync-cpu-8", fx: panning64, layeringMode: 8, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 8},
		{name: "mode9-3layer-sync-cpu0", fx: panning64, layeringMode: 9, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 0},
		{name: "mode12-3layer-no-sync-cpu0", fx: panning64, layeringMode: 12, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 0},
		{name: "mode9-3layer-sync-cpu-3", fx: panning64, layeringMode: 9, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3},
		{name: "mode9-3layer-sync-cpu-8", fx: panning64, layeringMode: 9, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 8},
		{name: "mode10-3layer-altref-sync-cpu0", fx: panning64, layeringMode: 10, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 0},
		{name: "mode10-3layer-altref-sync-cpu-3", fx: panning64, layeringMode: 10, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3},
		{name: "mode10-3layer-altref-sync-cpu-8", fx: panning64, layeringMode: 10, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 8},
		{name: "mode11-3layer-one-reference-cpu0", fx: panning64, layeringMode: 11, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 0},
		{name: "mode11-3layer-one-reference-cpu-3", fx: panning64, layeringMode: 11, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3},
		{name: "mode11-3layer-one-reference-cpu-8", fx: panning64, layeringMode: 11, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 8},
		{name: "mode12-3layer-no-sync-cpu-3", fx: panning64, layeringMode: 12, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3},
		{name: "mode12-3layer-no-sync-cpu-8", fx: panning64, layeringMode: 12, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 8},

		// ---- ErrorResilient cross (mode 5 = the standard WebRTC SVC pattern). ----
		// mode 5 + ER and mode 2 + ER are fully byte-identical.
		{name: "mode5-3layer-cpu-3-error-resilient", fx: panning64, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 3, errorResilient: true},
		{name: "mode2-2layer-cpu-3-error-resilient", fx: panning64, layeringMode: 2, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 3, errorResilient: true},
		{name: "mode10-3layer-altref-sync-cpu-3-error-resilient", fx: panning64, layeringMode: 10, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3, errorResilient: true},
		{name: "mode12-3layer-no-sync-cpu-3-error-resilient", fx: panning64, layeringMode: 12, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3, errorResilient: true},

		// ---- Threads=2 cross. ----
		// Both threaded crosses byte-match the full clip.
		{name: "mode5-3layer-cpu-3-threads2", fx: panning64, layeringMode: 5, numLayers: 3, bitratesKbps: [5]int{140, 280, 700}, speed: 3, threads: 2},
		{name: "mode2-2layer-cpu-3-threads2", fx: panning64, layeringMode: 2, numLayers: 2, bitratesKbps: [5]int{420, 700}, speed: 3, threads: 2},
		{name: "mode7-5layer-cpu-3-threads2", fx: panning64, layeringMode: 7, numLayers: 5, bitratesKbps: [5]int{100, 220, 360, 520, 700}, speed: 3, threads: 2},
		{name: "mode12-3layer-no-sync-cpu-3-threads2", fx: panning64, layeringMode: 12, numLayers: 3, bitratesKbps: [5]int{280, 420, 700}, speed: 3, threads: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}

			govpxMode, ok := govpxModeForLibvpxLayeringMode(tc.layeringMode)
			if !ok {
				t.Fatalf("no govpx TemporalLayeringMode mapping for libvpx layering_mode=%d", tc.layeringMode)
			}

			govpxLayerStreams := encodeFramesWithGovpxTemporalSVC(t, tc.fx, govpxMode, tc.numLayers, tc.bitratesKbps, tc.speed, tc.errorResilient, tc.threads, sources)
			libvpxLayerStreams := encodeFramesWithLibvpxTemporalSVC(t, svcEncoder, tc.fx, tc.layeringMode, tc.numLayers, tc.bitratesKbps, tc.speed, tc.errorResilient, tc.threads, sources)

			if len(govpxLayerStreams) != tc.numLayers || len(libvpxLayerStreams) != tc.numLayers {
				t.Fatalf("layer count mismatch: govpx=%d libvpx=%d want=%d", len(govpxLayerStreams), len(libvpxLayerStreams), tc.numLayers)
			}

			limit := frames
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}

			for layer := 0; layer < tc.numLayers; layer++ {
				govpxFrames := govpxLayerStreams[layer]
				libvpxFrames := libvpxLayerStreams[layer]
				if len(govpxFrames) != len(libvpxFrames) {
					if tc.limit < 0 {
						t.Logf("layer %d frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", layer, len(govpxFrames), len(libvpxFrames))
						assertStrictGateKnownGapMatchedPrefix(t, fmt.Sprintf("%s/layer-%d", tc.name, layer), govpxFrames, libvpxFrames, 1)
						continue
					}
					t.Errorf("layer %d frame count mismatch: govpx=%d libvpx=%d", layer, len(govpxFrames), len(libvpxFrames))
					continue
				}
				for i := 0; i < len(govpxFrames); i++ {
					gHash := sha256.Sum256(govpxFrames[i])
					lHash := sha256.Sum256(libvpxFrames[i])
					gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
					lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
					if gHash == lHash {
						t.Logf("layer %d frame %d byte MATCH: len=%d first_part=%d keyframe=%t", layer, i, len(govpxFrames[i]), gFP, gIsKey)
						continue
					}
					firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
					firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
					if firstNonTagDiff >= 0 {
						firstNonTagDiff += 3
					}
					if i >= limit {
						t.Logf("layer %d frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
							layer, i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
						continue
					}
					t.Errorf("layer %d frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
						layer, i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
						gFP, lFP, gIsKey, lIsKey,
						hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
				}
			}
		})
	}
}

// govpxModeForLibvpxLayeringMode maps the libvpx vpx_temporal_svc_encoder
// command-line layering_mode (set_temporal_layer_pattern() switch case)
// to the equivalent govpx TemporalLayeringMode. The mappings are
// validated structurally by TestTemporalLayeringPatternsMatchLibvpxExample
// in temporal_test.go.
func govpxModeForLibvpxLayeringMode(libvpxMode int) (TemporalLayeringMode, bool) {
	switch libvpxMode {
	case 0:
		return TemporalLayeringOneLayer, true
	case 1:
		return TemporalLayeringTwoLayers, true
	case 2:
		return TemporalLayeringTwoLayersThreeFrame, true
	case 3:
		return TemporalLayeringThreeLayersSixFrame, true
	case 4:
		return TemporalLayeringThreeLayersNoInterLayerPrediction, true
	case 5:
		return TemporalLayeringThreeLayersLayerOnePrediction, true
	case 6:
		return TemporalLayeringThreeLayers, true
	case 7:
		return TemporalLayeringFiveLayers, true
	case 8:
		return TemporalLayeringTwoLayersWithSync, true
	case 9:
		return TemporalLayeringThreeLayersWithSync, true
	case 10:
		return TemporalLayeringThreeLayersAltRefWithSync, true
	case 11:
		return TemporalLayeringThreeLayersOneReference, true
	case 12:
		return TemporalLayeringThreeLayersNoSync, true
	}
	return 0, false
}

// encodeFramesWithGovpxTemporalSVC runs govpx with the given
// TemporalScalabilityConfig and returns the per-layer reconstructed
// IVF payload stream — i.e. for output layer N, the slice of all
// emitted frame payloads where TemporalLayerID <= N. This mirrors
// libvpx's vpx_temporal_svc_encoder fanout rule
// (`for i = ts_layer_id; i < ts_number_layers; i++ write to
// outfile[i]`).
func encodeFramesWithGovpxTemporalSVC(
	t *testing.T,
	fx struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	},
	mode TemporalLayeringMode,
	numLayers int,
	bitratesKbps [5]int,
	speed int,
	errorResilient bool,
	threads int,
	sources []Image,
) [][][]byte {
	t.Helper()

	opts := EncoderOptions{
		Width:               fx.w,
		Height:              fx.h,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   bitratesKbps[numLayers-1],
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -speed,
		KeyFrameInterval:    3000,
		ErrorResilient:      errorResilient,
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		UndershootPct:       50,
		OvershootPct:        50,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TokenPartitions:     1, // libvpx example sets VP8E_SET_TOKEN_PARTITIONS = 1 (=> 2 partitions).
		Threads:             threads,
		Tuning:              TunePSNR,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                mode != TemporalLayeringOneLayer,
			Mode:                   mode,
			LayerTargetBitrateKbps: bitratesKbps,
		},
	}
	// libvpx vpx_temporal_svc_encoder sets layering_mode=0 with
	// ts_number_layers=1 and ts_periodicity=1, then forces flags=0
	// for every frame. govpx's TemporalLayeringOneLayer pattern
	// sets EncodeForceKeyFrame on slot 0, which the encoder masks
	// after frame 0; we additionally disable the TS state to mirror
	// libvpx's "layering_mode == 0 => flags = 0" branch exactly.
	if mode == TemporalLayeringOneLayer {
		opts.TemporalScalability = TemporalScalabilityConfig{Enabled: false}
	}

	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, fx.w*fx.h*4+8192)
	layerStreams := make([][][]byte, numLayers)
	for i := range layerStreams {
		layerStreams[i] = make([][]byte, 0, len(sources))
	}
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			// Frame drops produce no payload in libvpx either
			// (vpx_codec_get_cx_data returns no FRAME_PKT). Skip.
			continue
		}
		layerID := result.TemporalLayerID
		if uint(layerID) >= uint(numLayers) {
			t.Fatalf("frame %d: govpx emitted unexpected TemporalLayerID=%d (numLayers=%d)", i, layerID, numLayers)
		}
		payload := append([]byte(nil), result.Data...)
		for lo := layerID; lo < numLayers; lo++ {
			layerStreams[lo] = append(layerStreams[lo], payload)
		}
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped {
			continue
		}
		layerID := result.TemporalLayerID
		if uint(layerID) >= uint(numLayers) {
			t.Fatalf("flush: govpx emitted unexpected TemporalLayerID=%d (numLayers=%d)", layerID, numLayers)
		}
		payload := append([]byte(nil), result.Data...)
		for lo := layerID; lo < numLayers; lo++ {
			layerStreams[lo] = append(layerStreams[lo], payload)
		}
	}
	return layerStreams
}

// encodeFramesWithLibvpxTemporalSVC runs the upstream
// vpx_temporal_svc_encoder example and parses the per-layer IVFs it
// emits. Returns the per-layer slice of raw VP8 frame payloads,
// indexed by output layer.
func encodeFramesWithLibvpxTemporalSVC(
	t *testing.T,
	svcEncoder string,
	fx struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	},
	layeringMode int,
	numLayers int,
	bitratesKbps [5]int,
	speed int,
	errorResilient bool,
	threads int,
	sources []Image,
) [][][]byte {
	t.Helper()

	if threads == 0 {
		threads = 1
	}
	bitrates := make([]int, numLayers)
	for i := range bitrates {
		bitrates[i] = bitratesKbps[i]
	}
	layers, diag, err := vp8test.VpxTemporalSVCPayloadsI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxTemporalSVCConfig{
			BinaryPath:         svcEncoder,
			Width:              fx.w,
			Height:             fx.h,
			Frames:             len(sources),
			FPS:                30,
			Speed:              speed,
			FrameDropThreshold: 0,
			ErrorResilient:     errorResilient,
			Threads:            threads,
			LayeringMode:       layeringMode,
			LayerBitratesKbps:  bitrates,
		},
	)
	if err != nil {
		t.Fatalf("vpx_temporal_svc_encoder failed: %v\n%s", err, diag)
	}
	return layers
}
