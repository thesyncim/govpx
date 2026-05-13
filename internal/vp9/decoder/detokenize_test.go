package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// seedDefaultCoefProbs mirrors libvpx's vp9_default_coef_probs (the
// per-frame coefficient PMF seeded into FRAME_CONTEXT.coef_probs at
// past-independent reset). Copies the four default_coef_probs_*
// blobs into the FrameCoefProbs slot for the matching tx_size.
func seedDefaultCoefProbs() FrameCoefProbs {
	var out FrameCoefProbs
	out[common.Tx4x4] = CoefProbsModel(tables.DefaultCoefProbs4x4)
	out[common.Tx8x8] = CoefProbsModel(tables.DefaultCoefProbs8x8)
	out[common.Tx16x16] = CoefProbsModel(tables.DefaultCoefProbs16x16)
	out[common.Tx32x32] = CoefProbsModel(tables.DefaultCoefProbs32x32)
	return out
}

// TestDecodeCoefsEarlyEob: a single EOB-bit (=0) at scan position 0
// ends the block immediately. dqcoeff must be left untouched.
func TestDecodeCoefsEarlyEob(t *testing.T) {
	fc := seedDefaultCoefProbs()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 16}

	// Look up the band-0 ctx-0 eob prob for inter Y to know what
	// probability to write the 0-bit against.
	probs := fc[common.Tx4x4][0][0][0][0]
	eobProb := uint32(probs[eobContextNode])

	buf := make([]byte, 32)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(0, eobProb) // EOB
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	for i := range dqcoeff {
		dqcoeff[i] = 0x7777 // sentinel
	}
	got := DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 0 {
		t.Errorf("eob got %d, want 0", got)
	}
	for i, v := range dqcoeff {
		if v != 0x7777 {
			t.Errorf("dqcoeff[%d] = %d, sentinel disturbed", i, v)
		}
	}
}

// TestDecodeCoefsOneToken: ONE_TOKEN at scan position 0 with positive
// sign, then EOB at scan position 1. The decoded coefficient should
// equal the DC dequant value.
func TestDecodeCoefsOneToken(t *testing.T) {
	fc := seedDefaultCoefProbs()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	probs0 := fc[common.Tx4x4][0][0][0][0]
	// After the first token, ctx is recomputed from neighbors. For
	// scan position 1 with token_cache[0]=1, ctx = (1+1+0)>>1 = 1.
	probs1 := fc[common.Tx4x4][0][0][0][1]
	_ = probs1 // band-translate at scan-pos 1 stays in band 0

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// Token at scan[0]:
	w.Write(1, uint32(probs0[eobContextNode]))  // not EOB
	w.Write(1, uint32(probs0[zeroContextNode])) // not ZERO
	w.Write(0, uint32(probs0[oneContextNode]))  // == ONE_TOKEN
	w.Write(0, 128)                             // sign = +
	// Re-fetch probs for scan[1] (band-translate index 1 still maps to band 0
	// for 4x4; ctx becomes 1).
	band1 := tables.CoefbandTrans4x4[1]
	probs1b := fc[common.Tx4x4][0][0][band1][1]
	w.Write(0, uint32(probs1b[eobContextNode])) // EOB at pos 1
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob = %d, want 1", got)
	}
	if dqcoeff[scan[0]] != dq[0] {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], dq[0])
	}
	for i := 1; i < 16; i++ {
		if dqcoeff[i] != 0 {
			t.Errorf("dqcoeff[%d] = %d, want 0", i, dqcoeff[i])
		}
	}
}

// TestDecodeCoefsTwoToken: TWO_TOKEN at scan position 0 with negative
// sign (pareto8 path: oneCtx=1, p[0]=0, p[1]=0 → token=2). Then EOB.
func TestDecodeCoefsTwoToken(t *testing.T) {
	fc := seedDefaultCoefProbs()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{20, 20}

	probs0 := fc[common.Tx4x4][0][0][0][0]
	p := tables.Pareto8Full[probs0[pivotNode]-1]

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	w.Write(1, uint32(probs0[eobContextNode]))  // not EOB
	w.Write(1, uint32(probs0[zeroContextNode])) // not ZERO
	w.Write(1, uint32(probs0[oneContextNode]))  // token>=2 path
	w.Write(0, uint32(p[0]))                    // not high half
	w.Write(0, uint32(p[1]))                    // token = 2
	w.Write(1, 128)                             // sign = -

	// After this we need to escape; emit EOB at scan position 1.
	band1 := tables.CoefbandTrans4x4[1]
	probs1 := fc[common.Tx4x4][0][0][band1][1] // ctx=1 because token_cache[scan[0]]=2 → (1+2+0)>>1=1
	w.Write(0, uint32(probs1[eobContextNode]))
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob = %d, want 1", got)
	}
	want := int16(-(2 * dq[0]))
	if dqcoeff[scan[0]] != want {
		t.Errorf("dqcoeff[scan[0]] = %d, want %d", dqcoeff[scan[0]], want)
	}
}

// TestDecodeCoefsZeroRun: ZERO_TOKEN at scan positions 0 and 1, then
// ONE_TOKEN at scan position 2 (positive sign), EOB at 3.
func TestDecodeCoefsZeroRun(t *testing.T) {
	fc := seedDefaultCoefProbs()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 32}

	band := tables.CoefbandTrans4x4
	probs0 := fc[common.Tx4x4][0][0][band[0]][0]

	buf := make([]byte, 64)
	var w bitstream.Writer
	w.Start(buf)
	// scan[0]: not-EOB then ZERO → run starts
	w.Write(1, uint32(probs0[eobContextNode]))
	w.Write(0, uint32(probs0[zeroContextNode]))

	// scan[1]: ctx now = (1+0+0)>>1 = 0 (neighbor's token_cache=0).
	probs1 := fc[common.Tx4x4][0][0][band[1]][0]
	// In the ZERO-run inner loop, ONLY the ZERO node is read — no EOB.
	w.Write(0, uint32(probs1[zeroContextNode]))

	// scan[2]: ctx still 0. Inner ZERO loop reads ZERO again, gets 1
	// (exits to outer code).
	probs2 := fc[common.Tx4x4][0][0][band[2]][0]
	w.Write(1, uint32(probs2[zeroContextNode]))
	w.Write(0, uint32(probs2[oneContextNode])) // ONE_TOKEN
	w.Write(0, 128)                            // sign = +

	// scan[3]: ctx = (1+1+0)>>1 = 1, token_cache[scan[2]]=1.
	probs3 := fc[common.Tx4x4][0][0][band[3]][1]
	w.Write(0, uint32(probs3[eobContextNode])) // EOB

	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	got := DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 3 {
		t.Errorf("eob = %d, want 3", got)
	}
	if dqcoeff[scan[0]] != 0 || dqcoeff[scan[1]] != 0 {
		t.Errorf("expected zeros, got %d, %d", dqcoeff[scan[0]], dqcoeff[scan[1]])
	}
	if dqcoeff[scan[2]] != dq[1] {
		t.Errorf("dqcoeff[scan[2]] = %d, want %d (AC=%d)", dqcoeff[scan[2]], dq[1], dq[1])
	}
}
