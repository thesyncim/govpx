package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestMergeSegmentationHeaderCarriesForwardUnchangedFields(t *testing.T) {
	previous := SegmentationHeader{
		Enabled:   true,
		AbsDelta:  true,
		TreeProbs: [common.MBFeatureTreeProbs]uint8{11, 22, 33},
	}
	previous.FeatureData[common.MBLvlAltQ][2] = -17
	previous.FeatureData[common.MBLvlAltLF][3] = 9

	current := SegmentationHeader{Enabled: true}
	got := MergeSegmentationHeader(previous, current)

	if !got.AbsDelta {
		t.Fatalf("AbsDelta = false, want previous value carried forward")
	}
	if got.TreeProbs != previous.TreeProbs {
		t.Fatalf("TreeProbs = %+v, want %+v", got.TreeProbs, previous.TreeProbs)
	}
	if got.FeatureData != previous.FeatureData {
		t.Fatalf("FeatureData = %+v, want %+v", got.FeatureData, previous.FeatureData)
	}
}

func TestMergeSegmentationHeaderKeepsUpdatedFields(t *testing.T) {
	previous := SegmentationHeader{
		Enabled:   true,
		AbsDelta:  true,
		TreeProbs: [common.MBFeatureTreeProbs]uint8{11, 22, 33},
	}
	previous.FeatureData[common.MBLvlAltQ][1] = 7

	current := SegmentationHeader{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		TreeProbs:  [common.MBFeatureTreeProbs]uint8{44, 55, 66},
	}
	current.FeatureData[common.MBLvlAltLF][2] = -4

	got := MergeSegmentationHeader(previous, current)
	if got.AbsDelta {
		t.Fatalf("AbsDelta = true, want current value")
	}
	if got.TreeProbs != current.TreeProbs {
		t.Fatalf("TreeProbs = %+v, want %+v", got.TreeProbs, current.TreeProbs)
	}
	if got.FeatureData != current.FeatureData {
		t.Fatalf("FeatureData = %+v, want %+v", got.FeatureData, current.FeatureData)
	}
}

func TestMergeSegmentationHeaderDisabledDoesNotCarryState(t *testing.T) {
	previous := SegmentationHeader{
		Enabled:   true,
		AbsDelta:  true,
		TreeProbs: [common.MBFeatureTreeProbs]uint8{11, 22, 33},
	}
	current := SegmentationHeader{}

	got := MergeSegmentationHeader(previous, current)
	if got != current {
		t.Fatalf("disabled merge = %+v, want %+v", got, current)
	}
}
