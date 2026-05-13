package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteCoefSbBlock8x8AllZero: 8x8 luma block + 4x4 chroma (4:2:0),
// all coefficients zero. Walker should emit a per-plane / per-tx-block
// EOB-at-0 wire fragment and the decoder should observe eob=0 everywhere.
func TestWriteCoefSbBlock8x8AllZero(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1) // 4:2:0
	// Each plane gets enough above/left ctx slots for the leaf width.
	// 8x8 luma = 2 above bytes / 2 left bytes; 4x4 chroma = 1 / 1.
	planes[0].AboveContext = make([]uint8, 4)
	planes[0].LeftContext = make([]uint8, 4)
	planes[1].AboveContext = make([]uint8, 2)
	planes[1].LeftContext = make([]uint8, 2)
	planes[2].AboveContext = make([]uint8, 2)
	planes[2].LeftContext = make([]uint8, 2)

	// All-zero coeff buffer, per tx block. Same backing buffer is fine
	// for every block since the walker reads only.
	zeroCoeffs := make([]int16, 16)
	getCoeffs := func(plane, r, c int, tx common.TxSize) []int16 {
		return zeroCoeffs
	}

	args := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 16}, {16, 16}, {16, 16},
		},
		Fc:        &fc,
		GetCoeffs: getCoeffs,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	size, _ := bw.Stop()

	// Decode side: walk the same plane / tx-block layout and confirm
	// every block reads back eob=0 with the dqcoeff buffer left
	// untouched.
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Reset above/left context so the decoder sees the same fresh
	// state the encoder did before stamping residue bits.
	planes[0].AboveContext[0], planes[0].AboveContext[1] = 0, 0
	planes[0].LeftContext[0], planes[0].LeftContext[1] = 0, 0
	planes[1].AboveContext[0] = 0
	planes[1].LeftContext[0] = 0
	planes[2].AboveContext[0] = 0
	planes[2].LeftContext[0] = 0
	dqcoeff := make([]int16, 16)
	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &planes[plane]
		var txSize common.TxSize
		if plane == 0 {
			txSize = common.Tx4x4
		} else {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, common.Tx4x4, pd)
		}
		pbsize := vp9dec.GetPlaneBlockSize(common.Block8x8, pd)
		n4w := int(common.Num4x4BlocksWideLookup[pbsize])
		n4h := int(common.Num4x4BlocksHighLookup[pbsize])
		step := 1 << uint(txSize)
		scan, neighbors := scanForTxSize(txSize)
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		for rr := 0; rr < n4h; rr += step {
			for cc := 0; cc < n4w; cc += step {
				ec := vp9dec.GetEntropyContext(txSize,
					pd.AboveContext[cc:cc+step],
					pd.LeftContext[rr:rr+step])
				for i := range dqcoeff {
					dqcoeff[i] = 0
				}
				eob := vp9dec.DecodeCoefs(&r, txSize, planeType, 0,
					[2]int16{16, 16}, ec, scan, neighbors, &fc, dqcoeff)
				if eob != 0 {
					t.Errorf("plane=%d (rr,cc)=(%d,%d) eob=%d, want 0", plane, rr, cc, eob)
				}
				for j := 0; j < step; j++ {
					pd.AboveContext[cc+j] = 0
					pd.LeftContext[rr+j] = 0
				}
			}
		}
	}
}

// TestWriteCoefSbIntraScanPick: 8x8 Y intra block, V_PRED mode.
// libvpx's get_scan picks row_scan_8x8 for ADST_DCT, so the wire
// fragment is keyed to that scan. Both encoder and decoder consult
// common.GetScan; round-tripping verifies they agree.
func TestWriteCoefSbIntraScanPick(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 8)
	planes[0].LeftContext = make([]uint8, 8)
	planes[1].AboveContext = make([]uint8, 4)
	planes[1].LeftContext = make([]uint8, 4)
	planes[2].AboveContext = make([]uint8, 4)
	planes[2].LeftContext = make([]uint8, 4)

	dq := int16(16)
	// Block8x8 + Tx8x8 = 1 tx block per Y plane. V_PRED forces
	// ADST_DCT → row_scan_8x8. Plant a non-zero coeff at the raster
	// position that row_scan_8x8 reaches at scan[0].
	rowScan8x8 := common.GetScan(common.Tx8x8, 0, 0, false, common.VPred).Scan
	yCoeffs := make([]int16, 64)
	yCoeffs[rowScan8x8[0]] = dq
	uvCoeffs := make([]int16, 16)
	getCoeffs := func(plane, r, c int, tx common.TxSize) []int16 {
		if plane == 0 {
			return yCoeffs
		}
		return uvCoeffs
	}

	mi := &vp9dec.NeighborMi{
		SbType: common.Block8x8,
		Mode:   common.VPred,
		TxSize: common.Tx8x8,
	}
	args := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx8x8,
		IsInter:  0,
		Mi:       mi,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{dq, dq}, {dq, dq}, {dq, dq},
		},
		Fc:        &fc,
		GetCoeffs: getCoeffs,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	size, _ := bw.Stop()

	// Decode: reset context, walk planes, consult GetScan with the
	// same V_PRED mode.
	for i := range planes[0].AboveContext {
		planes[0].AboveContext[i] = 0
		planes[0].LeftContext[i] = 0
	}
	for i := range planes[1].AboveContext {
		planes[1].AboveContext[i] = 0
		planes[1].LeftContext[i] = 0
		planes[2].AboveContext[i] = 0
		planes[2].LeftContext[i] = 0
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	dqcoeff := make([]int16, 64)
	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &planes[plane]
		var txSize common.TxSize
		if plane == 0 {
			txSize = common.Tx8x8
		} else {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, common.Tx8x8, pd)
		}
		pbsize := vp9dec.GetPlaneBlockSize(common.Block8x8, pd)
		n4w := int(common.Num4x4BlocksWideLookup[pbsize])
		n4h := int(common.Num4x4BlocksHighLookup[pbsize])
		step := 1 << uint(txSize)
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		so := common.GetScan(txSize, planeType, 0, false, mi.Mode)
		for rr := 0; rr < n4h; rr += step {
			for cc := 0; cc < n4w; cc += step {
				ec := vp9dec.GetEntropyContext(txSize,
					pd.AboveContext[cc:cc+step],
					pd.LeftContext[rr:rr+step])
				for i := range dqcoeff {
					dqcoeff[i] = 0
				}
				eob := vp9dec.DecodeCoefs(&r, txSize, planeType, 0,
					[2]int16{dq, dq}, ec, so.Scan, so.Neighbors,
					&fc, dqcoeff)
				if plane == 0 {
					if eob != 1 {
						t.Errorf("Y eob=%d, want 1", eob)
					}
					if dqcoeff[so.Scan[0]] != dq {
						t.Errorf("Y dq[scan[0]]=%d, want %d", dqcoeff[so.Scan[0]], dq)
					}
				} else {
					if eob != 0 {
						t.Errorf("UV plane=%d eob=%d, want 0", plane, eob)
					}
				}
				hr := uint8(0)
				if eob > 0 {
					hr = 1
				}
				for j := 0; j < step; j++ {
					pd.AboveContext[cc+j] = hr
					pd.LeftContext[rr+j] = hr
				}
			}
		}
	}
}

// TestWriteCoefSbBlock8x8WithResidue: 8x8 Y has one tx block with a
// single DC=1 coefficient; the rest are zero. Verifies the walker
// emits the right wire fragment per block and the above/left
// entropy context propagates between blocks.
func TestWriteCoefSbBlock8x8WithResidue(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 4)
	planes[0].LeftContext = make([]uint8, 4)
	planes[1].AboveContext = make([]uint8, 2)
	planes[1].LeftContext = make([]uint8, 2)
	planes[2].AboveContext = make([]uint8, 2)
	planes[2].LeftContext = make([]uint8, 2)

	dq := int16(16)
	// Y has 4 tx blocks at (0,0), (0,2), (2,0), (2,2). Stamp non-zero
	// DC on the first one only.
	blockCoeffs := map[[3]int][]int16{}
	for plane := 0; plane < 3; plane++ {
		for r := 0; r < 2; r += 1 {
			for c := 0; c < 2; c += 1 {
				blockCoeffs[[3]int{plane, r, c}] = make([]int16, 16)
			}
		}
	}
	// Y (1,1) — bottom-right tx block — gets a non-zero DC. The
	// outgoing entropy context bytes (rightmost column for left, bottom
	// row for above) end up reflecting THIS block's residue after the
	// full raster walk overwrites earlier residue stamps.
	blockCoeffs[[3]int{0, 1, 1}][0] = dq

	getCoeffs := func(plane, r, c int, tx common.TxSize) []int16 {
		return blockCoeffs[[3]int{plane, r, c}]
	}

	args := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{dq, dq}, {dq, dq}, {dq, dq},
		},
		Fc:        &fc,
		GetCoeffs: getCoeffs,
	}

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	size, _ := bw.Stop()

	// After the raster walk, the rightmost-column / bottom-row entropy
	// bytes carry Y(1,1)'s residue (the last block to stamp each
	// position). The remaining slots end up at 0 since none of the
	// blocks at the (above[0], left[0]) slot had residue.
	if planes[0].AboveContext[1] != 1 || planes[0].LeftContext[1] != 1 {
		t.Errorf("Y above[1]/left[1] = %d/%d, want 1/1",
			planes[0].AboveContext[1], planes[0].LeftContext[1])
	}
	if planes[0].AboveContext[0] != 0 || planes[0].LeftContext[0] != 0 {
		t.Errorf("Y above[0]/left[0] = %d/%d, want 0/0",
			planes[0].AboveContext[0], planes[0].LeftContext[0])
	}

	// Decode: reset state and walk the same shape.
	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i := range planes[0].AboveContext {
		planes[0].AboveContext[i] = 0
		planes[0].LeftContext[i] = 0
	}
	for i := range planes[1].AboveContext {
		planes[1].AboveContext[i] = 0
		planes[1].LeftContext[i] = 0
		planes[2].AboveContext[i] = 0
		planes[2].LeftContext[i] = 0
	}
	dqcoeff := make([]int16, 16)
	for plane := 0; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &planes[plane]
		var txSize common.TxSize
		if plane == 0 {
			txSize = common.Tx4x4
		} else {
			txSize = vp9dec.GetUvTxSize(common.Block8x8, common.Tx4x4, pd)
		}
		pbsize := vp9dec.GetPlaneBlockSize(common.Block8x8, pd)
		n4w := int(common.Num4x4BlocksWideLookup[pbsize])
		n4h := int(common.Num4x4BlocksHighLookup[pbsize])
		step := 1 << uint(txSize)
		scan, neighbors := scanForTxSize(txSize)
		planeType := 0
		if plane > 0 {
			planeType = 1
		}
		for rr := 0; rr < n4h; rr += step {
			for cc := 0; cc < n4w; cc += step {
				ec := vp9dec.GetEntropyContext(txSize,
					pd.AboveContext[cc:cc+step],
					pd.LeftContext[rr:rr+step])
				for i := range dqcoeff {
					dqcoeff[i] = 0
				}
				eob := vp9dec.DecodeCoefs(&r, txSize, planeType, 0,
					[2]int16{dq, dq}, ec, scan, neighbors, &fc, dqcoeff)
				if plane == 0 && rr == 1 && cc == 1 {
					if eob != 1 {
						t.Errorf("Y(1,1) eob=%d, want 1", eob)
					}
					if dqcoeff[scan[0]] != dq {
						t.Errorf("Y(1,1) dq[scan[0]]=%d, want %d", dqcoeff[scan[0]], dq)
					}
				} else {
					if eob != 0 {
						t.Errorf("plane=%d (%d,%d) eob=%d, want 0", plane, rr, cc, eob)
					}
				}
				hr := uint8(0)
				if eob > 0 {
					hr = 1
				}
				for j := 0; j < step; j++ {
					pd.AboveContext[cc+j] = hr
					pd.LeftContext[rr+j] = hr
				}
			}
		}
	}
}
