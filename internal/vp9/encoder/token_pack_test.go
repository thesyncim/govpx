package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestStageCoefBlockPackMatchesDirectWriter(t *testing.T) {
	tests := []struct {
		name     string
		coeffs   []int16
		qcoeffs  []int16
		knownEOB int
		knownOK  bool
	}{
		{name: "all zero", coeffs: make([]int16, 16)},
		{name: "zero run then one", coeffs: coefBlockForStageTest(0, 0, 1, 32)},
		{name: "cat negative qcoeff", coeffs: make([]int16, 16), qcoeffs: qcoefBlockForStageTest(0, -72)},
		{name: "known eob zero", coeffs: coefBlockForStageTest(1, 32), knownEOB: 0, knownOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := seedDefaultCoefProbsForEnc()
			scan := tables.DefaultScan4x4[:]
			neighbors := tables.DefaultScan4x4Neighbors[:]
			dq := [2]int16{16, 32}
			directStats := FrameCoefBranchStats{}
			stagedStats := FrameCoefBranchStats{}

			direct, directEOB := writeCoefBlockForStageTest(t, WriteCoefBlockArgs{
				TxSize:          common.Tx4x4,
				DequantDC:       dq[0],
				DequantAC:       dq[1],
				Scan:            scan,
				Neighbors:       neighbors,
				Coeffs:          tc.coeffs,
				QCoeffs:         tc.qcoeffs,
				Fc:              &fc,
				CoefBranchStats: &directStats,
				KnownEOB:        tc.knownEOB,
				KnownEOBValid:   tc.knownOK,
			})

			tokens := make([]TokenExtra, 64)
			var stagedEOB int
			n, gotEOB, ok := StageCoefBlock(tokens, WriteCoefBlockArgs{
				TxSize:          common.Tx4x4,
				DequantDC:       dq[0],
				DequantAC:       dq[1],
				Scan:            scan,
				Neighbors:       neighbors,
				Coeffs:          tc.coeffs,
				QCoeffs:         tc.qcoeffs,
				Fc:              &fc,
				CoefBranchStats: &stagedStats,
				EOB:             &stagedEOB,
				KnownEOB:        tc.knownEOB,
				KnownEOBValid:   tc.knownOK,
			})
			if !ok {
				t.Fatal("StageCoefBlock returned !ok")
			}
			if gotEOB != directEOB || stagedEOB != directEOB {
				t.Fatalf("staged eob = (%d,%d), direct %d",
					gotEOB, stagedEOB, directEOB)
			}

			var bw bitstream.Writer
			buf := make([]byte, 256)
			bw.Start(buf)
			if consumed := PackTokens(&bw, tokens[:n], &fc); consumed != n {
				t.Fatalf("PackTokens consumed %d, want %d", consumed, n)
			}
			size, err := bw.Stop()
			if err != nil {
				t.Fatalf("staged Stop: %v", err)
			}
			staged := append([]byte(nil), buf[:size]...)
			if !bytes.Equal(staged, direct) {
				t.Fatalf("staged bytes %x, direct %x", staged, direct)
			}
			if stagedStats != directStats {
				t.Fatalf("staged branch stats differ from direct writer")
			}
		})
	}
}

func TestWriteCoefSbStagedPathsMatchDirectWriter(t *testing.T) {
	direct, directStats, directPlanes := writeCoefSbTokenPathForTest(t, 0)
	immediate, immediateStats, immediatePlanes := writeCoefSbTokenPathForTest(t, 1)
	if !bytes.Equal(immediate, direct) {
		t.Fatalf("stage+pack bytes %x, direct %x", immediate, direct)
	}
	if immediateStats != directStats {
		t.Fatalf("stage+pack branch stats differ from direct writer")
	}
	if !tokenPathPlaneContextsEqual(immediatePlanes, directPlanes) {
		t.Fatalf("stage+pack entropy contexts differ from direct writer")
	}

	stagedOnly, stagedOnlyStats, stagedOnlyPlanes := writeCoefSbTokenPathForTest(t, 2)
	if !bytes.Equal(stagedOnly, direct) {
		t.Fatalf("stage-only replay bytes %x, direct %x", stagedOnly, direct)
	}
	if stagedOnlyStats != directStats {
		t.Fatalf("stage-only branch stats differ from direct writer")
	}
	if !tokenPathPlaneContextsEqual(stagedOnlyPlanes, directPlanes) {
		t.Fatalf("stage-only entropy contexts differ from direct writer")
	}
}

func TestWriteCoefSbStagedPathReportsTokenBufferFull(t *testing.T) {
	fc := seedDefaultCoefProbsForEnc()
	planes := tokenPathPlanesForTest()
	idx := 0
	var bw bitstream.Writer
	bw.Start(make([]byte, 16))
	err := WriteCoefSb(&bw, WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:         &fc,
		GetCoeffs:  tokenPathCoeffsForTest,
		GetQCoeffs: tokenPathQCoeffsForTest,
		TokenDst:   make([]TokenExtra, 1),
		TokenIndex: &idx,
	})
	if err != ErrTokenBufferFull {
		t.Fatalf("WriteCoefSb staged tiny buffer err = %v, want %v",
			err, ErrTokenBufferFull)
	}
}

func writeCoefBlockForStageTest(t *testing.T, args WriteCoefBlockArgs) ([]byte, int) {
	t.Helper()
	var eob int
	args.EOB = &eob
	var bw bitstream.Writer
	buf := make([]byte, 256)
	bw.Start(buf)
	if err := WriteCoefBlock(&bw, args); err != nil {
		t.Fatalf("WriteCoefBlock: %v", err)
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("direct Stop: %v", err)
	}
	return append([]byte(nil), buf[:size]...), eob
}

func writeCoefSbTokenPathForTest(t *testing.T, mode int) ([]byte, FrameCoefBranchStats, [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane) {
	t.Helper()
	fc := seedDefaultCoefProbsForEnc()
	planes := tokenPathPlanesForTest()
	var stats FrameCoefBranchStats
	args := WriteCoefSbArgs{
		BSize:    common.Block8x8,
		MiTxSize: common.Tx4x4,
		Planes:   &planes,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			{16, 32}, {16, 32}, {16, 32},
		},
		Fc:              &fc,
		CoefBranchStats: &stats,
		GetCoeffs:       tokenPathCoeffsForTest,
		GetQCoeffs:      tokenPathQCoeffsForTest,
	}
	tokens := make([]TokenExtra, 128)
	idx := 0
	if mode > 0 {
		args.TokenDst = tokens
		args.TokenIndex = &idx
		args.TokenOnly = mode == 2
	}

	var bw bitstream.Writer
	buf := make([]byte, 512)
	bw.Start(buf)
	if err := WriteCoefSb(&bw, args); err != nil {
		t.Fatalf("WriteCoefSb mode %d: %v", mode, err)
	}
	if mode == 2 {
		if consumed := PackTokens(&bw, tokens[:idx], &fc); consumed != idx {
			t.Fatalf("PackTokens consumed %d, want %d", consumed, idx)
		}
	}
	size, err := bw.Stop()
	if err != nil {
		t.Fatalf("Stop mode %d: %v", mode, err)
	}
	return append([]byte(nil), buf[:size]...), stats, planes
}

func tokenPathPlanesForTest() [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane {
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

func tokenPathPlaneContextsEqual(a, b [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane) bool {
	for plane := range vp9dec.MaxMbPlane {
		if !bytes.Equal(a[plane].AboveContext, b[plane].AboveContext) ||
			!bytes.Equal(a[plane].LeftContext, b[plane].LeftContext) {
			return false
		}
	}
	return true
}

func tokenPathCoeffsForTest(plane, r, c int, tx common.TxSize) []int16 {
	return make([]int16, vp9dec.MaxEobForTxSize(tx))
}

func tokenPathQCoeffsForTest(plane, r, c int, tx common.TxSize) []int16 {
	out := make([]int16, vp9dec.MaxEobForTxSize(tx))
	switch {
	case plane == 0 && r == 0 && c == 0:
		out[0] = 1
	case plane == 0 && r == 0 && c == 1:
		out[1] = -6
	case plane == 1 && r == 0 && c == 0:
		out[0] = 72
	}
	return out
}

func coefBlockForStageTest(posVal ...int) []int16 {
	out := make([]int16, 16)
	for i := 0; i+1 < len(posVal); i += 2 {
		out[posVal[i]] = int16(posVal[i+1])
	}
	return out
}

func qcoefBlockForStageTest(posVal ...int) []int16 {
	out := make([]int16, 16)
	for i := 0; i+1 < len(posVal); i += 2 {
		out[posVal[i]] = int16(posVal[i+1])
	}
	return out
}
