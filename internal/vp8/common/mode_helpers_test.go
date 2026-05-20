package common

import "testing"

func TestBlockModeFromMacroblockMode(t *testing.T) {
	cases := []struct {
		mode MBPredictionMode
		want BPredictionMode
	}{
		{DCPred, BDCPred},
		{VPred, BVEPred},
		{HPred, BHEPred},
		{TMPred, BTMPred},
		{BPred, BDCPred},
	}
	for _, tc := range cases {
		if got := BlockModeFromMacroblockMode(tc.mode); got != tc.want {
			t.Fatalf("BlockModeFromMacroblockMode(%v) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestIsWholeInterMacroblockMode(t *testing.T) {
	for _, mode := range []MBPredictionMode{ZeroMV, NearestMV, NearMV, NewMV} {
		if !IsWholeInterMacroblockMode(mode) {
			t.Fatalf("IsWholeInterMacroblockMode(%v) = false, want true", mode)
		}
	}
	for _, mode := range []MBPredictionMode{DCPred, BPred, SplitMV} {
		if IsWholeInterMacroblockMode(mode) {
			t.Fatalf("IsWholeInterMacroblockMode(%v) = true, want false", mode)
		}
	}
}

func TestUVTokenContextIndex(t *testing.T) {
	cases := []struct {
		block     int
		wantAbove int
		wantLeft  int
	}{
		{16, 0, 0},
		{17, 1, 0},
		{18, 0, 1},
		{19, 1, 1},
		{20, 2, 2},
		{21, 3, 2},
		{22, 2, 3},
		{23, 3, 3},
	}
	for _, tc := range cases {
		gotAbove, gotLeft := UVTokenContextIndex(tc.block)
		if gotAbove != tc.wantAbove || gotLeft != tc.wantLeft {
			t.Fatalf("UVTokenContextIndex(%d) = (%d,%d), want (%d,%d)",
				tc.block, gotAbove, gotLeft, tc.wantAbove, tc.wantLeft)
		}
	}
}
