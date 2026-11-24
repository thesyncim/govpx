package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteBlockTokensRoundTripsMagnitudeCategories(t *testing.T) {
	var coeff [16]int16
	values := [10]int16{1, -2, 3, -4, 5, -7, 11, -19, 35, -67}
	for pos, value := range values {
		coeff[tables.DefaultZigZag1D[pos]] = value
	}

	payload := blockTokenPayload(t, 3, 0, 0, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 3, 0, 0, &got)

	if nonzeros != len(values) {
		t.Fatalf("nonzeros = %d, want %d", nonzeros, len(values))
	}
	if got != coeff {
		t.Fatalf("qcoeff = %v, want %v", got, coeff)
	}
}

func TestWriteBlockTokensRoundTripsZeroRunEOB(t *testing.T) {
	var coeff [16]int16
	coeff[8] = 3

	payload := blockTokenPayload(t, 3, 0, 0, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 3, 0, 0, &got)

	if nonzeros != 4 {
		t.Fatalf("nonzeros = %d, want 4", nonzeros)
	}
	if got != coeff {
		t.Fatalf("qcoeff = %v, want %v", got, coeff)
	}
}

func TestWriteBlockTokensRoundTripsSkipDC(t *testing.T) {
	coeff := [16]int16{9, -1}
	payload := blockTokenPayload(t, 0, 0, 1, &coeff)
	var got [16]int16
	nonzeros := decodeBlockTokens(t, payload, 0, 0, 1, &got)

	want := [16]int16{0, -1}
	if nonzeros != 2 {
		t.Fatalf("nonzeros = %d, want 2", nonzeros)
	}
	if got != want {
		t.Fatalf("qcoeff = %v, want %v", got, want)
	}
}

func TestWriteBlockTokensRejectsInvalidInput(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	coeff := [16]int16{tables.DCTMaxValue + 1}

	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, 3, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("large coeff error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := WriteBlockTokens(nil, &tables.DefaultCoefProbs, 3, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("nil writer error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, 9, 0, 0, &coeff); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid block type error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteCoefficientMacroblockTokensRoundTripsWholeBlock(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[24][0] = 1
	coeffs.QCoeff[0][1] = 2
	coeffs.QCoeff[16][0] = -3
	setAllMacroblockEOBs(&coeffs, false)

	payload := macroblockTokenPayload(t, false, &coeffs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var got vp8dec.MacroblockTokens
	_ = vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, false, &above, &left, &got)

	if got.QCoeff[24][0] != 1 || got.QCoeff[0][1] != 2 || got.QCoeff[16][0] != -3 {
		t.Fatalf("decoded key coeffs = Y2 %v Y1 %v UV %v", got.QCoeff[24], got.QCoeff[0], got.QCoeff[16])
	}
}

func TestWriteCoefficientMacroblockTokensRoundTripsBPred(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 4
	coeffs.QCoeff[23][15] = -5
	setAllMacroblockEOBs(&coeffs, true)

	payload := macroblockTokenPayload(t, true, &coeffs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var got vp8dec.MacroblockTokens
	_ = vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, true, &above, &left, &got)

	if got.QCoeff[0][0] != 4 || got.QCoeff[23][15] != -5 {
		t.Fatalf("decoded B_PRED key coeffs = Y %v UV %v", got.QCoeff[0], got.QCoeff[23])
	}
}

func TestWriteCoefficientTokenGridRoundTrips(t *testing.T) {
	modes := []KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.BPred, UVMode: common.VPred},
		{YMode: common.HPred, UVMode: common.HPred},
		{YMode: common.TMPred, UVMode: common.TMPred},
	}
	var coeffs [4]MacroblockCoefficients
	coeffs[0].QCoeff[24][0] = 1
	coeffs[0].QCoeff[0][1] = 2
	coeffs[1].QCoeff[0][0] = -3
	coeffs[2].QCoeff[16][0] = 4
	coeffs[3].QCoeff[23][15] = -5
	setAllMacroblockEOBs(&coeffs[0], false)
	setAllMacroblockEOBs(&coeffs[1], true)
	setAllMacroblockEOBs(&coeffs[2], false)
	setAllMacroblockEOBs(&coeffs[3], false)

	payload := coefficientTokenGridPayload(t, 2, 2, modes, coeffs[:])
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, len(modes))
	for i := range modes {
		decodedModes[i] = decoderModeFromKeyFrameMode(&modes[i])
	}
	above := make([]vp8dec.EntropyContextPlanes, 2)
	tokens := make([]vp8dec.MacroblockTokens, len(modes))
	if _, err := vp8dec.DecodeTokenGrid([]boolcoder.Decoder{br}, 2, 2, &tables.DefaultCoefProbs, decodedModes, above, tokens); err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}

	if tokens[0].QCoeff[24][0] != 1 || tokens[0].QCoeff[0][1] != 2 || tokens[1].QCoeff[0][0] != -3 || tokens[2].QCoeff[16][0] != 4 || tokens[3].QCoeff[23][15] != -5 {
		t.Fatalf("decoded grid key coeffs = %v %v %v %v", tokens[0].QCoeff[24], tokens[1].QCoeff[0], tokens[2].QCoeff[16], tokens[3].QCoeff[23])
	}
}

func TestWriteCoefficientTokenGridRejectsInvalidInput(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	coeffs := []MacroblockCoefficients{{}}
	above := []TokenContextPlanes{{}}

	if err := WriteCoefficientTokenGrid(&w, 1, 2, modes, coeffs, above, &tables.DefaultCoefProbs); !errors.Is(err, ErrModeBufferTooSmall) {
		t.Fatalf("short grid error = %v, want ErrModeBufferTooSmall", err)
	}
	badModes := []KeyFrameMacroblockMode{{YMode: common.MBPredictionMode(99), UVMode: common.DCPred}}
	if err := WriteCoefficientTokenGrid(&w, 1, 1, badModes, coeffs, above, &tables.DefaultCoefProbs); !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestBlockCoeffEOB(t *testing.T) {
	if got := BlockCoeffEOB(&[16]int16{}, 0); got != 0 {
		t.Fatalf("zero EOB = %d, want 0", got)
	}
	if got := BlockCoeffEOB(&[16]int16{}, 1); got != 1 {
		t.Fatalf("skip-DC zero EOB = %d, want 1", got)
	}
	var coeff [16]int16
	coeff[tables.DefaultZigZag1D[1]] = 2
	coeff[tables.DefaultZigZag1D[15]] = -3
	if got := BlockCoeffEOB(&coeff, 0); got != 16 {
		t.Fatalf("high coefficient EOB = %d, want 16", got)
	}
	coeff[tables.DefaultZigZag1D[15]] = 0
	if got := BlockCoeffEOB(&coeff, 1); got != 2 {
		t.Fatalf("skip-DC EOB = %d, want 2", got)
	}
}

func TestMacroblockCoefficientsBlockEOBTrustsStoredEOB(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][tables.DefaultZigZag1D[15]] = 7
	coeffs.SetBlockEOB(0, 2)

	if got := coeffs.BlockEOB(0, 0); got != 2 {
		t.Fatalf("cached EOB = %d, want 2", got)
	}
	if got := coeffs.BlockEOB(1, 0); got != 0 {
		t.Fatalf("zero-value EOB = %d, want stored zero despite stale qcoeff", got)
	}
	coeffs.SetBlockEOB(1, 0)
	if got := coeffs.BlockEOB(1, 1); got != 1 {
		t.Fatalf("cached zero skip-DC EOB = %d, want 1", got)
	}
}

func TestWriteCoefficientMacroblockTokensTrustsStoredEOB(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[24][0] = 9
	coeffs.QCoeff[0][1] = -7
	coeffs.QCoeff[16][0] = 5
	coeffs.SetBlockEOB(24, 0)
	for block := range 16 {
		coeffs.SetBlockEOB(block, 1)
	}
	for block := 16; block < 24; block++ {
		coeffs.SetBlockEOB(block, 0)
	}

	payload := macroblockTokenPayload(t, false, &coeffs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var above, left vp8dec.EntropyContextPlanes
	var got vp8dec.MacroblockTokens
	_ = vp8dec.DecodeMacroblockTokens(&br, &tables.DefaultCoefProbs, false, &above, &left, &got)

	if got.QCoeff[24][0] != 0 || got.QCoeff[0][1] != 0 || got.QCoeff[16][0] != 0 {
		t.Fatalf("decoded stale coefficients = Y2:%v Y1:%v UV:%v, want stored EOB to suppress qcoeff scan",
			got.QCoeff[24], got.QCoeff[0], got.QCoeff[16])
	}
}

func TestUpdateTokenContextPlanesFromCoefficientsMatchesWriter(t *testing.T) {
	tests := []struct {
		name  string
		is4x4 bool
		init  func(*MacroblockCoefficients)
	}{
		{
			name: "whole",
			init: func(coeffs *MacroblockCoefficients) {
				coeffs.QCoeff[24][0] = 2
				coeffs.QCoeff[0][1] = 1
				coeffs.QCoeff[5][2] = -3
				coeffs.QCoeff[16][0] = 4
				coeffs.QCoeff[22][3] = -5
			},
		},
		{
			name:  "bpred",
			is4x4: true,
			init: func(coeffs *MacroblockCoefficients) {
				coeffs.QCoeff[0][0] = 1
				coeffs.QCoeff[7][4] = -2
				coeffs.QCoeff[19][0] = 3
				coeffs.QCoeff[23][5] = -4
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var coeffs MacroblockCoefficients
			tt.init(&coeffs)
			setAllMacroblockEOBs(&coeffs, tt.is4x4)

			writerAbove := TokenContextPlanes{Y1: [4]uint8{1, 0, 1, 0}, U: [2]uint8{1, 0}, V: [2]uint8{0, 1}, Y2: 1}
			writerLeft := TokenContextPlanes{Y1: [4]uint8{0, 1, 0, 1}, U: [2]uint8{0, 1}, V: [2]uint8{1, 0}, Y2: 0}
			helperAbove := writerAbove
			helperLeft := writerLeft

			var w BoolWriter
			buf := make([]byte, 4096)
			w.Init(buf)
			if err := WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, tt.is4x4, &writerAbove, &writerLeft, &coeffs); err != nil {
				t.Fatalf("WriteCoefficientMacroblockTokens returned error: %v", err)
			}
			UpdateTokenContextPlanesFromCoefficients(&helperAbove, &helperLeft, tt.is4x4, &coeffs)

			if helperAbove != writerAbove || helperLeft != writerLeft {
				t.Fatalf("helper contexts = %+v/%+v, want writer %+v/%+v", helperAbove, helperLeft, writerAbove, writerLeft)
			}
		})
	}
}

func TestCoefficientTokenWritersAllocateZero(t *testing.T) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 1
	coeffs.QCoeff[24][0] = -2
	setAllMacroblockEOBs(&coeffs, false)
	buf := make([]byte, 4096)
	var w BoolWriter
	var above, left TokenContextPlanes

	allocs := testing.AllocsPerRun(1000, func() {
		above = TokenContextPlanes{}
		left = TokenContextPlanes{}
		w.Init(buf)
		_ = WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, false, &above, &left, &coeffs)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}

	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	gridCoeffs := []MacroblockCoefficients{coeffs}
	above = TokenContextPlanes{}
	gridAbove := []TokenContextPlanes{above}
	allocs = testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteCoefficientTokenGrid(&w, 1, 1, modes, gridCoeffs, gridAbove, &tables.DefaultCoefProbs)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("grid allocs = %v, want 0", allocs)
	}
}

func TestInterCoefficientTokenRecordsResetUsesInlineRowStarts(t *testing.T) {
	const rows = maxCoefficientTokenRecordRowStarts - 1

	var records InterCoefficientTokenRecords
	records.Reset(rows, 0)
	rowStarts := records.rowStarts()
	if len(rowStarts) != rows+1 {
		t.Fatalf("row starts len = %d, want %d", len(rowStarts), rows+1)
	}
	if &rowStarts[0] != &records.rowStartsInline[0] {
		t.Fatalf("row starts did not use inline storage")
	}

	allocs := testing.AllocsPerRun(1000, func() {
		var fresh InterCoefficientTokenRecords
		fresh.Reset(rows, 0)
		if len(fresh.rowStarts()) != rows+1 {
			panic("bad row starts length")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkBlockCoeffEOBHighCoefficient(b *testing.B) {
	var coeff [16]int16
	coeff[tables.DefaultZigZag1D[15]] = 1
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BlockCoeffEOB(&coeff, 0)
	}
}

func BenchmarkWriteCoefficientMacroblockTokens(b *testing.B) {
	var coeffs MacroblockCoefficients
	coeffs.QCoeff[0][0] = 1
	coeffs.QCoeff[1][1] = -2
	coeffs.QCoeff[16][0] = 3
	coeffs.QCoeff[24][0] = -4
	setAllMacroblockEOBs(&coeffs, false)
	buf := make([]byte, 4096)
	var w BoolWriter
	var above, left TokenContextPlanes

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		above = TokenContextPlanes{}
		left = TokenContextPlanes{}
		w.Init(buf)
		_ = WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, false, &above, &left, &coeffs)
		w.Finish()
	}
}

func BenchmarkWriteCoefficientTokenGrid(b *testing.B) {
	modes := make([]KeyFrameMacroblockMode, 16)
	coeffs := make([]MacroblockCoefficients, 16)
	for i := range modes {
		modes[i] = KeyFrameMacroblockMode{YMode: common.DCPred, UVMode: common.DCPred}
		coeffs[i].QCoeff[0][1] = int16((i % 4) + 1)
		setAllMacroblockEOBs(&coeffs[i], false)
	}
	above := make([]TokenContextPlanes, 4)
	buf := make([]byte, 8192)
	var w BoolWriter

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = WriteCoefficientTokenGrid(&w, 4, 4, modes, coeffs, above, &tables.DefaultCoefProbs)
		w.Finish()
	}
}

func BenchmarkWritePreparedCoefficientTokenRecords(b *testing.B) {
	const (
		rows = 4
		cols = 4
	)
	modes := make([]InterFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.ZeroMV}
		coeffs[i].QCoeff[0][1] = int16((i % 4) + 1)
		coeffs[i].QCoeff[1][0] = int16(-((i % 3) + 1))
		coeffs[i].QCoeff[16][0] = int16((i % 5) + 1)
		coeffs[i].QCoeff[24][0] = int16(-((i % 2) + 1))
		setAllMacroblockEOBs(&coeffs[i], false)
	}
	var counts InterCoefficientTokenCounts
	var records InterCoefficientTokenRecords
	ResetInterCoefficientTokenCounts(&counts)
	records.Reset(rows, rows*cols)
	above := make([]TokenContextPlanes, cols)
	for row := range rows {
		records.MarkRowStart(row)
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			if err := AccumulateInterMacroblockTokenCountsAndRecords(&counts, &records, interModeUses4x4Tokens(modes[index].Mode), &above[col], &left, &coeffs[index]); err != nil {
				b.Fatalf("AccumulateInterMacroblockTokenCountsAndRecords returned error: %v", err)
			}
		}
		records.MarkRowEnd(row)
	}
	buf := make([]byte, 8192)
	var w BoolWriter

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = writePreparedCoefficientTokenRecords(&w, &tables.DefaultCoefProbs, records.Records)
		w.Finish()
	}
}

func BenchmarkAccumulateInterMacroblockTokenCountsAndRecords(b *testing.B) {
	const (
		rows = 4
		cols = 4
	)
	modes := make([]InterFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.ZeroMV}
		coeffs[i].QCoeff[0][1] = int16((i % 4) + 1)
		coeffs[i].QCoeff[1][0] = int16(-((i % 3) + 1))
		coeffs[i].QCoeff[16][0] = int16((i % 5) + 1)
		coeffs[i].QCoeff[24][0] = int16(-((i % 2) + 1))
		setAllMacroblockEOBs(&coeffs[i], false)
	}
	above := make([]TokenContextPlanes, cols)
	var counts InterCoefficientTokenCounts
	var records InterCoefficientTokenRecords
	records.Reset(rows, rows*cols)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ResetInterCoefficientTokenCounts(&counts)
		records.Reset(rows, rows*cols)
		clear(above)
		for row := range rows {
			records.MarkRowStart(row)
			left := TokenContextPlanes{}
			for col := range cols {
				index := row*cols + col
				if err := AccumulateInterMacroblockTokenCountsAndRecords(&counts, &records, interModeUses4x4Tokens(modes[index].Mode), &above[col], &left, &coeffs[index]); err != nil {
					b.Fatalf("AccumulateInterMacroblockTokenCountsAndRecords returned error: %v", err)
				}
			}
			records.MarkRowEnd(row)
		}
	}
}

func setAllMacroblockEOBs(coeffs *MacroblockCoefficients, is4x4 bool) {
	if !is4x4 {
		coeffs.SetBlockEOB(24, BlockCoeffEOB(&coeffs.QCoeff[24], 0))
		for i := range 16 {
			coeffs.SetBlockEOB(i, BlockCoeffEOB(&coeffs.QCoeff[i], 1))
		}
	} else {
		for i := range 16 {
			coeffs.SetBlockEOB(i, BlockCoeffEOB(&coeffs.QCoeff[i], 0))
		}
	}
	for i := 16; i < 24; i++ {
		coeffs.SetBlockEOB(i, BlockCoeffEOB(&coeffs.QCoeff[i], 0))
	}
}

func blockTokenPayload(t *testing.T, blockType int, ctx int, skipDC int, coeff *[16]int16) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 256)
	w.Init(buf)
	if err := WriteBlockTokens(&w, &tables.DefaultCoefProbs, blockType, ctx, skipDC, coeff); err != nil {
		t.Fatalf("WriteBlockTokens returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func macroblockTokenPayload(t *testing.T, is4x4 bool, coeffs *MacroblockCoefficients) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 4096)
	w.Init(buf)
	var above, left TokenContextPlanes
	if err := WriteCoefficientMacroblockTokens(&w, &tables.DefaultCoefProbs, is4x4, &above, &left, coeffs); err != nil {
		t.Fatalf("WriteCoefficientMacroblockTokens returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func coefficientTokenGridPayload(t *testing.T, rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 8192)
	w.Init(buf)
	above := make([]TokenContextPlanes, cols)
	if err := WriteCoefficientTokenGrid(&w, rows, cols, modes, coeffs, above, &tables.DefaultCoefProbs); err != nil {
		t.Fatalf("WriteCoefficientTokenGrid returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	return w.Bytes()
}

func decodeBlockTokens(t *testing.T, payload []byte, blockType int, ctx int, skipDC int, out *[16]int16) int {
	t.Helper()
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	return vp8dec.DecodeBlockCoeffs(&br, &tables.DefaultCoefProbs, blockType, ctx, skipDC, out)
}
