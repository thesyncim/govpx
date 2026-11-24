package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestSegmentationTreeProbsMirrorLibvpxCounts(t *testing.T) {
	cfg := SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	keyModes := []KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 0}}

	UpdateKeyFrameSegmentationTreeProbs(&cfg, keyModes)
	if cfg.TreeProbUpdated != ([vp8common.MBFeatureTreeProbs]bool{}) {
		t.Fatalf("key tree prob updates = %v, want none for all-zero segment map", cfg.TreeProbUpdated)
	}

	cfg = SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	interModes := make([]InterFrameMacroblockMode, 40)
	interModes[0].SegmentID = StaticSegmentID
	interModes[1].SegmentID = StaticSegmentID

	UpdateInterFrameSegmentationTreeProbs(&cfg, interModes)
	if cfg.TreeProbUpdated[0] || !cfg.TreeProbUpdated[1] || cfg.TreeProbUpdated[2] {
		t.Fatalf("inter tree prob update flags = %v, want only branch 1 updated", cfg.TreeProbUpdated)
	}
	if got := cfg.TreeProbs[1]; got != 242 {
		t.Fatalf("inter tree prob[1] = %d, want libvpx count-derived 242", got)
	}
}

func TestSegmentationTreeProbsMatchLibvpxFormula(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		counts     [vp8common.MaxMBSegments]int
		wantProbs  [vp8common.MBFeatureTreeProbs]uint8
		wantWrites [vp8common.MBFeatureTreeProbs]bool
	}{
		{
			name:       "all-zero-keeps-defaults",
			counts:     [vp8common.MaxMBSegments]int{0, 0, 0, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{false, false, false},
		},
		{
			name:       "all-left-skews-prob0-low",
			counts:     [vp8common.MaxMBSegments]int{8, 0, 0, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{255, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{false, false, false},
		},
		{
			name:       "left-all-seg0-right-all-seg2",
			counts:     [vp8common.MaxMBSegments]int{2, 0, 5, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{72, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, false},
		},
		{
			name:       "left-zero-right-all-seg2",
			counts:     [vp8common.MaxMBSegments]int{0, 0, 4, 0},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{1, 255, 255},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, false},
		},
		{
			name:       "right-zero-clamps-slot2",
			counts:     [vp8common.MaxMBSegments]int{0, 0, 0, 4},
			wantProbs:  [vp8common.MBFeatureTreeProbs]uint8{1, 255, 1},
			wantWrites: [vp8common.MBFeatureTreeProbs]bool{true, false, true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := SegmentationConfig{
				Enabled:    true,
				UpdateMap:  true,
				UpdateData: true,
			}
			UpdateSegmentationTreeProbs(&cfg, tt.counts)
			for i := range cfg.TreeProbs {
				gotWrite := cfg.TreeProbUpdated[i]
				if gotWrite != tt.wantWrites[i] {
					t.Errorf("tree_prob[%d] writes magnitude = %t, want %t", i, gotWrite, tt.wantWrites[i])
				}
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

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshCadence(t *testing.T) {
	modes := make([]InterFrameMacroblockMode, 40)
	refreshCount := 2

	AssignInterFrameStaticSegments(4, 10, 0, refreshCount, modes)

	if modes[0].SegmentID != StaticSegmentID || modes[1].SegmentID != StaticSegmentID {
		t.Fatalf("first cyclic segment IDs = %d/%d, want refreshed", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != 0 || modes[len(modes)-1].SegmentID != 0 {
		t.Fatalf("later cyclic segment IDs = %d/%d, want zero", modes[2].SegmentID, modes[len(modes)-1].SegmentID)
	}

	AssignInterFrameStaticSegments(4, 10, 2, refreshCount, modes)
	if modes[0].SegmentID != 0 || modes[1].SegmentID != 0 {
		t.Fatalf("previous cyclic segment IDs = %d/%d, want cleared", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != StaticSegmentID || modes[3].SegmentID != StaticSegmentID {
		t.Fatalf("rotated cyclic segment IDs = %d/%d, want refreshed", modes[2].SegmentID, modes[3].SegmentID)
	}

	AssignInterFrameStaticSegments(4, 10, 39, refreshCount, modes)
	if modes[39].SegmentID != StaticSegmentID || modes[0].SegmentID != StaticSegmentID {
		t.Fatalf("wrapped cyclic segment IDs = %d/%d, want refreshed", modes[39].SegmentID, modes[0].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[38].SegmentID != 0 {
		t.Fatalf("wrapped neighbor segment IDs = %d/%d, want zero", modes[1].SegmentID, modes[38].SegmentID)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshMapEligibility(t *testing.T) {
	modes := make([]InterFrameMacroblockMode, 5)
	refreshMap := []int8{0, -1, 1, 0, 0}

	next := AssignInterFrameStaticSegmentsWithMap(1, 5, 0, 2, refreshMap, modes)

	if next != 4 {
		t.Fatalf("next cyclic refresh index = %d, want 4 after libvpx-style eligible refresh budget", next)
	}
	if modes[0].SegmentID != StaticSegmentID || modes[3].SegmentID != StaticSegmentID {
		t.Fatalf("segment IDs = %d/%d, want refreshed MB0 and MB3 under libvpx eligible budget", modes[0].SegmentID, modes[3].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[2].SegmentID != 0 || modes[4].SegmentID != 0 {
		t.Fatalf("ineligible segment IDs = %d/%d/%d, want zero", modes[1].SegmentID, modes[2].SegmentID, modes[4].SegmentID)
	}
	if refreshMap[1] != 0 {
		t.Fatalf("cooldown map[1] = %d, want incremented to candidate 0", refreshMap[1])
	}
	if refreshMap[2] != 1 {
		t.Fatalf("dirty map[2] = %d, want unchanged", refreshMap[2])
	}
}
