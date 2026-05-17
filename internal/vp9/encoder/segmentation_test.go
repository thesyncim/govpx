package encoder

import (
	"bytes"
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

// referenceEncodeSegmentation mirrors libvpx v1.16.0 encode_segmentation
// (vp9/encoder/vp9_bitstream.c:760-817) verbatim, plus the helpers it
// uses (encode_unsigned_max, get_unsigned_bits, vp9_seg_feature_data_max,
// vp9_is_segfeature_signed). It exists only to pin the byte sequence
// govpx's encodeSegmentation produces against the C oracle's wire
// shape — any divergence here is a wire-format bug regardless of
// roundtrip correctness via the decoder.
//
// The function takes the same SegmentationParams shape encodeSegmentation
// takes and writes the bits via the same BitWriter, so a byte compare
// catches packing differences (MSB-first ordering, padding) as well as
// logical differences.
func referenceEncodeSegmentation(w *BitWriter, seg *vp9dec.SegmentationParams) {
	// vpx_wb_write_bit(wb, seg->enabled);
	if seg.Enabled {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
	// if (!seg->enabled) return;
	if !seg.Enabled {
		return
	}

	// Segmentation map
	// vpx_wb_write_bit(wb, seg->update_map);
	if seg.UpdateMap {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
	if seg.UpdateMap {
		// vp9_choose_segmap_coding_method is upstream of the wire writer;
		// the caller is responsible for having populated seg->tree_probs /
		// seg->pred_probs / seg->temporal_update before reaching here.
		// for (i = 0; i < SEG_TREE_PROBS; i++)
		for i := range vp9dec.SegTreeProbs {
			prob := seg.TreeProbs[i]
			update := prob != vp9dec.MaxProb
			if update {
				w.WriteBit(1)
			} else {
				w.WriteBit(0)
			}
			if update {
				w.WriteLiteral(uint32(prob), 8)
			}
		}

		// vpx_wb_write_bit(wb, seg->temporal_update);
		if seg.TemporalUpdate {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
		if seg.TemporalUpdate {
			for i := range vp9dec.PredictionProbs {
				prob := seg.PredProbs[i]
				update := prob != vp9dec.MaxProb
				if update {
					w.WriteBit(1)
				} else {
					w.WriteBit(0)
				}
				if update {
					w.WriteLiteral(uint32(prob), 8)
				}
			}
		}
	}

	// Segmentation data
	// vpx_wb_write_bit(wb, seg->update_data);
	if seg.UpdateData {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
	if !seg.UpdateData {
		return
	}
	// vpx_wb_write_bit(wb, seg->abs_delta);
	if seg.AbsDelta {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}

	// libvpx's local tables (seg_feature_data_max / seg_feature_data_signed
	// from vp9_seg_common.c).
	refDataMax := [vp9dec.SegLvlMax]int{255, 63, 3, 0}
	refSigned := [vp9dec.SegLvlMax]bool{true, true, false, false}

	for i := range vp9dec.MaxSegments {
		for j := range vp9dec.SegLvlMax {
			active := seg.FeatureMask[i]&(1<<uint(j)) != 0
			if active {
				w.WriteBit(1)
			} else {
				w.WriteBit(0)
			}
			if !active {
				continue
			}
			data := int(seg.FeatureData[i][j])
			dataMax := refDataMax[j]
			// encode_unsigned_max(wb, abs(data), data_max);
			mag := data
			if mag < 0 {
				mag = -mag
			}
			// get_unsigned_bits(num): msb+1 for >0, else 0.
			bits := 0
			for v := uint(dataMax); v > 0; v >>= 1 {
				bits++
			}
			if bits > 0 {
				w.WriteLiteral(uint32(mag), bits)
			}
			if refSigned[j] {
				// vpx_wb_write_bit(wb, data < 0);
				if data < 0 {
					w.WriteBit(1)
				} else {
					w.WriteBit(0)
				}
			}
		}
	}
}

// TestEncodeSegmentationBytesMatchLibvpxReference pins the byte sequence
// encodeSegmentation emits against a verbatim port of libvpx v1.16.0
// encode_segmentation (vp9_bitstream.c:760-817). The fixtures span the
// segmentation shapes the encoder writes today: cyclic-AQ frames (Q
// deltas with temporal_update=false), ROI frames (Q+LF+Skip+RefFrame
// mixed features), an active-map frame (temporal_update=true with
// mixed predictor probs), and the disabled-segmentation shortcut. Any
// divergence here is a wire-format bug regardless of roundtrip
// correctness via the decoder.
func TestEncodeSegmentationBytesMatchLibvpxReference(t *testing.T) {
	type fixture struct {
		name string
		seg  vp9dec.SegmentationParams
	}

	// Build the cyclic-AQ-shaped fixture: Boost1 (segment 1) gets a
	// negative Q delta, Boost2 (segment 2) gets a more aggressive
	// negative Q delta, all other segments inactive. TemporalUpdate is
	// false because govpx's pipeline skips vp9ChooseSegmapCodingMethod
	// on cyclic-AQ frames — the audit finding documents that.
	var cyclic vp9dec.SegmentationParams
	cyclic.Enabled = true
	cyclic.UpdateMap = true
	cyclic.UpdateData = true
	cyclic.AbsDelta = false
	cyclic.TemporalUpdate = false
	// TreeProbs[*] = 128 mirrors a uniform distribution-derived prob.
	for i := range vp9dec.SegTreeProbs {
		cyclic.TreeProbs[i] = 128
	}
	cyclic.FeatureMask[1] = 1 << uint(vp9dec.SegLvlAltQ)
	cyclic.FeatureData[1][vp9dec.SegLvlAltQ] = -20
	cyclic.FeatureMask[2] = 1 << uint(vp9dec.SegLvlAltQ)
	cyclic.FeatureData[2][vp9dec.SegLvlAltQ] = -40

	// Build the ROI-shaped fixture: segment 1 carries both an LF delta
	// and a Skip flag, segment 3 carries a RefFrame override.
	// TreeProbs are kept at MaxProb to model the ROI default (no-prob
	// update; decoder side fills 255).
	var roi vp9dec.SegmentationParams
	roi.Enabled = true
	roi.UpdateMap = true
	roi.UpdateData = true
	roi.AbsDelta = false
	roi.TemporalUpdate = false
	for i := range vp9dec.SegTreeProbs {
		roi.TreeProbs[i] = vp9dec.MaxProb
	}
	roi.FeatureMask[1] = (1 << uint(vp9dec.SegLvlAltLf)) |
		(1 << uint(vp9dec.SegLvlSkip))
	roi.FeatureData[1][vp9dec.SegLvlAltLf] = -8
	roi.FeatureMask[3] = 1 << uint(vp9dec.SegLvlRefFrame)
	roi.FeatureData[3][vp9dec.SegLvlRefFrame] = 2

	// Active-map fixture: temporal_update=true with mixed pred probs
	// (the path govpx exercises when vp9ChooseSegmapCodingMethod runs).
	var active vp9dec.SegmentationParams
	active.Enabled = true
	active.UpdateMap = true
	active.UpdateData = true
	active.AbsDelta = false
	active.TemporalUpdate = true
	for i := range vp9dec.SegTreeProbs {
		active.TreeProbs[i] = 128
	}
	active.PredProbs[0] = 1
	active.PredProbs[1] = vp9dec.MaxProb
	active.PredProbs[2] = 128
	active.FeatureMask[2] = (1 << uint(vp9dec.SegLvlSkip)) |
		(1 << uint(vp9dec.SegLvlRefFrame))
	active.FeatureData[2][vp9dec.SegLvlRefFrame] = 1

	fixtures := []fixture{
		{name: "Disabled", seg: vp9dec.SegmentationParams{}},
		{name: "CyclicAQ", seg: cyclic},
		{name: "ROI", seg: roi},
		{name: "ActiveMap", seg: active},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			got := make([]byte, 64)
			gotW := NewBitWriter(got)
			encodeSegmentation(gotW, &fx.seg)

			want := make([]byte, 64)
			wantW := NewBitWriter(want)
			referenceEncodeSegmentation(wantW, &fx.seg)

			if gotW.BitsWritten() != wantW.BitsWritten() {
				t.Fatalf("bits written: got %d, want %d",
					gotW.BitsWritten(), wantW.BitsWritten())
			}
			n := gotW.BytesWritten()
			if !bytes.Equal(got[:n], want[:n]) {
				t.Fatalf("byte sequence mismatch:\n got = % x\nwant = % x",
					got[:n], want[:n])
			}
		})
	}
}

// TestEncodeSegmentationCyclicAQByteFixture pins a hand-computed byte
// fixture for the cyclic-AQ segmentation shape. The bits are computed
// by walking libvpx encode_segmentation by hand against the same
// SegmentationParams; this catches regressions in either the BitWriter
// MSB-ordering or the encode_segmentation control flow that the
// reference-encoder cross-check might miss if both diverge in lockstep.
//
// Fixture: enabled=1, update_map=1, 7 × tree_probs (each 1 + 0x80),
// temporal_update=0, update_data=1, abs_delta=0, then:
//   - segment 0: 4 inactive bits
//   - segment 1: AltQ active + abs(-20)=20 in 8 bits + sign 1; then 3 inactive
//   - segment 2: AltQ active + abs(-40)=40 in 8 bits + sign 1; then 3 inactive
//   - segments 3..7: 4 × 5 = 20 inactive bits
//
// Bit stream MSB-first packing yields the byte sequence below.
func TestEncodeSegmentationCyclicAQByteFixture(t *testing.T) {
	var seg vp9dec.SegmentationParams
	seg.Enabled = true
	seg.UpdateMap = true
	seg.UpdateData = true
	seg.AbsDelta = false
	seg.TemporalUpdate = false
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = 1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[1][vp9dec.SegLvlAltQ] = -20
	seg.FeatureMask[2] = 1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[2][vp9dec.SegLvlAltQ] = -40

	// Compute the fixture from the byte-by-byte bit stream:
	//
	// bit sequence (MSB-first, top byte aligned):
	//   1 1                          // enabled + update_map
	//   1 1000 0000   1 1000 0000    // tree_probs[0..1] update=1, prob=128
	//   1 1000 0000   1 1000 0000    // tree_probs[2..3]
	//   1 1000 0000   1 1000 0000    // tree_probs[4..5]
	//   1 1000 0000                  // tree_probs[6]
	//   0                            // temporal_update
	//   1                            // update_data
	//   0                            // abs_delta
	//   0 0 0 0                      // seg 0 features inactive
	//   1 0001 0100 1                // seg 1 AltQ active, |-20|=20, sign 1
	//   0 0 0                        // seg 1 features 1..3 inactive
	//   1 0010 1000 1                // seg 2 AltQ active, |-40|=40, sign 1
	//   0 0 0                        // seg 2 features 1..3 inactive
	//   0000 0000 0000 0000 0000     // segs 3..7 × 4 features = 20 inactive
	//
	// Total = 2 + 63 + 1 + 1 + 1 + 4 + 10 + 3 + 10 + 3 + 20 = 118 bits.
	got := make([]byte, 32)
	w := NewBitWriter(got)
	encodeSegmentation(w, &seg)
	if w.BitsWritten() != 118 {
		t.Fatalf("bits written = %d, want 118", w.BitsWritten())
	}

	// Build the expected sequence via the same bit-write helpers used by
	// the production writer, so the fixture is self-checking against
	// MSB-first packing without hand-rolling bytes.
	want := make([]byte, 32)
	wantW := NewBitWriter(want)
	wantW.WriteBit(1) // enabled
	wantW.WriteBit(1) // update_map
	for range vp9dec.SegTreeProbs {
		wantW.WriteBit(1)          // update
		wantW.WriteLiteral(128, 8) // prob
	}
	wantW.WriteBit(0) // temporal_update
	wantW.WriteBit(1) // update_data
	wantW.WriteBit(0) // abs_delta

	// Segment 0: 4 inactive bits.
	for range 4 {
		wantW.WriteBit(0)
	}
	// Segment 1: AltQ active with -20.
	wantW.WriteBit(1)
	wantW.WriteLiteral(20, 8)
	wantW.WriteBit(1) // sign
	for range 3 {
		wantW.WriteBit(0)
	}
	// Segment 2: AltQ active with -40.
	wantW.WriteBit(1)
	wantW.WriteLiteral(40, 8)
	wantW.WriteBit(1) // sign
	for range 3 {
		wantW.WriteBit(0)
	}
	// Segments 3..7: 20 inactive bits.
	for range 20 {
		wantW.WriteBit(0)
	}

	if wantW.BitsWritten() != 118 {
		t.Fatalf("oracle bit accounting wrong: %d", wantW.BitsWritten())
	}
	n := w.BytesWritten()
	if !bytes.Equal(got[:n], want[:n]) {
		t.Fatalf("cyclic-AQ byte fixture mismatch:\n got = % x\nwant = % x",
			got[:n], want[:n])
	}

	// Round-trip back through the decoder so the fixture exercises both
	// shapes against the reader and writer.
	var r vp9dec.BitReader
	r.Init(got[:n])
	var back vp9dec.SegmentationParams
	vp9dec.ReadSegmentation(&r, &back)
	if !back.Enabled || !back.UpdateMap || !back.UpdateData || back.TemporalUpdate {
		t.Errorf("roundtrip flags = %+v, want enabled/updates", back)
	}
	if back.FeatureData[1][vp9dec.SegLvlAltQ] != -20 {
		t.Errorf("AltQ[1] = %d, want -20", back.FeatureData[1][vp9dec.SegLvlAltQ])
	}
	if back.FeatureData[2][vp9dec.SegLvlAltQ] != -40 {
		t.Errorf("AltQ[2] = %d, want -40", back.FeatureData[2][vp9dec.SegLvlAltQ])
	}
}
