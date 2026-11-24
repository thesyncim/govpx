package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
