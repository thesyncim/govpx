package common

import "testing"

func TestConstantsMatchVP8Limits(t *testing.T) {
	if MaxMBSegments != 4 || MaxRefFrames != 4 || QIndexRange != 128 {
		t.Fatalf("VP8 limits changed: segments=%d refs=%d qrange=%d", MaxMBSegments, MaxRefFrames, QIndexRange)
	}
	if VP8YModes != 5 || VP8UVModes != 4 || VP8MVRefs != 5 || VP8SubMVRefs != 4 {
		t.Fatalf("mode counts changed: y=%d uv=%d mv=%d submv=%d", VP8YModes, VP8UVModes, VP8MVRefs, VP8SubMVRefs)
	}
	if MVPCount != 19 {
		t.Fatalf("MVPCount = %d, want 19", MVPCount)
	}
	if VP8FilterWeight != 128 || VP8FilterShift != 7 {
		t.Fatalf("filter constants = %d/%d, want 128/7", VP8FilterWeight, VP8FilterShift)
	}
}

func TestEnumSentinels(t *testing.T) {
	if KeyFrame != 0 || InterFrame != 1 {
		t.Fatalf("frame type values = %d/%d, want 0/1", KeyFrame, InterFrame)
	}
	if DCPred != 0 || BPred != 4 || SplitMV != 9 || MBModeCount != 10 {
		t.Fatalf("macroblock prediction sentinels changed")
	}
	if BDCPred != 0 || BHUPred != 9 || New4x4 != 13 || BModeCount != 14 {
		t.Fatalf("block prediction sentinels changed")
	}
	if OnePartition != 0 || EightPartition != 3 {
		t.Fatalf("token partition sentinels changed")
	}
}
