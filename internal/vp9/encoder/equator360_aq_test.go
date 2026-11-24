package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestEquator360AQSegmentID(t *testing.T) {
	cases := []struct {
		miRow, miRows int
		want          uint8
	}{
		{0, 64, 2},
		{7, 64, 2},
		{63, 64, 2},
		{57, 64, 2},
		{8, 64, 1},
		{15, 64, 1},
		{49, 64, 1},
		{56, 64, 1},
		{16, 64, 0},
		{32, 64, 0},
		{47, 64, 0},
		{48, 64, 0},
		{0, 0, 0},
		{-1, 64, 2},
	}
	for _, tc := range cases {
		if got := Equator360AQSegmentID(tc.miRow, tc.miRows); got != tc.want {
			t.Fatalf("Equator360AQSegmentID(%d,%d) = %d, want %d",
				tc.miRow, tc.miRows, got, tc.want)
		}
	}
}

func TestEquator360AQSegmentationParamsEmitsDeltasOnIntra(t *testing.T) {
	const baseQ = 96
	seg := Equator360AQSegmentationParams(baseQ, true)
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("seg flags = enabled:%t updateMap:%t updateData:%t, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	for i, ratio := range equator360AQRateRatios {
		hasAltQ := seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlAltQ)) != 0
		if ratio.num == ratio.den {
			if hasAltQ {
				t.Fatalf("segment %d unit ratio has unexpected AltQ", i)
			}
			continue
		}
		if !hasAltQ {
			t.Fatalf("segment %d non-unit ratio missing AltQ mask", i)
		}
	}
}

func TestEquator360AQSegmentationParamsSkipsDataUpdateOnInter(t *testing.T) {
	seg := Equator360AQSegmentationParams(96, false)
	if !seg.Enabled || !seg.UpdateMap {
		t.Fatalf("seg flags = enabled:%t updateMap:%t, want both true",
			seg.Enabled, seg.UpdateMap)
	}
	if seg.UpdateData {
		t.Fatalf("inter frame must inherit segment data; got UpdateData=true")
	}
}
