package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestWriteTokenForCoeffOneRoundTrip emits a single ONE coefficient
// at scan position 0 (with positive sign) followed by EOB at scan
// position 1, then decodes the block via DecodeCoefs and confirms
// the coefficient comes back equal to dq[0].
func TestWriteTokenForCoeffOneRoundTrip(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	probs := fc[common.Tx4x4][0][0][0][0]
	band1 := tables.CoefbandTrans4x4[1]
	probs1 := fc[common.Tx4x4][0][0][band1][1]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	// Coefficient at scan position 0: not EOB, not ZERO, ONE.
	bw.Write(1, uint32(probs[0])) // not EOB
	bw.Write(1, uint32(probs[1])) // not ZERO
	WriteTokenForCoeff(&bw, probs[:], 1, 0)
	// Coefficient at scan position 1: EOB.
	bw.Write(0, uint32(probs1[0]))

	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("bw.Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d, want 1", got)
	}
	if dqcoeff[scan[0]] != dq[0] {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], dq[0])
	}
}

// TestWriteTokenForCoeffCat1Negative writes a magnitude-5 (CAT1
// boundary) coefficient with negative sign and decodes it back.
func TestWriteTokenForCoeffCat1Negative(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	probs := fc[common.Tx4x4][0][0][0][0]
	band1 := tables.CoefbandTrans4x4[1]
	// After this coefficient ctx becomes (1+5+0)>>1 = 3? No — token cache
	// stamps 5 at scan[0], so ctx for next = (1+5+0)>>1 = 3.
	probs1 := fc[common.Tx4x4][0][0][band1][3]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	bw.Write(1, uint32(probs[0])) // not EOB
	bw.Write(1, uint32(probs[1])) // not ZERO
	WriteTokenForCoeff(&bw, probs[:], 5, 1)
	bw.Write(0, uint32(probs1[0]))

	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d, want 1", got)
	}
	want := -int16(5 * int(dq[0]))
	if dqcoeff[scan[0]] != want {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], want)
	}
}

// seedDefaultCoefProbsForEnc seeds FrameCoefProbs with libvpx's
// default coefficient probabilities. Mirrors the decoder-side test
// helper.
func seedDefaultCoefProbsForEnc() vp9dec.FrameCoefProbs {
	var out vp9dec.FrameCoefProbs
	out[common.Tx4x4] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs4x4)
	out[common.Tx8x8] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs8x8)
	out[common.Tx16x16] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs16x16)
	out[common.Tx32x32] = vp9dec.CoefProbsModel(tables.DefaultCoefProbs32x32)
	return out
}
