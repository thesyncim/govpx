package decoder

import "testing"

func TestReadSegmentationDisabled(t *testing.T) {
	var pk bitPacker
	pk.writeBit(0) // enabled = false
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	ReadSegmentation(&r, &seg)
	if seg.Enabled || seg.UpdateMap || seg.UpdateData {
		t.Errorf("expected all flags false, got %+v", seg)
	}
}

func TestReadSegmentationEnabledNoUpdates(t *testing.T) {
	// enabled=1, update_map=0, update_data=0.
	var pk bitPacker
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	// Seed previous-frame state to verify it is preserved.
	seg.TreeProbs[0] = 99
	seg.FeatureData[1][SegLvlAltQ] = 17
	ReadSegmentation(&r, &seg)
	if !seg.Enabled {
		t.Error("Enabled should be true")
	}
	if seg.UpdateMap || seg.UpdateData {
		t.Error("UpdateMap / UpdateData should be false")
	}
	if seg.TreeProbs[0] != 99 {
		t.Errorf("TreeProbs[0] = %d, want 99 (preserved)", seg.TreeProbs[0])
	}
	if seg.FeatureData[1][SegLvlAltQ] != 17 {
		t.Errorf("FeatureData[1][AltQ] = %d, want 17 (preserved)", seg.FeatureData[1][SegLvlAltQ])
	}
}

func TestReadSegmentationFullUpdate(t *testing.T) {
	// enabled=1, update_map=1, all 7 tree probs default to MAX_PROB,
	// temporal_update=0 (so PredProbs <- MAX_PROB), update_data=1,
	// abs_delta=0, segment 0 enables SEG_LVL_ALT_Q with value -10
	// and SEG_LVL_REF_FRAME with value 2, all other features off.
	var pk bitPacker
	pk.writeBit(1) // enabled
	pk.writeBit(1) // update_map
	for range SegTreeProbs {
		pk.writeBit(0) // each prob defaults
	}
	pk.writeBit(0) // temporal_update=0
	pk.writeBit(1) // update_data
	pk.writeBit(0) // abs_delta=0
	// segment 0: ALT_Q enabled, magnitude 10, sign negative
	pk.writeBit(1) // feature 0 enabled (ALT_Q, signed, max=255)
	pk.writeLiteral(10, 8)
	pk.writeBit(1) // sign = negative
	pk.writeBit(0) // feature 1 disabled (ALT_LF)
	pk.writeBit(1) // feature 2 enabled (REF_FRAME, unsigned, max=3, 2 bits)
	pk.writeLiteral(2, 2)
	pk.writeBit(0) // feature 3 disabled (SKIP, no data)
	// segments 1..7: all features disabled (28 bits).
	for i := 1; i < MaxSegments; i++ {
		for range SegLvlMax {
			pk.writeBit(0)
		}
	}
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	ReadSegmentation(&r, &seg)
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Errorf("flags wrong: %+v", seg)
	}
	for i := range SegTreeProbs {
		if seg.TreeProbs[i] != MaxProb {
			t.Errorf("TreeProbs[%d] = %d, want MaxProb", i, seg.TreeProbs[i])
		}
	}
	for i := range PredictionProbs {
		if seg.PredProbs[i] != MaxProb {
			t.Errorf("PredProbs[%d] = %d, want MaxProb", i, seg.PredProbs[i])
		}
	}
	if seg.FeatureMask[0] != (1<<SegLvlAltQ)|(1<<SegLvlRefFrame) {
		t.Errorf("FeatureMask[0] = %b, want %b", seg.FeatureMask[0], (1<<SegLvlAltQ)|(1<<SegLvlRefFrame))
	}
	if seg.FeatureData[0][SegLvlAltQ] != -10 {
		t.Errorf("AltQ = %d, want -10", seg.FeatureData[0][SegLvlAltQ])
	}
	if seg.FeatureData[0][SegLvlRefFrame] != 2 {
		t.Errorf("RefFrame = %d, want 2", seg.FeatureData[0][SegLvlRefFrame])
	}
}

func TestGetUnsignedBits(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 2}, {4, 3}, {63, 6}, {64, 7}, {255, 8}, {256, 9},
	}
	for _, c := range cases {
		if got := getUnsignedBits(c.n); got != c.want {
			t.Errorf("getUnsignedBits(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}
