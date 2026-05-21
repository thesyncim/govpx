package tables

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestInterModeContextsMatchLibvpx(t *testing.T) {
	want := [6][4]uint8{
		{7, 1, 1, 143},
		{14, 18, 14, 107},
		{135, 64, 57, 68},
		{60, 56, 128, 65},
		{159, 134, 128, 34},
		{234, 188, 128, 28},
	}
	if InterModeContexts != want {
		t.Fatalf("InterModeContexts = %v, want %v", InterModeContexts, want)
	}
}

func TestInterModeTreesMatchLibvpx(t *testing.T) {
	wantMVRefTree := [8]int16{
		-int16(common.ZeroMV), 2,
		-int16(common.NearestMV), 4,
		-int16(common.NearMV), 6,
		-int16(common.NewMV), -int16(common.SplitMV),
	}
	if MVRefTree != wantMVRefTree {
		t.Fatalf("MVRefTree = %v, want %v", MVRefTree, wantMVRefTree)
	}

	wantSubMVRefTree := [6]int16{
		-int16(common.Left4x4), 2,
		-int16(common.Above4x4), 4,
		-int16(common.Zero4x4), -int16(common.New4x4),
	}
	if SubMVRefTree != wantSubMVRefTree {
		t.Fatalf("SubMVRefTree = %v, want %v", SubMVRefTree, wantSubMVRefTree)
	}

	wantMBSplitTree := [6]int16{-3, 2, -2, 4, -0, -1}
	if MBSplitTree != wantMBSplitTree {
		t.Fatalf("MBSplitTree = %v, want %v", MBSplitTree, wantMBSplitTree)
	}
	if want := [3]uint8{110, 111, 150}; MBSplitProbs != want {
		t.Fatalf("MBSplitProbs = %v, want %v", MBSplitProbs, want)
	}
}
