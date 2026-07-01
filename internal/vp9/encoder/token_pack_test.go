package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
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
