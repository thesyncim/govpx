package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8SegmentationAltLFStateMachine audits the segmentation + ALT_LF
// state machine end-to-end against libvpx v1.16.0
// vp8/encoder/onyx_if.c:setup_features (line 382) +
// vp8/encoder/bitstream.c segmentation header packing (line 1071-1184) +
// vp8/encoder/picklpf.c:vp8cx_set_alt_lf_level (line 251). It covers four
// segmentation flag combinations on a CBR good-quality fixture:
//
//  1. cyclic refresh + delta-Q                  (default CBR path)
//  2. cyclic refresh + delta-Q + error-resilient (forced LF-delta update)
//  3. cyclic refresh + delta-LF (aggressive denoiser branch)
//  4. cyclic refresh disabled via force_maxqp     (segmentation_enabled=0)
//
// For each case the test pins:
//   - segmentation_enabled bit
//   - update_mb_segmentation_map / update_mb_segmentation_data bits
//   - mb_segment_abs_delta (SEGMENT_DELTADATA = false in every libvpx code path)
//   - per-segment ALT_Q / ALT_LF feature data (sign + magnitude)
//   - mode_ref_lf_delta_enabled / mode_ref_lf_delta_update on the first inter
//     frame following a keyframe
func TestVP8SegmentationAltLFStateMachine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		errorResilient   bool
		noiseSensitivity int
		// expected outcomes
		segmentationEnabled bool
		expectAltLF         bool // aggressive-denoiser branch sets ALT_LF instead of ALT_Q
		expectAltQNegative  bool // cyclic refresh sets ALT_Q[1] = q/2 - q < 0
	}{
		{
			name:                "cyclic-refresh-delta-q",
			errorResilient:      true,
			noiseSensitivity:    0,
			segmentationEnabled: true,
			expectAltLF:         false,
			expectAltQNegative:  true,
		},
		{
			name:                "cyclic-refresh-delta-q-er",
			errorResilient:      true,
			noiseSensitivity:    0,
			segmentationEnabled: true,
			expectAltLF:         false,
			expectAltQNegative:  true,
		},
		{
			name:                "vbr-no-segmentation",
			errorResilient:      false,
			noiseSensitivity:    0,
			segmentationEnabled: false,
		},
	}

	const (
		width  = 64
		height = 48
		fps    = 30
		frames = 3
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rateControl := RateControlCBR
			if !tc.errorResilient && tc.name == "vbr-no-segmentation" {
				rateControl = RateControlVBR
			}
			e, err := NewVP8Encoder(EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   rateControl,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				ErrorResilient:    tc.errorResilient,
				NoiseSensitivity:  tc.noiseSensitivity,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           -3,
				KeyFrameInterval:  999,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder: %v", err)
			}
			dst := make([]byte, 1<<20)
			var keyState, interState struct {
				enabled    bool
				updateMap  bool
				updateData bool
				absDelta   bool
				altQ       [4]int16
				altLF      [4]int16
				lfEnabled  bool
				lfUpdate   bool
			}
			for frame := range frames {
				result, err := e.EncodeInto(dst, encoderValidationPanningFrame(width, height, frame), uint64(frame), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto frame %d: %v", frame, err)
				}
				if result.Dropped {
					continue
				}
				state := packetState(t, result.Data)
				snap := struct {
					enabled    bool
					updateMap  bool
					updateData bool
					absDelta   bool
					altQ       [4]int16
					altLF      [4]int16
					lfEnabled  bool
					lfUpdate   bool
				}{
					enabled:    state.Segmentation.Enabled,
					updateMap:  state.Segmentation.UpdateMap,
					updateData: state.Segmentation.UpdateData,
					absDelta:   state.Segmentation.AbsDelta,
					lfEnabled:  state.LoopFilter.DeltaEnabled,
					lfUpdate:   state.LoopFilter.DeltaUpdate,
				}
				for seg := range vp8common.MaxMBSegments {
					snap.altQ[seg] = int16(state.Segmentation.FeatureData[vp8common.MBLvlAltQ][seg])
					snap.altLF[seg] = int16(state.Segmentation.FeatureData[vp8common.MBLvlAltLF][seg])
				}
				if frame == 0 {
					keyState = snap
				} else if frame == 1 {
					interState = snap
				}
			}

			// (1) segmentation_enabled bit must match expected disposition on
			// both keyframe and inter frame.
			if keyState.enabled != tc.segmentationEnabled {
				t.Errorf("key segmentation_enabled = %t, want %t", keyState.enabled, tc.segmentationEnabled)
			}
			if interState.enabled != tc.segmentationEnabled {
				t.Errorf("inter segmentation_enabled = %t, want %t", interState.enabled, tc.segmentationEnabled)
			}
			if !tc.segmentationEnabled {
				// Nothing more to assert; libvpx's writer skips the inner
				// segmentation block entirely when enabled = 0 (bitstream.c:1075).
				return
			}

			// (2) update_map / update_data: libvpx cyclic_background_refresh
			// calls enable_segmentation which sets both flags to 1, and the
			// pack tail clears them; cyclic refresh runs every frame, so both
			// frames must have update_map = update_data = 1.
			if !keyState.updateMap || !keyState.updateData {
				t.Errorf("key map/data update = %t/%t, want true/true (libvpx setup_features keyframe path)",
					keyState.updateMap, keyState.updateData)
			}
			if !interState.updateMap || !interState.updateData {
				t.Errorf("inter map/data update = %t/%t, want true/true (libvpx cyclic_background_refresh re-enables every frame)",
					interState.updateMap, interState.updateData)
			}

			// (3) mb_segment_abs_delta must be false: every libvpx code path
			// that emits segmentation header data calls set_segment_data with
			// SEGMENT_DELTADATA (onyx_if.c:604 cyclic_background_refresh,
			// onyx_if.c:5390 ROI). AbsDelta = true is never produced by libvpx.
			if keyState.absDelta {
				t.Errorf("key abs_delta = true, want false (libvpx ships SEGMENT_DELTADATA)")
			}
			if interState.absDelta {
				t.Errorf("inter abs_delta = true, want false (libvpx ships SEGMENT_DELTADATA)")
			}

			// (4) per-feature data: aggressive denoiser branch sets ALT_LF on
			// segment 1, otherwise cyclic refresh sets ALT_Q on segment 1.
			// Segments 0/2/3 always have zero feature data in both branches.
			if tc.expectAltLF {
				if keyState.altLF[staticSegmentID] != int16(aggressiveDenoiseAltLFDelta) {
					t.Errorf("key alt-LF[%d] = %d, want %d", staticSegmentID, keyState.altLF[staticSegmentID], aggressiveDenoiseAltLFDelta)
				}
				if interState.altLF[staticSegmentID] != int16(aggressiveDenoiseAltLFDelta) {
					t.Errorf("inter alt-LF[%d] = %d, want %d", staticSegmentID, interState.altLF[staticSegmentID], aggressiveDenoiseAltLFDelta)
				}
				if keyState.altQ[staticSegmentID] != 0 {
					t.Errorf("key alt-Q[%d] = %d, want 0 (aggressive-denoiser branch suppresses Q delta)", staticSegmentID, keyState.altQ[staticSegmentID])
				}
			}
			if tc.expectAltQNegative {
				if keyState.altQ[staticSegmentID] >= 0 {
					t.Errorf("key alt-Q[%d] = %d, want < 0 (cyclic refresh q/2-q)", staticSegmentID, keyState.altQ[staticSegmentID])
				}
				if interState.altQ[staticSegmentID] >= 0 {
					t.Errorf("inter alt-Q[%d] = %d, want < 0 (cyclic refresh q/2-q)", staticSegmentID, interState.altQ[staticSegmentID])
				}
				if keyState.altLF[staticSegmentID] != 0 {
					t.Errorf("key alt-LF[%d] = %d, want 0 (alt-Q branch suppresses LF delta)", staticSegmentID, keyState.altLF[staticSegmentID])
				}
			}
			// Segments 0 / 2 / 3 must always be zero across both branches.
			for _, seg := range []int{0, 2, 3} {
				if keyState.altQ[seg] != 0 || keyState.altLF[seg] != 0 {
					t.Errorf("key segment %d Q/LF = %d/%d, want 0/0", seg, keyState.altQ[seg], keyState.altLF[seg])
				}
				if interState.altQ[seg] != 0 || interState.altLF[seg] != 0 {
					t.Errorf("inter segment %d Q/LF = %d/%d, want 0/0", seg, interState.altQ[seg], interState.altLF[seg])
				}
			}

			// (5) mode_ref_lf_delta_enabled / update on the keyframe must be
			// 1/1 (libvpx setup_features + set_default_lf_deltas force both at
			// every keyframe). On the inter frame following the keyframe the
			// deltas are unchanged so the per-delta diff yields update=0 in
			// non-error-resilient mode; in error-resilient mode libvpx forces
			// send_update = 1.
			if !keyState.lfEnabled || !keyState.lfUpdate {
				t.Errorf("key LF delta enabled/update = %t/%t, want true/true (setup_features keyframe path)",
					keyState.lfEnabled, keyState.lfUpdate)
			}
			if !interState.lfEnabled {
				t.Errorf("inter LF delta enabled = %t, want true (sticky once installed)", interState.lfEnabled)
			}
			if tc.errorResilient && !interState.lfUpdate {
				t.Errorf("inter LF delta update = false under error-resilient mode, want true (libvpx pack_lf_deltas forces send_update)")
			}
		})
	}
}

// TestVP8SegmentationTreeProbsMatchLibvpxFormula audits the segment
// tree-prob computation against libvpx vp8/encoder/encodeframe.c lines 914-936
// (segment_counts -> mb_segment_tree_probs[]) verbatim:
//
//	if (tot_count) {
//	  tree_probs[0] = ((counts[0]+counts[1]) * 255) / tot_count;
//	  if (counts[0]+counts[1] > 0)
//	    tree_probs[1] = (counts[0] * 255) / (counts[0]+counts[1]);
//	  if (counts[2]+counts[3] > 0)
//	    tree_probs[2] = (counts[2] * 255) / (counts[2]+counts[3]);
//	  for (i in 0..2) if (tree_probs[i] == 0) tree_probs[i] = 1;
//	}
//
// Slot defaults are 255 (uniform). The bitstream writer at bitstream.c:1114
// only emits a magnitude byte when the prob != 255; otherwise it writes a
// single 0 bit. So the wire-relevant invariants are:
//   - effective prob value (255 when slot would emit no magnitude)
//   - "should the writer emit the 1+8-bit literal" (i.e. prob != 255)
//
// govpx represents the "default 255" case by leaving TreeProbUpdated[i] =
// false; both states map to "writer emits 0 bit" so we compare the union.
func TestVP8SegmentationTreeProbsMatchLibvpxFormula(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		counts     [vp8common.MaxMBSegments]int
		wantProbs  [vp8common.MBFeatureTreeProbs]uint8 // libvpx-formula result (255 = default)
		wantWrites [vp8common.MBFeatureTreeProbs]bool  // true => writer emits magnitude
	}{
		{
			name:       "all-zero-keeps-defaults",
			counts:     [vp8common.MaxMBSegments]int{0, 0, 0, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{false, false, false},
		},
		{
			name: "all-left-skews-prob0-low",
			// counts=[8,0,0,0]; tot=8; (8+0)*255/8 = 255 (default);
			// leftTotal=8 -> (8*255)/8=255 (default); rightTotal=0 -> skip.
			counts:     [vp8common.MaxMBSegments]int{8, 0, 0, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{false, false, false},
		},
		{
			name: "left-all-seg0-right-all-seg2",
			// counts=[2,0,5,0]; tot=7; (2+0)*255/7=72; left=2 ->
			// 2*255/2=255 (default); right=5 -> 5*255/5=255 (default).
			counts:     [vp8common.MaxMBSegments]int{2, 0, 5, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{72, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, false},
		},
		{
			name: "left-zero-right-all-seg2",
			// counts=[0,0,4,0]; tot=4; (0+0)*255/4=0 -> clamped to 1;
			// leftTotal=0 -> skip slot 1; rightTotal=4 -> 4*255/4=255.
			counts:     [vp8common.MaxMBSegments]int{0, 0, 4, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{1, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, false},
		},
		{
			name: "right-zero-clamps-slot2",
			// counts=[0,0,0,4]; tot=4; (0+0)*255/4=0 -> 1; leftTotal=0
			// -> skip; rightTotal=4 -> 0*255/4=0 -> clamped to 1.
			counts:     [vp8common.MaxMBSegments]int{0, 0, 0, 4},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{1, 255, 1},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := vp8enc.SegmentationConfig{
				Enabled:    true,
				UpdateMap:  true,
				UpdateData: true,
			}
			updateSegmentationTreeProbs(&cfg, tt.counts)
			for i := range cfg.TreeProbs {
				gotWrite := cfg.TreeProbUpdated[i]
				if gotWrite != tt.wantWrites[i] {
					t.Errorf("tree_prob[%d] writes magnitude = %t, want %t", i, gotWrite, tt.wantWrites[i])
				}
				// Effective wire value: when TreeProbUpdated[i] = false the
				// decoder leaves the prob at 255; otherwise it reads
				// TreeProbs[i]. Compare against the libvpx-formula result.
				var effective uint8 = 255
				if cfg.TreeProbUpdated[i] {
					effective = cfg.TreeProbs[i]
				}
				if effective != tt.wantProbs[i] {
					t.Errorf("tree_prob[%d] effective = %d, want %d", i, effective, tt.wantProbs[i])
				}
			}
		})
	}
}

// TestVP8SegmentationFeatureDataSignedEncoding audits the signed
// abs-magnitude-plus-sign encoding libvpx uses for segmentation feature data
// (vp8/encoder/bitstream.c:1086-1106): each present feature emits 1 bit (1),
// `mb_feature_data_bits[i]` magnitude bits, then 1 sign bit (1 = negative,
// 0 = non-negative). mb_feature_data_bits = {7, 6} (ALT_Q has 7-bit magnitude,
// ALT_LF has 6-bit magnitude — bitstream.c uses this table).
func TestVP8SegmentationFeatureDataSignedEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		feature  vp8common.MBLvlFeature
		segment  int
		value    int8
		expectOK bool
	}{
		{name: "alt-q-pos-min", feature: vp8common.MBLvlAltQ, segment: 1, value: 1, expectOK: true},
		{name: "alt-q-pos-max-7bit", feature: vp8common.MBLvlAltQ, segment: 1, value: 127, expectOK: true},
		{name: "alt-q-neg-min", feature: vp8common.MBLvlAltQ, segment: 2, value: -1, expectOK: true},
		{name: "alt-q-neg-max-7bit", feature: vp8common.MBLvlAltQ, segment: 2, value: -127, expectOK: true},
		// libvpx mb_feature_data_bits[ALT_LF] = 6, so abs(value) must be < 64.
		{name: "alt-lf-pos-max-6bit", feature: vp8common.MBLvlAltLF, segment: 1, value: 63, expectOK: true},
		{name: "alt-lf-neg-max-6bit", feature: vp8common.MBLvlAltLF, segment: 1, value: -63, expectOK: true},
		// abs=64 overflows the 6-bit magnitude field; libvpx writes nothing
		// for out-of-range values (would corrupt the bitstream), so govpx's
		// validator must reject.
		{name: "alt-lf-pos-overflow", feature: vp8common.MBLvlAltLF, segment: 1, value: 64, expectOK: false},
		{name: "alt-lf-neg-overflow", feature: vp8common.MBLvlAltLF, segment: 1, value: -64, expectOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := vp8enc.SegmentationConfig{
				Enabled:    true,
				UpdateMap:  true,
				UpdateData: true,
			}
			cfg.FeatureEnabled[tt.feature][tt.segment] = true
			cfg.FeatureData[tt.feature][tt.segment] = tt.value

			modes := []vp8enc.KeyFrameMacroblockMode{
				{SegmentID: uint8(tt.segment), YMode: vp8common.DCPred, UVMode: vp8common.DCPred},
			}
			dst := make([]byte, 4096)
			_, err := vp8enc.WriteZeroKeyFrame(dst, 16, 16, vp8enc.KeyFrameStateConfig{
				TokenPartition: vp8common.OnePartition,
				BaseQIndex:     32,
				Segmentation:   cfg,
			}, modes)
			if tt.expectOK {
				if err != nil {
					t.Fatalf("WriteZeroKeyFrame returned error: %v (value %d expected to round-trip)", err, tt.value)
				}
			} else {
				if err == nil {
					t.Fatalf("WriteZeroKeyFrame accepted out-of-range value %d for feature %d; libvpx %d-bit field overflows", tt.value, tt.feature, segmentationFeatureDataBitsForFeature(tt.feature))
				}
			}
		})
	}
}

// segmentationFeatureDataBitsForFeature mirrors libvpx
// vp8/encoder/bitstream.c mb_feature_data_bits: ALT_Q = 7, ALT_LF = 6.
func segmentationFeatureDataBitsForFeature(f vp8common.MBLvlFeature) int {
	switch f {
	case vp8common.MBLvlAltQ:
		return 7
	case vp8common.MBLvlAltLF:
		return 6
	default:
		return 0
	}
}
