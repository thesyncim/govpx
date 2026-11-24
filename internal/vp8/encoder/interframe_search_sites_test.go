package encoder

import "testing"

func TestInterFrameNstepSearchSitesMirrorLibvpx3StepTable(t *testing.T) {
	sites := InterFrameNstepSearchSites[:]
	if len(sites) != 65 {
		t.Fatalf("nstep search sites = %d, want 65", len(sites))
	}
	wantFirst := [...]MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
		{Row: -128, Col: -128},
		{Row: -128, Col: 128},
		{Row: 128, Col: -128},
		{Row: 128, Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[57] != (MotionVector{Row: -1}) || sites[64] != (MotionVector{Row: 1, Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1,+1", sites[57], sites[64])
	}
}

func TestInterFrameDiamondSearchSitesMirrorLibvpxDSMotionTable(t *testing.T) {
	sites := InterFrameDiamondSearchSites[:]
	if len(sites) != 33 {
		t.Fatalf("diamond search sites = %d, want 33", len(sites))
	}
	wantFirst := [...]MotionVector{
		{},
		{Row: -128},
		{Row: 128},
		{Col: -128},
		{Col: 128},
	}
	for i, want := range wantFirst {
		if sites[i] != want {
			t.Fatalf("site[%d] = %+v, want %+v", i, sites[i], want)
		}
	}
	if sites[29] != (MotionVector{Row: -1}) || sites[32] != (MotionVector{Col: 1}) {
		t.Fatalf("final step sites = %+v/%+v, want -1 row and +1 col", sites[29], sites[32])
	}
}
