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
	var stats FrameCoefBranchStats
	args.CoefBranchStats = &stats

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	size, _ := bw.Stop()
	if got := stats[common.Tx4x4][0][0][0][0][0]; got != [2]uint32{4, 0} {
		t.Fatalf("Y all-zero eob stats = %v, want [4 0]", got)
	}
	if got := stats[common.Tx4x4][1][0][0][0][0]; got != [2]uint32{2, 0} {
		t.Fatalf("UV all-zero eob stats = %v, want [2 0]", got)
	}

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
	for plane := range vp9dec.MaxMbPlane {
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
				for j := range step {
					pd.AboveContext[cc+j] = 0
					pd.LeftContext[rr+j] = 0
				}
			}
		}
	}
}

func TestCommitCoefSbContextsMatchesWriteCoefSb(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	makePlanes := func() [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane {
		var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
		vp9dec.SetupBlockPlanes(&planes, 1, 1)
		planes[0].AboveContext = make([]uint8, 4)
		planes[0].LeftContext = make([]uint8, 4)
		planes[1].AboveContext = make([]uint8, 2)
		planes[1].LeftContext = make([]uint8, 2)
		planes[2].AboveContext = make([]uint8, 2)
		planes[2].LeftContext = make([]uint8, 2)
		return planes
	}
	writePlanes := makePlanes()
	commitPlanes := makePlanes()

	zero := make([]int16, 16)
	one := make([]int16, 16)
	one[0] = 16
	getCoeffs := func(plane, r, c int, tx common.TxSize) []int16 {
		if (plane == 0 && r == 0 && c == 0) || plane == 1 {
			return one
		}
		return zero
	}
	getEOB := func(plane, r, c int, tx common.TxSize) (int, bool) {
		if (plane == 0 && r == 0 && c == 0) || plane == 1 {
			return 1, true
		}
		return 0, true
	}
	baseArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 16}, {16, 16}, {16, 16},
		},
		Fc:        &fc,
		GetCoeffs: getCoeffs,
		GetEOB:    getEOB,
	}

	writeArgs := baseArgs
	writeArgs.Planes = &writePlanes
	var bw bitstream.Writer
	bw.Start(make([]byte, 256))
	if err := WriteCoefSb(&bw, writeArgs); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	commitArgs := baseArgs
	commitArgs.Planes = &commitPlanes
	if err := CommitCoefSbContexts(commitArgs); err != nil {
		t.Fatalf("CommitCoefSbContexts: %v", err)
	}
	for plane := range vp9dec.MaxMbPlane {
		if got, want := commitPlanes[plane].AboveContext, writePlanes[plane].AboveContext; !equalUint8s(got, want) {
			t.Fatalf("plane %d above context = %v, want %v", plane, got, want)
		}
		if got, want := commitPlanes[plane].LeftContext, writePlanes[plane].LeftContext; !equalUint8s(got, want) {
			t.Fatalf("plane %d left context = %v, want %v", plane, got, want)
		}
	}
}

func TestCommitCoefSbContextsFromTokensMatchesWriteCoefSb(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	makePlanes := func() [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane {
		var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
		vp9dec.SetupBlockPlanes(&planes, 1, 1)
		planes[0].AboveContext = make([]uint8, 4)
		planes[0].LeftContext = make([]uint8, 4)
		planes[1].AboveContext = make([]uint8, 2)
		planes[1].LeftContext = make([]uint8, 2)
		planes[2].AboveContext = make([]uint8, 2)
		planes[2].LeftContext = make([]uint8, 2)
		return planes
	}
	writePlanes := makePlanes()
	stagePlanes := makePlanes()
	commitPlanes := makePlanes()

	zero := make([]int16, 16)
	dc := make([]int16, 16)
	dc[0] = 16
	ac := make([]int16, 16)
	ac[1] = 16
	getCoeffs := func(plane, r, c int, tx common.TxSize) []int16 {
		switch {
		case plane == 0 && r == 0 && c == 0:
			return dc
		case plane == 0 && r == 1 && c == 1:
			return ac
		case plane == 1:
			return dc
		default:
			return zero
		}
	}
	getEOB := func(plane, r, c int, tx common.TxSize) (int, bool) {
		switch {
		case plane == 0 && r == 0 && c == 0:
			return 1, true
		case plane == 0 && r == 1 && c == 1:
			return 2, true
		case plane == 1:
			return 1, true
		default:
			return 0, true
		}
	}
	baseArgs := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		IsInter:  1,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 16}, {16, 16}, {16, 16},
		},
		Fc:        &fc,
		GetCoeffs: getCoeffs,
		GetEOB:    getEOB,
	}

	writeArgs := baseArgs
	writeArgs.Planes = &writePlanes
	var bw bitstream.Writer
	bw.Start(make([]byte, 256))
	if err := WriteCoefSb(&bw, writeArgs); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}
	if _, err := bw.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	tokens := make([]TokenExtra, 64)
	tokenIndex := 0
	stageArgs := baseArgs
	stageArgs.Planes = &stagePlanes
	stageArgs.TokenDst = tokens
	stageArgs.TokenIndex = &tokenIndex
	stageArgs.TokenOnly = true
	var discard bitstream.Writer
	discard.StartDiscard()
	if err := WriteCoefSb(&discard, stageArgs); err != nil {
		t.Fatalf("stage WriteCoefSb: %v", err)
	}
	if tokenIndex >= len(tokens) {
		t.Fatal("token stage filled buffer before EOSB")
	}
	tokens[tokenIndex] = TokenExtra{Token: EOSBToken}
	tokenIndex++

	commitArgs := baseArgs
	commitArgs.Planes = &commitPlanes
	if err := CommitCoefSbContextsFromTokens(commitArgs, tokens[:tokenIndex]); err != nil {
		t.Fatalf("CommitCoefSbContextsFromTokens: %v", err)
	}
	for plane := range vp9dec.MaxMbPlane {
		if got, want := commitPlanes[plane].AboveContext, writePlanes[plane].AboveContext; !equalUint8s(got, want) {
			t.Fatalf("plane %d above context = %v, want %v", plane, got, want)
		}
		if got, want := commitPlanes[plane].LeftContext, writePlanes[plane].LeftContext; !equalUint8s(got, want) {
			t.Fatalf("plane %d left context = %v, want %v", plane, got, want)
		}
	}
}

func equalUint8s(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWriteCoefSbClipsFrameEdges(t *testing.T) {
	cases := []struct {
		name               string
		miRows, miCols     int
		miRow, miCol       int
		wantY, wantUV      uint32
		maxYRow, maxYCol   int
		maxUVRow, maxUVCol int
	}{
		{
			name:     "bottom",
			miRows:   3,
			miCols:   4,
			miRow:    2,
			miCol:    0,
			wantY:    8,
			wantUV:   4,
			maxYRow:  2,
			maxYCol:  4,
			maxUVRow: 1,
			maxUVCol: 2,
		},
		{
			name:     "right",
			miRows:   4,
			miCols:   3,
			miRow:    0,
			miCol:    2,
			wantY:    8,
			wantUV:   4,
			maxYRow:  4,
			maxYCol:  2,
			maxUVRow: 2,
			maxUVCol: 1,
		},
		{
			name:     "bottom-right",
			miRows:   3,
			miCols:   3,
			miRow:    2,
			miCol:    2,
			wantY:    4,
			wantUV:   2,
			maxYRow:  2,
			maxYCol:  2,
			maxUVRow: 1,
			maxUVCol: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := seedDefaultCoefProbsForEnc()

			var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
			vp9dec.SetupBlockPlanes(&planes, 1, 1)
			planes[0].AboveContext = make([]uint8, 4)
			planes[0].LeftContext = make([]uint8, 4)
			planes[1].AboveContext = make([]uint8, 2)
			planes[1].LeftContext = make([]uint8, 2)
			planes[2].AboveContext = make([]uint8, 2)
			planes[2].LeftContext = make([]uint8, 2)

			zeroCoeffs := make([]int16, 16)
			calls := map[[3]int]int{}
			var stats FrameCoefBranchStats
			buf := make([]byte, 256)
			var bw bitstream.Writer
			bw.Start(buf)
			if err := WriteCoefSb(&bw, WriteCoefSbArgs{
				BSize:    common.Block16x16,
				MiTxSize: common.Tx4x4,
				MiRows:   tc.miRows,
				MiCols:   tc.miCols,
				MiRow:    tc.miRow,
				MiCol:    tc.miCol,
				Planes:   &planes,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					{16, 16}, {16, 16}, {16, 16},
				},
				Fc:              &fc,
				CoefBranchStats: &stats,
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					calls[[3]int{plane, r, c}]++
					return zeroCoeffs
				},
			}); err != nil {
				t.Fatalf("WriteCoefSb: %v", err)
			}
			if _, err := bw.Stop(); err != nil {
				t.Fatalf("Stop: %v", err)
			}

			if got := stats[common.Tx4x4][0][0][0][0][0]; got != [2]uint32{tc.wantY, 0} {
				t.Fatalf("clipped Y eob stats = %v, want [%d 0]", got, tc.wantY)
			}
			if got := stats[common.Tx4x4][1][0][0][0][0]; got != [2]uint32{tc.wantUV, 0} {
				t.Fatalf("clipped UV eob stats = %v, want [%d 0]", got, tc.wantUV)
			}
			for key := range calls {
				plane, r, c := key[0], key[1], key[2]
				if plane == 0 && (r >= tc.maxYRow || c >= tc.maxYCol) {
					t.Fatalf("Y out-of-frame tx encoded at r=%d c=%d", r, c)
				}
				if plane > 0 && (r >= tc.maxUVRow || c >= tc.maxUVCol) {
					t.Fatalf("UV out-of-frame tx encoded at r=%d c=%d", r, c)
				}
			}
		})
	}
}

func TestWriteCoefSbACOnlyResidueStampsEntropyContext(t *testing.T) {
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
	scan, _ := scanForTxSize(common.Tx4x4)
	blockCoeffs := map[[3]int][]int16{}
	for plane := range 3 {
		for r := range 2 {
			for c := range 2 {
				blockCoeffs[[3]int{plane, r, c}] = make([]int16, 16)
			}
		}
	}
	blockCoeffs[[3]int{0, 1, 1}][scan[1]] = dq

	buf := make([]byte, 256)
	var bw bitstream.Writer
	bw.Start(buf)
	if err := WriteCoefSb(&bw, WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{dq, dq}, {dq, dq}, {dq, dq},
		},
		Fc: &fc,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
			return blockCoeffs[[3]int{plane, r, c}]
		},
	}); err != nil {
		t.Fatalf("WriteCoefSb: %v", err)
	}

	if planes[0].AboveContext[1] != 1 || planes[0].LeftContext[1] != 1 {
		t.Fatalf("AC-only Y above[1]/left[1] = %d/%d, want 1/1",
			planes[0].AboveContext[1], planes[0].LeftContext[1])
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
	for plane := range vp9dec.MaxMbPlane {
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
				for j := range step {
					pd.AboveContext[cc+j] = hr
					pd.LeftContext[rr+j] = hr
				}
			}
		}
	}
}

func TestWriteCoefSbDenseInterTx8LeavesRoundTrip(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 16)
	planes[0].LeftContext = make([]uint8, 16)
	planes[1].AboveContext = make([]uint8, 8)
	planes[1].LeftContext = make([]uint8, 8)
	planes[2].AboveContext = make([]uint8, 8)
	planes[2].LeftContext = make([]uint8, 8)

	dq := int16(16)
	scan8, _ := scanForTxSize(common.Tx8x8)
	yCoeffs := make([]int16, 64)
	yCoeffs[scan8[1]] = dq
	zero4 := make([]int16, 16)
	mi := &vp9dec.NeighborMi{
		SbType: common.Block8x8,
		Mode:   common.NearestMv,
		TxSize: common.Tx8x8,
		RefFrame: [2]int8{
			vp9dec.LastFrame,
			vp9dec.NoRefFrame,
		},
	}

	buf := make([]byte, 4096)
	var bw bitstream.Writer
	bw.Start(buf)
	for miRow := range 8 {
		for miCol := range 8 {
			aboveOffsets := [vp9dec.MaxMbPlane]int{miCol * 2, miCol, miCol}
			leftOffsets := [vp9dec.MaxMbPlane]int{miRow * 2, miRow, miRow}
			if err := WriteCoefSb(&bw, WriteCoefSbArgs{
				BSize:        common.Block8x8,
				MiTxSize:     common.Tx8x8,
				IsInter:      1,
				Mi:           mi,
				Planes:       &planes,
				AboveOffsets: aboveOffsets,
				LeftOffsets:  leftOffsets,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					{dq, dq}, {dq, dq}, {dq, dq},
				},
				Fc: &fc,
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					if plane == 0 {
						return yCoeffs
					}
					return zero4
				},
			}); err != nil {
				t.Fatalf("WriteCoefSb leaf (%d,%d): %v", miRow, miCol, err)
			}
		}
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	for plane := range vp9dec.MaxMbPlane {
		for i := range planes[plane].AboveContext {
			planes[plane].AboveContext[i] = 0
		}
		for i := range planes[plane].LeftContext {
			planes[plane].LeftContext[i] = 0
		}
	}

	var r bitstream.Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for miRow := range 8 {
		for miCol := range 8 {
			aboveOffsets := [vp9dec.MaxMbPlane]int{miCol * 2, miCol, miCol}
			leftOffsets := [vp9dec.MaxMbPlane]int{miRow * 2, miRow, miRow}
			for plane := range vp9dec.MaxMbPlane {
				pd := &planes[plane]
				planeType := 0
				txSize := common.Tx8x8
				dequant := [2]int16{dq, dq}
				if plane > 0 {
					planeType = 1
					txSize = common.Tx4x4
				}
				step := 1 << uint(txSize)
				aboveCtx := pd.AboveContext[aboveOffsets[plane] : aboveOffsets[plane]+step]
				leftCtx := pd.LeftContext[leftOffsets[plane] : leftOffsets[plane]+step]
				initCtx := vp9dec.GetEntropyContext(txSize, aboveCtx, leftCtx)
				scan, neighbors := scanForTxSize(txSize)
				coeffs := make([]int16, vp9dec.MaxEobForTxSize(txSize))
				eob := vp9dec.DecodeCoefs(&r, txSize, planeType, 1, dequant,
					initCtx, scan, neighbors, &fc, coeffs)
				if plane == 0 {
					if eob != 2 {
						t.Fatalf("Y leaf (%d,%d) eob=%d, want 2", miRow, miCol, eob)
					}
					if coeffs[scan8[1]] != dq {
						t.Fatalf("Y leaf (%d,%d) coeff=%d, want %d",
							miRow, miCol, coeffs[scan8[1]], dq)
					}
				} else if eob != 0 {
					t.Fatalf("UV plane %d leaf (%d,%d) eob=%d, want 0",
						plane, miRow, miCol, eob)
				}
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for i := range step {
					aboveCtx[i] = hasResidue
					leftCtx[i] = hasResidue
				}
			}
		}
	}
	if r.HasError() {
		t.Fatal("reader reported a dense Tx8 coefficient stream error")
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
	for plane := range 3 {
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
	for plane := range vp9dec.MaxMbPlane {
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
				for j := range step {
					pd.AboveContext[cc+j] = hr
					pd.LeftContext[rr+j] = hr
				}
			}
		}
	}
}

// TestWriteCoefSbEOBBoundedReadsWithDirtyState pins the read contract the
// zero-copy coefficient path relies on: when GetEOB supplies the
// quantizer-produced EOB, WriteCoefSb must never consume coefficient values
// at scan positions >= eob, and a dirty persistent TokenCache must be
// byte-equivalent to a zeroed one (libvpx tokenize_b keeps token_cache
// uninitialized; qcoeff/dqcoeff hold stale garbage past eob after
// vp9_xform_quant). The bitstream bytes, branch counts, and entropy-context
// stamps must all be identical between the clean and dirty runs, on both the
// direct write path and the staged token path.
func TestWriteCoefSbEOBBoundedReadsWithDirtyState(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	makePlanes := func() [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane {
		var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
		vp9dec.SetupBlockPlanes(&planes, 1, 1)
		planes[0].AboveContext = make([]uint8, 8)
		planes[0].LeftContext = make([]uint8, 8)
		planes[1].AboveContext = make([]uint8, 4)
		planes[1].LeftContext = make([]uint8, 4)
		planes[2].AboveContext = make([]uint8, 4)
		planes[2].LeftContext = make([]uint8, 4)
		return planes
	}

	// Per-block EOBs for a 16x16 leaf: Y = 2x2 grid of Tx8x8 blocks, UV = one
	// Tx8x8 block each. Block (0,0) has a zero run inside eob so the
	// zero-run loop's token-cache neighbor reads are exercised.
	yEOB := map[[2]int]int{{0, 0}: 5, {0, 2}: 0, {2, 0}: 1, {2, 2}: 2}
	makeBuffers := func(garbage int16) (get func(plane, r, c int, tx common.TxSize) []int16,
		getQ func(plane, r, c int, tx common.TxSize) []int16,
	) {
		build := func(eob int, tx common.TxSize, scale int16) []int16 {
			maxEob := vp9dec.MaxEobForTxSize(tx)
			scan := common.DefaultScanOrders[tx].Scan
			buf := make([]int16, maxEob)
			// Garbage at every scan position >= eob: the contract says these
			// are never read when KnownEOB is valid.
			for i := eob; i < maxEob; i++ {
				buf[scan[i]] = garbage
			}
			if eob > 0 {
				buf[scan[0]] = 4 * scale
				buf[scan[eob-1]] = 1 * scale
			}
			if eob > 3 {
				buf[scan[2]] = -2 * scale
			}
			return buf
		}
		eobFor := func(plane, r, c int) int {
			if plane == 0 {
				return yEOB[[2]int{r, c}]
			}
			return 1
		}
		get = func(plane, r, c int, tx common.TxSize) []int16 {
			return build(eobFor(plane, r, c), tx, 16)
		}
		getQ = func(plane, r, c int, tx common.TxSize) []int16 {
			return build(eobFor(plane, r, c), tx, 1)
		}
		return get, getQ
	}
	getEOB := func(plane, r, c int, tx common.TxSize) (int, bool) {
		if plane == 0 {
			return yEOB[[2]int{r, c}], true
		}
		return 1, true
	}

	type result struct {
		bytes  []byte
		stats  FrameCoefBranchStats
		planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	}
	run := func(garbage int16, cache *[1024]uint8, staged bool) result {
		planes := makePlanes()
		getCoeffs, getQCoeffs := makeBuffers(garbage)
		var stats FrameCoefBranchStats
		args := WriteCoefSbArgs{
			BSize:    common.Block16x16,
			MiTxSize: common.Tx8x8,
			IsInter:  1,
			Planes:   &planes,
			PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
				{16, 16}, {16, 16}, {16, 16},
			},
			Fc:              &fc,
			CoefBranchStats: &stats,
			GetCoeffs:       getCoeffs,
			GetQCoeffs:      getQCoeffs,
			GetEOB:          getEOB,
			TokenCache:      cache,
		}
		var tokens []TokenExtra
		tokenIndex := 0
		if staged {
			tokens = make([]TokenExtra, 512)
			args.TokenDst = tokens
			args.TokenIndex = &tokenIndex
		}
		buf := make([]byte, 512)
		var bw bitstream.Writer
		bw.Start(buf)
		if err := WriteCoefSb(&bw, args); err != nil {
			t.Fatalf("WriteCoefSb: %v", err)
		}
		size, err := bw.Stop()
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		return result{bytes: append([]byte(nil), buf[:size]...), stats: stats, planes: planes}
	}

	for _, staged := range []bool{false, true} {
		clean := run(0, nil, staged)
		dirtyCache := new([1024]uint8)
		for i := range dirtyCache {
			dirtyCache[i] = 0xAA
		}
		dirty := run(-1234, dirtyCache, staged)
		if !equalInt16Bytes(clean.bytes, dirty.bytes) {
			t.Fatalf("staged=%v: bitstream differs with dirty beyond-EOB coeffs + dirty token cache:\nclean=%x\ndirty=%x",
				staged, clean.bytes, dirty.bytes)
		}
		if clean.stats != dirty.stats {
			t.Fatalf("staged=%v: branch counts differ with dirty state", staged)
		}
		for plane := range vp9dec.MaxMbPlane {
			if !equalUint8s(clean.planes[plane].AboveContext, dirty.planes[plane].AboveContext) ||
				!equalUint8s(clean.planes[plane].LeftContext, dirty.planes[plane].LeftContext) {
				t.Fatalf("staged=%v plane=%d: entropy context stamps differ with dirty state",
					staged, plane)
			}
		}
	}
}

func equalInt16Bytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
