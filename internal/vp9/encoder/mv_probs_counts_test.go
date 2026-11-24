package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestUpdateMvEmitsLiteral plants counts heavily biased away from
// curP so the cost comparison picks an update. The decoder side
// UpdateMvProbs reads the 7-bit literal and recovers (lit << 1) | 1
// — which is the same odd-only newp the writer used.
func TestUpdateMvEmitsLiteral(t *testing.T) {
	cur := uint8(128)
	ct := [2]uint32{1000, 100}

	buf := make([]byte, 8)
	var bw bitstream.Writer
	bw.Start(buf)
	writerCur := cur
	updated := UpdateMv(&bw, ct, &writerCur)
	size, _ := bw.Stop()
	if !updated {
		t.Fatal("expected UpdateMv to emit an update for heavy-bias counts")
	}

	// Decode through UpdateMvProbs (single-slot slice).
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dec := []uint8{cur}
	vp9dec.UpdateMvProbs(&r, dec)
	if dec[0] != writerCur {
		t.Errorf("decoded=%d, writer=%d", dec[0], writerCur)
	}
	if writerCur&1 != 1 {
		t.Errorf("writer newp=%d is even; should be odd", writerCur)
	}
}

// TestUpdateMvSkipsWhenAligned: when counts already agree with curP
// the cost comparison fails and no update is emitted (single 0 bit).
func TestUpdateMvSkipsWhenAligned(t *testing.T) {
	cur := uint8(128)
	ct := [2]uint32{500, 500} // 50/50 → newp ≈ 128

	buf := make([]byte, 8)
	var bw bitstream.Writer
	bw.Start(buf)
	writerCur := cur
	updated := UpdateMv(&bw, ct, &writerCur)
	size, _ := bw.Stop()
	if updated {
		t.Errorf("expected no update for balanced counts")
	}
	if writerCur != cur {
		t.Errorf("writerCur changed: %d != %d", writerCur, cur)
	}
	var r bitstream.Reader
	r.Init(buf[:size])
	dec := []uint8{cur}
	vp9dec.UpdateMvProbs(&r, dec)
	if dec[0] != cur {
		t.Errorf("decoded changed: %d != %d", dec[0], cur)
	}
}

// TestWriteNmvProbsFromCountsRoundTripWithHp: full NMV walk through
// joints + 2 axes + class0_fp + fp + hp slabs. Decoder side reads
// the same wire fragment via ReadMvProbs.
func TestWriteNmvProbsFromCountsRoundTripWithHp(t *testing.T) {
	var probs vp9dec.NmvContext
	for i := range probs.Joints {
		probs.Joints[i] = 128
	}
	for c := range probs.Comps {
		comp := &probs.Comps[c]
		comp.Sign = 128
		for i := range comp.Classes {
			comp.Classes[i] = 128
		}
		for i := range comp.Class0 {
			comp.Class0[i] = 128
		}
		for i := range comp.Bits {
			comp.Bits[i] = 128
		}
		for i := range comp.Class0Fp {
			for j := range comp.Class0Fp[i] {
				comp.Class0Fp[i][j] = 128
			}
		}
		for i := range comp.Fp {
			comp.Fp[i] = 128
		}
		comp.Class0Hp = 128
		comp.Hp = 128
	}

	counts := NmvContextCounts{}
	counts.Joints[0] = 500
	counts.Joints[1] = 20
	counts.Joints[2] = 20
	counts.Joints[3] = 100
	for c := range counts.Comps {
		ccnt := &counts.Comps[c]
		ccnt.Sign = [2]uint32{300, 50}
		ccnt.Classes[0] = 400
		ccnt.Classes[1] = 100
		for i := 2; i < len(ccnt.Classes); i++ {
			ccnt.Classes[i] = 10
		}
		ccnt.Class0[0] = 200
		ccnt.Class0[1] = 50
		for i := range ccnt.Bits {
			ccnt.Bits[i] = [2]uint32{50, 10}
		}
		ccnt.Class0Hp = [2]uint32{200, 30}
		ccnt.Hp = [2]uint32{180, 25}
		ccnt.Fp[0] = 100
		ccnt.Fp[1] = 80
		ccnt.Fp[2] = 60
		ccnt.Fp[3] = 50
		for i := range ccnt.Class0Fp {
			ccnt.Class0Fp[i][0] = 80
			ccnt.Class0Fp[i][1] = 60
			ccnt.Class0Fp[i][2] = 40
			ccnt.Class0Fp[i][3] = 30
		}
	}

	scratch := make([][2]uint32, 32)
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	writerProbs := probs
	WriteNmvProbsFromCounts(&bw, &writerProbs, &counts, true, scratch)
	size, _ := bw.Stop()

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	decProbs := probs
	vp9dec.ReadMvProbs(&r, &decProbs, true)
	if decProbs != writerProbs {
		t.Errorf("decoder side probs diverged from encoder side")
	}
}
