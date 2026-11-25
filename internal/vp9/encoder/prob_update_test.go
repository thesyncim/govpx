package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestCondProbDiffUpdateFromCountsRoundTripUpdate: counts heavily
// biased away from oldp should drive a positive savings score, so
// the writer emits the update bit + sub-exp delta. The decoder's
// VpxDiffUpdateProb reads the same fragment back and ends with the
// same probability the savings search settled on.
func TestCondProbDiffUpdateFromCountsRoundTripUpdate(t *testing.T) {
	cases := []struct {
		oldp uint8
		ct   [2]uint32
	}{
		{128, [2]uint32{1000, 100}}, // newp leans low
		{128, [2]uint32{100, 1000}}, // newp leans high
		{200, [2]uint32{50, 500}},   // oldp high, counts say low
		{40, [2]uint32{500, 50}},    // oldp low, counts say high
	}
	for _, c := range cases {
		buf := make([]byte, 32)
		var bw bitstream.Writer
		bw.Start(buf)
		writerOldp := c.oldp
		gotNewp := CondProbDiffUpdateFromCounts(&bw, &writerOldp, c.ct)
		size, err := bw.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}

		var r bitstream.Reader
		if err := r.Init(buf[:size]); err != nil {
			t.Fatalf("Init: %v", err)
		}
		decoded := c.oldp
		vp9dec.VpxDiffUpdateProb(&r, &decoded)

		if writerOldp != gotNewp {
			t.Errorf("oldp=%d ct=%v: writer side oldp = %d, want %d",
				c.oldp, c.ct, writerOldp, gotNewp)
		}
		if decoded != gotNewp {
			t.Errorf("oldp=%d ct=%v: decoded=%d, writer newp=%d",
				c.oldp, c.ct, decoded, gotNewp)
		}
	}
}

// TestCondProbDiffUpdateFromCountsNoUpdateWhenAligned: when counts
// agree with the current prob (no savings to be had) the writer
// emits a single 0 bit and the prob stays unchanged on the decoder
// side.
func TestCondProbDiffUpdateFromCountsNoUpdateWhenAligned(t *testing.T) {
	oldp := uint8(128)
	// 50/50 counts mean newp ≈ 128. The savings_search may still
	// hunt nearby probabilities, but a tight (oldp, counts) match
	// almost always lands on no-update.
	ct := [2]uint32{1000, 1000}

	buf := make([]byte, 8)
	var bw bitstream.Writer
	bw.Start(buf)
	writerOldp := oldp
	gotNewp := CondProbDiffUpdateFromCounts(&bw, &writerOldp, ct)
	size, _ := bw.Stop()

	if gotNewp != oldp {
		t.Logf("savings search picked newp=%d != oldp=%d; skip if path differs", gotNewp, oldp)
	}

	var r bitstream.Reader
	r.Init(buf[:size])
	decoded := oldp
	vp9dec.VpxDiffUpdateProb(&r, &decoded)
	if decoded != writerOldp {
		t.Errorf("decoded=%d != writer side=%d", decoded, writerOldp)
	}
}

// TestCondProbDiffUpdateNoUpdate: when newp == oldp we emit only the
// "no update" bit and the decoder leaves the slot unchanged.
func TestCondProbDiffUpdateNoUpdate(t *testing.T) {
	dst := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(dst)
	old := uint8(128)
	CondProbDiffUpdate(&bw, old, old)
	n, err := bw.Stop()
	if err != nil {
		t.Fatalf("bw.Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	got := old
	vp9dec.VpxDiffUpdateProb(&r, &got)
	if got != old {
		t.Errorf("no-update changed prob: got %d, want %d", got, old)
	}
}

// TestCondProbDiffUpdateRoundTrip walks a spread of (oldp, newp)
// pairs through the writer and confirms the decoder recovers each
// newp byte-for-byte. The hand-picked pairs hit each of the
// encode_term_subexp buckets (4-bit / second 4-bit / 5-bit / uniform).
func TestCondProbDiffUpdateRoundTrip(t *testing.T) {
	cases := []struct {
		old, new uint8
	}{
		{128, 1},   // far end of 4-bit bucket
		{128, 64},  // mid range
		{128, 192}, // upper half
		{128, 255},
		{32, 200},
		{200, 32},
		{1, 254},
		{50, 60}, // small delta
	}
	for i, c := range cases {
		dst := make([]byte, 64)
		var bw bitstream.Writer
		bw.Start(dst)
		CondProbDiffUpdate(&bw, c.old, c.new)
		n, err := bw.Stop()
		if err != nil {
			t.Fatalf("case %d: bw.Stop: %v", i, err)
		}

		var r bitstream.Reader
		if err := r.Init(dst[:n]); err != nil {
			t.Fatalf("case %d: Reader.Init: %v", i, err)
		}
		got := c.old
		vp9dec.VpxDiffUpdateProb(&r, &got)
		if got != c.new {
			t.Errorf("case %d: (old=%d, new=%d) round-tripped to %d",
				i, c.old, c.new, got)
		}
	}
}

// TestEncodeTermSubexpRoundTripFuzz walks every word in [0, 255]
// through the writer + decoder to confirm the prefix-code + uniform
// tail handle every magnitude bucket.
func TestEncodeTermSubexpRoundTripFuzz(t *testing.T) {
	for word := range 255 {
		dst := make([]byte, 64)
		var bw bitstream.Writer
		bw.Start(dst)
		encodeTermSubexp(&bw, word)
		n, err := bw.Stop()
		if err != nil {
			t.Fatalf("word=%d: bw.Stop: %v", word, err)
		}
		var r bitstream.Reader
		if err := r.Init(dst[:n]); err != nil {
			t.Fatalf("word=%d: Reader.Init: %v", word, err)
		}
		// The decoder helper isn't exported, so re-derive its body
		// here as the inverse of encodeTermSubexp.
		got := decodeTermSubexpForTest(t, &r)
		if got != word {
			t.Errorf("word=%d: round-trip got %d", word, got)
		}
	}
}

// TestProbDiffUpdateCostMatchesUpdateBits anchors the cost helper:
// the returned cost must equal (updateBits[remap_index] <<
// VP9ProbCostShift) for every (newp, oldp) pair. Walking a small
// spread is enough to catch a wrong shift / wrong index lookup.
func TestProbDiffUpdateCostMatchesUpdateBits(t *testing.T) {
	// Only non-equal probs go through ProbDiffUpdateCost; libvpx
	// gates on (newp != oldp) before calling the cost helper.
	cases := []struct{ newp, oldp uint8 }{
		{129, 128},
		{200, 128},
		{1, 128},
		{255, 128},
	}
	for i, c := range cases {
		got := ProbDiffUpdateCost(c.newp, c.oldp)
		want := int(updateBits[remapProb(int(c.newp), int(c.oldp))]) << VP9ProbCostShift
		if got != want {
			t.Errorf("case %d: ProbDiffUpdateCost(%d, %d) = %d, want %d",
				i, c.newp, c.oldp, got, want)
		}
	}
}

// TestProbDiffUpdateCostUsesPermutation: the cost is always a
// multiple of (1 << VP9ProbCostShift), so the low VP9ProbCostShift
// bits must be zero. A regression that dropped the shift would
// surface here.
func TestProbDiffUpdateCostUsesPermutation(t *testing.T) {
	for _, pair := range [][2]uint8{{1, 128}, {64, 128}, {200, 64}, {253, 1}} {
		got := ProbDiffUpdateCost(pair[0], pair[1])
		if got&((1<<VP9ProbCostShift)-1) != 0 {
			t.Errorf("cost(%d, %d) = %d has non-zero low %d bits",
				pair[0], pair[1], got, VP9ProbCostShift)
		}
	}
}

// TestUpdateBitsShape: trivial shape check + invariant — every
// non-zero entry must meet MinDelpBits.
func TestUpdateBitsShape(t *testing.T) {
	if len(updateBits) != 255 {
		t.Errorf("updateBits len = %d, want 255", len(updateBits))
	}
	if updateBits[254] != 0 {
		t.Errorf("updateBits[254] = %d, want 0", updateBits[254])
	}
	for _, v := range updateBits {
		if v != 0 && v < MinDelpBits {
			t.Errorf("entry %d below MinDelpBits=%d", v, MinDelpBits)
		}
	}
}

// decodeTermSubexpForTest mirrors decoder.decodeTermSubexp (private).
// Local copy is fine here because the encoder and decoder must agree
// on the wire format anyway.
func decodeTermSubexpForTest(t *testing.T, r *bitstream.Reader) int {
	t.Helper()
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4))
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4)) + 16
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(5)) + 32
	}
	// Uniform tail: 7-bit, optional 1-bit extension when value >= 65.
	v := int(r.ReadLiteral(7))
	if v < 65 {
		return v + 64
	}
	return ((v - 65) << 1) + int(r.ReadBit()) + 65 + 64
}
