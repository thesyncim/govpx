package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestWriteCoefBlockEarlyEob: an all-zero block emits only the
// EOB-at-position-0 bit; the decoder must read back eob=0 and leave
// dqcoeff untouched.
func TestWriteCoefBlockEarlyEob(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]

	coeffs := make([]int16, 16)
	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: 16,
		DequantAC: 16,
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	})
	if err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 16)
	for i := range dqcoeff {
		dqcoeff[i] = 0x7777
	}
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, [2]int16{16, 16}, 0, scan, neigh, &fc, dqcoeff)
	if got != 0 {
		t.Errorf("eob got %d, want 0", got)
	}
	for i, v := range dqcoeff {
		if v != 0x7777 {
			t.Errorf("dqcoeff[%d] disturbed: %d", i, v)
		}
	}
}

// TestWriteCoefBlockSingleOne: a block whose first coefficient is
// the DC dequant (i.e. absVal=1) round-trips with eob=1 and the
// matching dqcoeff value at scan[0].
func TestWriteCoefBlockSingleOne(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 24}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = dq[0] // sign +1, absVal = 1

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: dq[0],
		DequantAC: dq[1],
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 1 {
		t.Errorf("eob got %d want 1", got)
	}
	if dqcoeff[scan[0]] != dq[0] {
		t.Errorf("dqcoeff[scan[0]] = %d want %d", dqcoeff[scan[0]], dq[0])
	}
}

// TestWriteCoefBlockZeroRunThenOne: scan position 0 is zero, scan
// position 1 has the AC dequant (absVal=1), then EOB.
func TestWriteCoefBlockZeroRunThenOne(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	scan := tables.DefaultScan4x4[:]
	neigh := tables.DefaultScan4x4Neighbors[:]
	dq := [2]int16{16, 32}

	coeffs := make([]int16, 16)
	coeffs[scan[0]] = 0
	coeffs[scan[1]] = dq[1]

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, WriteCoefBlockArgs{
		TxSize:    common.Tx4x4,
		DequantDC: dq[0],
		DequantAC: dq[1],
		Scan:      scan,
		Neighbors: neigh,
		Coeffs:    coeffs,
		Fc:        &fc,
	}); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, _ := bw.Stop()
	var r bitstream.Reader
	r.Init(buf[:size])
	dqcoeff := make([]int16, 16)
	got := vp9dec.DecodeCoefs(&r, common.Tx4x4, 0, 0, dq, 0, scan, neigh, &fc, dqcoeff)
	if got != 2 {
		t.Errorf("eob got %d want 2", got)
	}
	if dqcoeff[scan[0]] != 0 {
		t.Errorf("dqcoeff[scan[0]] = %d want 0", dqcoeff[scan[0]])
	}
	if dqcoeff[scan[1]] != dq[1] {
		t.Errorf("dqcoeff[scan[1]] = %d want %d", dqcoeff[scan[1]], dq[1])
	}
}
