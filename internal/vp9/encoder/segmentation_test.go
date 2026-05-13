package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestEncodeSegmentationDisabled emits the single 0 bit and stops;
// the decoder's ReadSegmentation reads it back and leaves
// Enabled=false plus the rest at zero defaults.
func TestEncodeSegmentationDisabled(t *testing.T) {
	seg := &vp9dec.SegmentationParams{}
	buf := make([]byte, 8)
	w := NewBitWriter(buf)
	encodeSegmentation(w, seg)

	var r vp9dec.BitReader
	r.Init(buf[:w.BytesWritten()])
	var got vp9dec.SegmentationParams
	vp9dec.ReadSegmentation(&r, &got)
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

// TestEncodeSegmentationMapAndDataRoundTrip exercises every branch:
// tree probs with mixed update / no-update, temporal predictor probs,
// per-feature data with both signed and unsigned slots.
func TestEncodeSegmentationMapAndDataRoundTrip(t *testing.T) {
	seg := &vp9dec.SegmentationParams{
		Enabled:        true,
		UpdateMap:      true,
		UpdateData:     true,
		AbsDelta:       true,
		TemporalUpdate: true,
	}
	// Mix updated (≠ MaxProb) and not-updated (= MaxProb) tree probs.
	seg.TreeProbs[0] = 200
	seg.TreeProbs[1] = vp9dec.MaxProb // no update
	seg.TreeProbs[2] = 50
	for i := 3; i < vp9dec.SegTreeProbs; i++ {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	seg.PredProbs[0] = 100
	seg.PredProbs[1] = vp9dec.MaxProb
	seg.PredProbs[2] = 200

	// Segment 0: SegLvlAltQ = -32 (signed), SegLvlSkip = active+0.
	seg.FeatureMask[0] = (1 << vp9dec.SegLvlAltQ) | (1 << vp9dec.SegLvlSkip)
	seg.FeatureData[0][vp9dec.SegLvlAltQ] = -32
	seg.FeatureData[0][vp9dec.SegLvlSkip] = 0
	// Segment 3: SegLvlAltLf = +12 (signed), SegLvlRefFrame = 2.
	seg.FeatureMask[3] = (1 << vp9dec.SegLvlAltLf) | (1 << vp9dec.SegLvlRefFrame)
	seg.FeatureData[3][vp9dec.SegLvlAltLf] = 12
	seg.FeatureData[3][vp9dec.SegLvlRefFrame] = 2

	buf := make([]byte, 128)
	w := NewBitWriter(buf)
	encodeSegmentation(w, seg)

	var r vp9dec.BitReader
	r.Init(buf[:w.BytesWritten()])
	var got vp9dec.SegmentationParams
	vp9dec.ReadSegmentation(&r, &got)

	if !got.Enabled || !got.UpdateMap || !got.UpdateData || !got.TemporalUpdate {
		t.Fatalf("flags = %+v, want all true (except UpdateMap+UpdateData+TemporalUpdate)", got)
	}
	if got.AbsDelta != true {
		t.Errorf("AbsDelta = %v, want true", got.AbsDelta)
	}
	for i := range vp9dec.SegTreeProbs {
		if got.TreeProbs[i] != seg.TreeProbs[i] {
			t.Errorf("TreeProbs[%d] = %d, want %d", i, got.TreeProbs[i], seg.TreeProbs[i])
		}
	}
	for i := range vp9dec.PredictionProbs {
		if got.PredProbs[i] != seg.PredProbs[i] {
			t.Errorf("PredProbs[%d] = %d, want %d", i, got.PredProbs[i], seg.PredProbs[i])
		}
	}
	for i := range vp9dec.MaxSegments {
		if got.FeatureMask[i] != seg.FeatureMask[i] {
			t.Errorf("FeatureMask[%d] = %#x, want %#x", i, got.FeatureMask[i], seg.FeatureMask[i])
		}
		for j := range vp9dec.SegLvlMax {
			if seg.FeatureMask[i]&(1<<uint(j)) == 0 {
				continue
			}
			if got.FeatureData[i][j] != seg.FeatureData[i][j] {
				t.Errorf("FeatureData[%d][%d] = %d, want %d",
					i, j, got.FeatureData[i][j], seg.FeatureData[i][j])
			}
		}
	}
}
